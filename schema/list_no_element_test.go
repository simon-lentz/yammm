package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// A List with no element, which Build and CheckConstraint refuse, still answers
// every method without a panic: it prints as List<> and its bounds, equals
// another such List with the same bounds, narrows to nothing and is not
// resolved, the last three alone or behind a reference.
func TestListConstraint_WithNoElementAnswersEveryMethod(t *testing.T) {
	t.Parallel()
	for name, build := range map[string]func() schema.ListConstraint{
		"the zero value":      func() schema.ListConstraint { return schema.ListConstraint{} },
		"a nil element":       func() schema.ListConstraint { return schema.NewListConstraint(nil) },
		"bounded, no element": func() schema.ListConstraint { return schema.ListLenBetween(nil, 1, 3) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			list, twin := build(), build()
			ref := schema.NewAliasConstraint("A", list)
			if got := list.String(); got != "List<>" && got != "List<>[1, 3]" {
				t.Errorf("String = %q", got)
			}
			if !list.Equal(twin) || !ref.Equal(twin) {
				t.Error("a List with no element is not equal to itself")
			}
			if list.Equal(schema.NewListConstraint(schema.NewStringConstraint())) || schema.NewListConstraint(schema.NewStringConstraint()).Equal(list) {
				t.Error("a List with no element equals a List of String")
			}
			if list.NarrowsTo(twin) || ref.NarrowsTo(twin) {
				t.Error("a List with no element narrows")
			}
			if list.IsResolved() || ref.IsResolved() {
				t.Error("a List with no element is resolved")
			}
		})
	}
}
