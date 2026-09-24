package json_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	csvad "github.com/simon-lentz/yammm/adapter/csv"
	jsonad "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const agreementSchema = `schema "agree"

type T {
	id String primary
	n Integer
	f Float
}
`

// agreementLiterals are JSON number literals that decide the number rule: whole
// values spelled as floats, digit runs past uint64 before an indicator, the
// int64 and float64 edges on both sides, and a negative zero.
var agreementLiterals = []string{
	"0", "-0", "5", "-5", "5.0", "5.5", "-5.5", "1e2", "1E2", "5e0", "5e-1", "1.5e3",
	"100000000000000000000e-19", "18446744073709551616e-18", "123456789012345678901234.5e-20",
	"-9223372036854775809.0", "-9.223372036854775809e18",
	"9007199254740993", "9007199254740993.0",
	"9223372036854775807", "-9223372036854775808", "9223372036854775808", "-9223372036854775809",
	"18446744073709551615", "99999999999999999999",
	"1e400", "-1e400", "1e-400", "4.9e-324", "1.7976931348623157e308",
	"2" + strings.Repeat("0", 308), "1" + strings.Repeat("0", 400),
}

// The second implementation is encoding/json decoding into the Go type each
// kind stores: an Integer accepts exactly what it decodes into an int64, with
// the same value, and a Float exactly what it decodes into a float64, bit for
// bit, the sign of a zero included. The JSON adapter and the validator, and
// the CSV adapter and the validator, agree with it on every literal, on what
// they accept and what they store; the .ys reader under revalidation agrees on
// what it accepts.
func TestNumberRule_EveryReaderAgreesWithEncodingJSON(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, agreementSchema, "agree.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	ty, _ := s.Type("T")
	v := instance.NewValidator(s)
	ys := ysTemplate(t, s, v)

	for _, prop := range []string{"n", "f"} {
		for _, lit := range agreementLiterals {
			name := lit
			if len(name) > 24 {
				name = fmt.Sprintf("%s…(%d)", name[:12], len(lit))
			}
			t.Run(prop+"="+name, func(t *testing.T) {
				t.Parallel()
				want, wantOK := decodeAs(t, prop, lit)

				got, ok := jsonReader(ctx, t, v, prop, lit)
				check(t, "JSON", ok, wantOK, got, want)

				got, ok = csvReader(ctx, t, v, ty, prop, lit)
				check(t, "CSV", ok, wantOK, got, want)

				if revalidated := ysReader(ctx, t, s, ys, prop, lit); revalidated != wantOK {
					t.Errorf(".ys revalidation accepted=%v, encoding/json accepted=%v", revalidated, wantOK)
				}
			})
		}
	}
}

// decodeAs decodes lit with encoding/json into the Go type prop stores.
func decodeAs(t *testing.T, prop, lit string) (any, bool) {
	t.Helper()
	if prop == "n" {
		var n int64
		if err := json.Unmarshal([]byte(lit), &n); err != nil {
			return nil, false
		}
		return n, true
	}
	var f float64
	if err := json.Unmarshal([]byte(lit), &f); err != nil {
		return nil, false
	}
	return f, true
}

func check(t *testing.T, reader string, ok, wantOK bool, got, want any) {
	t.Helper()
	if ok != wantOK {
		t.Errorf("%s accepted=%v (%#v), encoding/json accepted=%v (%#v)", reader, ok, got, wantOK, want)
		return
	}
	if !ok {
		return
	}
	if gf, isFloat := got.(float64); isFloat {
		if wf, _ := want.(float64); math.Float64bits(gf) != math.Float64bits(wf) {
			t.Errorf("%s stored %v (bits %x), encoding/json decoded %v (bits %x)", reader, gf, math.Float64bits(gf), wf, math.Float64bits(wf))
		}
		return
	}
	if got != want {
		t.Errorf("%s stored %#v, encoding/json decoded %#v", reader, got, want)
	}
}

// jsonReader parses a document holding lit at prop and validates it.
func jsonReader(ctx context.Context, t *testing.T, v *instance.Validator, prop, lit string) (any, bool) {
	t.Helper()
	other := `"f":0.5`
	if prop == "f" {
		other = `"n":0`
	}
	doc := fmt.Sprintf(`{"T":[{"id":"a",%s,"%s":%s}]}`, other, prop, lit)
	parsed, pres := jsonad.New().ParseObject(ctx, location.NewSourceID("agree.json"), []byte(doc))
	if err := pres.Err(); err != nil {
		t.Fatalf("JSON parse %s: %v", doc, err)
	}
	return validated(ctx, v, parsed["T"][0], prop)
}

// csvReader reads a CSV file holding lit at prop and validates the row.
func csvReader(ctx context.Context, t *testing.T, v *instance.Validator, ty *schema.Type, prop, lit string) (any, bool) {
	t.Helper()
	text := "id,n,f\na," + lit + ",0.5\n"
	if prop == "f" {
		text = "id,n,f\na,0," + lit + "\n"
	}
	raws, cres := csvad.New().ParseTyped(ctx, location.MustNewSourceID("test://agree.csv"), "T", strings.NewReader(text), ty)
	if !cres.OK() {
		if !cres.HasCode(csvad.E_CSV_COERCE) {
			t.Fatalf("CSV parse %q drew %s, want at most E_CSV_COERCE", text, cres)
		}
		return nil, false
	}
	return validated(ctx, v, raws[0], prop)
}

func validated(ctx context.Context, v *instance.Validator, raw instance.RawInstance, prop string) (any, bool) {
	vi, vres := v.ValidateOne(ctx, "T", raw)
	if !vres.OK() {
		return nil, false
	}
	got, _ := vi.Property(prop)
	return got.Unwrap(), true
}

// ysTemplate writes a .ys document holding n 0 and f 0.5, whose numbers
// ysReader replaces.
func ysTemplate(t *testing.T, s *schema.Schema, v *instance.Validator) []byte {
	t.Helper()
	ctx := context.Background()
	vi, vres := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{"id": "a", "n": int64(0), "f": 0.5}})
	if vres.HasErrors() {
		t.Fatalf("validate: %s", vres)
	}
	g := graph.New(s)
	if r := g.Add(ctx, vi); r.HasErrors() {
		t.Fatalf("add: %s", r)
	}
	data, mres := snapshot.Marshal(ctx, g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	for _, anchor := range []string{`"n":0`, `"f":0.5`} {
		if bytes.Count(data, []byte(anchor)) != 1 {
			t.Fatalf("the document holds %s other than once:\n%s", anchor, data)
		}
	}
	return data
}

// ysReader loads the template with lit at prop under revalidation and reports
// whether the validator accepted it.
func ysReader(ctx context.Context, t *testing.T, s *schema.Schema, ys []byte, prop, lit string) bool {
	t.Helper()
	anchor := map[string]string{"n": `"n":0`, "f": `"f":0.5`}[prop]
	data := bytes.Replace(ys, []byte(anchor), []byte(`"`+prop+`":`+lit), 1)
	_, lres := snapshot.Load(ctx, data, s, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error))
	for issue := range lres.Issues() {
		if issue.Code() != diag.E_TYPE_MISMATCH {
			t.Fatalf(".ys load drew %s, want at most E_TYPE_MISMATCH", lres)
		}
	}
	return !lres.HasErrors()
}

type celsius float64

const heldSchema = `schema "held"

