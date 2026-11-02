package json

import (
	"bytes"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/snapparts"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const numberJSONSchema = `schema "numbers"

type Target {
	key String primary
}

type Holder {
	id String primary
	f Float
	n Integer
	s String
	fs List<Float>
	v Vector[2]
	--> LINK (_:many) Target {
		w Float
	}
}
`

// heldNumbers are the number literals a document spells, those no float64 holds
// among them, which snapshot.Load returns as they were written.
var heldNumbers = []string{
	"0", "5", "-5", "1.5", "-1.5", "1e2", "1E2", "1e-2", "0.0", "-0.0",
	"9223372036854775807", "9223372036854775808", "-9223372036854775809", "99999999999999999999",
	"1e308", "1.7976931348623157e308", "1.7976931348623159e308", "1e309", "1e400", "-1e400", "1.5e999",
	"1e-400",
	strings.Repeat("9", 400), "0." + strings.Repeat("0", 400) + "1", "1" + strings.Repeat("0", 400) + ".0",
}

// numberSnapshot reads a document holding props beside Holder h1's key, and weight
// as the w on its one LINK to Target t1.
func numberSnapshot(t *testing.T, s schema.Schema, props map[string]any, weight any) graph.Snapshot {
	t.Helper()
	all := map[string]any{"id": "h1"}
	maps.Copy(all, props)
	edge := map[string]any{}
	if weight != nil {
		edge["w"] = weight
	}
	return snappartstest.Loaded(t, s, snapparts.Parts{
		Instances: []snapparts.Instance{
			heldRoot(t, s, "Target", "t1", map[string]any{"key": "t1"}),
			heldRoot(t, s, "Holder", "h1", all),
		},
		Edges: []snapparts.Edge{heldEdge(t, s, "Holder", "h1", "LINK", "t1", edge)},
	})
}

// readBackHolder parses data with the adapter's own parser and validates the
// one Holder it holds, the read a written document is made for.
func readBackHolder(t *testing.T, s schema.Schema, data []byte) (instance.ValidInstance, error) {
	t.Helper()
	parsed, res := New().ParseObject(t.Context(), location.NewSourceID("numbers.json"), data)
	if res.HasErrors() || len(parsed["Holder"]) != 1 {
		return instance.ValidInstance{}, fmt.Errorf("ParseObject: %s", res)
	}
	inst, vres := instance.NewValidator(s).ValidateOne(t.Context(), "Holder", parsed["Holder"][0])
	if inst.IsZero() || vres.HasErrors() {
		return instance.ValidInstance{}, fmt.Errorf("ValidateOne: %s", vres)
	}
	return inst, nil
}

// What the writer writes for a number, the parser and the validator read back
// as that number, at every position a Float stands in; a number they would not
// read back so is refused as the class, naming its site and the fault, and
// nothing else is. At a String, where no numeric kind stores the number in its
// own form first, the same numbers are written and the same refused. strconv says
// which text is a number a float64 holds.
func TestMarshalObject_ANumberIsReadBackOrRefused(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberJSONSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	for _, text := range heldNumbers {
		n := stdjson.Number(text)
		nearest, ferr := strconv.ParseFloat(text, 64)
		readable := ferr == nil

		for _, p := range []struct {
			name, mentions string
			props          map[string]any
			weight         any
			read           func(instance.ValidInstance) (float64, bool)
		}{
			{
				"a Float property", `property "f"`,
				map[string]any{"f": n},
				nil,
				func(i instance.ValidInstance) (float64, bool) { return floatProperty(i, "f") },
			},
			{
				"a List<Float> element", `property "fs"`,
				map[string]any{"fs": []any{1.5, n}},
				nil,
				func(i instance.ValidInstance) (float64, bool) { return floatElement(i, "fs", 1) },
			},
			{
				"a Vector element", `property "v"`,
				map[string]any{"v": []any{n, 2.5}},
				nil,
				func(i instance.ValidInstance) (float64, bool) { return floatElement(i, "v", 0) },
			},
			{"a String property", `property "s"`, map[string]any{"s": n}, nil, nil},
			{
				"a Float edge property", `edge property "w"`, nil, n,
				func(i instance.ValidInstance) (float64, bool) {
					link, _ := i.Edge("LINK")
					targets := slices.Collect(link.Targets())
					if len(targets) == 0 {
						return 0, false
					}
					w, ok := targets[0].Properties().Get("w")
					if !ok {
						return 0, false
					}
					return w.Float()
				},
			},
		} {
			t.Run(fmt.Sprintf("%q at %s", text, p.name), func(t *testing.T) {
				t.Parallel()
				snap := numberSnapshot(t, s, p.props, p.weight)
				data, err := New().MarshalObject(t.Context(), snap)
				switch {
				case errors.Is(err, ErrUnrepresentable):
					if readable {
						t.Errorf("refused a number the document can hold: %v", err)
					}
					if !strings.Contains(err.Error(), p.mentions) || !strings.Contains(err.Error(), "a number literal this adapter does not read back as a number") {
						t.Errorf("the refusal does not name %s and the fault: %v", p.mentions, err)
					}
				case err != nil:
					t.Errorf("MarshalObject failed outside the class: %v", err)
				case p.read == nil:
					// Nothing stored the number in a kind's own form, so
					// what was written is the literal itself.
					if !readable {
						t.Errorf("written as %s, though %q is no number a float64 holds", data, text)
					}
				default:
					inst, rerr := readBackHolder(t, s, data)
					if rerr != nil {
						t.Fatalf("written as %s, which does not read back: %v", data, rerr)
					}
					got, ok := p.read(inst)
					if !ok || math.Float64bits(got) != math.Float64bits(nearest) {
						t.Errorf("written as %s and read back as %v (%t), want the float64 nearest %q", data, got, ok, text)
					}
				}
			})
		}
	}
}

// An integer literal an int64 holds reads back at an Integer as that integer.
func TestMarshalObject_AnIntegerLiteralIsReadBackAtAnInteger(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberJSONSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	for _, text := range []string{"0", "5", "-5", "9223372036854775807", "-9223372036854775808"} {
		want, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		data, err := New().MarshalObject(t.Context(), numberSnapshot(t, s, map[string]any{"n": stdjson.Number(text)}, nil))
		if err != nil {
			t.Fatalf("MarshalObject(%s): %v", text, err)
		}
		inst, rerr := readBackHolder(t, s, data)
		if rerr != nil {
			t.Fatalf("%s written as %s, which does not read back: %v", text, data, rerr)
		}
		v, _ := inst.Property("n")
		if got, ok := v.Unwrap().(int64); !ok || got != want {
			t.Errorf("%s read back as %v (%T), want the int64 %d", text, v.Unwrap(), v.Unwrap(), want)
		}
	}
}

// At a String no numeric kind stores a number first, so a literal that reads back
// is written as the literal it is, never as a JSON string.
func TestMarshalObject_ANumberAtAStringIsWrittenAsItsLiteral(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberJSONSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	for _, text := range []string{"5", "1.5"} {
		data, err := New().MarshalObject(t.Context(), numberSnapshot(t, s, map[string]any{"s": stdjson.Number(text)}, nil))
		if err != nil || !bytes.Contains(data, []byte(`"s":`+text+`,`)) && !bytes.Contains(data, []byte(`"s":`+text+`}`)) {
			t.Errorf("json.Number(%s) at a String wrote %s, %v; want the literal unquoted", text, data, err)
		}
	}
}

func floatProperty(i instance.ValidInstance, name string) (float64, bool) {
	v, ok := i.Property(name)
	if !ok {
		return 0, false
	}
	f, ok := v.Unwrap().(float64)
	return f, ok
}

func floatElement(i instance.ValidInstance, name string, at int) (float64, bool) {
	v, ok := i.Property(name)
	if !ok {
		return 0, false
	}
	switch list := v.Clone().(type) {
	case []any:
		if at < len(list) {
			f, ok := list[at].(float64)
			return f, ok
		}
	case []float64:
		if at < len(list) {
			return list[at], true
		}
	}
	return 0, false
}
