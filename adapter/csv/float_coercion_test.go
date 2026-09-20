package csv

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A Vector's elements coerce as a List<Float>'s and a scalar Float do: as
// written, never trimmed. A Vector alone tolerated space around an element,
// which the writer never emits and nothing promised, so one kind of three
// disagreed about a cell a person wrote by hand.
func TestCoerce_AFloatIsReadAsWrittenInEveryKind(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "floats"

type Reading {
	id String primary
	ratio Float
	ratios List<Float>
	at Vector[2]
}
`, "floats.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	st, _ := s.Type("Reading")

	// A space on either side of an element: trimming one side alone would
	// pass a fixture that only ever led with one.
	for _, c := range []struct{ column, cell string }{
		{"ratio", " 2.0"},
		{"ratio", "2.0 "},
		{"ratios", "1.0| 2.0"},
		{"ratios", "1.0 |2.0"},
		{"at", "1.0| 2.0"},
		{"at", "1.0 |2.0"},
	} {
		input := "id," + c.column + "\nr1,\"" + c.cell + "\"\n"
		_, result := New().ParseTyped(t.Context(), location.MustNewSourceID("test://floats.csv"), "Reading", strings.NewReader(input), st)
		issue, ok := issueContaining(result, `column "`+c.column+`"`)
		if !ok || issue.Code() != E_CSV_COERCE {
			t.Errorf("%s = %q: want an E_CSV_COERCE naming the column, got %s", c.column, c.cell, result)
		}
	}
}

// scalarCell renders the scalar kinds a cell holds, and a nil as the empty
// cell, which is what a bypass-built key component holding none writes.
func TestScalarCell_RendersEveryKindItNames(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		in   any
		want string
	}{
		{"text", "text"},
		{int64(-7), "-7"},
		{float64(1e21), "1000000000000000000000"},
		{true, "true"},
		{nil, ""},
		{uint8(3), "3"},
	} {
		if got := scalarCell(c.in); got != c.want {
			t.Errorf("scalarCell(%#v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The exact text a Float is written as, as a property, in a List and in a
// Vector. The
// round-trip tests compare two marshals of this same writer, so a change to how
// a Float renders moves both sides and they still agree; only the bytes can
// see it. Positional notation at the shortest precision, never an exponent.
func TestMarshalSnapshot_AFloatIsWrittenInPositionalNotationEverywhere(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "floats"

type Reading {
	id String primary
	ratio Float
	ratios List<Float>
	at Vector[2]
}
`, "floats.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	snap := emptyCellSnapshot(t, s, []typedRow{{"Reading", map[string]any{
		"id": "r1", "ratio": float64(1),
		"ratios": []any{float64(3), float64(-0.25), float64(1e-7)},
		"at":     []any{float64(1e21), float64(2.5)},
	}}})

	files, err := New(WithSchema(s)).MarshalSnapshot(t.Context(), snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = "at,id,ratio,ratios\n" +
		"1000000000000000000000|2.5,r1,1,3|-0.25|0.0000001\n"
	if got := string(files["Reading"]); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
