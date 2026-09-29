package jschema

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// Completion resolves a datatype chain the same way from either front door and
// in either declaration order. The check reads past resolution, through the
// readers of the resolved constraint: the validator's verdict on each value and
// the structural hash agree across the DSL in both orders and the Builder, and
// the generated document agrees between the two front doors in one order ($defs
// follows declaration order).
func TestMarshal_DataTypeChainAgreesAcrossOrderAndFrontDoor(t *testing.T) {
	const body = "type T {\n\tid String primary\n\tv A required\n\tw List<B> required\n}\n"
	load := func(t *testing.T, datatypes string) *schema.Schema {
		t.Helper()
		s, res := schema.LoadString(t.Context(), "schema \"p\"\n\n"+datatypes+body, "p.yammm")
		if res.HasErrors() {
			t.Fatalf("load: %v", res.Err())
		}
		return s
	}
	built, res := schema.NewBuilder().WithName("p").
		AddDataType("A", schema.NewListConstraint(schema.NewAliasConstraint("B", nil))).
		AddDataType("B", schema.NewListConstraint(schema.NewAliasConstraint("C", nil))).
		AddDataType("C", schema.IntegerBetween(0, 9)).
		AddType("T").WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("v", schema.NewAliasConstraint("A", nil)).
		WithProperty("w", schema.NewListConstraint(schema.NewAliasConstraint("B", nil))).
		Done().Build()
	if res.HasErrors() {
		t.Fatalf("build: %v", res.Err())
	}
	forward := load(t, "type A = List<B>\ntype B = List<C>\ntype C = Integer[0, 9]\n")
	schemas := map[string]*schema.Schema{
		"the DSL, referenced before declared": forward,
		"the DSL, declared before referenced": load(t, "type C = Integer[0, 9]\ntype B = List<C>\ntype A = List<B>\n"),
		"the Builder":                         built,
	}

	values := []struct {
		name  string
		v     any
		valid bool
	}{
		{"a nested list in bounds", []any{[]any{int64(1), int64(9)}}, true},
		{"an element past the bound", []any{[]any{int64(10)}}, false},
		{"an element of the wrong kind", []any{[]any{"x"}}, false},
		{"an element one level short", []any{int64(1)}, false},
	}
	hashes := map[string]string{}
	for name, s := range schemas {
		tt, _ := s.Type("T")
		for _, field := range []string{"v", "w"} {
			p, _ := tt.Property(field)
			for _, val := range values {
				err := instance.CheckValue(val.v, p.Constraint())
				if (err == nil) != val.valid {
					t.Errorf("%s: %s = %s: CheckValue = %v, want valid %v", name, field, val.name, err, val.valid)
				}
			}
		}
		hashes[name] = schema.StructuralHash(s)
	}
	if got := slices.Compact(slices.Sorted(maps.Values(hashes))); len(got) != 1 {
		t.Errorf("the structural hashes differ: %v", hashes)
	}
	fromDSL, err := Marshal(forward)
	if err != nil {
		t.Fatalf("Marshal the DSL's: %v", err)
	}
	fromBuilder, err := Marshal(built)
	if err != nil {
		t.Fatalf("Marshal the Builder's: %v", err)
	}
	if !bytes.Equal(fromDSL, fromBuilder) {
		t.Errorf("the generated documents differ:\n%s\n----\n%s", fromDSL, fromBuilder)
	}
}
