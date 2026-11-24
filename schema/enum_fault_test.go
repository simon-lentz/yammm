package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

// An Enum holding fewer than two values is refused, the zero value too.
func TestEnumConstraint_IsJudgedOnTheValuesItHolds(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"the zero value": schema.EnumConstraint{},
		"nil values":     schema.TestParsedEnumConstraint(nil),
	} {
		if err := schema.CheckConstraint(c); err == nil || err.Error() != "schema: enum must have at least two values (got 0)" {
			t.Errorf("%s: CheckConstraint = %v; want the two-value minimum", name, err)
		}
	}
}

// Two Enums of one length holding one set of values are equal and narrow to
// each other, whichever value each repeats.
func TestEnumConstraint_ComparesByItsValuesAlone(t *testing.T) {
	a := schema.TestParsedEnumConstraint([]string{"a", "a", "b"})
	b := schema.TestParsedEnumConstraint([]string{"a", "b", "b"})
	if !schema.TestConstraintEqual(a, b) || !schema.TestConstraintEqual(b, a) {
		t.Error("Enums holding one set of values are not equal")
	}
	if !schema.TestConstraintNarrowsTo(a, b) || !schema.TestConstraintNarrowsTo(b, a) {
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
	enum := schema.TestParsedEnumConstraint(values)
	for name, c := range map[string]schema.Constraint{
		"an Enum of forty values": enum,
		"a reference to it":       schema.TestNewAliasConstraint("Colour", enum),
		"a List of a reference":   schema.TestNewListConstraint(schema.TestNewAliasConstraint("Colour", enum)),
		"a bounded String":        schema.TestStringLenBetween(1, 64),
		"a bounded Float":         schema.TestFloatBetween(0.5, 2.5),
		"a Timestamp":             schema.TestNewTimestampConstraint(),
		"a List of a List":        schema.TestNewListConstraint(schema.TestNewListConstraint(schema.TestIntegerBetween(1, 5))),
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
