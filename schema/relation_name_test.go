package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A relation name is UPPER_SNAKE and its field name is that name in lower
// case, so the two spellings differ only by case — including a name that
// carries digits, which is where a word-splitting derivation would diverge.
func TestRelation_FieldNameIsTheLowerCaseName(t *testing.T) {
	t.Parallel()

	const src = `schema "s"

part type Line {
    id String primary
}

type Order {
    id String primary
    *-> LINE2 (many) Line
    --> HAS_2_PARTS (one:many) Order
}
`
	s, res := schema.LoadString(t.Context(), src, "s.yammm")
	if res.Err() != nil {
		t.Fatalf("load: %v", res.Err())
	}
	order, _ := s.Type("Order")
	comp, ok := order.Relation("LINE2")
	if !ok || comp.FieldName() != strings.ToLower(comp.Name()) || comp.FieldName() != "line2" {
		t.Errorf("composition LINE2 has field name %q, want %q", comp.FieldName(), "line2")
	}
	assoc, ok := order.Relation("HAS_2_PARTS")
	if !ok || assoc.FieldName() != "has_2_parts" {
		t.Errorf("association HAS_2_PARTS has field name %q, want %q", assoc.FieldName(), "has_2_parts")
	}
}

// TestBuilder_RelationNameIsUpperSnake pins that the Builder front door applies
// the same production the parser does.
func TestBuilder_RelationNameIsUpperSnake(t *testing.T) {
	t.Parallel()

	camel := "works" + "At"
	b := schema.NewBuilder().WithName("s").
		AddType("Company").WithPrimaryKey("id", schema.NewStringConstraint()).Done().
		AddType("Person").WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation(camel, schema.NewTypeRef("", "Company", location.Span{}), false, false).Done()
	_, res := b.Build()
	if !res.HasCode(diag.E_INVALID_NAME) {
		t.Fatalf("a camelCase relation name must draw E_INVALID_NAME; got %v", res.Err())
	}
}
