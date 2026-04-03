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

func TestInvariantInheritance_SingleParent(t *testing.T) {
	t.Parallel()

	// Abstract Auditable has invariant "must_be_active".
	// Account extends Auditable with no own invariants.
	// Account.InvariantsSlice() should be empty (own only).
	// Account.AllInvariantsSlice() should contain ["must_be_active"].
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Auditable",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "is_active",
						Constraint: schema.NewBooleanConstraint(),
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "must_be_active",
						Expr: expr.NewLiteral(true),
					},
				},
			},
			{
				Name: "Account",
				Inherits: []*parse.TypeRef{
					{Name: "Auditable"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:         "account_id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "inv_single_parent.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	require.False(t, collector.HasErrors(), "no errors expected")

	account, ok := s.Type("Account")
	require.True(t, ok)

	// Own invariants should be empty
	assert.Empty(t, account.InvariantsSlice(), "Account should have no own invariants")

	// All invariants should include inherited
	allInvs := account.AllInvariantsSlice()
	require.Len(t, allInvs, 1, "Account should inherit 1 invariant")
	assert.Equal(t, "must_be_active", allInvs[0].Name())
}

func TestInvariantInheritance_ChildOverride(t *testing.T) {
	t.Parallel()

	// Abstract Base has invariant "check".
	// Child extends Base, declares own "check" (override) and "extra".
	// Child.AllInvariantsSlice() should have 2: child's "check" first, then "extra".
	// The parent's "check" is deduplicated because child already has it.
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Base",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "value",
						Constraint: schema.NewIntegerConstraint(),
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "check",
						Expr: expr.NewLiteral(true),
					},
				},
			},
			{
				Name: "Child",
				Inherits: []*parse.TypeRef{
					{Name: "Base"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:         "child_id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "check",
						Expr: expr.NewLiteral(false), // overrides parent's "check"
					},
					{
						Name: "extra",
						Expr: expr.NewLiteral(true),
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "inv_child_override.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	require.False(t, collector.HasErrors(), "no errors expected")

	child, ok := s.Type("Child")
	require.True(t, ok)

	// Own invariants: "check" and "extra"
	ownInvs := child.InvariantsSlice()
	require.Len(t, ownInvs, 2, "Child should have 2 own invariants")

	// All invariants: child's "check", "extra" (parent's "check" deduplicated)
	allInvs := child.AllInvariantsSlice()
	require.Len(t, allInvs, 2, "Child should have 2 total invariants (parent's check deduplicated)")
	assert.Equal(t, "check", allInvs[0].Name())
	assert.Equal(t, "extra", allInvs[1].Name())
}

func TestInvariantInheritance_Diamond(t *testing.T) {
	t.Parallel()

	// Root (abstract, invariant "root_check")
	// Left extends Root (invariant "left_check")
	// Right extends Root (invariant "right_check")
	// Bottom extends Left, Right
	// Bottom.AllInvariantsSlice() should have 3: root_check, left_check, right_check
	// root_check is inherited via both Left and Right but deduplicated.
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Root",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "root_check",
						Expr: expr.NewLiteral(true),
					},
				},
			},
			{
				Name:       "Left",
				IsAbstract: true,
				Inherits: []*parse.TypeRef{
					{Name: "Root"},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "left_check",
						Expr: expr.NewLiteral(true),
					},
				},
			},
			{
				Name:       "Right",
				IsAbstract: true,
				Inherits: []*parse.TypeRef{
					{Name: "Root"},
				},
				Invariants: []*parse.InvariantDecl{
					{
						Name: "right_check",
						Expr: expr.NewLiteral(true),
					},
				},
			},
			{
				Name: "Bottom",
				Inherits: []*parse.TypeRef{
					{Name: "Left"},
					{Name: "Right"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "bottom_field",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "inv_diamond.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile")
	require.False(t, collector.HasErrors(), "no errors expected")

	// Verify intermediate types first
	left, ok := s.Type("Left")
	require.True(t, ok)
	leftAll := left.AllInvariantsSlice()
	require.Len(t, leftAll, 2, "Left should have left_check + root_check")

	right, ok := s.Type("Right")
	require.True(t, ok)
	rightAll := right.AllInvariantsSlice()
	require.Len(t, rightAll, 2, "Right should have right_check + root_check")

	// Verify Bottom
	bottom, ok := s.Type("Bottom")
	require.True(t, ok)

	// No own invariants
	assert.Empty(t, bottom.InvariantsSlice(), "Bottom should have no own invariants")

	// All invariants: inherited from Left (left_check, root_check) and Right (right_check)
	// root_check appears in both Left and Right but is deduplicated.
	allInvs := bottom.AllInvariantsSlice()
	require.Len(t, allInvs, 3, "Bottom should have 3 invariants (root_check deduplicated)")

	names := make([]string, len(allInvs))
	for i, inv := range allInvs {
		names[i] = inv.Name()
	}
	assert.Contains(t, names, "root_check")
	assert.Contains(t, names, "left_check")
	assert.Contains(t, names, "right_check")
}
