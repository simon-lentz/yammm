package neo4j

import (
	"maps"
	"reflect"
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
	var nilString *string
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
		"a nil pointer at a Float":        {schema.NewFloatConstraint(), nilString},
		"a nil slice at a String":         {schema.NewStringConstraint(), []string(nil)},
		"a nil map at a String":           {schema.NewStringConstraint(), map[string]any(nil)},
		"a nil slice at a List<String>":   {schema.NewListConstraint(schema.NewStringConstraint()), []any(nil)},
		"a nil slice under no constraint": {nil, []any(nil)},
	} {
		if got, err := Coerce(tc.c, tc.raw); err != nil || got != nil {
			t.Errorf("Coerce(%s) = %#v, %v; want nil", name, got, err)
		}
		out, err := CoerceParams(map[string]any{"p": tc.raw}, ParamTypes{"p": tc.c})
		if err != nil || out["p"] != nil {
			t.Errorf("CoerceParams(%s) = %#v, %v; want nil", name, out["p"], err)
		}
	}

	rel := typ.AssociationsSlice()[0]
	for name, v := range map[string]any{"a nil slice": []any(nil), "a nil map": map[string]any(nil)} {
		out, err := CoerceRelProps(map[string]any{"note": v}, rel)
		if err != nil || out["note"] != nil {
			t.Errorf("CoerceRelProps(%s at a String) = %#v, %v; want nil", name, out["note"], err)
		}
	}
}

// The one form of a nil slice or map is nil, as instance.ScalarOf reads it, at
// a declared position or not, and so is a nested param map's field.
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
		map[string]any{"row": map[string]any{"tags": []any(nil), "other": []any(nil)}},
		ParamTypes{"row.tags": list},
	)
	if err != nil {
		t.Fatalf("CoerceParams: %v", err)
	}
	row, ok := out["row"].(map[string]any)
	if !ok {
		t.Fatalf("row = %#v, want a map", out["row"])
	}
	for _, field := range []string{"tags", "other"} {
		if v, ok := row[field]; !ok || v != nil {
			t.Errorf("row.%s = %#v, %v; want nil", field, v, ok)
		}
	}
}

