package json

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// numberSchemaless parses a document with no schema, so every property value
// reaches the caller as the parser normalized it.
func numberSchemaless(t *testing.T, doc string) map[string]any {
	t.Helper()
	parsed, res := New().ParseObject(context.Background(), location.NewSourceID("numbers.json"), []byte(doc))
	if err := res.Err(); err != nil {
		t.Fatalf("parse %s: %v", doc, err)
	}
	items := parsed["T"]
	if len(items) != 1 {
		t.Fatalf("parse %s: %d instances, want 1", doc, len(items))
	}
	return items[0].Properties
}

// The parser takes the module's canonical number rule rather than its own.
// Classification is lexical: a float indicator means float64 and an int-shaped
// literal means int64, which is what immutable.NormalizeNumber does and what
// every other decoder in the module already agreed on.
func TestParseObject_ReadsANumberByTheModuleRule(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, literal string
		want          any
	}{
		{name: "an int-shaped literal is int64", literal: "7", want: int64(7)},
		{name: "a negative int-shaped literal is int64", literal: "-7", want: int64(-7)},
		{name: "a decimal point means float64", literal: "7.5", want: 7.5},
		{name: "a whole value with a point is float64", literal: "7.0", want: 7.0},
		{name: "an exponent means float64 even when the value is whole", literal: "1e2", want: 100.0},
		{name: "a capital exponent means float64", literal: "1E2", want: 100.0},
		{name: "past int64 keeps its exact text", literal: "99999999999999999999", want: json.Number("99999999999999999999")},
		{name: "below int64 keeps its exact text", literal: "-9223372036854775809", want: json.Number("-9223372036854775809")},
		{name: "zero is int64", literal: "0", want: int64(0)},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc := fmt.Sprintf(`{"T":[{"v":%s}]}`, c.literal)
			got := numberSchemaless(t, doc)["v"]
			if got != c.want {
				t.Errorf("%s parsed as %#v, want %#v", c.literal, got, c.want)
			}
		})
	}
}

// A number no float64 holds keeps its json.Number rather than becoming a
// string. A string silently changes the value's kind: internal/value's
// Classify reads one as StringKind, so nothing downstream can tell it from a
// declared String, where it reads an unconvertible json.Number as
// UnspecifiedKind.
func TestParseObject_KeepsANumberNoFloatHolds(t *testing.T) {
	t.Parallel()
	got := numberSchemaless(t, `{"T":[{"v":1e400}]}`)["v"]
	n, ok := got.(json.Number)
	if !ok {
		t.Fatalf("1e400 parsed as %T (%#v), want json.Number", got, got)
	}
	if n.String() != "1e400" {
		t.Errorf("1e400 parsed as %q, want the literal unchanged", n)
	}
}

// A number nested far below the decoder's own limit still normalizes. The
// walk is uncapped because encoding/json refuses a document past 10,000
// levels before the walk sees it; a 64-level cap here would leave a number
// between those depths a json.Number with nothing saying so.
func TestParseObject_NormalizesANumberBelowTheDecodersNestingLimit(t *testing.T) {
	t.Parallel()
	const depth = 5000
	doc := `{"T":[{"v":` + strings.Repeat("[", depth) + "7" + strings.Repeat("]", depth) + `}]}`

	cur := numberSchemaless(t, doc)["v"]
	for range depth {
		next, ok := cur.([]any)
		if !ok || len(next) != 1 {
			t.Fatalf("the nesting did not survive the parse at %#v", cur)
		}
		cur = next[0]
	}
	if cur != int64(7) {
		t.Errorf("the innermost value is %#v (%T), want int64(7)", cur, cur)
	}
}

// The decoder refuses a document nested past its own limit, so the walk never
// meets one deeper. This is the bound that makes the uncapped walk safe.
func TestParseObject_RefusesWhatTheDecoderWillNotNest(t *testing.T) {
	t.Parallel()
	const depth = 10000
	doc := `{"T":[{"v":` + strings.Repeat("[", depth) + "1" + strings.Repeat("]", depth) + `}]}`

	_, res := New().ParseObject(context.Background(), location.NewSourceID("deep.json"), []byte(doc))

	if res.Err() == nil {
		t.Fatal("a document nested 10,000 levels parsed; want a diagnostic")
	}
}

