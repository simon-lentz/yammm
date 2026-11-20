package neo4j

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/internal/schematest"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// Every refusal of a value carries ErrUnrepresentable, whichever property's
// fault the call meets first, so a caller tells a refusal of the data from its
// own error by the class alone.
func TestErrUnrepresentable_MarksEveryRefusalOfAValue(t *testing.T) {
	t.Parallel()
	for name, h := range map[string]holderValues{
		"a Timestamp that does not parse": {props: map[string]any{"at": "not a time"}},
		"a list nested past the depth":    {props: map[string]any{"text": deepList(maxFormDepth+1, "x")}},
		"a number no driver number holds": {props: map[string]any{"text": json.Number("1e400")}},
		"a nested collection":             {props: map[string]any{"nested": []any{[]any{"x"}}}},
		"a map":                           {props: map[string]any{"text": map[string]any{"a": "b"}}},
		"a map in a list":                 {props: map[string]any{"tags": []any{map[string]any{"a": "b"}}}},
		"a part's map":                    {note: map[string]any{"body": map[string]any{"a": "b"}}},
		"an edge's map":                   {label: map[string]any{"a": "b"}},
		"an edge's Timestamp":             {when: "not a time"},
	} {
		a, snap := cypher.Config{}, holderSnapshot(t, h)
		_, nodeErr := NodeQueries(t.Context(), a, snap)
		_, edgeErr := EdgeQueries(t.Context(), a, snap)
		err := errors.Join(nodeErr, edgeErr)
		if err == nil || !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want ErrUnrepresentable", name, err)
		}
	}

	nested := schematest.Constraint(t, "List<List<Integer>>")
	for name, err := range map[string]error{
		"a value, an Integer holding a float": second(coerceValue(schematest.Constraint(t, "Integer"), 2.5)),
		"a value, a nested collection":        second(coerceValue(nested, [][]int64{{1}})),
		"a value, a map":                      second(coerceValue(schematest.Constraint(t, "String"), map[string]string{"a": "b"})),
		"CoerceParams, a Date that does not parse": second(CoerceParams(
			map[string]any{"d": "x"}, ParamTypes{"d": schematest.Constraint(t, "Date")})),
		"CoerceParams, a map at a declared key": second(CoerceParams(
			map[string]any{"p": map[string]any{"x": 1}}, ParamTypes{"p": schematest.Constraint(t, "String")})),
		"CoerceParams, rows at a declared key": second(CoerceParams(
			map[string]any{"rows": []map[string]any{{"x": 1}}}, ParamTypes{"rows": schematest.Constraint(t, "String")})),
		"a primary key its constraint refuses": second(extractKeyProps(immutable.WrapProperties(map[string]any{"id": "x"}),
			&nodeShape{label: "Holder", primaryKeys: []string{"id"}, keyConstraints: map[string]schema.Constraint{"id": schematest.Constraint(t, "Date")}})),
	} {
		if !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want ErrUnrepresentable", name, err)
		}
	}
	// A typed slice and the []any the validator stores reach different arms.
	for _, v := range []any{[][]int64{{1}}, []any{[]any{int64(1)}}} {
		if _, err := coerceValue(nested, v); !errors.Is(err, ErrUnsupportedListElem) || !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("a nested collection %#v's refusal = %v; want ErrUnsupportedListElem beside the class", v, err)
		}
	}
}

// A caller's own error is not a refusal of the data: a constraint
// CheckConstraint refuses and a zero snapshot carry no class.
func TestErrUnrepresentable_MarksNoCallersError(t *testing.T) {
	t.Parallel()
	for name, err := range map[string]error{
		"a refused constraint at CoerceParams": second(CoerceParams(nil, ParamTypes{"p": schema.ListConstraint{}})),
		"a zero snapshot at NodeQueries":       second(NodeQueries(t.Context(), cypher.Config{}, graph.Snapshot{})),
		"a zero snapshot at EdgeQueries":       second(EdgeQueries(t.Context(), cypher.Config{}, graph.Snapshot{})),
	} {
		if err == nil || errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want an error without ErrUnrepresentable", name, err)
		}
	}
}

// second returns the error of a call's two results.
func second[T any](_ T, err error) error { return err }
