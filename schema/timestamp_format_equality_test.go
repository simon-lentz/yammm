package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// The recorded verdict is no part of what a Timestamp is: two built from one
// format are equal and print alike, whether or not the format is refused.
func TestTimestampConstraint_ComparesByItsFormatAlone(t *testing.T) {
	for _, format := range []string{"", "2006-01-02", "\xff", "2006\xc3"} {
		a := schema.NewTimestampConstraintFormatted(format)
		b := schema.NewTimestampConstraintFormatted(strings.Clone(format))
		if a != b || !a.Equal(b) || !a.NarrowsTo(b) || a.String() != b.String() || a.Format() != format {
			t.Errorf("format %q: two constraints built from it differ: %v, %v", format, a, b)
		}
	}
	if schema.NewTimestampConstraintFormatted("") != schema.NewTimestampConstraint() {
		t.Error("a Timestamp built with an empty format is not the one built with none")
	}
	if (schema.TimestampConstraint{}) != schema.NewTimestampConstraint() {
		t.Error("the zero Timestamp is not the one built with no format")
	}
	if err := schema.CheckConstraint(schema.TimestampConstraint{}); err != nil {
		t.Errorf("CheckConstraint refuses the zero Timestamp: %v", err)
	}
}
