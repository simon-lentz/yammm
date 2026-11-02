package csv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const numberCSVSchema = `schema "numbers"

type Target {
	key String primary
}

type Holder {
	id String primary
	f Float
	s String
	fs List<Float>
	v Vector[2]
	--> LINK (_:many) Target {
		w Float
	}
}
`

// heldNumbers reads a document holding props beside Holder h1's key, and weight as
// the w on its one LINK to Target t1: a document alone delivers a literal no finite
// float64 holds.
func heldNumbers(t *testing.T, s schema.Schema, props map[string]any, weight any) graph.Snapshot {
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

// numberPosition is one place a Float stands in a Holder: how to hold a value
// there and how to read it out of the validated instance.
type numberPosition struct {
	name, mentions string
	hold           func(v any) (props map[string]any, weight any)
	read           func(instance.ValidInstance) (float64, bool)
}

var numberPositions = []numberPosition{
	{
		"a Float property", `property "f"`,
		func(v any) (map[string]any, any) { return map[string]any{"f": v}, nil },
		func(i instance.ValidInstance) (float64, bool) {
			p, ok := i.Property("f")
			if !ok {
				return 0, false
			}
			f, ok := p.Unwrap().(float64)
			return f, ok
		},
	},
	{
		"a List<Float> element", `property "fs": list element 1`,
		func(v any) (map[string]any, any) { return map[string]any{"fs": []any{1.5, v}}, nil },
		func(i instance.ValidInstance) (float64, bool) { return floatAt(i, "fs", 1) },
	},
	{
		"a Vector element", `property "v": list element 0`,
		func(v any) (map[string]any, any) { return map[string]any{"v": []any{v, 2.5}}, nil },
		func(i instance.ValidInstance) (float64, bool) { return floatAt(i, "v", 0) },
	},
	{
		"a String property", `property "s"`,
		func(v any) (map[string]any, any) { return map[string]any{"s": v}, nil }, nil,
	},
	{
		"a Float edge property", `edge property "w"`,
		func(v any) (map[string]any, any) { return nil, v },
		func(i instance.ValidInstance) (float64, bool) {
			link, _ := i.Edge("LINK")
			targets := slices.Collect(link.Targets())
			if len(targets) == 0 {
				return 0, false
			}
			w, ok := targets[0].Property("w")
			if !ok {
				return 0, false
			}
			return w.Float()
		},
	},
}

func floatAt(i instance.ValidInstance, name string, at int) (float64, bool) {
	p, ok := i.Property(name)
	if !ok {
		return 0, false
	}
	switch list := p.Clone().(type) {
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

// writeAndReadBack writes snap, and when the writer does not refuse it parses
// the Holder file with the adapter's own parser and validates its one row.
func writeAndReadBack(t *testing.T, s schema.Schema, snap graph.Snapshot) (written []byte, inst instance.ValidInstance, writeErr, readErr error) {
	t.Helper()
	holder, _ := s.Type("Holder")
	a := New(WithSchema(s))
	files, err := a.MarshalSnapshot(t.Context(), snap)
	if err != nil {
		return nil, instance.ValidInstance{}, err, nil
	}
	written = files["Holder"]
	raws, res := a.ParseTyped(t.Context(), location.NewSourceID("numbers.csv"), "Holder", bytes.NewReader(written), holder)
	if res.HasErrors() || len(raws) != 1 {
		return written, instance.ValidInstance{}, nil, fmt.Errorf("ParseTyped: %d rows: %s", len(raws), res)
	}
	inst, vres := instance.NewValidator(s).ValidateOne(t.Context(), "Holder", raws[0])
	if inst.IsZero() || vres.HasErrors() {
		return written, instance.ValidInstance{}, nil, fmt.Errorf("ValidateOne: %s", vres)
	}
	return written, inst, nil, nil
}

// What the writer writes for a number literal, the parser and the validator read
// back as that number at every position a Float stands in; a literal they would
// not read back so is refused as the class, naming its site and the fault, and
// nothing else is. At a String, where no numeric kind stores the number in its own
// form first, the same literals are written and the same refused. strconv says
// which text is a number a float64 holds.
func TestMarshalSnapshot_ANumberLiteralIsReadBackOrRefused(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberCSVSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	texts := []string{
		"0", "5", "-5", "1.5", "-1.5", "1e2", "1E2", "1e-2", "0.0", "-0.0",
		"9223372036854775807", "9223372036854775808", "-9223372036854775809", "99999999999999999999",
		"1e308", "1.7976931348623157e308", "1.7976931348623159e308", "1e309", "1e400", "-1e400", "1.5e999",
		"1e-400",
		strings.Repeat("9", 400), "0." + strings.Repeat("0", 400) + "1", "1" + strings.Repeat("0", 400) + ".0",
	}
	for _, text := range texts {
		nearest, ferr := strconv.ParseFloat(text, 64)
		readable := ferr == nil
		for _, p := range numberPositions {
			t.Run(fmt.Sprintf("%q at %s", text, p.name), func(t *testing.T) {
				t.Parallel()
				props, weight := p.hold(json.Number(text))
				written, inst, werr, rerr := writeAndReadBack(t, s, heldNumbers(t, s, props, weight))
				switch {
				case errors.Is(werr, ErrUnrepresentable):
					if readable {
						t.Errorf("refused a number a cell can hold: %v", werr)
					}
					if !strings.Contains(werr.Error(), p.mentions) || !errors.Is(werr, errUnreadNumber) {
						t.Errorf("the refusal does not name %s and the fault: %v", p.mentions, werr)
					}
				case werr != nil:
					t.Errorf("MarshalSnapshot failed outside the class: %v", werr)
				case p.read == nil:
					// Nothing stored the number in a kind's own form, so
					// the cell is the literal itself.
					if !readable {
						t.Errorf("written as %q, though %q is no number a float64 holds", written, text)
					}
				case rerr != nil:
					t.Errorf("written as %q, which does not read back: %v", written, rerr)
				default:
					got, ok := p.read(inst)
					if !ok || math.Float64bits(got) != math.Float64bits(nearest) {
						t.Errorf("written as %q and read back as %v (%t), want the float64 nearest %q", written, got, ok, text)
					}
				}
			})
		}
	}
}

// A float a document holds, and an integer at a Float position, read back as the
// number they are.
func TestMarshalSnapshot_AHeldNumberIsReadBack(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberCSVSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	for _, c := range []struct {
		name string
		val  any
		want float64
	}{
		{"1.5", 1.5, 1.5},
		{"a whole float", 5.0, 5},
		{"negative zero", math.Copysign(0, -1), math.Copysign(0, -1)},
		{"the largest float64", math.MaxFloat64, math.MaxFloat64},
		{"the smallest float64", math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64},
		{"an int64", int64(-7), -7},
		{"the largest uint64 as a literal", json.Number("18446744073709551615"), float64(math.MaxUint64)},
	} {
		for _, p := range numberPositions {
			t.Run(c.name+" at "+p.name, func(t *testing.T) {
				t.Parallel()
				props, weight := p.hold(c.val)
				written, inst, werr, rerr := writeAndReadBack(t, s, heldNumbers(t, s, props, weight))
				switch {
				case werr != nil:
					t.Errorf("MarshalSnapshot refused a finite number: %v", werr)
				case p.read == nil:
				case rerr != nil:
					t.Errorf("written as %q, which does not read back: %v", written, rerr)
				default:
					if got, ok := p.read(inst); !ok || math.Float64bits(got) != math.Float64bits(c.want) {
						t.Errorf("written as %q and read back as %v (%t), want %v", written, got, ok, c.want)
					}
				}
			})
		}
	}
}
