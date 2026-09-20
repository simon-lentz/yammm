package json

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// indicatorSchema declares a Float reached three ways: a plain property, a
// List<Float> and a Vector, the last two both directly and through a datatype
// alias. A Float primary key is not among them because the schema refuses one
// (E_INVALID_PRIMARY_KEY_TYPE).
const indicatorSchema = `schema "ind"

type Samples = List<Float>
type Embedding = Vector[2]
type Ratio = Float
type Weights = List<Ratio>

type Station {
	code String primary
}

type Reading {
	id String primary
	ratio Float
	samples List<Float>
	embedding Vector[2]
	aliasedSamples Samples
	aliasedEmbedding Embedding
	aliasedRatio Ratio
	aliasedElements Weights
	--> LINK (_) Station {
		w Float
	}
}
`

func indicatorDoc(t *testing.T, props map[string]any) string {
	t.Helper()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, indicatorSchema, "ind.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	ty, ok := s.Type("Reading")
	if !ok {
		t.Fatal("no Reading type")
	}
	g := graph.New(s)
	inst := instance.NewValidInstance("Reading", ty.ID(),
		immutable.WrapKey([]any{"r1"}),
		immutable.WrapProperties(props), nil, nil, nil)
	if r := g.Add(ctx, inst); r.Err() != nil {
		t.Fatalf("add: %v", r.Err())
	}
	doc, err := New().MarshalObject(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(doc)
}

// A whole-valued Float carries a float indicator, as snapshot.wireValue
// already emits it, so the value survives this package's own reader: without
// it the number is int-shaped and immutable.NormalizeNumber's lexical rule
// reads it back as int64.
func TestMarshalObject_WholeFloatCarriesTheIndicator(t *testing.T) {
	t.Parallel()
	doc := indicatorDoc(t, map[string]any{"id": "r1", "ratio": float64(1)})
	if !strings.Contains(doc, `"ratio":1.0}`) {
		t.Errorf("a whole Float wrote %s, want a ratio of 1.0", doc)
	}
}

// The indicator survives the round trip this package owns both halves of.
func TestMarshalObject_WholeFloatSurvivesItsOwnReader(t *testing.T) {
	t.Parallel()
	doc := indicatorDoc(t, map[string]any{"id": "r1", "ratio": float64(1)})
	parsed, res := New().ParseObject(context.Background(),
		location.NewSourceID("ind.json"), []byte(doc))
	if err := res.Err(); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	got := parsed["Reading"][0].Properties["ratio"]
	if f, ok := got.(float64); !ok || f != 1 {
		t.Errorf("a whole Float re-read as %#v (%T), want float64(1)", got, got)
	}
}

// The sign of a graph-held negative zero survives the write side. A
// document literal -0 carries no indicator, so the reader's lexical rule
// classifies it int64 and the sign is gone before any writer runs; that half
// is immutable.NormalizeNumber's ratified rule and is not repaired here.
func TestMarshalObject_NegativeZeroKeepsItsSign(t *testing.T) {
	t.Parallel()
	doc := indicatorDoc(t, map[string]any{"id": "r1", "ratio": math.Copysign(0, -1)})
	if !strings.Contains(doc, `"ratio":-0.0}`) {
		t.Errorf("a negative zero wrote %s, want a ratio of -0.0", doc)
	}
	parsed, res := New().ParseObject(context.Background(),
		location.NewSourceID("ind.json"), []byte(doc))
	if err := res.Err(); err != nil {
		t.Fatalf("reparse: %v", err)
	}
	got := parsed["Reading"][0].Properties["ratio"]
	f, ok := got.(float64)
	if !ok || !math.Signbit(f) {
		t.Errorf("a negative zero re-read as %#v (%T), want a float64 with its sign", got, got)
	}
}

// Every collection whose elements are floats carries the indicator at its
// elements, whether it names the collection directly or through an alias.
// The Vector arms reach the Float constraint through [elementConstraint].
func TestMarshalObject_FloatBearingCollectionsCarryTheIndicator(t *testing.T) {
	t.Parallel()
	doc := indicatorDoc(t, map[string]any{
		"id":               "r1",
		"ratio":            float64(2),
		"samples":          []any{float64(1), 2.5},
		"embedding":        []any{float64(3), float64(4)},
		"aliasedSamples":   []any{float64(5)},
		"aliasedEmbedding": []any{float64(6), float64(7)},
	})
	for _, want := range []string{
		`"samples":[1.0,2.5]`,
		`"embedding":[3.0,4.0]`,
		`"aliasedSamples":[5.0]`,
		`"aliasedEmbedding":[6.0,7.0]`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("wrote %s, want it to contain %s", doc, want)
		}
	}
}

