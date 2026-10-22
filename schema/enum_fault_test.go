package schema_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

// An Enum's value fault is recorded where the Enum is built, from the copy it
// holds; the count is read at the judge, so the zero value is refused too.
func TestEnumConstraint_IsJudgedOnTheValuesItHolds(t *testing.T) {
	given := []string{"a", "b"}
	sound := schema.NewEnumConstraint(given)
	given[1] = "a"
	if err := schema.CheckConstraint(sound); err != nil {
		t.Errorf("an Enum built from two distinct values is refused after its argument changed: %v", err)
	}
	given = []string{"a", "a"}
	repeated := schema.NewEnumConstraint(given)
	given[1] = "b"
	if err := schema.CheckConstraint(repeated); err == nil || !strings.Contains(err.Error(), `duplicate enum value "a"`) {
		t.Errorf("CheckConstraint = %v; want the duplicate the Enum holds", err)
	}
	for name, c := range map[string]schema.Constraint{
		"the zero value": schema.EnumConstraint{},
		"nil values":     schema.NewEnumConstraint(nil),
	} {
		if err := schema.CheckConstraint(c); err == nil || err.Error() != "schema: enum must have at least two values (got 0)" {
			t.Errorf("%s: CheckConstraint = %v; want the two-value minimum", name, err)
		}
	}
}

// Two Enums of one length holding one set of values are equal and narrow to
// each other, whichever value each repeats.
func TestEnumConstraint_ComparesByItsValuesAlone(t *testing.T) {
	a := schema.NewEnumConstraint([]string{"a", "a", "b"})
	b := schema.NewEnumConstraint([]string{"a", "b", "b"})
	if !a.Equal(b) || !b.Equal(a) {
		t.Error("Enums holding one set of values are not equal")
	}
	if !a.NarrowsTo(b) || !b.NarrowsTo(a) {
		t.Error("Enums holding one set of values do not narrow to each other")
	}
	if got := a.String(); got != `Enum["a", "a", "b"]` {
		t.Errorf("String = %s; want the values as given", got)
	}
}

// Judging a constraint a schema holds allocates nothing, whatever its kind.
func TestCheckConstraint_AcceptingAllocatesNothing(t *testing.T) {
	raceskip.Skip(t)
	values := make([]string, 40)
	for i := range values {
		values[i] = "value_" + string(rune('A'+i))
	}
	enum := schema.NewEnumConstraint(values)
	for name, c := range map[string]schema.Constraint{
		"an Enum of forty values": enum,
		"a reference to it":       schema.NewAliasConstraint("Colour", enum),
		"a List of a reference":   schema.NewListConstraint(schema.NewAliasConstraint("Colour", enum)),
		"a bounded String":        schema.StringLenBetween(1, 64),
		"a bounded Float":         schema.FloatBetween(0.5, 2.5),
		"a formatted Timestamp":   schema.NewTimestampConstraintFormatted("2006-01-02"),
		"a List of a List":        schema.NewListConstraint(schema.NewListConstraint(schema.IntegerBetween(1, 5))),
	} {
		if allocs := testing.AllocsPerRun(100, func() {
			if err := schema.CheckConstraint(c); err != nil {
				t.Fatal(err)
			}
		}); allocs != 0 {
			t.Errorf("CheckConstraint allocates %v per call on %s; want 0", allocs, name)
		}
	}
}
