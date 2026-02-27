package complete_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
	"github.com/simon-lentz/yammm/schema/internal/complete"
	"github.com/simon-lentz/yammm/schema/internal/parse"
)

// TestValidateInvariant_ValidProperty verifies that an invariant referencing
// an existing property on its type compiles without error.
func TestValidateInvariant_ValidProperty(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "name_not_empty",
						// name != ""
						Expr: expr.SExpr{
							expr.Op("!="),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "name"}},
							&expr.Literal{Val: ""},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "valid_prop.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for valid property reference")
}

// TestValidateInvariant_UnknownProperty verifies that an invariant referencing
// a nonexistent property produces E_UNKNOWN_PROPERTY.
func TestValidateInvariant_UnknownProperty(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "bad_invariant",
						// fake_property != ""
						Expr: expr.SExpr{
							expr.Op("!="),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "fake_property"}},
							&expr.Literal{Val: ""},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "unknown_prop.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.Nil(t, s, "schema should fail to compile")
	require.True(t, collector.HasErrors())

	issues := collector.Result().IssuesSlice()
	require.NotEmpty(t, issues)

	found := false
	for _, issue := range issues {
		if issue.Code() == diag.E_UNKNOWN_PROPERTY {
			found = true
			assert.Contains(t, issue.Message(), "fake_property")
			assert.Contains(t, issue.Message(), "Person")
		}
	}
	assert.True(t, found, "expected E_UNKNOWN_PROPERTY diagnostic")
}

// TestValidateInvariant_LambdaValidProperty verifies that lambda parameters
// bound to composition targets validate member access correctly.
func TestValidateInvariant_LambdaValidProperty(t *testing.T) {
	t.Parallel()

	// Order has composition *-> ITEMS (many) LineItem.
	// LineItem has property "quantity".
	// Invariant: ITEMS -> All |$item| { $item.quantity > 0 }
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:   "LineItem",
				IsPart: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "quantity",
						Constraint: schema.NewIntegerConstraint(),
					},
				},
			},
			{
				Name: "Order",
				Properties: []*parse.PropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Relations: []*parse.RelationDecl{
					{
						Name:   "ITEMS",
						Kind:   parse.RelationComposition,
						Target: &parse.TypeRef{Name: "LineItem"},
						Many:   true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "all_positive",
						// ITEMS -> All |$item| { $item.quantity > 0 }
						Expr: expr.SExpr{
							expr.Op("All"),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "ITEMS"}},
							&expr.Literal{Val: []string{"item"}},
							expr.SExpr{
								expr.Op(">"),
								expr.SExpr{
									expr.Op("."),
									expr.SExpr{expr.Op("$"), &expr.Literal{Val: "item"}},
									&expr.Literal{Val: "quantity"},
								},
								&expr.Literal{Val: int64(0)},
							},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "lambda_valid.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for valid lambda property")
}

// TestValidateInvariant_LambdaUnknownProperty verifies that lambda parameters
// accessing nonexistent properties on the target type produce E_UNKNOWN_PROPERTY.
func TestValidateInvariant_LambdaUnknownProperty(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:   "LineItem",
				IsPart: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "quantity",
						Constraint: schema.NewIntegerConstraint(),
					},
				},
			},
			{
				Name: "Order",
				Properties: []*parse.PropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Relations: []*parse.RelationDecl{
					{
						Name:   "ITEMS",
						Kind:   parse.RelationComposition,
						Target: &parse.TypeRef{Name: "LineItem"},
						Many:   true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "bad_lambda",
						// ITEMS -> All |$item| { $item.nonexistent > 0 }
						Expr: expr.SExpr{
							expr.Op("All"),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "ITEMS"}},
							&expr.Literal{Val: []string{"item"}},
							expr.SExpr{
								expr.Op(">"),
								expr.SExpr{
									expr.Op("."),
									expr.SExpr{expr.Op("$"), &expr.Literal{Val: "item"}},
									&expr.Literal{Val: "nonexistent"},
								},
								&expr.Literal{Val: int64(0)},
							},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "lambda_bad.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.Nil(t, s, "schema should fail to compile")
	require.True(t, collector.HasErrors())

	found := false
	for _, issue := range collector.Result().IssuesSlice() {
		if issue.Code() == diag.E_UNKNOWN_PROPERTY {
			found = true
			assert.Contains(t, issue.Message(), "nonexistent")
			assert.Contains(t, issue.Message(), "LineItem")
		}
	}
	assert.True(t, found, "expected E_UNKNOWN_PROPERTY for nonexistent on LineItem")
}

// TestValidateInvariant_SelfDotProperty verifies that $self.name references
// resolve correctly against the owning type's properties.
func TestValidateInvariant_SelfDotProperty(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "self_name_not_empty",
						// $self.name != ""
						Expr: expr.SExpr{
							expr.Op("!="),
							expr.SExpr{
								expr.Op("."),
								expr.SExpr{expr.Op("$"), &expr.Literal{Val: "self"}},
								&expr.Literal{Val: "name"},
							},
							&expr.Literal{Val: ""},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "self_dot_prop.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for $self.name reference")
}

// TestValidateInvariant_SelfDotUnknownProperty verifies that $self.fake_prop
// produces E_UNKNOWN_PROPERTY when the property does not exist on the type.
func TestValidateInvariant_SelfDotUnknownProperty(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "self_bad_prop",
						// $self.fake_prop != ""
						Expr: expr.SExpr{
							expr.Op("!="),
							expr.SExpr{
								expr.Op("."),
								expr.SExpr{expr.Op("$"), &expr.Literal{Val: "self"}},
								&expr.Literal{Val: "fake_prop"},
							},
							&expr.Literal{Val: ""},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "self_dot_unknown.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.Nil(t, s, "schema should fail to compile")
	require.True(t, collector.HasErrors())

	found := false
	for _, issue := range collector.Result().IssuesSlice() {
		if issue.Code() == diag.E_UNKNOWN_PROPERTY {
			found = true
			assert.Contains(t, issue.Message(), "fake_prop")
		}
	}
	assert.True(t, found, "expected E_UNKNOWN_PROPERTY for $self.fake_prop")
}

// TestValidateInvariant_InheritedProperty verifies that invariants on a child
// type can reference properties declared on an abstract parent type.
func TestValidateInvariant_InheritedProperty(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Auditable",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "created_at",
						Constraint: schema.NewTimestampConstraint(),
					},
				},
			},
			{
				Name:     "Account",
				Inherits: []*parse.TypeRef{{Name: "Auditable"}},
				Properties: []*parse.PropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "created_at_set",
						// created_at != nil
						Expr: expr.SExpr{
							expr.Op("!="),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "created_at"}},
							&expr.Literal{Val: nil},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "inherited_prop.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for inherited property reference")
}

