package claude_plugin_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestSchemaAuthor_SchemaCompilation verifies the schema-author.md quick reference compiles.
func TestSchemaAuthor_SchemaCompilation(t *testing.T) {
	t.Parallel()
	loadSchema(t, "testdata/schema_author/quick_reference.yammm")
}

// TestSchemaAuthor_Customer tests Customer type with inherited audit fields and email pattern.
func TestSchemaAuthor_Customer(t *testing.T) {
	t.Parallel()

	data := "testdata/schema_author/data.json"
	v := loadSchema(t, "testdata/schema_author/quick_reference.yammm")

	t.Run("valid_customers", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Customer")
		for _, r := range records {
			assertValid(t, v, "Customer", r)
		}
	})

	t.Run("invalid_email", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Customer__invalid_email")
		assertInvalid(t, v, "Customer", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestSchemaAuthor_Address tests Address type with zip code bounds.
func TestSchemaAuthor_Address(t *testing.T) {
	t.Parallel()

	data := "testdata/schema_author/data.json"
	v := loadSchema(t, "testdata/schema_author/quick_reference.yammm")

	t.Run("valid_address", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Address")
		assertValid(t, v, "Address", records[0])
	})

	t.Run("invalid_zip_too_short", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Address__invalid_zip")
		assertInvalid(t, v, "Address", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestSchemaAuthor_Order tests Order type with enum status and composition invariants.
// NOTE: Order has composition invariants (has_items, all_positive_qty) that evaluate
// against empty ITEMS at instance level. Valid Order instances without graph-layer
// composition data will fail has_items. We test this expected behavior and separately
// test the enum constraint violation.
func TestSchemaAuthor_Order(t *testing.T) {
	t.Parallel()

	data := "testdata/schema_author/data.json"
	v := loadSchema(t, "testdata/schema_author/quick_reference.yammm")

	t.Run("order_has_items_invariant_fires_without_composition", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Order")
		// Without composition data, ITEMS is empty → has_items fails (expected behavior)
		assertInvariantFails(t, v, "Order", records[0], "has_items")
	})

	t.Run("invalid_status", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Order__invalid_status")
		assertInvalid(t, v, "Order", records[0], diag.E_CONSTRAINT_FAIL)
	})
}
