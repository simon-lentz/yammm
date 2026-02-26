package claude_plugin_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestTypeSystem_SchemaCompilation verifies all type-system.md schemas load.
func TestTypeSystem_SchemaCompilation(t *testing.T) {
	t.Parallel()

	schemas := []string{
		"testdata/type_system/builtin_types.yammm",
		"testdata/type_system/bounds.yammm",
		"testdata/type_system/aliases.yammm",
		"testdata/type_system/abstract_part.yammm",
		"testdata/type_system/narrowing_valid.yammm",
	}

	for _, path := range schemas {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			loadSchema(t, path)
		})
	}
}

// TestTypeSystem_NarrowingInvalid verifies that property re-declaration (widening) fails compilation.
func TestTypeSystem_NarrowingInvalid(t *testing.T) {
	t.Parallel()
	loadSchemaExpectError(t, "testdata/type_system/narrowing_invalid.yammm")
}

// TestTypeSystem_ModifierOverrideValid verifies that promoting optional-to-required
// via re-declaration is accepted as a valid narrowing operation, and that the
// promoted field is enforced during instance validation.
func TestTypeSystem_ModifierOverrideValid(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/modifier_override.yammm")

	t.Run("valid_with_description", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Strict")
		assertValid(t, v, "Strict", records[0])
	})

	t.Run("invalid_missing_required", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Strict__invalid_missing_required")
		assertInvalid(t, v, "Strict", records[0], diag.E_MISSING_REQUIRED)
	})
}

// TestTypeSystem_BuiltinTypes tests all built-in types with valid and invalid data.
func TestTypeSystem_BuiltinTypes(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/builtin_types.yammm")

	t.Run("valid_all_types", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "AllTypes")
		assertValid(t, v, "AllTypes", records[0])
	})

	t.Run("invalid_integer_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "AllTypes__invalid_integer_bounds")
		assertInvalid(t, v, "AllTypes", records[0], diag.E_CONSTRAINT_FAIL)
	})

	t.Run("invalid_float_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "AllTypes__invalid_float_bounds")
		assertInvalid(t, v, "AllTypes", records[0], diag.E_CONSTRAINT_FAIL)
	})

	t.Run("invalid_string_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "AllTypes__invalid_string_bounds")
		assertInvalid(t, v, "AllTypes", records[0], diag.E_CONSTRAINT_FAIL)
	})

	t.Run("invalid_enum", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "AllTypes__invalid_enum")
		assertInvalid(t, v, "AllTypes", records[0], diag.E_CONSTRAINT_FAIL)
	})

	t.Run("invalid_pattern", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "AllTypes__invalid_pattern")
		assertInvalid(t, v, "AllTypes", records[0], diag.E_CONSTRAINT_FAIL)
	})

	t.Run("invalid_vector_dims", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "AllTypes__invalid_vector_dims")
		assertInvalid(t, v, "AllTypes", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestTypeSystem_Bounds tests bound variations at boundary edges.
func TestTypeSystem_Bounds(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/bounds.yammm")

	t.Run("valid_boundary_edges", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "BoundVariations")
		for _, r := range records {
			assertValid(t, v, "BoundVariations", r)
		}
	})

	t.Run("invalid_boundary_crossings", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "BoundVariations__invalid")
		for _, r := range records {
			assertInvalid(t, v, "BoundVariations", r, diag.E_CONSTRAINT_FAIL)
		}
	})
}

// TestTypeSystem_Aliases tests custom type aliases.
func TestTypeSystem_Aliases(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/aliases.yammm")

	t.Run("valid_product", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Product")
		for _, r := range records {
			assertValid(t, v, "Product", r)
		}
	})

	t.Run("invalid_money_negative", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Product__invalid_money")
		assertInvalid(t, v, "Product", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestTypeSystem_AbstractPart tests abstract type inheritance and part composition.
func TestTypeSystem_AbstractPart(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/abstract_part.yammm")

	t.Run("valid_customer", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Customer")
		assertValid(t, v, "Customer", records[0])
	})
}

// TestTypeSystem_NarrowingValid tests that constraint narrowing works:
// child re-declares parent properties with tighter bounds.
func TestTypeSystem_NarrowingValid(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/narrowing_valid.yammm")

	t.Run("valid_restricted", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Restricted")
		assertValid(t, v, "Restricted", records[0])
	})

	t.Run("invalid_parent_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Restricted__invalid_parent_bounds")
		for _, r := range records {
			assertInvalid(t, v, "Restricted", r, diag.E_CONSTRAINT_FAIL)
		}
	})

	t.Run("invalid_narrowed_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Restricted__invalid_narrowed_bounds")
		for _, r := range records {
			assertInvalid(t, v, "Restricted", r, diag.E_CONSTRAINT_FAIL)
		}
	})
}

// TestTypeSystem_ModifierOverrideInvalid verifies that demoting required-to-optional
// (removing obligation) is rejected as E_PROPERTY_CONFLICT.
func TestTypeSystem_ModifierOverrideInvalid(t *testing.T) {
	t.Parallel()
	loadSchemaExpectError(t, "testdata/type_system/modifier_override_invalid.yammm")
}