// TestValidateInvariant_CaseInsensitive verifies that property references in
// invariants are resolved case-insensitively, matching the collation behavior.
func TestValidateInvariant_CaseInsensitive(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "case_check",
						// Name != "" (uppercase N, property declared lowercase)
						Expr: expr.SExpr{
							expr.Op("!="),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "Name"}},
							&expr.Literal{Val: ""},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "case_insensitive.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for case-insensitive property reference")
}

// TestValidateInvariant_RelationNameValid verifies that invariants can reference
// composition relation names directly (e.g., ITEMS -> Len > 0).
func TestValidateInvariant_RelationNameValid(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:   "LineItem",
				IsPart: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "quantity",
						Constraint: schema.NewIntegerConstraint(),
					},
				},
			},
			{
				Name: "Order",
				Properties: []*parse.PropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Relations: []*parse.RelationDecl{
					{
						Name:   "ITEMS",
						Kind:   parse.RelationComposition,
						Target: &parse.TypeRef{Name: "LineItem"},
						Many:   true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "has_items",
						// ITEMS -> Len > 0
						Expr: expr.SExpr{
							expr.Op(">"),
							expr.SExpr{
								expr.Op("Len"),
								expr.SExpr{expr.Op("p"), &expr.Literal{Val: "ITEMS"}},
							},
							&expr.Literal{Val: int64(0)},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "relation_name.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for relation name reference in invariant")
}

// TestValidateInvariant_ReduceBuiltin verifies that Reduce with 2 lambda params
// (accumulator + element) validates element member access against the relation target type.
func TestValidateInvariant_ReduceBuiltin(t *testing.T) {
	t.Parallel()

	// Order has composition *-> ITEMS (many) LineItem.
	// LineItem has property "quantity".
	// Invariant: ITEMS -> Reduce(0) |$acc, $item| { $acc + $item.quantity }
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:   "LineItem",
				IsPart: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "quantity",
						Constraint: schema.NewIntegerConstraint(),
					},
				},
			},
			{
				Name: "Order",
				Properties: []*parse.PropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Relations: []*parse.RelationDecl{
					{
						Name:   "ITEMS",
						Kind:   parse.RelationComposition,
						Target: &parse.TypeRef{Name: "LineItem"},
						Many:   true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "total_quantity",
						// ITEMS -> Reduce(0) |$acc, $item| { $acc + $item.quantity }
						Expr: expr.SExpr{
							expr.Op("Reduce"),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "ITEMS"}},
							&expr.Literal{Val: []string{"acc", "item"}},
							expr.SExpr{
								expr.Op("+"),
								expr.SExpr{expr.Op("$"), &expr.Literal{Val: "acc"}},
								expr.SExpr{
									expr.Op("."),
									expr.SExpr{expr.Op("$"), &expr.Literal{Val: "item"}},
									&expr.Literal{Val: "quantity"},
								},
							},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "reduce_builtin.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for valid Reduce builtin")
}

