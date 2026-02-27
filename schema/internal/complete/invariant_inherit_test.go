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
