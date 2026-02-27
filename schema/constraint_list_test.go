package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
	"github.com/stretchr/testify/assert"
)

func TestListConstraint_Kind(t *testing.T) {
	t.Parallel()
	c := schema.NewListConstraint(schema.NewStringConstraint())
	assert.Equal(t, schema.KindList, c.Kind())
}

func TestListConstraint_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		c    schema.ListConstraint
		want string
	}{
		{
			name: "bare",
			c:    schema.NewListConstraint(schema.NewStringConstraint()),
			want: "List<String>",
		},
		{
			name: "element constrained",
			c:    schema.NewListConstraint(schema.NewStringConstraintBounded(-1, 6)),
			want: "List<String[_, 6]>",
		},
		{
			name: "list bounded",
			c:    schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 5),
			want: "List<String>[1, 5]",
		},
		{
			name: "both constrained",
			c:    schema.NewListConstraintBounded(schema.NewStringConstraintBounded(-1, 6), 1, 5),
			want: "List<String[_, 6]>[1, 5]",
		},
		{
			name: "nested",
			c:    schema.NewListConstraint(schema.NewListConstraint(schema.NewIntegerConstraint())),
			want: "List<List<Integer>>",
		},
		{
			name: "one-sided min",
			c:    schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, -1),
			want: "List<String>[1, _]",
		},
		{
			name: "one-sided max",
			c:    schema.NewListConstraintBounded(schema.NewStringConstraint(), -1, 10),
			want: "List<String>[_, 10]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.c.String())
		})
	}
}

func TestListConstraint_Equal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		a, b schema.Constraint
		want bool
	}{
		{
			name: "same bare",
			a:    schema.NewListConstraint(schema.NewStringConstraint()),
			b:    schema.NewListConstraint(schema.NewStringConstraint()),
			want: true,
		},
		{
			name: "same bounded",
			a:    schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 5),
			b:    schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 5),
			want: true,
		},
		{
			name: "different element",
			a:    schema.NewListConstraint(schema.NewStringConstraint()),
			b:    schema.NewListConstraint(schema.NewIntegerConstraint()),
			want: false,
		},
		{
			name: "different bounds",
			a:    schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 5),
			b:    schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 10),
			want: false,
		},
		{
			name: "bare vs bounded",
			a:    schema.NewListConstraint(schema.NewStringConstraint()),
			b:    schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 5),
			want: false,
		},
		{
			name: "non-list",
			a:    schema.NewListConstraint(schema.NewStringConstraint()),
			b:    schema.NewStringConstraint(),
			want: false,
		},
		{
			name: "alias wrapping list",
			a:    schema.NewListConstraint(schema.NewStringConstraint()),
			b:    schema.NewAliasConstraint("Tags", schema.NewListConstraint(schema.NewStringConstraint())),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.a.Equal(tt.b))
		})
	}
}

func TestListConstraint_NarrowsTo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		parent schema.Constraint
		child  schema.Constraint
		want   bool
	}{
		{
			name:   "bare to bare same element",
			parent: schema.NewListConstraint(schema.NewStringConstraint()),
			child:  schema.NewListConstraint(schema.NewStringConstraint()),
			want:   true,
		},
		{
			name:   "bare to bounded",
			parent: schema.NewListConstraint(schema.NewStringConstraint()),
			child:  schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 10),
			want:   true,
		},
		{
			name:   "bounded to tighter bounds",
			parent: schema.NewListConstraintBounded(schema.NewStringConstraint(), 0, 100),
			child:  schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 50),
			want:   true,
		},
		{
			name:   "bounded to wider bounds fails",
			parent: schema.NewListConstraintBounded(schema.NewStringConstraint(), 5, 10),
			child:  schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 10),
			want:   false,
		},
		{
			name:   "element narrows",
			parent: schema.NewListConstraint(schema.NewStringConstraint()),
			child:  schema.NewListConstraint(schema.NewStringConstraintBounded(1, 50)),
			want:   true,
		},
		{
			name:   "element widens fails",
			parent: schema.NewListConstraint(schema.NewStringConstraintBounded(1, 50)),
			child:  schema.NewListConstraint(schema.NewStringConstraint()),
			want:   false,
		},
		{
			name:   "different element kind fails",
			parent: schema.NewListConstraint(schema.NewStringConstraint()),
			child:  schema.NewListConstraint(schema.NewIntegerConstraint()),
			want:   false,
		},
		{
			name:   "non-list child fails",
			parent: schema.NewListConstraint(schema.NewStringConstraint()),
			child:  schema.NewStringConstraint(),
			want:   false,
		},
		{
			name:   "child lacks max when parent has max fails",
			parent: schema.NewListConstraintBounded(schema.NewStringConstraint(), -1, 10),
			child:  schema.NewListConstraint(schema.NewStringConstraint()),
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

func TestListConstraint_IsResolved(t *testing.T) {
	t.Parallel()

	t.Run("resolved element", func(t *testing.T) {
		t.Parallel()
		c := schema.NewListConstraint(schema.NewStringConstraint())
		assert.True(t, c.IsResolved())
	})

	t.Run("unresolved alias element", func(t *testing.T) {
		t.Parallel()
		c := schema.NewListConstraint(schema.NewAliasConstraint("Unknown", nil))
		assert.False(t, c.IsResolved())
	})

	t.Run("resolved alias element", func(t *testing.T) {
		t.Parallel()
		c := schema.NewListConstraint(schema.NewAliasConstraint("MyStr", schema.NewStringConstraint()))
		assert.True(t, c.IsResolved())
	})
}

func TestListConstraint_Accessors(t *testing.T) {
	t.Parallel()

	t.Run("element", func(t *testing.T) {
		t.Parallel()
		elem := schema.NewStringConstraint()
		c := schema.NewListConstraint(elem)
		assert.Equal(t, elem, c.Element())
	})

	t.Run("bounds present", func(t *testing.T) {
		t.Parallel()
		c := schema.NewListConstraintBounded(schema.NewStringConstraint(), 1, 5)
		minVal, hasMin := c.MinLen()
		maxVal, hasMax := c.MaxLen()
		assert.True(t, hasMin)
		assert.Equal(t, int64(1), minVal)
		assert.True(t, hasMax)
		assert.Equal(t, int64(5), maxVal)
	})

	t.Run("bounds absent", func(t *testing.T) {
		t.Parallel()
		c := schema.NewListConstraint(schema.NewStringConstraint())
		_, hasMin := c.MinLen()
		_, hasMax := c.MaxLen()
		assert.False(t, hasMin)
		assert.False(t, hasMax)
	})
}
