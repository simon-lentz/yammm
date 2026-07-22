package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// mustEdgeProperty digs an association's edge property out of a loaded schema.
func mustEdgeProperty(t *testing.T, s *schema.Schema, typeName, relName, propName string) *schema.Property {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("type %q missing", typeName)
	}
	rel, ok := typ.Relation(relName)
	if !ok {
		t.Fatalf("relation %q missing on %q", relName, typeName)
	}
	prop, ok := rel.Property(propName)
	if !ok {
		t.Fatalf("edge property %q missing on %q", propName, relName)
	}
	return prop
}

// A DataType-typed edge property must leave completion with a resolved
// AliasConstraint, exactly like a DataType-typed type property.
func TestLoad_EdgePropertyDataTypeAliasResolved(t *testing.T) {
	s, result := schema.LoadString(t.Context(), `schema "geo"

type StateCode = String [2, 2]

type State {
	code StateCode primary
}

type County {
	fips String primary
	--> IN_STATE (one) State {
		code_tag StateCode
	}
}
`, "test.yammm")
	if result.HasErrors() {
		t.Fatalf("load: %v", result.Err())
	}

	prop := mustEdgeProperty(t, s, "County", "IN_STATE", "code_tag")
	alias, isAlias := prop.Constraint().(schema.AliasConstraint)
	if !isAlias {
		t.Fatalf("edge property constraint is %T, want AliasConstraint", prop.Constraint())
	}
	if !alias.IsResolved() {
		t.Error("edge-property alias constraint must be resolved by completion")
	}
}

// A qualified (imported) datatype reference on an edge property resolves
// against the declaring schema's import bindings.
func TestLoadSources_EdgePropertyImportedDataTypeResolved(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "geo"

import "common.yammm" as common

type State {
	code String primary
	--> TAGGED (one) State {
		tag common.Code
	}
}
`),
		"common.yammm": []byte(`schema "common"

type Code = String [2, 2]

type Anchor {
	id String primary
}
`),
	}
	s, result := schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", ".", schema.WithSourcesOnly())
	if result.HasErrors() {
		t.Fatalf("load: %v", result.Err())
	}

	prop := mustEdgeProperty(t, s, "State", "TAGGED", "tag")
	alias, isAlias := prop.Constraint().(schema.AliasConstraint)
	if !isAlias {
		t.Fatalf("edge property constraint is %T, want AliasConstraint", prop.Constraint())
	}
	if !alias.IsResolved() {
		t.Error("imported-datatype edge-property alias must be resolved by completion")
	}
}

// An edge property naming an undeclared datatype is rejected at load with
// E_UNKNOWN_TYPE, matching the type-property contract.
func TestLoad_EdgePropertyUnknownDataTypeRejected(t *testing.T) {
	_, result := schema.LoadString(t.Context(), `schema "geo"

type State {
	code String primary
}

type County {
	fips String primary
	--> IN_STATE (one) State {
		tag Mystery
	}
}
`, "test.yammm")
	if !result.HasErrors() {
		t.Fatal("an undeclared edge-property datatype must fail the load")
	}
	if !result.HasCode(diag.E_UNKNOWN_TYPE) {
		t.Errorf("want E_UNKNOWN_TYPE, got: %v", result.Err())
	}
}

// A datatype alias that resolves to Vector cannot appear on an edge property:
// the Vector-on-edge ban applies through aliases.
func TestLoad_EdgePropertyVectorAliasRejected(t *testing.T) {
	_, result := schema.LoadString(t.Context(), `schema "geo"

type Emb = Vector [3]

type State {
	code String primary
}

type County {
	fips String primary
	--> IN_STATE (one) State {
		e Emb
	}
}
`, "test.yammm")
	if !result.HasErrors() {
		t.Fatal("a Vector-aliased edge property must fail the load")
	}
	if !result.HasCode(diag.E_INVALID_CONSTRAINT) {
		t.Errorf("want E_INVALID_CONSTRAINT, got: %v", result.Err())
	}
}

// A datatype alias that resolves to List cannot appear on an edge property:
// the List-on-edge ban applies through aliases.
func TestLoad_EdgePropertyListAliasRejected(t *testing.T) {
	_, result := schema.LoadString(t.Context(), `schema "geo"

type Lst = List <String>

type State {
	code String primary
}

type County {
	fips String primary
	--> IN_STATE (one) State {
		l Lst
	}
}
`, "test.yammm")
	if !result.HasErrors() {
		t.Fatal("a List-aliased edge property must fail the load")
	}
	if !result.HasCode(diag.E_LIST_ON_EDGE) {
		t.Errorf("want E_LIST_ON_EDGE, got: %v", result.Err())
	}
}
