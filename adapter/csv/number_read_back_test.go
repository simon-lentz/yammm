package csv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
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

// numberBypass builds Holder h1 through instancetest, which runs no
// validation, holding props beside its key and one LINK to Target t1 whose w
// is weight.
func numberBypass(t *testing.T, s *schema.Schema, props map[string]any, weight any) *graph.Snapshot {
	t.Helper()
	target, _ := s.Type("Target")
	holder, _ := s.Type("Holder")
	all := map[string]any{"id": "h1"}
	maps.Copy(all, props)
	edge := map[string]any{}
	if weight != nil {
		edge["w"] = weight
	}
	g := graph.New(s)
	for _, inst := range []*instance.ValidInstance{
		instancetest.VI("Target", instancetest.TypeID(target.ID()), instancetest.PK("t1"),
			instancetest.Props(map[string]any{"key": "t1"})),
		instancetest.VI("Holder", instancetest.TypeID(holder.ID()), instancetest.PK("h1"),
			instancetest.Props(all),
			instancetest.Edges(map[string]*instance.ValidEdgeData{
				"LINK": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
					instance.NewValidEdgeTarget(immutable.WrapKey([]any{"t1"}), immutable.WrapProperties(edge)),
				}),
			})),
	} {
		if res := g.Add(context.Background(), inst); !res.OK() {
			t.Fatalf("add: %s", res)
		}
	}
	return g.Snapshot()
}

// numberPosition is one place a Float stands in a Holder: how to hold a value
// there and how to read it out of the validated instance.
type numberPosition struct {
	name, mentions string
	hold           func(v any) (props map[string]any, weight any)
	read           func(*instance.ValidInstance) (float64, bool)
}

