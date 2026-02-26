package schema_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/schema"
)

func TestStringConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "identical unbounded",
			parent: schema.NewStringConstraint(),
			child:  schema.NewStringConstraint(),
			want:   true,
		},
		{
			name:   "identical bounded",
			parent: schema.NewStringConstraintBounded(1, 100),
			child:  schema.NewStringConstraintBounded(1, 100),
			want:   true,
		},
		{
			name:   "raise min",
			parent: schema.NewStringConstraintBounded(1, 100),
			child:  schema.NewStringConstraintBounded(5, 100),
			want:   true,
		},
		{
			name:   "lower max",
			parent: schema.NewStringConstraintBounded(1, 100),
			child:  schema.NewStringConstraintBounded(1, 50),
			want:   true,
		},
		{
			name:   "raise min and lower max",
			parent: schema.NewStringConstraintBounded(1, 100),
			child:  schema.NewStringConstraintBounded(5, 50),
			want:   true,
		},
		{
			name:   "lower min rejected",
			parent: schema.NewStringConstraintBounded(5, 100),
			child:  schema.NewStringConstraintBounded(1, 100),
			want:   false,
		},
		{
			name:   "raise max rejected",
			parent: schema.NewStringConstraintBounded(1, 100),
			child:  schema.NewStringConstraintBounded(1, 200),
			want:   false,
		},
		{
			name:   "add upper bound to unbounded parent",
			parent: schema.NewStringConstraintBounded(1, -1),
			child:  schema.NewStringConstraintBounded(1, 100),
			want:   true,
		},
		{
			name:   "remove upper bound rejected",
			parent: schema.NewStringConstraintBounded(1, 100),
			child:  schema.NewStringConstraintBounded(1, -1),
			want:   false,
		},
		{
			name:   "unbounded parent to bounded child",
			parent: schema.NewStringConstraint(),
			child:  schema.NewStringConstraintBounded(5, 50),
			want:   true,
		},
		{
			name:   "bounded parent to unbounded child rejected",
			parent: schema.NewStringConstraintBounded(1, 100),
			child:  schema.NewStringConstraint(),
			want:   false,
		},
		{
			name:   "wrong kind rejected",
			parent: schema.NewStringConstraint(),
			child:  schema.NewIntegerConstraint(),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.parent.NarrowsTo(tt.child))
		})
	}
}

func TestIntegerConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "identical unbounded",
			parent: schema.NewIntegerConstraint(),
			child:  schema.NewIntegerConstraint(),
			want:   true,
		},
		{
			name:   "identical bounded",
			parent: schema.NewIntegerConstraintBounded(0, true, 100, true),
			child:  schema.NewIntegerConstraintBounded(0, true, 100, true),
			want:   true,
		},
		{
			name:   "raise min",
			parent: schema.NewIntegerConstraintBounded(0, true, 100, true),
			child:  schema.NewIntegerConstraintBounded(10, true, 100, true),
			want:   true,
		},
		{
			name:   "lower max",
			parent: schema.NewIntegerConstraintBounded(0, true, 100, true),
			child:  schema.NewIntegerConstraintBounded(0, true, 50, true),
			want:   true,
		},
		{
			name:   "lower min rejected",
			parent: schema.NewIntegerConstraintBounded(10, true, 100, true),
			child:  schema.NewIntegerConstraintBounded(0, true, 100, true),
			want:   false,
		},
		{
			name:   "raise max rejected",
			parent: schema.NewIntegerConstraintBounded(0, true, 100, true),
			child:  schema.NewIntegerConstraintBounded(0, true, 200, true),
			want:   false,
		},
		{
			name:   "unbounded to bounded",
			parent: schema.NewIntegerConstraint(),
			child:  schema.NewIntegerConstraintBounded(0, true, 100, true),
			want:   true,
		},
		{
			name:   "add upper bound",
			parent: schema.NewIntegerConstraintBounded(0, true, 0, false),
			child:  schema.NewIntegerConstraintBounded(0, true, 100, true),
			want:   true,
		},
		{
			name:   "remove lower bound rejected",
			parent: schema.NewIntegerConstraintBounded(0, true, 100, true),
			child:  schema.NewIntegerConstraintBounded(0, false, 100, true),
			want:   false,
		},
		{
			name:   "wrong kind rejected",
			parent: schema.NewIntegerConstraint(),
			child:  schema.NewFloatConstraint(),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.parent.NarrowsTo(tt.child))
		})
	}
}

func TestFloatConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "identical unbounded",
			parent: schema.NewFloatConstraint(),
			child:  schema.NewFloatConstraint(),
			want:   true,
		},
		{
			name:   "raise min",
			parent: schema.NewFloatConstraintBounded(0.0, true, 100.0, true),
			child:  schema.NewFloatConstraintBounded(10.0, true, 100.0, true),
			want:   true,
		},
		{
			name:   "lower max",
			parent: schema.NewFloatConstraintBounded(0.0, true, 100.0, true),
			child:  schema.NewFloatConstraintBounded(0.0, true, 50.0, true),
			want:   true,
		},
		{
			name:   "lower min rejected",
			parent: schema.NewFloatConstraintBounded(10.0, true, 100.0, true),
			child:  schema.NewFloatConstraintBounded(0.0, true, 100.0, true),
			want:   false,
		},
		{
			name:   "raise max rejected",
			parent: schema.NewFloatConstraintBounded(0.0, true, 100.0, true),
			child:  schema.NewFloatConstraintBounded(0.0, true, 200.0, true),
			want:   false,
		},
		{
			name:   "unbounded to bounded",
			parent: schema.NewFloatConstraint(),
			child:  schema.NewFloatConstraintBounded(0.0, true, 100.0, true),
			want:   true,
		},
		{
			name:   "wrong kind rejected",
			parent: schema.NewFloatConstraint(),
			child:  schema.NewIntegerConstraint(),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.parent.NarrowsTo(tt.child))
		})
	}
}

func TestEnumConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "identical",
			parent: schema.NewEnumConstraint([]string{"a", "b", "c"}),
			child:  schema.NewEnumConstraint([]string{"a", "b", "c"}),
			want:   true,
		},
		{
			name:   "subset",
			parent: schema.NewEnumConstraint([]string{"a", "b", "c"}),
			child:  schema.NewEnumConstraint([]string{"a", "c"}),
			want:   true,
		},
		{
			name:   "single value",
			parent: schema.NewEnumConstraint([]string{"a", "b", "c"}),
			child:  schema.NewEnumConstraint([]string{"b"}),
			want:   true,
		},
		{
			name:   "superset rejected",
			parent: schema.NewEnumConstraint([]string{"a", "b"}),
			child:  schema.NewEnumConstraint([]string{"a", "b", "c"}),
			want:   false,
		},
		{
			name:   "different values rejected",
			parent: schema.NewEnumConstraint([]string{"a", "b"}),
			child:  schema.NewEnumConstraint([]string{"x", "y"}),
			want:   false,
		},
		{
			name:   "empty child",
			parent: schema.NewEnumConstraint([]string{"a", "b", "c"}),
			child:  schema.NewEnumConstraint([]string{}),
			want:   true,
		},
		{
			name:   "wrong kind rejected",
			parent: schema.NewEnumConstraint([]string{"a"}),
			child:  schema.NewStringConstraint(),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.parent.NarrowsTo(tt.child))
		})
	}
}

func TestNonNarrowableConstraints_NarrowsTo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "boolean same",
			parent: schema.NewBooleanConstraint(),
			child:  schema.NewBooleanConstraint(),
			want:   true,
		},
		{
			name:   "boolean wrong kind",
			parent: schema.NewBooleanConstraint(),
			child:  schema.NewStringConstraint(),
			want:   false,
		},
		{
			name:   "date same",
			parent: schema.NewDateConstraint(),
			child:  schema.NewDateConstraint(),
			want:   true,
		},
		{
			name:   "date wrong kind",
			parent: schema.NewDateConstraint(),
			child:  schema.NewTimestampConstraint(),
			want:   false,
		},
		{
			name:   "uuid same",
			parent: schema.NewUUIDConstraint(),
			child:  schema.NewUUIDConstraint(),
			want:   true,
		},
		{
			name:   "uuid wrong kind",
			parent: schema.NewUUIDConstraint(),
			child:  schema.NewStringConstraint(),
			want:   false,
		},
		{
			name:   "timestamp same",
			parent: schema.NewTimestampConstraint(),
			child:  schema.NewTimestampConstraint(),
			want:   true,
		},
		{
			name:   "timestamp same format",
			parent: schema.NewTimestampConstraintFormatted("2006-01-02"),
			child:  schema.NewTimestampConstraintFormatted("2006-01-02"),
			want:   true,
		},
		{
			name:   "timestamp different format",
			parent: schema.NewTimestampConstraintFormatted("2006-01-02"),
			child:  schema.NewTimestampConstraintFormatted("2006-01-02T15:04:05Z07:00"),
			want:   false,
		},
		{
			name:   "pattern same",
			parent: schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^test")}),
			child:  schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^test")}),
			want:   true,
		},
		{
			name:   "pattern different",
			parent: schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a")}),
			child:  schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^b")}),
			want:   false,
		},
		{
			name:   "vector same",
			parent: schema.NewVectorConstraint(128),
			child:  schema.NewVectorConstraint(128),
			want:   true,
		},
		{
			name:   "vector different dims",
			parent: schema.NewVectorConstraint(128),
			child:  schema.NewVectorConstraint(256),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.parent.NarrowsTo(tt.child))
		})
	}
}

func TestAliasConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()

	t.Run("alias wrapping Integer narrows to narrower Integer", func(t *testing.T) {
		t.Parallel()
		parent := schema.NewAliasConstraint("Age",
			schema.NewIntegerConstraintBounded(0, true, 150, true))
		child := schema.NewIntegerConstraintBounded(18, true, 150, true)

		assert.True(t, parent.NarrowsTo(child))
	})

	t.Run("alias wrapping Integer does not narrow to wider Integer", func(t *testing.T) {
		t.Parallel()
		parent := schema.NewAliasConstraint("AdultAge",
			schema.NewIntegerConstraintBounded(18, true, 150, true))
		child := schema.NewIntegerConstraintBounded(0, true, 150, true)

		assert.False(t, parent.NarrowsTo(child))
	})

	t.Run("child is alias wrapping narrower Integer", func(t *testing.T) {
		t.Parallel()
		parent := schema.NewIntegerConstraintBounded(0, true, 150, true)
		child := schema.NewAliasConstraint("AdultAge",
			schema.NewIntegerConstraintBounded(18, true, 150, true))

		assert.True(t, parent.NarrowsTo(child))
	})

	t.Run("both aliases wrapping compatible constraints", func(t *testing.T) {
		t.Parallel()
		parent := schema.NewAliasConstraint("Age",
			schema.NewIntegerConstraintBounded(0, true, 150, true))
		child := schema.NewAliasConstraint("AdultAge",
			schema.NewIntegerConstraintBounded(18, true, 150, true))

		assert.True(t, parent.NarrowsTo(child))
	})

	t.Run("unresolved alias returns false", func(t *testing.T) {
		t.Parallel()
		unresolved := schema.NewAliasConstraint("Unknown", nil)
		child := schema.NewIntegerConstraint()

		assert.False(t, unresolved.NarrowsTo(child))
	})

	t.Run("deep alias chain narrows correctly", func(t *testing.T) {
		t.Parallel()
		// A -> B -> Integer[0,150]
		terminal := schema.NewIntegerConstraintBounded(0, true, 150, true)
		aliasB := schema.NewAliasConstraint("B", terminal)
		aliasA := schema.NewAliasConstraint("A", aliasB)

		child := schema.NewIntegerConstraintBounded(18, true, 120, true)
		assert.True(t, aliasA.NarrowsTo(child))
	})

	t.Run("deep alias chain rejects widening", func(t *testing.T) {
		t.Parallel()
		terminal := schema.NewIntegerConstraintBounded(18, true, 65, true)
		aliasB := schema.NewAliasConstraint("B", terminal)
		aliasA := schema.NewAliasConstraint("A", aliasB)

		child := schema.NewIntegerConstraintBounded(0, true, 150, true)
		assert.False(t, aliasA.NarrowsTo(child))
	})

	t.Run("alias wrapping enum narrows to subset", func(t *testing.T) {
		t.Parallel()
		parent := schema.NewAliasConstraint("Status",
			schema.NewEnumConstraint([]string{"active", "inactive", "pending"}))
		child := schema.NewEnumConstraint([]string{"active", "inactive"})

		assert.True(t, parent.NarrowsTo(child))
	})
}
