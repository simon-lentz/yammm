package neo4j

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/schema"
)

// Neo4j has no nested collection property type, so the write path refuses a
// value of a List<List<T>>, List<Vector[N]> or list-of-a-list-alias property,
// as ConstraintsForSchema refuses the property itself. An empty list is refused
// too: the property's type is what Neo4j cannot hold.
func TestBatchNodeQueries_RefusesANestedCollectionValue(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "nested_collection.yammm")
	for name, tc := range map[string]struct {
		property string
		value    any
	}{
		"a list of lists":              {"matrix", []any{[]any{int64(1), int64(2)}}},
		"an empty list of lists":       {"matrix", []any{}},
		"a list of vectors":            {"embeddings", []any{[]any{1.0, 2.0, 3.0}}},
		"a list of a list-typed alias": {"codes", []any{[]any{"a"}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			snap := buildGraphResult(t, s, v, map[string][]map[string]any{
				"Grid": {{"id": "g1", tc.property: tc.value}},
			})
			_, err := a.BatchNodeQueries(context.Background(), snap, shape)
			if !errors.Is(err, ErrUnsupportedListElem) {
				t.Fatalf("BatchNodeQueries error = %v; want ErrUnsupportedListElem", err)
			}
			if !strings.Contains(err.Error(), `"`+tc.property+`"`) {
				t.Errorf("error %q does not name the property %q", err, tc.property)
			}
		})
	}
}

// A composed part's nested collection property is refused as a root's is.
func TestBatchNodeQueries_RefusesAComposedPartsNestedCollectionValue(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "nested_collection.yammm")
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Grid": {{"id": "g1", "cells": []any{map[string]any{"label": "c1", "matrix": []any{[]any{int64(1)}}}}}},
	})
	_, err := a.BatchNodeQueries(context.Background(), snap, shape)
	if !errors.Is(err, ErrUnsupportedListElem) {
		t.Fatalf("BatchNodeQueries error = %v; want ErrUnsupportedListElem", err)
	}
	if !strings.Contains(err.Error(), `"matrix"`) {
		t.Errorf("error %q does not name the property", err)
	}
}

// A nested collection property is refused by its type before its value is read,
// as Coerce refuses it, so an element of a bypass-built value that the one form
// refuses draws the same refusal.
func TestBatchNodeQueries_RefusesANestedCollectionBeforeItsElements(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]any{
		"a string that is not valid UTF-8": []any{[]any{"ok"}, []any{"a\xffb"}},
		"a value of no form":               []any{[]any{[2]string{"a", "b"}}},
		"a time":                           []any{[]any{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{props: map[string]any{"nested": value}})
			_, err := a.BatchNodeQueries(t.Context(), snap, shape)
			if !errors.Is(err, ErrUnsupportedListElem) || !errors.Is(err, ErrUnrepresentable) {
				t.Fatalf("BatchNodeQueries error = %v; want ErrUnsupportedListElem and ErrUnrepresentable", err)
			}
			if !strings.Contains(err.Error(), `property "nested"`) {
				t.Errorf("error %q does not name the property", err)
			}
		})
	}
}

// A nested collection property that holds no value writes as null, as every
// absent property does.
func TestBatchNodeQueries_WritesAnAbsentNestedCollection(t *testing.T) {
	t.Parallel()
	a, s, v, shape := setupWrite(t, "nested_collection.yammm")
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Grid": {{"id": "g1", "cells": []any{map[string]any{"label": "c1"}}}},
	})
	if _, err := a.BatchNodeQueries(context.Background(), snap, shape); err != nil {
		t.Fatalf("BatchNodeQueries: %v", err)
	}
	for name, value := range map[string]any{
		"a nil []any":      []any(nil),
		"a nil [][]string": [][]string(nil),
	} {
		a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{props: map[string]any{"nested": value}})
		nodes, err := a.BatchNodeQueries(t.Context(), snap, shape)
		if err != nil {
			t.Fatalf("BatchNodeQueries, %s: %v", name, err)
		}
		for _, q := range nodes {
			for _, row := range q.Params["rows"].([]map[string]any) {
				props, _ := row["props"].(map[string]any)
				if v, ok := props["nested"]; ok && v != nil {
					t.Errorf("BatchNodeQueries, %s: sent %#v; want nil", name, v)
				}
			}
		}
	}
}

// Coerce and CoerceParams refuse a present value at a nested collection
// constraint whatever Go type carries it, a typed slice included, and a nil
// inner list too, which the driver would pack as an empty one. The refusal is the
// one neo4jListElementType gives, whatever Go type carries the value, and Coerce
// returns the value with it, as its other refusals do.
func TestCoerce_RefusesANestedCollectionValue(t *testing.T) {
	t.Parallel()
	codes := schema.NewAliasConstraint("Codes", schema.NewListConstraint(schema.NewStringConstraint()))
	for name, tc := range map[string]struct {
		c      schema.Constraint
		kind   string
		values map[string]any
	}{
		"List<List<Integer>>": {
			schema.NewListConstraint(schema.NewListConstraint(schema.NewIntegerConstraint())), "List",
			map[string]any{
				"a []any of []any":   []any{[]any{int64(1)}},
				"a [][]int64":        [][]int64{{1}},
				"a nil inner list":   []any{[]any(nil)},
				"an empty []any":     []any{},
				"an empty [][]int64": [][]int64{},
			},
		},
		"List<Vector[3]>": {
			schema.NewListConstraint(schema.NewVectorConstraint(3)), "Vector",
			map[string]any{
				"a []any of []any": []any{[]any{1.0, 2.0, 3.0}},
				"a [][]float64":    [][]float64{{1, 2, 3}},
			},
		},
		"List<Codes>": {
			schema.NewListConstraint(codes), "List",
			map[string]any{
				"a []any of []any": []any{[]any{"a"}},
				"a [][]string":     [][]string{{"a"}},
			},
		},
	} {
		for vname, raw := range tc.values {
			at := name + ", " + vname
			refusal := "neo4j adapter: unsupported list element type: list element kind " + tc.kind
			got, err := Coerce(tc.c, raw)
			if !errors.Is(err, ErrUnsupportedListElem) {
				t.Errorf("Coerce(%s) error = %v; want ErrUnsupportedListElem", at, err)
			} else if err.Error() != refusal {
				t.Errorf("Coerce(%s) error = %q; want %q", at, err, refusal)
			}
			if !reflect.DeepEqual(got, raw) {
				t.Errorf("Coerce(%s) returned %#v with its error; want the value unchanged", at, got)
			}
			for key, params := range map[string]map[string]any{
				"m":      {"m": raw},
				"p.m":    {"p": map[string]any{"m": raw}},
				"rows.m": {"rows": []map[string]any{{"m": raw}}},
			} {
				_, err := CoerceParams(params, ParamTypes{key: tc.c})
				want := `param "` + key + `": ` + refusal
				if !errors.Is(err, ErrUnsupportedListElem) {
					t.Errorf("CoerceParams(%s at %s) error = %v; want ErrUnsupportedListElem", at, key, err)
				} else if err.Error() != want {
					t.Errorf("CoerceParams(%s at %s) error = %q; want %q", at, key, err, want)
				}
			}
		}
		if got, err := Coerce(tc.c, nil); err != nil || got != nil {
			t.Errorf("Coerce(%s, nil) = %#v, %v; want nil, nil", name, got, err)
		}
	}
}
