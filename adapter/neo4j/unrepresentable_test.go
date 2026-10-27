package neo4j

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Every refusal of a value carries ErrUnrepresentable, whichever property's
// fault the call meets first, so a caller tells a refusal of the data from its
// own error by the class alone.
func TestErrUnrepresentable_MarksEveryRefusalOfAValue(t *testing.T) {
	t.Parallel()
	for name, h := range map[string]utf8Holder{
		"a string that is not valid UTF-8":    {props: map[string]any{"text": "a\xffb"}},
		"a Timestamp that does not parse":     {props: map[string]any{"at": "not a time"}},
		"a time its constraint does not show": {props: map[string]any{"text": time.Unix(0, 0)}},
		"a value of no form":                  {props: map[string]any{"text": struct{}{}}},
		"a number no driver number holds":     {props: map[string]any{"text": json.Number("1e400")}},
		"a nested collection":                 {props: map[string]any{"nested": []any{[]any{"x"}}}},
		"a map":                               {props: map[string]any{"text": map[string]any{"a": "b"}}},
		"a map in a list":                     {props: map[string]any{"tags": []any{map[string]any{"a": "b"}}}},
		"a part's map":                        {note: map[string]any{"body": map[string]any{"a": "b"}}},
		"an edge's text":                      {label: "a\xffb"},
		"an edge's map":                       {label: map[string]any{"a": "b"}},
		"an edge's Timestamp":                 {when: "not a time"},
	} {
		a, shape, snap := utf8Neo4jSnapshot(t, h)
		_, nodeErr := a.BatchNodeQueries(t.Context(), snap, shape)
		_, edgeErr := a.BatchEdgeQueries(t.Context(), snap, shape)
		err := errors.Join(nodeErr, edgeErr)
		if err == nil || !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want ErrUnrepresentable", name, err)
		}
	}

	nested := schema.NewListConstraint(schema.NewListConstraint(schema.NewIntegerConstraint()))
	for name, err := range map[string]error{
		"Coerce, an Integer holding a float": second(Coerce(schema.NewIntegerConstraint(), 2.5)),
		"Coerce, a nested collection":        second(Coerce(nested, [][]int64{{1}})),
		"Coerce, a map":                      second(Coerce(schema.NewStringConstraint(), map[string]string{"a": "b"})),
		"CoerceParams, a Date that does not parse": second(CoerceParams(
			map[string]any{"d": "x"}, ParamTypes{"d": schema.NewDateConstraint()})),
		"CoerceParams, a row's field": second(CoerceParams(
			map[string]any{"rows": []map[string]any{{"n": "x"}}}, ParamTypes{"rows.n": schema.NewIntegerConstraint()})),
		"a nil primary key": second(extractKeyProps(immutable.WrapProperties(map[string]any{"id": nil}),
			&NodeShape{Label: "Holder", PrimaryKeys: []string{"id"}})),
		"a missing primary key": second(extractKeyProps(immutable.WrapProperties(map[string]any{}),
			&NodeShape{Label: "Holder", PrimaryKeys: []string{"id"}})),
	} {
		if !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want ErrUnrepresentable", name, err)
		}
	}
	if _, err := Coerce(nested, [][]int64{{1}}); !errors.Is(err, ErrUnsupportedListElem) {
		t.Errorf("a nested collection's refusal = %v; want ErrUnsupportedListElem beside the class", err)
	}
}

// A caller's own error is not a refusal of the data: a constraint
// CheckConstraint refuses, a constraint at a container param's key, and a shape
// the adapter did not build carry no class.
func TestErrUnrepresentable_MarksNoCallersError(t *testing.T) {
	t.Parallel()
	a, _, snap := utf8Neo4jSnapshot(t, utf8Holder{})
	for name, err := range map[string]error{
		"a refused constraint at Coerce":       second(Coerce(schema.ListConstraint{}, []any{1})),
		"a refused constraint at CoerceParams": second(CoerceParams(nil, ParamTypes{"p": schema.ListConstraint{}})),
		"a constraint at a nested map's key": second(CoerceParams(
			map[string]any{"p": map[string]any{"x": 1}}, ParamTypes{"p": schema.NewStringConstraint()})),
		"a constraint at a rows param's key": second(CoerceParams(
			map[string]any{"rows": []map[string]any{{"x": 1}}}, ParamTypes{"rows": schema.NewStringConstraint()})),
		"a shape the adapter did not build": second(a.BatchNodeQueries(t.Context(), snap, &GraphShape{})),
		"a shape with no primary key":       second(extractKeyProps(immutable.WrapProperties(map[string]any{}), &NodeShape{Label: "Holder"})),
	} {
		if err == nil || errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error = %v; want an error without ErrUnrepresentable", name, err)
		}
	}
}

// second returns the error of a call's two results.
func second[T any](_ T, err error) error { return err }
