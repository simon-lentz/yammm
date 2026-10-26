package json

import (
	"bytes"
	stdjson "encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/schema"
)

// jsonTextMarshaler spells itself through MarshalText, which encoding/json
// writes as a JSON string.
type jsonTextMarshaler string

func (t jsonTextMarshaler) MarshalText() ([]byte, error) { return []byte(t), nil }

// jsonRawMarshaler spells itself through MarshalJSON, whose bytes encoding/json
// copies into the document as they stand.
type jsonRawMarshaler string

func (t jsonRawMarshaler) MarshalJSON() ([]byte, error) {
	return append(append([]byte{'"'}, t...), '"'), nil
}

type textHolder struct{ S string }

// A value the immutable layer holds unwrapped is read in the one form every
// writer writes: a pointer as the scalar it points to, a named type as its base
// value. A string that is not valid UTF-8 there is refused as the class, as a
// bare one is, and an array, a struct or a map keyed by no string is no such
// form and is refused too.
func TestMarshalObject_TextBehindACarrierIsJudgedAsTheText(t *testing.T) {
	t.Parallel()
	bad := "a\xffb"
	for _, c := range []struct {
		name  string
		props map[string]any
		label any
	}{
		{"a pointer to it", map[string]any{"text": &bad}, nil},
		{"an array holding it", map[string]any{"text": [1]string{bad}}, nil},
		{"a struct holding it", map[string]any{"text": textHolder{bad}}, nil},
		{"a map keyed by an integer holding it", map[string]any{"text": map[int]string{1: bad}}, nil},
		{"a list element pointing to it", map[string]any{"tags": []any{&bad}}, nil},
		{"a type spelling it with MarshalText", map[string]any{"text": jsonTextMarshaler(bad)}, nil},
		{"a type spelling it with MarshalJSON", map[string]any{"text": jsonRawMarshaler(bad)}, nil},
		{"an edge property pointing to it", nil, &bad},
	} {
		_, err := New().MarshalObject(t.Context(), utf8JSONSnapshot(t, c.props, c.label))
		if !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: MarshalObject error = %v; want one matching ErrUnrepresentable", c.name, err)
		}
	}
}

// Valid text behind a pointer or in a named type is written as the text it
// holds, its method unasked: U+FFFD itself, and text spelling the escape U+FFFD
// has in JSON, pass, since the rule refuses bytes that are no character, never
// a character.
func TestMarshalObject_ValidTextBehindACarrierIsWritten(t *testing.T) {
	t.Parallel()
	replacement := "a�b"
	escapeText := "a" + string(rune(0x5c)) + "ufffdb"
	for _, c := range []struct {
		name  string
		props map[string]any
		want  string
	}{
		{"a pointer to U+FFFD", map[string]any{"text": &replacement}, "a�b"},
		{"a pointer to the escape's text", map[string]any{"text": &escapeText}, "a" + string(rune(0x5c)) + string(rune(0x5c)) + "ufffdb"},
		{"a type spelling U+FFFD with MarshalText", map[string]any{"text": jsonTextMarshaler(replacement)}, "a�b"},
		{"a type spelling U+FFFD with MarshalJSON", map[string]any{"text": jsonRawMarshaler(replacement)}, "a�b"},
	} {
		data, err := New().MarshalObject(t.Context(), utf8JSONSnapshot(t, c.props, "ok"))
		if err != nil {
			t.Errorf("%s: MarshalObject: %v", c.name, err)
			continue
		}
		if !utf8.Valid(data) || !bytes.Contains(data, []byte(c.want)) {
			t.Errorf("%s: wrote %s; want valid UTF-8 holding %s", c.name, data, c.want)
		}
	}
}

// A pointer is written as the value it points to, so it is judged as that
// value: a float carries its indicator, and a number literal reads back or is
// refused, as each does held bare.
func TestMarshalObject_APointerIsJudgedAsTheValueItPointsTo(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberJSONSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	five := 5.0
	half := stdjson.Number("1.5")
	huge := stdjson.Number("1e400")
	empty := stdjson.Number("")
	data, err := New().MarshalObject(t.Context(), numberSnapshot(t, s, map[string]any{"n": &five}, nil))
	if err != nil || !bytes.Contains(data, []byte(`"n":5.0`)) {
		t.Errorf("*float64(5) at an Integer wrote %s, %v; want \"n\":5.0, which the Integer refuses on the way back", data, err)
	}
	data, err = New().MarshalObject(t.Context(), numberSnapshot(t, s, map[string]any{"f": &half}, nil))
	if err != nil {
		t.Fatalf("*json.Number(1.5) at a Float: %v", err)
	}
	if inst, rerr := readBackHolder(t, s, data); rerr != nil {
		t.Errorf("*json.Number(1.5) wrote %s, which does not read back: %v", data, rerr)
	} else if f, _ := floatProperty(inst, "f"); f != 1.5 {
		t.Errorf("*json.Number(1.5) read back as %v", f)
	}
	for name, p := range map[string]*stdjson.Number{"1e400": &huge, "empty": &empty} {
		if _, err := New().MarshalObject(t.Context(), numberSnapshot(t, s, map[string]any{"f": p}, nil)); !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("a pointer to the json.Number %s at a Float: error = %v; want one matching ErrUnrepresentable", name, err)
		}
	}
}

