package claude_plugin_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestPatterns_SchemaCompilation verifies all patterns.md schemas load.
func TestPatterns_SchemaCompilation(t *testing.T) {
	t.Parallel()

	schemas := []string{
		"testdata/patterns/audit_fields.yammm",
		"testdata/patterns/soft_delete.yammm",
		"testdata/patterns/normalization.yammm",
		"testdata/patterns/identifiers.yammm",
		"testdata/patterns/enumerations.yammm",
		"testdata/patterns/cross_field.yammm",
		"testdata/patterns/collection_invariants.yammm",
		"testdata/patterns/relationships.yammm",
		"testdata/patterns/edge_properties.yammm",
		"testdata/patterns/list_patterns.yammm",
	}

	for _, path := range schemas {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			loadSchema(t, path)
		})
	}
}

// TestPatterns_AuditFields tests audit field abstract type inheritance.
func TestPatterns_AuditFields(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/audit_fields.yammm")

	t.Run("valid_article", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Article")
		assertValid(t, v, "Article", records[0])
	})
}

// TestPatterns_SoftDelete tests the soft delete invariant pattern.
func TestPatterns_SoftDelete(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/soft_delete.yammm")

	t.Run("valid_active", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Account")
		assertValid(t, v, "Account", records[0])
	})

	t.Run("valid_deactivated", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Account")
		assertValid(t, v, "Account", records[1])
	})

	t.Run("invalid_deactivation_missing_timestamp", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Account__invalid_deactivation")
		assertInvariantFails(t, v, "Account", records[0], "deactivation_consistency")
	})
}

// TestPatterns_Normalization tests the normalization invariant pattern.
func TestPatterns_Normalization(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/normalization.yammm")

	t.Run("valid_lowercase_norm", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Organization")
		assertValid(t, v, "Organization", records[0])
	})

	t.Run("invalid_mixed_case_norm", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Organization__invalid_mixed_case")
		assertInvariantFails(t, v, "Organization", records[0], "norm_is_lowercase")
	})
}

// TestPatterns_Identifiers tests identifier patterns and composite key invariants.
func TestPatterns_Identifiers(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"

	t.Run("valid_user", func(t *testing.T) {
		t.Parallel()
		v := loadSchema(t, "testdata/patterns/identifiers.yammm")
		records := loadTestData(t, data, "User")
		assertValid(t, v, "User", records[0])
	})

	t.Run("valid_product_sku", func(t *testing.T) {
		t.Parallel()
		v := loadSchema(t, "testdata/patterns/identifiers.yammm")
		records := loadTestData(t, data, "Product")
		for _, r := range records {
			assertValid(t, v, "Product", r)
		}
	})

	t.Run("invalid_sku_bounds", func(t *testing.T) {
		t.Parallel()
		v := loadSchema(t, "testdata/patterns/identifiers.yammm")
		records := loadTestData(t, data, "Product__invalid_sku_bounds")
		for _, r := range records {
			assertInvalid(t, v, "Product", r, diag.E_CONSTRAINT_FAIL)
		}
	})

	t.Run("valid_enrollment", func(t *testing.T) {
		t.Parallel()
		v := loadSchema(t, "testdata/patterns/identifiers.yammm")
		records := loadTestData(t, data, "Enrollment")
		assertValid(t, v, "Enrollment", records[0])
	})

	t.Run("invalid_empty_composite_id", func(t *testing.T) {
		t.Parallel()
		v := loadSchema(t, "testdata/patterns/identifiers.yammm")
		records := loadTestData(t, data, "Enrollment__invalid_empty_id")
		assertInvariantFails(t, v, "Enrollment", records[0], "composite_populated")
	})
}

// TestPatterns_Enumerations tests enum patterns and enum aliases.
func TestPatterns_Enumerations(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/enumerations.yammm")

	t.Run("valid_task", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Task")
		assertValid(t, v, "Task", records[0])
	})

	t.Run("invalid_task_enum", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Task__invalid_enum")
		assertInvalid(t, v, "Task", records[0], diag.E_CONSTRAINT_FAIL)
	})

	t.Run("valid_incident", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Incident")
		assertValid(t, v, "Incident", records[0])
	})
}

// TestPatterns_CrossField_DateRange tests date range cross-field invariant.
func TestPatterns_CrossField_DateRange(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/cross_field.yammm")

	t.Run("valid_with_end_date", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Event")
		assertValid(t, v, "Event", records[0])
	})

	t.Run("valid_nil_end_date", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Event")
		assertValid(t, v, "Event", records[1])
	})

	t.Run("invalid_inverted_dates", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Event__invalid_dates")
		assertInvariantFails(t, v, "Event", records[0], "end_after_start")
	})
}