var numberPositions = []numberPosition{
	{
		"a Float property", `property "f"`,
		func(v any) (map[string]any, any) { return map[string]any{"f": v}, nil },
		func(i *instance.ValidInstance) (float64, bool) {
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
		func(i *instance.ValidInstance) (float64, bool) { return floatAt(i, "fs", 1) },
	},
	{
		"a Vector element", `property "v": list element 0`,
		func(v any) (map[string]any, any) { return map[string]any{"v": []any{v, 2.5}}, nil },
		func(i *instance.ValidInstance) (float64, bool) { return floatAt(i, "v", 0) },
	},
	{
		"a String property", `property "s"`,
		func(v any) (map[string]any, any) { return map[string]any{"s": v}, nil }, nil,
	},
	{
		"a Float edge property", `edge property "w"`,
		func(v any) (map[string]any, any) { return nil, v },
		func(i *instance.ValidInstance) (float64, bool) {
			link, _ := i.Edge("LINK")
			for target := range link.TargetsIter() {
				w, ok := target.Property("w")
				if !ok {
					return 0, false
				}
				return w.Float()
			}
			return 0, false
		},
	},
}

func floatAt(i *instance.ValidInstance, name string, at int) (float64, bool) {
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
func writeAndReadBack(t *testing.T, s *schema.Schema, snap *graph.Snapshot) (written []byte, inst *instance.ValidInstance, writeErr, readErr error) {
	t.Helper()
	holder, _ := s.Type("Holder")
	a := New(WithSchema(s))
	files, err := a.MarshalSnapshot(t.Context(), snap)
	if err != nil {
		return nil, nil, err, nil
	}
	written = files["Holder"]
	raws, res := a.ParseTyped(t.Context(), location.NewSourceID("numbers.csv"), "Holder", bytes.NewReader(written), holder)
	if res.HasErrors() || len(raws) != 1 {
		return written, nil, nil, fmt.Errorf("ParseTyped: %d rows: %s", len(raws), res)
	}
	inst, vres := instance.NewValidator(s).ValidateOne(t.Context(), "Holder", raws[0])
	if inst == nil || vres.HasErrors() {
		return written, nil, nil, fmt.Errorf("ValidateOne: %s", vres)
	}
	return written, inst, nil, nil
}

// What the writer writes for a number literal, the parser and the validator
// read back as that number at every position a Float stands in; a literal they
// would not read back so is refused as the class, naming its site and the
// fault, and nothing else is. Text that is no number literal is refused as a
// value no cell spells. At a String, where no numeric kind stores the number in
// its own form first, the same literals are written and the same refused. The
// standard library says which text is a number a float64 holds: encoding/json
// for the literal and strconv for the float.
func TestMarshalSnapshot_ANumberLiteralIsReadBackOrRefused(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberCSVSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	texts := []string{
		"0", "-0", "5", "-5", "007", "+5", "1.5", "-1.5", "1e2", "1E2", "1e-2", "0.0", "-0.0",
		"9223372036854775807", "9223372036854775808", "-9223372036854775809", "99999999999999999999",
		"1e308", "1.7976931348623157e308", "1.7976931348623159e308", "1e309", "1e400", "-1e400", "1.5e999",
		"1e-400", "", " ", "abc", "NaN", "Inf", "-Inf", "+Inf", "0x10", "0x1p4", "1_000", ".5", "5.", "1e", "1e+",
		" 5", "5 ", "5\n", "1,5", "1|5", "١٢",
		strings.Repeat("9", 400), "0." + strings.Repeat("0", 400) + "1", "1" + strings.Repeat("0", 400) + ".0",
	}
	for _, text := range texts {
		nearest, ferr := strconv.ParseFloat(text, 64)
		readable := json.Valid([]byte(text)) && strings.TrimSpace(text) == text && ferr == nil
		fault := "reads back as a number in no column"
		if !immutable.IsNumberLiteral(json.Number(text)) {
			fault = "has no spelling in a cell"
		}
		for _, p := range numberPositions {
			t.Run(fmt.Sprintf("%q at %s", text, p.name), func(t *testing.T) {
				t.Parallel()
				props, weight := p.hold(json.Number(text))
				written, inst, werr, rerr := writeAndReadBack(t, s, numberBypass(t, s, props, weight))
				switch {
				case errors.Is(werr, ErrUnrepresentable):
					if readable {
						t.Errorf("refused a number a cell can hold: %v", werr)
					}
					if !strings.Contains(werr.Error(), p.mentions) || !strings.Contains(werr.Error(), fault) {
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
					if !ok || ferr != nil || math.Float64bits(got) != math.Float64bits(nearest) {
						t.Errorf("written as %q and read back as %v (%t), want the float64 nearest %q", written, got, ok, text)
					}
				}
			})
		}
	}
}

// A Go number is read back as the number it is, or
// refused: no cell spells a NaN or an infinity that the parser reads.
func TestMarshalSnapshot_AGoNumberIsReadBackOrRefused(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), numberCSVSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	type namedFloat float64
	for _, c := range []struct {
		name string
		val  any
		want float64
	}{
		{"NaN", math.NaN(), math.NaN()},
		{"+Inf", math.Inf(1), math.Inf(1)},
		{"-Inf", math.Inf(-1), math.Inf(-1)},
		{"a float32 NaN", float32(math.NaN()), math.NaN()},
		{"a named NaN", namedFloat(math.NaN()), math.NaN()},
		{"a float32 infinity", float32(math.Inf(1)), math.Inf(1)},
		{"1.5", 1.5, 1.5},
		{"a whole float", 5.0, 5},
		{"negative zero", math.Copysign(0, -1), math.Copysign(0, -1)},
		{"the largest float64", math.MaxFloat64, math.MaxFloat64},
		{"the smallest float64", math.SmallestNonzeroFloat64, math.SmallestNonzeroFloat64},
		{"a float32", float32(0.1), float64(float32(0.1))},
		{"a named float", namedFloat(2.5), 2.5},
		{"an int64", int64(-7), -7},
		{"the largest uint64", uint64(math.MaxUint64), float64(math.MaxUint64)},
	} {
		finite := !math.IsNaN(c.want) && !math.IsInf(c.want, 0)
		for _, p := range numberPositions {
			t.Run(c.name+" at "+p.name, func(t *testing.T) {
				t.Parallel()
				props, weight := p.hold(c.val)
				written, inst, werr, rerr := writeAndReadBack(t, s, numberBypass(t, s, props, weight))
				switch {
				case errors.Is(werr, ErrUnrepresentable):
					if finite {
						t.Errorf("refused a finite number: %v", werr)
					}
					if !strings.Contains(werr.Error(), p.mentions) {
						t.Errorf("the refusal does not name %s: %v", p.mentions, werr)
					}
				case werr != nil:
					t.Errorf("MarshalSnapshot failed outside the class: %v", werr)
				case !finite:
					t.Errorf("written as %q, though the number is not finite", written)
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
