package spec_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// =============================================================================
// Type Declaration
// =============================================================================

// TestTypes_BasicDeclaration verifies that a schema with single extends compiles.
// Source: SPEC.md, "Type Declarations" and "Inheritance"
func TestTypes_BasicDeclaration(t *testing.T) {
	t.Parallel()
	loadSchema(t, "testdata/types/single_extends.yammm")
}

// =============================================================================
// Type Modifiers — abstract
// =============================================================================

// TestTypes_AbstractCannotInstantiate verifies that validating an instance directly
// against an abstract type returns a validation failure (not an error).
// Source: SPEC.md, "Abstract types cannot be instantiated directly"
func TestTypes_AbstractCannotInstantiate(t *testing.T) {
	t.Parallel()

	data := "testdata/types/data.json"
	v := loadSchema(t, "testdata/types/abstract.yammm")

	records := loadTestData(t, data, "Vehicle__abstract")
	assertInvalid(t, v, "Vehicle", records[0], diag.E_ABSTRACT_TYPE)
}

// TestTypes_AbstractChildCanInstantiate verifies that a concrete child of an
// abstract type can be instantiated successfully.
// Source: SPEC.md, "Abstract types ... can be extended by other types"
func TestTypes_AbstractChildCanInstantiate(t *testing.T) {
	t.Parallel()

	data := "testdata/types/data.json"
	v := loadSchema(t, "testdata/types/abstract.yammm")

	records := loadTestData(t, data, "Car")
	assertValid(t, v, "Car", records[0])
}

// =============================================================================
// Type Modifiers — part
// =============================================================================

// TestTypes_PartTypeCompiles verifies that a schema with a part type and
// composition compiles cleanly.
// Source: SPEC.md, "Part types are used as composition targets"
func TestTypes_PartTypeCompiles(t *testing.T) {
	t.Parallel()
	loadSchema(t, "testdata/types/part_type.yammm")
}

// TestTypes_CompositionRequiresPart verifies that using a non-part type as a
// composition target causes a compilation failure.
// Source: SPEC.md, "The target must be a concrete part type (not abstract)."
func TestTypes_CompositionRequiresPart(t *testing.T) {
	t.Parallel()
	result := loadSchemaExpectError(t, "testdata/types/composition_non_part.yammm")
	assertDiagHasCode(t, result, diag.E_INVALID_COMPOSITION_TARGET)
}

// =============================================================================
// Inheritance — single extends
// =============================================================================

// TestTypes_SingleExtends validates a Child instance that provides both
// inherited (from Base) and own properties.
// Source: SPEC.md, "Properties, associations, and compositions are inherited"
func TestTypes_SingleExtends(t *testing.T) {
	t.Parallel()

	data := "testdata/types/data.json"
	v := loadSchema(t, "testdata/types/single_extends.yammm")

	records := loadTestData(t, data, "Child")
	assertValid(t, v, "Child", records[0])
}

// TestTypes_PropertiesInherited validates a Child instance that provides only
// inherited properties (name from Base), omitting the child-defined extra field.
// Source: SPEC.md, "Properties ... are inherited from parent types"
func TestTypes_PropertiesInherited(t *testing.T) {
	t.Parallel()

	data := "testdata/types/data.json"
	v := loadSchema(t, "testdata/types/single_extends.yammm")

	records := loadTestData(t, data, "Child__inherited_only")
	assertValid(t, v, "Child", records[0])
}

// =============================================================================
// Inheritance — multiple extends
// =============================================================================

// TestTypes_MultipleInheritance validates a Document instance that inherits
// from both Named (name) and Timestamped (created_at).
// Source: SPEC.md, "Multiple inheritance is supported"
func TestTypes_MultipleInheritance(t *testing.T) {
	t.Parallel()

	data := "testdata/types/data.json"
	v := loadSchema(t, "testdata/types/multiple_inheritance.yammm")

	records := loadTestData(t, data, "Document")
	assertValid(t, v, "Document", records[0])
}

// =============================================================================
// Inheritance — constraint narrowing / widening
// =============================================================================

// TestTypes_NarrowingValid verifies that a child type may re-declare an
// inherited property with tighter (narrower) bounds — the schema compiles cleanly.
// Source: SPEC.md, "Child types may override inherited properties with compatible narrower constraints"
func TestTypes_NarrowingValid(t *testing.T) {
	t.Parallel()

	v := loadSchema(t, "testdata/types/narrowing_valid.yammm")

	// Valid: age 30 is within narrowed bounds [18, 65]
	data := "testdata/types/data.json"
	records := loadTestData(t, data, "Restricted__valid")
	assertValid(t, v, "Restricted", records[0])

	// Invalid: age 10 is below narrowed lower bound 18
	t.Run("below_narrowed_lower", func(t *testing.T) {
		t.Parallel()
		tooYoung := loadTestData(t, data, "Restricted__too_young")
		assertInvalid(t, v, "Restricted", tooYoung[0], diag.E_CONSTRAINT_FAIL)
	})

	// Invalid: age 100 is above narrowed upper bound 65
	t.Run("above_narrowed_upper", func(t *testing.T) {
		t.Parallel()
		tooOld := loadTestData(t, data, "Restricted__too_old")
		assertInvalid(t, v, "Restricted", tooOld[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestTypes_WideningInvalid verifies that a child type may NOT re-declare an
// inherited property with wider bounds — the schema fails compilation.
// Source: SPEC.md, "compatible narrower constraints" (widening is a conflict)
func TestTypes_WideningInvalid(t *testing.T) {
	t.Parallel()
	result := loadSchemaExpectError(t, "testdata/types/widening_invalid.yammm")
	assertDiagHasCode(t, result, diag.E_PROPERTY_CONFLICT)
}