// An undeclared property has no constraint, so nothing says its value is a
// float and no indicator is added. A whole float stored under a name the type
// does not declare stays int-shaped, as the schema-driven rule requires.
func TestMarshalObject_UndeclaredPropertyTakesNoIndicator(t *testing.T) {
	t.Parallel()
	doc := indicatorDoc(t, map[string]any{
		"id": "r1", "ratio": float64(1), "extra": float64(8),
	})
	if !strings.Contains(doc, `"extra":8`) || strings.Contains(doc, `"extra":8.0`) {
		t.Errorf("an undeclared whole float wrote %s, want an extra of 8", doc)
	}
}

// An Integer property is untouched: the indicator belongs to float-bearing
// constraints alone.
func TestMarshalObject_IntegerTakesNoIndicator(t *testing.T) {
	t.Parallel()
	const s = `schema "i"

type Counter {
	id String primary
	n Integer
}
`
	ctx := context.Background()
	sc, res := schema.LoadString(ctx, s, "i.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	ty, _ := sc.Type("Counter")
	g := graph.New(sc)
	inst := instance.NewValidInstance("Counter", ty.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"id": "c1", "n": int64(5)}),
		nil, nil, nil)
	if r := g.Add(ctx, inst); r.Err() != nil {
		t.Fatalf("add: %v", r.Err())
	}
	doc, err := New().MarshalObject(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(doc), `"n":5`) || strings.Contains(string(doc), `"n":5.0`) {
		t.Errorf("an Integer wrote %s, want an n of 5", doc)
	}
}

// The indented path re-scans each rendered number, so the indicator has to
// survive that scan as well as the compact one.
func TestMarshalObject_IndentedOutputKeepsTheIndicator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, indicatorSchema, "ind.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	ty, _ := s.Type("Reading")
	g := graph.New(s)
	inst := instance.NewValidInstance("Reading", ty.ID(),
		immutable.WrapKey([]any{"r1"}),
		immutable.WrapProperties(map[string]any{"id": "r1", "ratio": float64(1)}),
		nil, nil, nil)
	if r := g.Add(ctx, inst); r.Err() != nil {
		t.Fatalf("add: %v", r.Err())
	}
	doc, err := New().MarshalObject(ctx, g.Snapshot(), WithIndent("  "))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(doc), "\"ratio\": 1.0\n") {
		t.Errorf("indented output wrote %s, want a ratio of 1.0", doc)
	}
}

// A Float reached through a datatype alias is still float-bearing. The
// constraint arrives as a schema.AliasConstraint, whose Kind is KindAlias, so
// only the alias resolution keeps the indicator on it. A list of a Float alias
// reaches the same line unresolved, because ListConstraint.Element returns its
// element as declared.
func TestMarshalObject_AliasDeclaredFloatCarriesTheIndicator(t *testing.T) {
	t.Parallel()
	doc := indicatorDoc(t, map[string]any{
		"id":              "r1",
		"ratio":           float64(1),
		"aliasedRatio":    float64(3),
		"aliasedElements": []any{float64(9)},
	})
	for _, want := range []string{`"aliasedRatio":3.0`, `"aliasedElements":[9.0]`} {
		if !strings.Contains(doc, want) {
			t.Errorf("wrote %s, want it to contain %s", doc, want)
		}
	}
}

// A whole-valued Float edge property carries the indicator too: an edge
// property renders through its declared constraint, as a node property does.
func TestMarshalObject_EdgePropertyFloatCarriesTheIndicator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, indicatorSchema, "ind.yammm")
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, c := range []struct {
		typeName string
		props    map[string]any
	}{
		{"Station", map[string]any{"code": "sA"}},
		{"Reading", map[string]any{
			"id":    "r1",
			"ratio": float64(1),
			"link":  map[string]any{"_target_code": "sA", "w": int64(2)},
		}},
	} {
		vi, vres := v.ValidateOne(ctx, c.typeName, instance.RawInstance{Properties: c.props})
		if vres.HasErrors() {
			t.Fatalf("validate %s: %s", c.typeName, vres.String())
		}
		if r := g.Add(ctx, vi); r.Err() != nil {
			t.Fatalf("add %s: %v", c.typeName, r.Err())
		}
	}
	doc, err := New().MarshalObject(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(doc), `"w":2.0`) {
		t.Errorf("a whole Float edge property wrote %s, want a w of 2.0", doc)
	}
}

// A whole Float that encoding/json renders in exponent form already carries an
// indicator, so nothing is appended and its bytes do not move. This is the
// only place the "e" member of the indicator set is pinned in this package.
func TestMarshalObject_ExponentFormTakesNoAppendedZero(t *testing.T) {
	t.Parallel()
	doc := indicatorDoc(t, map[string]any{"id": "r1", "ratio": 1e21})
	if !strings.Contains(doc, `"ratio":1e+21`) {
		t.Errorf("wrote %s, want a ratio of 1e+21", doc)
	}
	if strings.Contains(doc, `1e+21.0`) {
		t.Errorf("wrote %s, want no zero appended to an exponent form", doc)
	}
}