// TestValidateInvariant_ReduceUnknownProperty verifies that Reduce detects
// unknown properties on the element parameter's target type.
func TestValidateInvariant_ReduceUnknownProperty(t *testing.T) {
	t.Parallel()

	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:   "LineItem",
				IsPart: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "quantity",
						Constraint: schema.NewIntegerConstraint(),
					},
				},
			},
			{
				Name: "Order",
				Properties: []*parse.PropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Relations: []*parse.RelationDecl{
					{
						Name:   "ITEMS",
						Kind:   parse.RelationComposition,
						Target: &parse.TypeRef{Name: "LineItem"},
						Many:   true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "bad_reduce",
						// ITEMS -> Reduce(0) |$acc, $item| { $acc + $item.fake_field }
						Expr: expr.SExpr{
							expr.Op("Reduce"),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "ITEMS"}},
							&expr.Literal{Val: []string{"acc", "item"}},
							expr.SExpr{
								expr.Op("+"),
								expr.SExpr{expr.Op("$"), &expr.Literal{Val: "acc"}},
								expr.SExpr{
									expr.Op("."),
									expr.SExpr{expr.Op("$"), &expr.Literal{Val: "item"}},
									&expr.Literal{Val: "fake_field"},
								},
							},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "reduce_bad.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.Nil(t, s, "schema should fail to compile")
	require.True(t, collector.HasErrors())

	found := false
	for _, issue := range collector.Result().IssuesSlice() {
		if issue.Code() == diag.E_UNKNOWN_PROPERTY {
			found = true
			assert.Contains(t, issue.Message(), "fake_field")
			assert.Contains(t, issue.Message(), "LineItem")
		}
	}
	assert.True(t, found, "expected E_UNKNOWN_PROPERTY for fake_field on LineItem")
}

// TestValidateInvariant_ThenBuiltin verifies that Then binds a lambda parameter
// with unknown type (nil) and does not produce false positives on member access.
func TestValidateInvariant_ThenBuiltin(t *testing.T) {
	t.Parallel()

	// Person has property "name".
	// Invariant: name -> Then |$n| { $n -> Len > 0 }
	// $n has nil type (Then doesn't know the LHS type), so $n.anything should be skipped.
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "name_then_check",
						// name -> Then |$n| { $n -> Len > 0 }
						Expr: expr.SExpr{
							expr.Op("Then"),
							expr.SExpr{expr.Op("p"), &expr.Literal{Val: "name"}},
							&expr.Literal{Val: []string{"n"}},
							expr.SExpr{
								expr.Op(">"),
								expr.SExpr{
									expr.Op("Len"),
									expr.SExpr{expr.Op("$"), &expr.Literal{Val: "n"}},
								},
								&expr.Literal{Val: int64(0)},
							},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "then_builtin.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for valid Then builtin")
}

// TestValidateInvariant_NestedCollectionBuiltins verifies that nested collection
// builtins (e.g., Filter inside All) correctly scope lambda parameters.
func TestValidateInvariant_NestedCollectionBuiltins(t *testing.T) {
	t.Parallel()

	// Order has composition *-> ITEMS (many) LineItem.
	// LineItem has properties "quantity" and "active".
	// Invariant: ITEMS -> Filter |$i| { $i.active } -> All |$j| { $j.quantity > 0 }
	// Both $i and $j should resolve to LineItem.
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:   "LineItem",
				IsPart: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "quantity",
						Constraint: schema.NewIntegerConstraint(),
					},
					{
						Name:       "active",
						Constraint: schema.NewBooleanConstraint(),
					},
				},
			},
			{
				Name: "Order",
				Properties: []*parse.PropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Relations: []*parse.RelationDecl{
					{
						Name:   "ITEMS",
						Kind:   parse.RelationComposition,
						Target: &parse.TypeRef{Name: "LineItem"},
						Many:   true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "filtered_all_positive",
						// ITEMS -> Filter |$i| { $i.active } -> All |$j| { $j.quantity > 0 }
						Expr: expr.SExpr{
							expr.Op("All"),
							expr.SExpr{
								expr.Op("Filter"),
								expr.SExpr{expr.Op("p"), &expr.Literal{Val: "ITEMS"}},
								&expr.Literal{Val: []string{"i"}},
								expr.SExpr{
									expr.Op("."),
									expr.SExpr{expr.Op("$"), &expr.Literal{Val: "i"}},
									&expr.Literal{Val: "active"},
								},
							},
							&expr.Literal{Val: []string{"j"}},
							expr.SExpr{
								expr.Op(">"),
								expr.SExpr{
									expr.Op("."),
									expr.SExpr{expr.Op("$"), &expr.Literal{Val: "j"}},
									&expr.Literal{Val: "quantity"},
								},
								&expr.Literal{Val: int64(0)},
							},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nested_builtins.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	assert.False(t, collector.HasErrors(), "no errors expected for nested collection builtins")
}
