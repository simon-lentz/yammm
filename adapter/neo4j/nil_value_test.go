package neo4j

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// A value that holds none reaches the driver as nil, which it writes as null, as
// every other writer of this module writes it. The driver packs a nil slice as
// an empty list, so a nil slice or map must not reach it as itself.
func TestWrite_AValueThatHoldsNoneReachesTheDriverAsNil(t *testing.T) {
	t.Parallel()
	typ := nilValueType(t)
	nones := map[string]any{
		"tags":   []any(nil),
		"vec":    []float64(nil),
		"stamps": []any(nil),
		"name":   []string(nil),
		"extra":  map[string]any(nil),
	}
	raw := map[string]any{"id": "x"}
	maps.Copy(raw, nones)
	params, err := propsToParamMap(immutable.WrapProperties(raw, immutable.WithClone(true)), typ)
	if err != nil {
		t.Fatalf("propsToParamMap: %v", err)
	}
	for k := range nones {
		if v, ok := params[k]; !ok || v != nil {
			t.Errorf("param %q = %#v, %v; want nil", k, v, ok)
		}
	}

	for name, tc := range map[string]struct {
		c   schema.Constraint
		raw any
	}{
		"a nil slice at a Float":          {schema.NewFloatConstraint(), []float64(nil)},
		"a nil slice at a String":         {schema.NewStringConstraint(), []string(nil)},
		"a nil map at a String":           {schema.NewStringConstraint(), map[string]any(nil)},
		"a nil slice at a List<String>":   {schema.NewListConstraint(schema.NewStringConstraint()), []any(nil)},
		"a nil slice under no constraint": {nil, []any(nil)},
	} {
		if got, err := coerceScalar(tc.c, tc.raw); err != nil || got != nil {
			t.Errorf("coerceScalar(%s) = %#v, %v; want nil", name, got, err)
		}
		out, err := CoerceParams(map[string]any{"p": tc.raw}, ParamTypes{"p": tc.c})
		if err != nil || out["p"] != nil {
			t.Errorf("CoerceParams(%s) = %#v, %v; want nil", name, out["p"], err)
		}
	}

	rel := slices.Collect(typ.Associations())[0]
	for name, v := range map[string]any{"a nil slice": []any(nil), "a nil map": map[string]any(nil)} {
		out, err := coerceRelProps(map[string]any{"note": v}, rel)
		if err != nil || out["note"] != nil {
			t.Errorf("coerceRelProps(%s at a String) = %#v, %v; want nil", name, out["note"], err)
		}
	}
}

// A nil slice or map holds no value, so it is nil at a declared position or
// not, and CoerceParams returns nil at a declared key and at an undeclared one.
func TestHeldForm_ANilSliceOrMapHasTheFormNil(t *testing.T) {
	t.Parallel()
	list := schema.NewListConstraint(schema.NewStringConstraint())
	for name, v := range map[string]any{"a nil []any": []any(nil), "a nil map[string]any": map[string]any(nil)} {
		for _, c := range []schema.Constraint{nil, list} {
			if got, err := heldForm(v, c); err != nil || got != nil {
				t.Errorf("heldForm(%s, %v) = %#v, %v; want nil", name, c, got, err)
			}
		}
	}
	out, err := CoerceParams(
		map[string]any{"tags": []any(nil), "other": []any(nil), "rows": []map[string]any(nil), "props": map[string]any(nil)},
		ParamTypes{"tags": list},
	)
	if err != nil {
		t.Fatalf("CoerceParams: %v", err)
	}
	for _, key := range []string{"tags", "other", "rows", "props"} {
		if v, ok := out[key]; !ok || v != nil {
			t.Errorf("%s = %#v, %v; want nil", key, v, ok)
		}
	}
}

// A value that holds none at a List whose element is a collection is nil too,
// on the write path and through CoerceParams: the nested-collection refusal is
// of a value the property holds.
func TestWrite_AValueThatHoldsNoneAtANestedCollectionIsNil(t *testing.T) {
	t.Parallel()
	_, s, _, _ := setupWrite(t, "nested_collection.yammm")
	typ, ok := s.Type("Grid")
	if !ok {
		t.Fatal("no type Grid")
	}
	for name, none := range map[string]any{
		"nil":         nil,
		"a nil slice": []any(nil),
		"a nil map":   map[string]any(nil),
	} {
		raw := map[string]any{"id": "g1", "matrix": none}
		params, err := propsToParamMap(immutable.WrapProperties(raw, immutable.WithClone(true)), typ)
		if err != nil {
			t.Errorf("%s: propsToParamMap: %v", name, err)
			continue
		}
		if v, ok := params["matrix"]; !ok || v != nil {
			t.Errorf("%s: param matrix = %#v, %v; want nil", name, v, ok)
		}
		coerced, err := CoerceParams(map[string]any{"matrix": none}, ParamTypesForType(typ))
		if err != nil {
			t.Errorf("%s: CoerceParams: %v", name, err)
			continue
		}
		if v, ok := coerced["matrix"]; !ok || v != nil {
			t.Errorf("%s: CoerceParams gives matrix = %#v, %v; want nil", name, v, ok)
		}
	}
}

