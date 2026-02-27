package claude_plugin_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestSkillCore_SchemaCompilation verifies all SKILL.md example schemas load.
func TestSkillCore_SchemaCompilation(t *testing.T) {
	t.Parallel()

	schemas := []string{
		"testdata/skill_core/schema_structure.yammm",
		"testdata/skill_core/properties_modifiers.yammm",
		"testdata/skill_core/relationships.yammm",
		"testdata/skill_core/invariants.yammm",
		"testdata/skill_core/inheritance.yammm",
	}

	for _, path := range schemas {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			loadSchema(t, path)
		})
	}
}

// TestSkillCore_SchemaStructure tests type aliases, concrete/abstract/part types.
func TestSkillCore_SchemaStructure(t *testing.T) {
	t.Parallel()

	data := "testdata/skill_core/data.json"
	v := loadSchema(t, "testdata/skill_core/schema_structure.yammm")

	t.Run("valid_item", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Item")
		for _, r := range records {
			assertValid(t, v, "Item", r)
		}
	})

	t.Run("invalid_code_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Item__invalid_bounds")
		for _, r := range records {
			assertInvalid(t, v, "Item", r, diag.E_CONSTRAINT_FAIL)
		}
	})

	t.Run("invalid_missing_required", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Item__invalid_required")
		for _, r := range records {
			assertInvalid(t, v, "Item", r, diag.E_MISSING_REQUIRED)
		}
	})
}

// TestSkillCore_PropertiesModifiers tests primary/required/optional and type aliases.
func TestSkillCore_PropertiesModifiers(t *testing.T) {
	t.Parallel()

	data := "testdata/skill_core/data.json"
	v := loadSchema(t, "testdata/skill_core/properties_modifiers.yammm")

	t.Run("valid_full_record", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Record")
		assertValid(t, v, "Record", records[0])
	})

	t.Run("valid_minimal_record", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Record")
		assertValid(t, v, "Record", records[1])
	})

	t.Run("invalid_email_pattern", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Record__invalid_email")
		assertInvalid(t, v, "Record", records[0], diag.E_CONSTRAINT_FAIL)
	})

	t.Run("invalid_score_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Record__invalid_score")
		assertInvalid(t, v, "Record", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestSkillCore_Relationships tests association/composition compilation and property validation.
func TestSkillCore_Relationships(t *testing.T) {
	t.Parallel()

	data := "testdata/skill_core/data.json"
	v := loadSchema(t, "testdata/skill_core/relationships.yammm")

	t.Run("valid_company", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Company")
		assertValid(t, v, "Company", records[0])
	})

	t.Run("valid_employee", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Employee")
		assertValid(t, v, "Employee", records[0])
	})
}

// TestSkillCore_Invariants tests scalar and collection invariants from SKILL.md.
func TestSkillCore_Invariants(t *testing.T) {
	t.Parallel()

	data := "testdata/skill_core/data.json"
	v := loadSchema(t, "testdata/skill_core/invariants.yammm")

	t.Run("valid_product", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Product")
		for _, r := range records {
			assertValid(t, v, "Product", r)
		}
	})

	t.Run("invalid_discount_too_high", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Product__invalid_discount")
		assertInvariantFails(t, v, "Product", records[0], "discount_reasonable")
	})

	t.Run("invalid_empty_name", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Product__invalid_name")
		// Empty string "" has Len 0, violating String[1,100] constraint before invariant check.
		assertInvalid(t, v, "Product", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestSkillCore_Inheritance tests abstract extends, multiple inheritance, and narrowing.
func TestSkillCore_Inheritance(t *testing.T) {
	t.Parallel()

	data := "testdata/skill_core/data.json"
	v := loadSchema(t, "testdata/skill_core/inheritance.yammm")

	t.Run("valid_document", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Document")
		assertValid(t, v, "Document", records[0])
	})

	t.Run("invalid_missing_inherited_fields", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Document__invalid_missing_inherited")
		assertInvalid(t, v, "Document", records[0], diag.E_MISSING_REQUIRED)
	})

	t.Run("valid_adult", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Adult")
		for _, r := range records {
			assertValid(t, v, "Adult", r)
		}
	})

	t.Run("invalid_adult_parent_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Adult__invalid_parent_bounds")
		// age=-1 violates both parent [0,150] and child [18,150]
		assertInvalid(t, v, "Adult", records[0], diag.E_CONSTRAINT_FAIL)
		// age=10 violates narrowed child bounds [18,150] but would pass parent [0,150]
		assertInvalid(t, v, "Adult", records[1], diag.E_CONSTRAINT_FAIL)
	})
}
