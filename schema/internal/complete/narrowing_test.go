package complete_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/internal/complete"
	"github.com/simon-lentz/yammm/schema/internal/parse"
)

func TestNarrowing_ValidConstraintNarrowing(t *testing.T) {
	t.Parallel()

	// Abstract Entity{age Integer[0,150] optional}, Adult extends Entity{age Integer[18,150] optional}
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Entity",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "age",
						Constraint: schema.NewIntegerConstraintBounded(0, true, 150, true),
						Optional:   true,
					},
				},
			},
			{
				Name: "Adult",
				Inherits: []*parse.TypeRef{
					{Name: "Entity"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "age",
						Constraint: schema.NewIntegerConstraintBounded(18, true, 150, true),
						Optional:   true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_valid.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile with valid narrowing")
	require.False(t, collector.HasErrors(), "valid narrowing should not produce errors")

	adult, ok := s.Type("Adult")
	require.True(t, ok)

	// Adult's age should be the narrowed version: Integer[18, 150]
	ageProp, ok := adult.Property("age")
	require.True(t, ok)

	ic, ok := ageProp.Constraint().(schema.IntegerConstraint)
	require.True(t, ok, "age constraint should be IntegerConstraint")

	min, hasMin := ic.Min()
	max, hasMax := ic.Max()
	assert.True(t, hasMin)
	assert.True(t, hasMax)
	assert.Equal(t, int64(18), min, "narrowed min should be 18")
	assert.Equal(t, int64(150), max, "max should remain 150")
}

func TestNarrowing_ValidModifierOverride(t *testing.T) {
	t.Parallel()

	// Abstract Base{name String[1,100] optional}, Child extends Base{name String[1,100] required}
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Base",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraintBounded(1, 100),
						Optional:   true,
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
						Name:       "name",
						Constraint: schema.NewStringConstraintBounded(1, 100),
						Optional:   false, // required overrides optional
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_modifier.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile with optional->required narrowing")
	require.False(t, collector.HasErrors(), "optional->required narrowing should not produce errors")

	child, ok := s.Type("Child")
	require.True(t, ok)

	nameProp, ok := child.Property("name")
	require.True(t, ok)
	assert.False(t, nameProp.IsOptional(), "child's name should be required (not optional)")
}

func TestNarrowing_WideningRejected(t *testing.T) {
	t.Parallel()

	// Entity{age Integer[0,150]}, BadChild extends Entity{age Integer[0,200]} -> E_PROPERTY_CONFLICT
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Entity",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "age",
						Constraint: schema.NewIntegerConstraintBounded(0, true, 150, true),
						Optional:   true,
					},
				},
			},
			{
				Name: "BadChild",
				Inherits: []*parse.TypeRef{
					{Name: "Entity"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "age",
						Constraint: schema.NewIntegerConstraintBounded(0, true, 200, true),
						Optional:   true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_widening.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s, "widening should cause schema completion to fail")
	assert.True(t, collector.HasErrors(), "widening should produce E_PROPERTY_CONFLICT")

	issues := collector.Result().IssuesSlice()
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_PROPERTY_CONFLICT, issues[0].Code())
}

func TestNarrowing_RequiredToOptionalRejected(t *testing.T) {
	t.Parallel()

	// Base{field String required}, Child extends Base{field String optional} -> E_PROPERTY_CONFLICT
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Base",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "field",
						Constraint: schema.NewStringConstraint(),
						Optional:   false, // required
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
						Name:       "field",
						Constraint: schema.NewStringConstraint(),
						Optional:   true, // optional (widens)
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_req_opt.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s, "required->optional widening should cause schema completion to fail")
	assert.True(t, collector.HasErrors(), "required->optional should produce E_PROPERTY_CONFLICT")

	issues := collector.Result().IssuesSlice()
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_PROPERTY_CONFLICT, issues[0].Code())
}

func TestNarrowing_EnumSubset(t *testing.T) {
	t.Parallel()

	// Base{status Enum["a","b","c"]}, Restricted extends Base{status Enum["a","b"]} -> compiles
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "Base",
				IsAbstract: true,
				Properties: []*parse.PropertyDecl{
					{
						Name:       "status",
						Constraint: schema.NewEnumConstraint([]string{"a", "b", "c"}),
						Optional:   true,
					},
				},
			},
			{
				Name: "Restricted",
				Inherits: []*parse.TypeRef{
					{Name: "Base"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "status",
						Constraint: schema.NewEnumConstraint([]string{"a", "b"}),
						Optional:   true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_enum.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile with enum subset narrowing")
	require.False(t, collector.HasErrors(), "enum subset narrowing should not produce errors")

	restricted, ok := s.Type("Restricted")
	require.True(t, ok)

	statusProp, ok := restricted.Property("status")
	require.True(t, ok)

	ec, ok := statusProp.Constraint().(schema.EnumConstraint)
	require.True(t, ok, "status constraint should be EnumConstraint")
	assert.Equal(t, []string{"a", "b"}, ec.Values(), "narrowed enum should have only [a, b]")
}