type T {
	id String primary
	n Integer
	s String
	ts Timestamp
	d Date
	u UUID
	tss List<Timestamp>
	v Vector[2]
	l List<Integer>
	f Float
}
`

// The JSON and .ys writers mark a float in a property value by the Go type it
// holds, and the CSV writer every one a cell can spell: a float32 at its 32-bit
// width, a named float, a float under a String, a Timestamp, a Date, a UUID and
// a List<Timestamp> element, a float standing where a Vector or a List belongs,
// a float nested in a list or a string-keyed map that stands where a scalar
// belongs, a list standing at a Float or nested as a Vector element, and, in
// the .ys writer, under a property the type does not declare. A snapshot holds
// such a value only when nothing validated it. A CSV cell has no spelling for a
// map, so the row naming no CSV cell asserts of CSV only that the file is
// written.
func TestWriters_MarkEveryHeldFloat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, heldSchema, "held.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	ty, _ := s.Type("T")
	for _, c := range []struct {
		prop            string
		held            any
		jsonYS, csvCell string
	}{
		{"n", float32(0.1), `"n":0.1}`, ",0.1,"},
		{"n", float32(7), `"n":7.0`, ",7.0,"},
		{"n", celsius(6), `"n":6.0`, ",6.0,"},
		{"s", float64(6), `"s":6.0`, ",6.0"},
		{"ts", float64(6), `"ts":6.0`, ",6.0"},
		{"d", float64(6), `"d":6.0`, "6.0,"},
		{"u", float64(6), `"u":6.0`, ",6.0"},
		{"tss", []any{float64(6)}, `"tss":[6.0]`, ",6.0"},
		{"v", float64(7), `"v":7.0`, ",7.0"},
		{"l", float64(8), `"l":8.0`, ",8.0"},
		{"n", []any{float64(6)}, `"n":[6.0]`, ",6.0,"},
		{"n", map[string]any{"k": float64(6)}, `"n":{"k":6.0}`, ""},
		{"ts", []any{float64(6)}, `"ts":[6.0]`, ",6.0"},
		{"f", []any{float64(6)}, `"f":[6.0]`, ",6.0,"},
		{"v", []any{[]any{float64(6)}, float64(1)}, `"v":[[6.0],1.0]`, ",6.0|1.0"},
	} {
		g := graph.New(s)
		inst := instance.NewValidInstance("T", ty.ID(), immutable.WrapKey([]any{"a"}),
			immutable.WrapProperties(map[string]any{"id": "a", c.prop: c.held}), nil, nil, nil)
		if r := g.Add(ctx, inst); r.HasErrors() {
			t.Fatalf("add %s=%#v: %s", c.prop, c.held, r)
		}
		snap := g.Snapshot()
		doc, err := jsonad.New().MarshalObject(ctx, snap)
		if err != nil || !bytes.Contains(doc, []byte(c.jsonYS)) {
			t.Errorf("JSON %s=%#v wrote %s (%v), want %s", c.prop, c.held, doc, err, c.jsonYS)
		}
		ys, mres := snapshot.Marshal(ctx, snap)
		if mres.HasErrors() || !bytes.Contains(ys, []byte(c.jsonYS)) {
			t.Errorf(".ys %s=%#v wrote %s (%s), want %s", c.prop, c.held, ys, mres, c.jsonYS)
		}
		files, err := csvad.New(csvad.WithSchema(s)).MarshalSnapshot(ctx, snap)
		if err != nil || !strings.Contains(string(files["T"]), c.csvCell) {
			t.Errorf("CSV %s=%#v wrote %q (%v), want %s", c.prop, c.held, files["T"], err, c.csvCell)
		}
	}

	g := graph.New(s)
	inst := instance.NewValidInstance("T", ty.ID(), immutable.WrapKey([]any{"a"}),
		immutable.WrapProperties(map[string]any{"id": "a", "extra": float64(8), "extras": []any{float64(9)}}), nil, nil, nil)
	if r := g.Add(ctx, inst); r.HasErrors() {
		t.Fatalf("add: %s", r)
	}
	ys, mres := snapshot.Marshal(ctx, g.Snapshot())
	if mres.HasErrors() || !bytes.Contains(ys, []byte(`"extra":8.0`)) || !bytes.Contains(ys, []byte(`"extras":[9.0]`)) {
		t.Errorf(".ys undeclared floats wrote %s (%s), want 8.0 and [9.0]", ys, mres)
	}
}