// nilValueType loads a type with a property of each shape a nil slice or map
// can stand at, and an association that declares an edge property.
func nilValueType(t *testing.T) schema.Type {
	t.Helper()
	s, res := schema.LoadString(t.Context(), `schema "p"

type C {
	id String primary
}

type T {
	id String primary
	tags List<String>
	vec Vector[2]
	stamps List<Timestamp>
	name String
	--> TO (_) C {
		note String
	}
}
`, "p.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	typ, ok := s.Type("T")
	if !ok {
		t.Fatal("no type T")
	}
	return typ
}

// coerceRelProps returns a value that holds none as nil where the relation
// declares no property, and under no relation.
func TestCoerceRelProps_AnUndeclaredValueThatHoldsNoneIsNil(t *testing.T) {
	t.Parallel()
	for name, rel := range map[string]schema.Relation{"no relation": {}, "a relation": slices.Collect(nilValueType(t).Associations())[0]} {
		got, err := coerceRelProps(map[string]any{"undeclared": []any(nil), "kept": "x"}, rel)
		if err != nil || got["undeclared"] != nil || got["kept"] != "x" {
			t.Errorf("coerceRelProps under %s = %#v, %v; want undeclared nil and kept unchanged", name, got, err)
		}
	}
}

// A key that is a nil slice or a nil map holds none: it is a nil key, as a nil
// one is. A pointer at a key is a value of no form, a nil one too, and the
// key's constraint refuses it.
func TestExtractKeyProps_AKeyThatHoldsNoneIsANilKey(t *testing.T) {
	t.Parallel()
	shape := &nodeShape{label: "T", primaryKeys: []string{"id"}, keyConstraints: map[string]schema.Constraint{"id": schema.NewStringConstraint()}}
	for name, none := range map[string]any{"nil": nil, "a nil slice": []any(nil), "a nil map": map[string]any(nil)} {
		_, err := extractKeyProps(immutable.WrapProperties(map[string]any{"id": none}), shape)
		if err == nil || !strings.Contains(err.Error(), "nil primary key(s): [id]") {
			t.Errorf("%s: extractKeyProps error = %v; want a nil key", name, err)
		}
	}
	var nilString *string
	text := "x"
	for name, p := range map[string]any{"a nil pointer": nilString, "a pointer to a nil pointer": &nilString, "a pointer to a string": &text} {
		got, err := extractKeyProps(immutable.WrapProperties(map[string]any{"id": p}), shape)
		if !errors.Is(err, ErrUnrepresentable) || !strings.Contains(err.Error(), `primary key "id": coerce String: cannot use `) {
			t.Errorf("%s: extractKeyProps = %#v, %v; want the key's constraint to refuse it", name, got, err)
		}
	}
}

// A []any is a list whatever it holds: under a List its nil element is refused
// and an empty one is an empty typed slice, and under no key of types it is
// kept as given.
func TestCoerceParams_ReadsAListAsAList(t *testing.T) {
	t.Parallel()
	list := ParamTypes{"xs": schema.NewListConstraint(schema.NewStringConstraint())}
	if out, err := CoerceParams(map[string]any{"xs": []any{nil}}, list); err == nil || err.Error() != `param "xs": element [0]: expected string, got null` {
		t.Errorf("a []any of nils under a List<String> = %#v, %v; want the nil element refused", out["xs"], err)
	}
	out, err := CoerceParams(map[string]any{"xs": []any{}}, list)
	if xs, ok := out["xs"].([]string); err != nil || !ok || len(xs) != 0 {
		t.Errorf("an empty []any under a List<String> = %#v, %v; want an empty []string", out["xs"], err)
	}
	out, err = CoerceParams(map[string]any{"xs": []any{[]string(nil)}}, nil)
	if elems, ok := out["xs"].([]any); err != nil || !ok || len(elems) != 1 || elems[0] == nil {
		t.Errorf("an untyped []any holding a nil list = %#v, %v; want the list kept as given", out["xs"], err)
	}
	mixed, err := CoerceParams(map[string]any{"xs": []any{map[string]any{"p": 1}, "x"}}, nil)
	if elems, ok := mixed["xs"].([]any); err != nil || !ok || len(elems) != 2 || elems[1] != "x" {
		t.Errorf("an untyped []any of a map and a string = %#v, %v; want the list kept as given", mixed["xs"], err)
	}
}
