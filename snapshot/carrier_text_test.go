package snapshot_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// textUnwritable is the detail set Marshal's refusal of Holder h1's text holds.
func textUnwritable() map[string]string {
	return map[string]string{
		diag.DetailKeyTypeName:     snapshot.TypeRef{Schema: "utf8wire", Name: "Holder"}.String(),
		diag.DetailKeyPrimaryKey:   `["h1"]`,
		diag.DetailKeyPropertyName: "text",
	}
}

type wireHolder struct{ S string }

// encoding/json walks an array, a struct and a map keyed by no string, and
// writes U+FFFD in place of each invalid byte of a string inside one, so the
// document would read back as other text. Marshal refuses such a value by
// name, as it refuses a bare string.
func TestMarshal_TextBehindACarrierIsJudgedAsTheText(t *testing.T) {
	t.Parallel()
	const bad = "a\xffb"
	for name, v := range map[string]any{
		"an array":                    [1]string{bad},
		"a struct":                    wireHolder{bad},
		"a map keyed by an integer":   map[int]string{1: bad},
		"a pointer to a struct":       &wireHolder{bad},
		"an array inside a list":      []any{[1]string{bad}},
		"a struct inside a map value": map[string]any{"x": wireHolder{bad}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, snap := utf8WireSnapshot(t, map[string]any{"text": v}, "t1", "ok")
			data, res := snapshot.Marshal(t.Context(), snap)
			if data != nil {
				t.Errorf("Marshal wrote %d bytes for text the wire cannot carry", len(data))
			}
			requireUnwritable(t, res, textUnwritable(), `property "text"`)
		})
	}
}

// A json.Number whose text is no JSON number literal is no value of the one
// form, so Marshal refuses it: encoding/json writes an empty one as 0, which
// Load reads back as the integer 0, and fails the document on any other. A
// json.Number it writes, Load reads back as the literal it is.
func TestMarshal_AJSONNumberThatIsNoLiteralIsRefused(t *testing.T) {
	t.Parallel()
	signed := json.Number("+5")
	for name, v := range map[string]any{
		"an empty property":         json.Number(""),
		"an empty list element":     []any{json.Number("")},
		"an empty map value":        map[string]any{"x": json.Number("")},
		"a leading '+'":             json.Number("+5"),
		"leading zeros":             json.Number("007"),
		"a hexadecimal float":       json.Number("0x1p4"),
		"an underscore":             json.Number("1_000"),
		"a pointer to a '+' number": &signed,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, snap := utf8WireSnapshot(t, map[string]any{"text": v}, "t1", "ok")
			data, res := snapshot.Marshal(t.Context(), snap)
			if data != nil {
				t.Errorf("Marshal wrote %d bytes for a json.Number that is no literal", len(data))
			}
			requireUnwritable(t, res, textUnwritable(), `property "text"`, "a json.Number that is no number literal")
		})
	}
	s, snap := utf8WireSnapshot(t, map[string]any{"text": json.Number("1e400")}, "t1", "ok")
	data, res := snapshot.Marshal(t.Context(), snap)
	if res.HasErrors() {
		t.Fatalf("Marshal of json.Number 1e400: %s", res)
	}
	back, lres := snapshot.Load(t.Context(), data, s)
	if lres.HasErrors() {
		t.Fatalf("Load: %s", lres)
	}
	holderT, _ := s.Type("Holder")
	inst, ok := back.InstanceByKey(holderT.ID(), `["h1"]`)
	if !ok {
		t.Fatal("Load: no Holder h1")
	}
	if v, _ := inst.Property("text"); v.Unwrap() != json.Number("1e400") {
		t.Errorf("1e400 read back as %#v", v.Unwrap())
	}
}

// A named type over string holding valid text is written, and Load reads its
// text back.
func TestMarshal_ANamedTypeHoldingValidTextIsWritten(t *testing.T) {
	t.Parallel()
	s, snap := utf8WireSnapshot(t, map[string]any{"text": wireText("ok")}, "t1", "ok")
	data, res := snapshot.Marshal(t.Context(), snap)
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	back, lres := snapshot.Load(t.Context(), data, s)
	if lres.HasErrors() {
		t.Fatalf("Load: %s", lres)
	}
	holderT, _ := s.Type("Holder")
	inst, _ := back.InstanceByKey(holderT.ID(), `["h1"]`)
	if v, _ := inst.Property("text"); v.Unwrap() != "ok" {
		t.Errorf("text read back as %#v", v.Unwrap())
	}
}

// A time.Time or a uuid.UUID is written only where its constraint renders it
// as text, as every writer of this module writes it: at a Date or a UUID it is
// written as its text and Load reads that text back, and anywhere else Marshal
// refuses it by name.
func TestMarshal_ATimeOrAUUIDItsConstraintDoesNotRenderIsRefused(t *testing.T) {
	t.Parallel()
	day := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	for name, v := range map[string]any{
		"a time":                day,
		"a time past year 9999": time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		"a uuid":                id,
		"a pointer to a time":   &day,
		"a time in a map value": map[string]any{"x": day},
		"a uuid in a list":      []any{id},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, snap := utf8WireSnapshot(t, map[string]any{"text": v}, "t1", "ok")
			data, res := snapshot.Marshal(t.Context(), snap)
			if data != nil {
				t.Errorf("Marshal wrote %d bytes for a value no constraint renders", len(data))
			}
			requireUnwritable(t, res, textUnwritable(), `property "text"`, "a time or a UUID its constraint does not render")
		})
	}
	s, snap := utf8WireSnapshot(t, map[string]any{"day": day, "uid": id}, "t1", "ok")
	data, res := snapshot.Marshal(t.Context(), snap)
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	back, lres := snapshot.Load(t.Context(), data, s)
	if lres.HasErrors() {
		t.Fatalf("Load: %s", lres)
	}
	holderT, _ := s.Type("Holder")
	inst, _ := back.InstanceByKey(holderT.ID(), `["h1"]`)
	for name, want := range map[string]string{"day": "2024-01-02", "uid": id.String()} {
		if v, _ := inst.Property(name); v.Unwrap() != want {
			t.Errorf("%s read back as %#v, want %q", name, v.Unwrap(), want)
		}
	}
}

// A time whose stored text does not read back is no value the wire carries, at
// its own kind too: a Date past year 9999 renders as text no date parse reads,
// and a pointer is how such a time reaches the writer.
func TestMarshal_ATimeWhoseTextDoesNotReadBackIsRefused(t *testing.T) {
	t.Parallel()
	far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)
	want := map[string]string{
		diag.DetailKeyTypeName:     snapshot.TypeRef{Schema: "utf8wire", Name: "Holder"}.String(),
		diag.DetailKeyPrimaryKey:   `["h1"]`,
		diag.DetailKeyPropertyName: "day",
	}
	for name, v := range map[string]any{"past year 9999": far, "a pointer past year 9999": &far, "a pointer before year 0": &before} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, snap := utf8WireSnapshot(t, map[string]any{"day": v}, "t1", "ok")
			data, res := snapshot.Marshal(t.Context(), snap)
			if data != nil {
				t.Errorf("Marshal wrote %d bytes for a time whose text does not read back", len(data))
			}
			requireUnwritable(t, res, want, `property "day"`)
		})
	}
}
