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
		"testdata/type_system/list_types.yammm",
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

// TestTypeSystem_PKAllowedDate verifies Date is accepted as a primary key type.
func TestTypeSystem_PKAllowedDate(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/pk_date.yammm")

	records := loadTestData(t, data, "DateKeyed")
	assertValid(t, v, "DateKeyed", records[0])
}

// TestTypeSystem_PKAllowedTimestamp verifies Timestamp is accepted as a primary key type.
func TestTypeSystem_PKAllowedTimestamp(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/pk_timestamp.yammm")

	records := loadTestData(t, data, "TimestampKeyed")
	assertValid(t, v, "TimestampKeyed", records[0])
}

// TestTypeSystem_PKBannedTypes verifies all banned primary key types are rejected.
func TestTypeSystem_PKBannedTypes(t *testing.T) {
	t.Parallel()

	banned := []string{
		"testdata/type_system/pk_integer_banned.yammm",
		"testdata/type_system/pk_float_banned.yammm",
		"testdata/type_system/pk_boolean_banned.yammm",
		"testdata/type_system/pk_enum_banned.yammm",
		"testdata/type_system/pk_pattern_banned.yammm",
		"testdata/type_system/pk_vector_banned.yammm",
	}

	for _, path := range banned {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			loadSchemaExpectError(t, path)
		})
	}
}

// TestTypeSystem_PKAliasBanned verifies that a type alias resolving to a banned
// type is still rejected as primary key.
func TestTypeSystem_PKAliasBanned(t *testing.T) {
	t.Parallel()
	loadSchemaExpectError(t, "testdata/type_system/pk_alias_banned.yammm")
}

// TestTypeSystem_ListTypes tests List type variants from type-system.md.
func TestTypeSystem_ListTypes(t *testing.T) {
	t.Parallel()

	data := "testdata/type_system/data.json"
	v := loadSchema(t, "testdata/type_system/list_types.yammm")

	t.Run("valid_all_list_variants", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ListBasics")
		assertValid(t, v, "ListBasics", records[0])
	})

	t.Run("invalid_list_length_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ListBasics__invalid_bounds")
		assertInvalid(t, v, "ListBasics", records[0], diag.E_CONSTRAINT_FAIL)
	})

	t.Run("invalid_element_constraint", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ListBasics__invalid_element")
		assertInvalid(t, v, "ListBasics", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestTypeSystem_ListNarrowingInvalid verifies that changing List element type
// in a child is rejected (E_PROPERTY_CONFLICT).
func TestTypeSystem_ListNarrowingInvalid(t *testing.T) {
	t.Parallel()
	loadSchemaExpectError(t, "testdata/type_system/list_narrowing_invalid.yammm")
}

// TestTypeSystem_ListEdgeBanned verifies that List is banned in edge properties.
func TestTypeSystem_ListEdgeBanned(t *testing.T) {
	t.Parallel()
	loadSchemaExpectError(t, "testdata/type_system/list_edge_banned.yammm")
}

// TestTypeSystem_ListPKBanned verifies that List cannot be used as a primary key.
func TestTypeSystem_ListPKBanned(t *testing.T) {
	t.Parallel()
	loadSchemaExpectError(t, "testdata/type_system/list_pk_banned.yammm")
}