// A json.Number whose text is no JSON number literal is a value of no form,
// refused at the kind that would read its number too: the writer never
// converts text strconv reads and no JSON reader does.
func TestMarshalObject_AJSONNumberThatIsNoLiteralIsRefused(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberJSONSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	signed := stdjson.Number("+5")
	for _, c := range []struct {
		prop string
		v    any
	}{
		{"n", stdjson.Number("+5")},
		{"n", stdjson.Number("007")},
		{"n", &signed},
		{"f", stdjson.Number("0x1p4")},
		{"f", stdjson.Number("1_000")},
		{"f", stdjson.Number(".5")},
	} {
		data, err := New().MarshalObject(t.Context(), numberSnapshot(t, s, map[string]any{c.prop: c.v}, nil))
		if !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s = %#v: wrote %s, error = %v; want one matching ErrUnrepresentable", c.prop, c.v, data, err)
		}
	}
}

// A refusal of a value names every class the writer refuses, so a reader of
// the message learns what the value may have been.
func TestMarshalObject_ARefusalNamesEveryClass(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberJSONSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	_, err := New().MarshalObject(t.Context(), numberSnapshot(t, s, map[string]any{"f": math.NaN()}, nil))
	if !errors.Is(err, ErrUnrepresentable) {
		t.Fatalf("NaN at a Float: error = %v; want one matching ErrUnrepresentable", err)
	}
	for _, class := range []string{
		"a non-finite float", "a number literal this adapter does not read back as a number",
		"a string that is not valid UTF-8", "a time or a UUID its constraint does not render as text that reads back",
		"a value that is no scalar, list or string-keyed map",
	} {
		if !strings.Contains(err.Error(), class) {
			t.Errorf("the refusal does not name %q: %v", class, err)
		}
	}
}

// At a String no numeric kind stores a json.Number first, so a literal that
// reads back is written as the literal it is, never as a JSON string.
func TestMarshalObject_ANumberAtAStringIsWrittenAsItsLiteral(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberJSONSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	for _, text := range []string{"5", "-0", "1.5"} {
		data, err := New().MarshalObject(t.Context(), numberSnapshot(t, s, map[string]any{"s": stdjson.Number(text)}, nil))
		if err != nil || !bytes.Contains(data, []byte(`"s":`+text+`,`)) && !bytes.Contains(data, []byte(`"s":`+text+`}`)) {
			t.Errorf("json.Number(%s) at a String wrote %s, %v; want the literal unquoted", text, data, err)
		}
	}
}

// A time.Time or a uuid.UUID is written only where its constraint renders it
// as text, as every writer of this module writes it: at a Date or a UUID it is
// written as its text, and anywhere else it is refused as the class.
func TestMarshalObject_ATimeOrAUUIDItsConstraintDoesNotRenderIsRefused(t *testing.T) {
	t.Parallel()
	day := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	for _, c := range []struct {
		name  string
		props map[string]any
		label any
	}{
		{"a time at a String", map[string]any{"text": day}, nil},
		{"a time past year 9999 at a String", map[string]any{"text": time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}, nil},
		{"a uuid at a String", map[string]any{"text": id}, nil},
		{"a time in a map value", map[string]any{"text": map[string]any{"x": day}}, nil},
		{"a uuid in a list of strings", map[string]any{"tags": []any{id}}, nil},
		{"a time at an edge property", nil, day},
	} {
		_, err := New().MarshalObject(t.Context(), utf8JSONSnapshot(t, c.props, c.label))
		if !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: MarshalObject error = %v; want one matching ErrUnrepresentable", c.name, err)
		}
	}
	data, err := New().MarshalObject(t.Context(), utf8JSONSnapshot(t, map[string]any{"day": day, "uid": id}, "ok"))
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}
	for _, want := range []string{`"day":"2024-01-02"`, `"uid":"` + id.String() + `"`} {
		if !bytes.Contains(data, []byte(want)) {
			t.Errorf("the document does not hold %s: %s", want, data)
		}
	}
}

// A time whose stored text does not read back is no value JSON writes, at its
// own kind too: a Date past year 9999 renders as text no date parse reads, and
// a pointer is how such a time reaches the writer.
func TestMarshalObject_ATimeWhoseTextDoesNotReadBackIsRefused(t *testing.T) {
	t.Parallel()
	far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)
	for name, v := range map[string]any{"past year 9999": far, "a pointer past year 9999": &far, "a pointer before year 0": &before} {
		_, err := New().MarshalObject(t.Context(), utf8JSONSnapshot(t, map[string]any{"day": v}, "ok"))
		if !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: MarshalObject error = %v; want one matching ErrUnrepresentable", name, err)
		}
	}
}