// nilValueType loads a type with a property of each shape a nil slice or map
// can stand at, and an association that declares an edge property.
func nilValueType(t *testing.T) *schema.Type {
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

// A row that holds none reaches the driver as null, so CoerceParams returns the
// rows as a list that can hold one. CoerceRelProps returns a value that holds
// none as nil where the relation declares no property, and under no relation.
func TestCoerce_ARowOrAnUndeclaredValueThatHoldsNoneIsNil(t *testing.T) {
	t.Parallel()
	out, err := CoerceParams(map[string]any{"rows": []map[string]any{nil, {"id": "a"}}}, nil)
	if err != nil {
		t.Fatalf("CoerceParams: %v", err)
	}
	rows, ok := out["rows"].([]any)
	if !ok || len(rows) != 2 || rows[0] != nil {
		t.Fatalf("rows = %#v; want a nil row, then the row", out["rows"])
	}
	if row, ok := rows[1].(map[string]any); !ok || row["id"] != "a" {
		t.Errorf("rows[1] = %#v; want the row", rows[1])
	}
	for name, rel := range map[string]*schema.Relation{"no relation": nil, "a relation": nilValueType(t).AssociationsSlice()[0]} {
		got, err := CoerceRelProps(map[string]any{"undeclared": []any(nil), "kept": "x"}, rel)
		if err != nil || got["undeclared"] != nil || got["kept"] != "x" {
			t.Errorf("CoerceRelProps under %s = %#v, %v; want undeclared nil and kept unchanged", name, got, err)
		}
	}
}

// A key that holds none behind a carrier is a nil key, as a nil one is.
func TestExtractKeyProps_AKeyThatHoldsNoneIsANilKey(t *testing.T) {
	t.Parallel()
	shape := &NodeShape{Label: "T", PrimaryKeys: []string{"id"}}
	var nilString *string
	for name, none := range map[string]any{"a pointer to a nil pointer": &nilString, "a nil slice": []any(nil)} {
		_, err := extractKeyProps(immutable.WrapProperties(map[string]any{"id": none}), shape)
		if err == nil || !strings.Contains(err.Error(), "nil primary key(s): [id]") {
			t.Errorf("%s: extractKeyProps error = %v; want a nil key", name, err)
		}
	}
}

// The rows CoerceParams returns are rows to it too: a second pass coerces their
// declared fields as the first did, and so does a []any of rows built by hand.
func TestCoerceParams_ReadsTheRowsItReturnsAsRows(t *testing.T) {
	t.Parallel()
	types := ParamTypes{"rows.p": schema.NewFloatConstraint()}
	once, err := CoerceParams(map[string]any{"rows": []map[string]any{{"p": 1}}}, nil)
	if err != nil {
		t.Fatalf("CoerceParams: %v", err)
	}
	for name, params := range map[string]map[string]any{
		"the rows it returned": once,
		"a []any of rows":      {"rows": []any{map[string]any{"p": 1}, map[string]any(nil), nil}},
	} {
		out, err := CoerceParams(params, types)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rows, ok := out["rows"].([]any)
		if !ok || len(rows) == 0 {
			t.Fatalf("%s: rows = %#v; want a []any", name, out["rows"])
		}
		if row, ok := rows[0].(map[string]any); !ok || row["p"] != float64(1) {
			t.Errorf("%s: rows[0] = %#v; want p coerced to float64(1)", name, rows[0])
		}
		for i, row := range rows[1:] {
			if row != nil {
				t.Errorf("%s: rows[%d] = %#v; want nil", name, i+1, row)
			}
		}
	}
	list := ParamTypes{"xs": schema.NewListConstraint(schema.NewStringConstraint())}
	if out, err := CoerceParams(map[string]any{"xs": []any{nil}}, list); err == nil || !strings.Contains(err.Error(), `param "xs": list element 0:`) {
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

// A nested map's fields are typed at "<key>.<field>", so a constraint at the
// map's own key would judge nothing and is refused, as at a rows param's key. An
// empty map is refused too; a nil map holds no value and is nil.
func TestCoerceParams_ANestedMapTakesNoConstraintAtItsOwnKey(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]schema.Constraint{
		"String":     schema.NewStringConstraint(),
		"List<List>": schema.NewListConstraint(schema.NewListConstraint(schema.NewIntegerConstraint())),
	} {
		types := ParamTypes{"p": c, "p.x": schema.NewFloatConstraint()}
		for shape, m := range map[string]map[string]any{"a map": {"x": 1}, "an empty map": {}} {
			out, err := CoerceParams(map[string]any{"p": m}, types)
			if err == nil || err.Error() != `param "p": a nested map takes no constraint at its own key; declare each field as "<key>.<field>"` {
				t.Errorf("%s under %s = %#v, %v; want the constraint at the map's key refused", shape, name, out, err)
			}
		}
		out, err := CoerceParams(map[string]any{"p": map[string]any(nil)}, types)
		if err != nil || out["p"] != nil {
			t.Errorf("a nil map under %s = %#v, %v; want nil", name, out["p"], err)
		}
	}
	for name, types := range map[string]ParamTypes{"absent": {"p.x": schema.NewFloatConstraint()}, "nil": {"p": nil, "p.x": schema.NewFloatConstraint()}} {
		out, err := CoerceParams(map[string]any{"p": map[string]any{"x": 1}}, types)
		if m, ok := out["p"].(map[string]any); err != nil || !ok || m["x"] != 1.0 {
			t.Errorf("a nested map, constraint %s = %#v, %v; want its field coerced", name, out["p"], err)
		}
	}
}

// A row's fields are typed at "rows.<field>", so a constraint at the rows key
// itself is refused, as a second pass would refuse rows that hold no map.
func TestCoerceParams_ARowsParamTakesNoConstraintAtItsOwnKey(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]schema.Constraint{
		"String":       schema.NewStringConstraint(),
		"List<String>": schema.NewListConstraint(schema.NewStringConstraint()),
		"List<List>":   schema.NewListConstraint(schema.NewListConstraint(schema.NewStringConstraint())),
	} {
		types := ParamTypes{"rows": c, "rows.p": schema.NewFloatConstraint()}
		for shape, rows := range map[string]any{
			"rows":            []map[string]any{{"p": 1}},
			"empty rows":      []map[string]any{},
			"all-nil rows":    []map[string]any{nil},
			"a []any of rows": []any{map[string]any{"p": 1}, nil},
		} {
			out, err := CoerceParams(map[string]any{"rows": rows}, types)
			if err == nil || !strings.Contains(err.Error(), `param "rows": rows take no constraint at their own key`) {
				t.Errorf("%s under %s = %#v, %v; want the constraint at the rows key refused", shape, name, out, err)
			}
		}
	}
	for name, types := range map[string]ParamTypes{"absent": {}, "nil": {"rows": nil}} {
		for shape, rows := range map[string][]map[string]any{"empty": {}, "all nil": {nil, nil}, "one map": {{"p": 1}, nil}} {
			first, err := CoerceParams(map[string]any{"rows": rows}, types)
			if err != nil {
				t.Fatalf("%s rows, constraint %s: %v", shape, name, err)
			}
			second, err := CoerceParams(first, types)
			if err != nil || !reflect.DeepEqual(second, first) {
				t.Errorf("%s rows, constraint %s: second pass = %#v, %v; want the first pass's %#v", shape, name, second, err, first)
			}
		}
	}
}
