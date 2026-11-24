package schema_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
)

// mustEdgeProperty digs an association's edge property out of a loaded schema.
func mustEdgeProperty(t *testing.T, s schema.Schema, typeName, relName, propName string) schema.Property {
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
	s, err := schema.LoadString(t.Context(), `schema "geo"

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
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	prop := mustEdgeProperty(t, s, "County", "IN_STATE", "code_tag")
	alias, isAlias := prop.Constraint().(schema.AliasConstraint)
	if !isAlias {
		t.Fatalf("edge property constraint is %T, want AliasConstraint", prop.Constraint())
	}
	if !schema.TestConstraintIsResolved(alias) {
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
	s, err := schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("."))
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	prop := mustEdgeProperty(t, s, "State", "TAGGED", "tag")
	alias, isAlias := prop.Constraint().(schema.AliasConstraint)
	if !isAlias {
		t.Fatalf("edge property constraint is %T, want AliasConstraint", prop.Constraint())
	}
	if !schema.TestConstraintIsResolved(alias) {
		t.Error("imported-datatype edge-property alias must be resolved by completion")
	}
}

// An edge property naming an undeclared datatype is rejected at load with
// E_UNKNOWN_TYPE, matching the type-property contract.
func TestLoad_EdgePropertyUnknownDataTypeRejected(t *testing.T) {
	_, err := schema.LoadString(t.Context(), `schema "geo"

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
	if err == nil {
		t.Fatal("an undeclared edge-property datatype must fail the load")
	}
	if !errors.Is(err, diag.E_UNKNOWN_TYPE) {
		t.Errorf("want E_UNKNOWN_TYPE, got: %v", err)
	}
}

// A datatype alias that resolves to Vector cannot appear on an edge property:
// the Vector-on-edge ban applies through aliases.
func TestLoad_EdgePropertyVectorAliasRejected(t *testing.T) {
	_, result := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.LoadString(t.Context(), `schema "geo"

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
`, "test.yammm", schema.WithWarnings(warn))
	})
	if diagtest.Err(result) == nil {
		t.Fatal("a Vector-aliased edge property must fail the load")
	}
	issues := slices.Collect(result.Issues())
	if len(issues) != 1 || issues[0].Code() != diag.E_INVALID_EDGE_PROPERTY_TYPE {
		t.Fatalf("want one E_INVALID_EDGE_PROPERTY_TYPE, got: %v", result)
	}
	if want := `relationship property "e" cannot use Vector type`; issues[0].Message() != want {
		t.Errorf("message %q, want %q", issues[0].Message(), want)
	}
	if line := issues[0].Span().Start.Line; line != 12 {
		t.Errorf("the issue stands on line %d, want the property's line 12", line)
	}
}

// A datatype alias that resolves to List cannot appear on an edge property:
// the List-on-edge ban applies through aliases.
func TestLoad_EdgePropertyListAliasRejected(t *testing.T) {
	_, err := schema.LoadString(t.Context(), `schema "geo"

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
	if err == nil {
		t.Fatal("a List-aliased edge property must fail the load")
	}
	if !errors.Is(err, diag.E_INVALID_EDGE_PROPERTY_TYPE) {
		t.Errorf("want E_INVALID_EDGE_PROPERTY_TYPE, got: %v", err)
	}
}
