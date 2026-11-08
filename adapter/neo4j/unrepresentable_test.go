package neo4j

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
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
		a, shape, snap := holderSnapshot(t, h)
		_, nodeErr := a.BatchNodeQueries(t.Context(), snap, shape)
		_, edgeErr := a.BatchEdgeQueries(t.Context(), snap, shape)
		err := errors.Join(nodeErr, edgeErr)
		if err == nil || !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want ErrUnrepresentable", name, err)
		}
	}

	nested := schema.NewListConstraint(schema.NewListConstraint(schema.NewIntegerConstraint()))
	for name, err := range map[string]error{
		"a value, an Integer holding a float": second(coerceValue(schema.NewIntegerConstraint(), 2.5)),
		"a value, a nested collection":        second(coerceValue(nested, [][]int64{{1}})),
		"a value, a map":                      second(coerceValue(schema.NewStringConstraint(), map[string]string{"a": "b"})),
		"CoerceParams, a Date that does not parse": second(CoerceParams(
			map[string]any{"d": "x"}, ParamTypes{"d": schema.NewDateConstraint()})),
		"CoerceParams, a row's field": second(CoerceParams(
			map[string]any{"rows": []map[string]any{{"n": "x"}}}, ParamTypes{"rows.n": schema.NewIntegerConstraint()})),
		"a nil primary key": second(extractKeyProps(immutable.WrapProperties(map[string]any{"id": nil}),
			&nodeShape{label: "Holder", primaryKeys: []string{"id"}})),
		"a missing primary key": second(extractKeyProps(immutable.WrapProperties(map[string]any{}),
			&nodeShape{label: "Holder", primaryKeys: []string{"id"}})),
		"a missing primary key beside a nil one": second(extractKeyProps(immutable.WrapProperties(map[string]any{"b": nil}),
			&nodeShape{label: "Holder", primaryKeys: []string{"a", "b"}})),
	} {
		if !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want ErrUnrepresentable", name, err)
		}
	}
	if _, err := coerceValue(nested, [][]int64{{1}}); !errors.Is(err, ErrUnsupportedListElem) {
		t.Errorf("a nested collection's refusal = %v; want ErrUnsupportedListElem beside the class", err)
	}
}

// A caller's own error is not a refusal of the data: a constraint
// CheckConstraint refuses, a constraint at a container param's key, and a shape
// the adapter did not build carry no class.
func TestErrUnrepresentable_MarksNoCallersError(t *testing.T) {
	t.Parallel()
	a, _, snap := holderSnapshot(t, holderValues{})
	for name, err := range map[string]error{
		"a refused constraint at CoerceParams": second(CoerceParams(nil, ParamTypes{"p": schema.ListConstraint{}})),
		"a constraint at a nested map's key": second(CoerceParams(
			map[string]any{"p": map[string]any{"x": 1}}, ParamTypes{"p": schema.NewStringConstraint()})),
		"a constraint at a rows param's key": second(CoerceParams(
			map[string]any{"rows": []map[string]any{{"x": 1}}}, ParamTypes{"rows": schema.NewStringConstraint()})),
		"a shape the adapter did not build": second(a.BatchNodeQueries(t.Context(), snap, &GraphShape{})),
		"a shape with no primary key":       second(extractKeyProps(immutable.WrapProperties(map[string]any{}), &nodeShape{label: "Holder"})),
	} {
		if err == nil || errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want an error without ErrUnrepresentable", name, err)
		}
	}
}

// second returns the error of a call's two results.
func second[T any](_ T, err error) error { return err }