// TestPatterns_CrossField_ConditionalRequirement tests conditional field requirement invariants.
func TestPatterns_CrossField_ConditionalRequirement(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/cross_field.yammm")

	t.Run("valid_payments", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Payment")
		for _, r := range records {
			assertValid(t, v, "Payment", r)
		}
	})

	t.Run("invalid_card_no_number", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Payment__invalid_card_no_number")
		assertInvariantFails(t, v, "Payment", records[0], "card_requires_number")
	})

	t.Run("invalid_wire_no_ref", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Payment__invalid_wire_no_ref")
		assertInvariantFails(t, v, "Payment", records[0], "wire_requires_ref")
	})
}

// TestPatterns_CrossField_MutualExclusion tests mutual exclusion invariant.
func TestPatterns_CrossField_MutualExclusion(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/cross_field.yammm")

	t.Run("valid_features", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Feature")
		for _, r := range records {
			assertValid(t, v, "Feature", r)
		}
	})

	t.Run("invalid_both_true", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Feature__invalid_both")
		assertInvariantFails(t, v, "Feature", records[0], "not_both")
	})
}

// TestPatterns_CrossField_PercentageSum tests percentage sum invariant.
func TestPatterns_CrossField_PercentageSum(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/cross_field.yammm")

	t.Run("valid_allocation", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Allocation")
		assertValid(t, v, "Allocation", records[0])
	})

	t.Run("invalid_sum_not_100", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Allocation__invalid_sum")
		assertInvariantFails(t, v, "Allocation", records[0], "pct_total")
	})
}

// TestPatterns_CollectionInvariants tests compilation of collection invariant schemas.
// NOTE: Collection invariants via compositions require graph-layer testing for full
// instance validation. This test verifies the schemas compile correctly.
func TestPatterns_CollectionInvariants(t *testing.T) {
	t.Parallel()
	loadSchema(t, "testdata/patterns/collection_invariants.yammm")
}

// TestPatterns_Relationships tests relationship schema compilation and property validation.
func TestPatterns_Relationships(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/relationships.yammm")

	t.Run("valid_author", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Author")
		assertValid(t, v, "Author", records[0])
	})

	t.Run("valid_book", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Book")
		assertValid(t, v, "Book", records[0])
	})

	t.Run("valid_student", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Student")
		assertValid(t, v, "Student", records[0])
	})

	t.Run("valid_enrollment", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ManyToManyEnrollment")
		assertValid(t, v, "ManyToManyEnrollment", records[0])
	})
}

// TestPatterns_EdgeProperties tests edge property schema compilation and property validation.
func TestPatterns_EdgeProperties(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/edge_properties.yammm")

	t.Run("valid_person", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Person")
		for _, r := range records {
			assertValid(t, v, "Person", r)
		}
	})

	t.Run("valid_employee", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Employee")
		assertValid(t, v, "Employee", records[0])
	})
}

// TestPatterns_ListTags tests tag/multi-value List pattern from patterns.md.
func TestPatterns_ListTags(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/list_patterns.yammm")

	t.Run("valid_article", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ListArticle")
		assertValid(t, v, "ListArticle", records[0])
	})

	t.Run("invalid_categories_bounds", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ListArticle__invalid_categories_bounds")
		assertInvalid(t, v, "ListArticle", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestPatterns_ListNumeric tests bounded numeric List with invariants from patterns.md.
func TestPatterns_ListNumeric(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/list_patterns.yammm")

	t.Run("valid_survey", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Survey")
		assertValid(t, v, "Survey", records[0])
	})

	t.Run("invalid_empty_scores", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Survey__invalid_empty_scores")
		assertInvalid(t, v, "Survey", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestPatterns_ListAlias tests List with alias element type from patterns.md.
func TestPatterns_ListAlias(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/list_patterns.yammm")

	t.Run("valid_contact", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ContactCard")
		assertValid(t, v, "ContactCard", records[0])
	})

	t.Run("invalid_email", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ContactCard__invalid_email")
		assertInvalid(t, v, "ContactCard", records[0], diag.E_CONSTRAINT_FAIL)
	})
}

// TestPatterns_ListInvariants tests collection invariants on List from patterns.md.
func TestPatterns_ListInvariants(t *testing.T) {
	t.Parallel()

	data := "testdata/patterns/data.json"
	v := loadSchema(t, "testdata/patterns/list_patterns.yammm")

	t.Run("valid_config", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Config")
		assertValid(t, v, "Config", records[0])
	})

	t.Run("invalid_empty_host", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "Config__invalid_empty_host")
		assertInvariantFails(t, v, "Config", records[0], "no_empty_hosts")
	})
}