// vectorSchema declares the two collection shapes whose elements are floats,
// each both directly and through a datatype alias.
const vectorSchema = `schema "v"

type Embedding = Vector[2]
type Samples = List<Float>

type Reading {
	id String primary
	embedding Vector[2]
	samples List<Float>
	aliasedEmbedding Embedding
	aliasedSamples Samples
}
`

// A Vector's elements render through the Float constraint, as a List's render
// through its declared element constraint, whether the property names the
// collection directly or through a datatype alias. Without the Float
// constraint a value stored in 32 bits reaches the document at its own width,
// where the same value in a List<Float> is widened.
func TestMarshalObject_RendersAVectorElementThroughFloat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, vectorSchema, "v.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	ty, ok := s.Type("Reading")
	if !ok {
		t.Fatal("no Reading type")
	}
	g := graph.New(s)
	inst := instance.NewValidInstance("Reading", ty.ID(),
		immutable.WrapKey([]any{"r"}),
		immutable.WrapProperties(map[string]any{
			"id":               "r",
			"embedding":        []any{float32(0.1), float32(2.5)},
			"samples":          []any{float32(0.1), float32(2.5)},
			"aliasedEmbedding": []any{float32(0.1), float32(2.5)},
			"aliasedSamples":   []any{float32(0.1), float32(2.5)},
		}), nil, nil, nil)
	if r := g.Add(ctx, inst); r.Err() != nil {
		t.Fatalf("add: %v", r.Err())
	}

	data, err := New().MarshalObject(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	doc := string(data)

	// float64(float32(0.1)) is 0.10000000149011612: the widened spelling the
	// Float constraint stores. One per property, and a property declared
	// through a datatype alias renders as the same shape declared directly —
	// the alias reaches the writer unresolved, so a renderer that reads the
	// constraint without resolving it loses the element rule.
	const widened = "0.10000000149011612"
	if got := strings.Count(doc, widened); got != 4 {
		t.Errorf("0.1 renders widened %d times, want 4 (Vector, List<Float>, and each through an alias):\n%s", got, doc)
	}
}

const integerRangeSchema = `schema "r"

type T {
	id String primary
	n Integer
	f Float
}
`

// An integer literal outside int64 is refused at an Integer property, on both
// sides of the range. The nearest float64 of a literal just below MinInt64 is
// exactly -2^63, so reading the literal through float64 would store MinInt64,
// a different integer from the one the document wrote.
func TestParseObject_IntegerLiteralOutsideInt64IsRefusedAtAnInteger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, integerRangeSchema, "r.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	for _, literal := range []string{
		"9223372036854775808",
		"-9223372036854775809",
		"-9223372036854776832",
		"99999999999999999999",
	} {
		t.Run(literal, func(t *testing.T) {
			t.Parallel()
			doc := fmt.Sprintf(`{"T":[{"id":"a","n":%s,"f":1.5}]}`, literal)
			parsed, pres := New().ParseObject(ctx, location.NewSourceID("r.json"), []byte(doc))
			if err := pres.Err(); err != nil {
				t.Fatalf("parse: %v", err)
			}
			_, vres := instance.NewValidator(s).Validate(ctx, "T", parsed["T"])
			if !vres.HasErrors() {
				t.Fatalf("%s validated at an Integer property", literal)
			}
			if !strings.Contains(vres.String(), "E_TYPE_MISMATCH") || !strings.Contains(vres.String(), "outside the int64 range") {
				t.Errorf("%s drew %s, want E_TYPE_MISMATCH naming the int64 range", literal, vres.String())
			}
		})
	}
}

// At a Float property the same literal is read as its nearest float64, as any
// number without a float indicator is.
func TestParseObject_IntegerLiteralOutsideInt64IsAFloatAtAFloat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, integerRangeSchema, "r.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	parsed, pres := New().ParseObject(ctx, location.NewSourceID("r.json"),
		[]byte(`{"T":[{"id":"a","n":1,"f":99999999999999999999}]}`))
	if err := pres.Err(); err != nil {
		t.Fatalf("parse: %v", err)
	}
	valid, vres := instance.NewValidator(s).Validate(ctx, "T", parsed["T"])
	if vres.HasErrors() {
		t.Fatalf("validate: %s", vres.String())
	}
	got, _ := valid[0].Property("f")
	if f, ok := got.Unwrap().(float64); !ok || f != 1e20 {
		t.Errorf("f = %#v, want float64(1e20)", got.Unwrap())
	}
}
