package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// helper builds a Property with minimal boilerplate for narrowing tests.
func narrowProp(name string, constraint schema.Constraint, optional, pk bool) *schema.Property {
	return schema.NewProperty(name, location.Span{}, "", constraint, schema.DataTypeRef{}, optional, pk, schema.DeclaringScope{})
}

func TestProperty_CanNarrowFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		child  *schema.Property
		parent *schema.Property
		want   bool
	}{
		{
			name:   "identical properties same constraint same modifier",
			child:  narrowProp("age", schema.NewIntegerConstraintBounded(0, true, 150, true), false, false),
			parent: narrowProp("age", schema.NewIntegerConstraintBounded(0, true, 150, true), false, false),
			want:   true,
		},
		{
			name:   "narrow bounds parent Integer[0,150] child Integer[18,150]",
			child:  narrowProp("age", schema.NewIntegerConstraintBounded(18, true, 150, true), false, false),
			parent: narrowProp("age", schema.NewIntegerConstraintBounded(0, true, 150, true), false, false),
			want:   true,
		},
		{
			name:   "optional to required valid narrowing",
			child:  narrowProp("name", schema.NewStringConstraint(), false, false),
			parent: narrowProp("name", schema.NewStringConstraint(), true, false),
			want:   true,
		},
		{
			name:   "required to optional rejected",
			child:  narrowProp("name", schema.NewStringConstraint(), true, false),
			parent: narrowProp("name", schema.NewStringConstraint(), false, false),
			want:   false,
		},
		{
			name:   "narrow bounds AND promote required both at once",
			child:  narrowProp("age", schema.NewIntegerConstraintBounded(18, true, 150, true), false, false),
			parent: narrowProp("age", schema.NewIntegerConstraintBounded(0, true, 150, true), true, false),
			want:   true,
		},
		{
			name:   "widen bounds rejected",
			child:  narrowProp("age", schema.NewIntegerConstraintBounded(0, true, 200, true), false, false),
			parent: narrowProp("age", schema.NewIntegerConstraintBounded(0, true, 150, true), false, false),
			want:   false,
		},
		{
			name:   "PK change rejected child adds PK",
			child:  narrowProp("id", schema.NewStringConstraint(), false, true),
			parent: narrowProp("id", schema.NewStringConstraint(), false, false),
			want:   false,
		},
		{
			name:   "PK change rejected child removes PK",
			child:  narrowProp("id", schema.NewStringConstraint(), false, false),
			parent: narrowProp("id", schema.NewStringConstraint(), false, true),
			want:   false,
		},
		{
			name:   "different name rejected",
			child:  narrowProp("first_name", schema.NewStringConstraint(), false, false),
			parent: narrowProp("last_name", schema.NewStringConstraint(), false, false),
			want:   false,
		},
		{
			name:   "nil constraints both ok",
			child:  narrowProp("data", nil, false, false),
			parent: narrowProp("data", nil, false, false),
			want:   true,
		},
		{
			name:   "nil parent constraint non-nil child rejected",
			child:  narrowProp("data", schema.NewStringConstraint(), false, false),
			parent: narrowProp("data", nil, false, false),
			want:   false,
		},
		{
			name:   "non-nil parent constraint nil child rejected",
			child:  narrowProp("data", nil, false, false),
			parent: narrowProp("data", schema.NewStringConstraint(), false, false),
			want:   false,
		},
		{
			name:   "nil properties both",
			child:  nil,
			parent: nil,
			want:   true,
		},
		{
			name:   "nil child non-nil parent",
			child:  nil,
			parent: narrowProp("x", schema.NewStringConstraint(), false, false),
			want:   false,
		},
		{
			name:   "non-nil child nil parent",
			child:  narrowProp("x", schema.NewStringConstraint(), false, false),
			parent: nil,
			want:   false,
		},
		{
			name:   "narrow string bounds",
			child:  narrowProp("code", schema.NewStringConstraintBounded(2, 5), false, false),
			parent: narrowProp("code", schema.NewStringConstraintBounded(1, 10), false, false),
			want:   true,
		},
		{
			name:   "widen string min rejected",
			child:  narrowProp("code", schema.NewStringConstraintBounded(0, 10), false, false),
			parent: narrowProp("code", schema.NewStringConstraintBounded(1, 10), false, false),
			want:   false,
		},
		{
			name:   "narrow float bounds",
			child:  narrowProp("score", schema.NewFloatConstraintBounded(0.1, true, 0.9, true), false, false),
			parent: narrowProp("score", schema.NewFloatConstraintBounded(0.0, true, 1.0, true), false, false),
			want:   true,
		},
		{
			name:   "narrow enum subset",
			child:  narrowProp("status", schema.NewEnumConstraint([]string{"active"}), false, false),
			parent: narrowProp("status", schema.NewEnumConstraint([]string{"active", "inactive"}), false, false),
			want:   true,
		},
		{
			name:   "widen enum superset rejected",
			child:  narrowProp("status", schema.NewEnumConstraint([]string{"active", "inactive", "pending"}), false, false),
			parent: narrowProp("status", schema.NewEnumConstraint([]string{"active", "inactive"}), false, false),
			want:   false,
		},
		{
			name:   "different constraint kinds rejected",
			child:  narrowProp("val", schema.NewIntegerConstraint(), false, false),
			parent: narrowProp("val", schema.NewStringConstraint(), false, false),
			want:   false,
		},
		{
			name:   "boolean same ok",
			child:  narrowProp("flag", schema.NewBooleanConstraint(), false, false),
			parent: narrowProp("flag", schema.NewBooleanConstraint(), false, false),
			want:   true,
		},
		{
			name:   "optional to optional same constraint ok",
			child:  narrowProp("x", schema.NewStringConstraint(), true, false),
			parent: narrowProp("x", schema.NewStringConstraint(), true, false),
			want:   true,
		},
		{
			name:   "required to required same constraint ok",
			child:  narrowProp("x", schema.NewStringConstraint(), false, false),
			parent: narrowProp("x", schema.NewStringConstraint(), false, false),
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.child.CanNarrowFrom(tt.parent)
			assert.Equal(t, tt.want, got)
		})
	}
}
