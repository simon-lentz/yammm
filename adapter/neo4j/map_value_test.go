package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// Neo4j stores no map as a property value, so a map is refused under every
// constraint, at a property, a list element, a part's property and an edge's.
func TestBatchQueries_RefuseAMapValue(t *testing.T) {
	t.Parallel()
	const refusal = "cannot store map[string]interface {}, a map, as a property value"
	for name, tc := range map[string]struct {
		h    utf8Holder
		want string
	}{
		"a String":         {utf8Holder{props: map[string]any{"text": map[string]any{"a": "b"}}}, `type "Holder": property "text": coerce String: ` + refusal},
		"a list element":   {utf8Holder{props: map[string]any{"tags": []any{map[string]any{"a": "b"}}}}, `type "Holder": property "tags": list element 0: cannot use map[string]interface {} as a String element (want string)`},
		"a part's String":  {utf8Holder{note: map[string]any{"body": map[string]any{"a": "b"}}}, `part utf8neo__Note: property "body": coerce String: ` + refusal},
		"an edge's String": {utf8Holder{label: map[string]any{"a": "b"}}, `relation "LINK": property "label": coerce String: ` + refusal},
	} {
		a, shape, snap := utf8Neo4jSnapshot(t, tc.h)
		_, nodeErr := a.BatchNodeQueries(t.Context(), snap, shape)
		_, edgeErr := a.BatchEdgeQueries(t.Context(), snap, shape)
		err := nodeErr
		if err == nil {
			err = edgeErr
		}
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: error = %v; want %s", name, err, tc.want)
		}
	}
}

type namedMap map[string]any

// Coerce and CoerceParams refuse a map whatever its key and element types.
func TestCoerce_RefusesAMapValue(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		c   schema.Constraint
		raw any
		typ string
	}{
		"a map[string]string at a String":  {schema.NewStringConstraint(), map[string]string{"a": "b"}, "map[string]string"},
		"an empty map at a Boolean":        {schema.NewBooleanConstraint(), map[string]any{}, "map[string]interface {}"},
		"a map[int]int at a List<String>":  {schema.NewListConstraint(schema.NewStringConstraint()), map[int]int{1: 2}, "map[int]int"},
		"a map[string]float64 at a Vector": {schema.NewVectorConstraint(2), map[string]float64{}, "map[string]float64"},
		"an empty map at an Integer":       {schema.NewIntegerConstraint(), map[string]any{}, "map[string]interface {}"},
		"a named map at a UUID":            {schema.NewUUIDConstraint(), namedMap{}, "neo4j.namedMap"},
	} {
		want := "coerce " + tc.c.Kind().String() + ": cannot store " + tc.typ + ", a map, as a property value"
		if _, err := Coerce(tc.c, tc.raw); err == nil || err.Error() != want {
			t.Errorf("Coerce, %s: error = %v; want %s", name, err, want)
		}
	}
	_, err := CoerceParams(map[string]any{"p": map[string]any{"q": map[string]any{}}}, ParamTypes{"p.q": schema.NewStringConstraint()})
	if want := `param "p.q": coerce String: cannot store map[string]interface {}, a map, as a property value`; err == nil || err.Error() != want {
		t.Errorf("CoerceParams, a map in a nested map: error = %v; want %s", err, want)
	}
}

// A nested collection's property type is refused before the value is read, so
// a map there draws the nested collection's refusal.
func TestCoerce_ANestedCollectionIsRefusedBeforeAMap(t *testing.T) {
	t.Parallel()
	nested := schema.NewListConstraint(schema.NewListConstraint(schema.NewStringConstraint()))
	_, err := Coerce(nested, map[string]any{"a": "b"})
	if want := "neo4j adapter: unsupported list element type: list element kind List"; err == nil || err.Error() != want {
		t.Errorf("error = %v; want %s", err, want)
	}
}
