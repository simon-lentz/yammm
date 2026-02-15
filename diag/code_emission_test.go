package diag_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// TestCodeEmission_AllCodes verifies that every defined code can be used
// to create a valid issue that passes through the diagnostic pipeline.
func TestCodeEmission_AllCodes(t *testing.T) {
	t.Parallel()

	codes := diag.AllCodes()
	require.NotEmpty(t, codes, "AllCodes should return all defined codes")

	for _, code := range codes {
		t.Run(code.String(), func(t *testing.T) {
			t.Parallel()
			// Create an issue with this code
			issue := diag.NewIssue(diag.Error, code, "test message for "+code.String()).Build()

			// Verify the issue is valid
			assert.True(t, issue.IsValid(), "Issue with %s should be valid", code.String())
			assert.Equal(t, code, issue.Code())
			assert.Contains(t, issue.Message(), code.String())

			// Verify it can be collected
			collector := diag.NewCollector(100)
			collector.Collect(issue)

			result := collector.Result()
			assert.True(t, result.HasErrors())

			// Verify the code round-trips
			foundCode := false
			for i := range result.Issues() {
				if i.Code() == code {
					foundCode = true
					break
				}
			}
			assert.True(t, foundCode, "Code %s should be present in result", code.String())
		})
	}
}

// TestCodeEmission_Categories verifies that each category has at least one code.
func TestCodeEmission_Categories(t *testing.T) {
	t.Parallel()

	categories := []diag.CodeCategory{
		diag.CategorySentinel,
		diag.CategorySchema,
		diag.CategorySyntax,
		diag.CategoryImport,
		diag.CategoryInstance,
		diag.CategoryGraph,
		diag.CategoryAdapter,
	}

	for _, cat := range categories {
		t.Run(cat.String(), func(t *testing.T) {
			t.Parallel()
			codes := diag.CodesByCategory(cat)
			assert.NotEmpty(t, codes, "Category %s should have at least one code", cat.String())
		})
	}
}

// TestCodeEmission_Uniqueness verifies that all code string values are unique.
func TestCodeEmission_Uniqueness(t *testing.T) {
	t.Parallel()

	codes := diag.AllCodes()
	seen := make(map[string]bool)

	for _, code := range codes {
		str := code.String()
		assert.False(t, seen[str], "Duplicate code string: %s", str)
		seen[str] = true
	}
}

// TestCodeEmission_SentinelCodes verifies the sentinel codes behave correctly.
func TestCodeEmission_SentinelCodes(t *testing.T) {
	t.Parallel()

	t.Run("E_LIMIT_REACHED", func(t *testing.T) {
		t.Parallel()
		issue := diag.NewIssue(diag.Fatal, diag.E_LIMIT_REACHED, "limit reached").Build()
		assert.Equal(t, diag.E_LIMIT_REACHED, issue.Code())
		assert.Equal(t, diag.Fatal, issue.Severity())
	})

	t.Run("E_INTERNAL", func(t *testing.T) {
		t.Parallel()
		issue := diag.NewIssue(diag.Error, diag.E_INTERNAL, "internal error").Build()
		assert.Equal(t, diag.E_INTERNAL, issue.Code())
	})
}

// TestCodeEmission_WithSpan verifies codes work with source spans.
func TestCodeEmission_WithSpan(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://code_test.yammm")
	span := location.Range(sourceID, 1, 1, 1, 10)

	codes := []diag.Code{
		diag.E_SYNTAX,
		diag.E_TYPE_MISMATCH,
		diag.E_MISSING_REQUIRED,
		diag.E_DUPLICATE_PK,
	}

	for _, code := range codes {
		t.Run(code.String(), func(t *testing.T) {
			t.Parallel()
			issue := diag.NewIssue(diag.Error, code, "test message").
				WithSpan(span).
				Build()

			assert.Equal(t, span, issue.Span())
			assert.Equal(t, code, issue.Code())
		})
	}
}

// TestCodeEmission_WithDetails verifies codes work with detail fields.
func TestCodeEmission_WithDetails(t *testing.T) {
	t.Parallel()

	issue := diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "type mismatch").
		WithExpectedGot("string", "number").
		WithDetail("property", "age").
		Build()

	assert.Equal(t, diag.E_TYPE_MISMATCH, issue.Code())

	// Check details by iterating
	details := issue.Details()
	detailMap := make(map[string]string)
	for _, d := range details {
		detailMap[d.Key] = d.Value
	}
	assert.Equal(t, "string", detailMap["expected"])
	assert.Equal(t, "number", detailMap["got"])
	assert.Equal(t, "age", detailMap["property"])
}

// TestCodeEmission_SchemaCodes verifies schema codes can be created.
func TestCodeEmission_SchemaCodes(t *testing.T) {
	t.Parallel()

	codes := diag.CodesByCategory(diag.CategorySchema)
	require.NotEmpty(t, codes)

	for _, code := range codes {
		assert.Equal(t, diag.CategorySchema, code.Category())
	}
}

// TestCodeEmission_InstanceCodes verifies instance codes can be created.
func TestCodeEmission_InstanceCodes(t *testing.T) {
	t.Parallel()

	codes := diag.CodesByCategory(diag.CategoryInstance)
	require.NotEmpty(t, codes)

	for _, code := range codes {
		assert.Equal(t, diag.CategoryInstance, code.Category())
	}
}

// TestCodeEmission_GraphCodes verifies graph codes can be created.
func TestCodeEmission_GraphCodes(t *testing.T) {
	t.Parallel()

	codes := diag.CodesByCategory(diag.CategoryGraph)
	require.NotEmpty(t, codes)

	for _, code := range codes {
		assert.Equal(t, diag.CategoryGraph, code.Category())
	}
}

// TestCodeEmission_AdapterCodes verifies adapter codes can be created.
func TestCodeEmission_AdapterCodes(t *testing.T) {
	t.Parallel()

	codes := diag.CodesByCategory(diag.CategoryAdapter)
	require.NotEmpty(t, codes)

	for _, code := range codes {
		assert.Equal(t, diag.CategoryAdapter, code.Category())
	}
}

// TestCodeEmission_ImportCodes verifies import codes can be created.
func TestCodeEmission_ImportCodes(t *testing.T) {
	t.Parallel()

	codes := diag.CodesByCategory(diag.CategoryImport)
	require.NotEmpty(t, codes)

	for _, code := range codes {
		assert.Equal(t, diag.CategoryImport, code.Category())
	}
}

// TestCodeEmission_ZeroCode verifies zero code behavior.
func TestCodeEmission_ZeroCode(t *testing.T) {
	t.Parallel()

	var zeroCode diag.Code
	assert.True(t, zeroCode.IsZero())
	assert.Equal(t, "", zeroCode.String())
}

// TestCodeEmission_SpecificCodes tests specific codes mentioned in the architecture.
func TestCodeEmission_SpecificCodes(t *testing.T) {
	t.Parallel()

	// These are codes specifically mentioned in the architecture spec
	specificCodes := []struct {
		code        diag.Code
		category    diag.CodeCategory
		description string
	}{
		{diag.E_CASE_FOLD_COLLISION, diag.CategoryInstance, "case folding collision"},
		{diag.E_MISSING_TYPE_TAG, diag.CategoryInstance, "missing $type tag"},
		{diag.E_INVALID_TYPE_TAG, diag.CategoryInstance, "invalid $type tag format"},
		{diag.E_MISSING_SOURCE_ID, diag.CategorySchema, "missing source ID"},
		{diag.E_INVALID_SYNTHETIC_ID, diag.CategorySchema, "invalid synthetic source ID"},
		{diag.E_RESERVED_PREFIX, diag.CategorySchema, "reserved prefix usage"},
		{diag.E_CASE_COLLISION, diag.CategorySchema, "case collision in names"},
	}

	for _, tc := range specificCodes {
		t.Run(tc.code.String(), func(t *testing.T) {
			t.Parallel()
			assert.False(t, tc.code.IsZero(), "Code should not be zero")
			assert.Equal(t, tc.category, tc.code.Category(), "Category mismatch")

			// Create an issue with this code
			issue := diag.NewIssue(diag.Error, tc.code, tc.description).Build()
			assert.True(t, issue.IsValid())
		})
	}
}

// TestCodeEmission_CollectorPreservesCode verifies the collector preserves codes.
func TestCodeEmission_CollectorPreservesCode(t *testing.T) {
	t.Parallel()

	collector := diag.NewCollector(100)

	// Add issues with different codes
	codes := []diag.Code{
		diag.E_TYPE_MISMATCH,
		diag.E_MISSING_REQUIRED,
		diag.E_DUPLICATE_PK,
		diag.E_SYNTAX,
	}

	for _, code := range codes {
		issue := diag.NewIssue(diag.Error, code, "test "+code.String()).Build()
		collector.Collect(issue)
	}

	result := collector.Result()
	assert.True(t, result.HasErrors())

	// Verify each code is present
	collectedCodes := make(map[string]bool)
	for issue := range result.Issues() {
		collectedCodes[issue.Code().String()] = true
	}

	for _, code := range codes {
		assert.True(t, collectedCodes[code.String()], "Code %s should be in result", code.String())
	}
}

// TestCodeEmission_ResultFilterByCode tests filtering issues by code.
func TestCodeEmission_ResultFilterByCode(t *testing.T) {
	t.Parallel()

	collector := diag.NewCollector(100)
	collector.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "type error 1").Build())
	collector.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "type error 2").Build())
	collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "syntax error").Build())

	result := collector.Result()

	// Count issues by code
	typeMismatchCount := 0
	syntaxCount := 0
	for issue := range result.Issues() {
		switch issue.Code() {
		case diag.E_TYPE_MISMATCH:
			typeMismatchCount++
		case diag.E_SYNTAX:
			syntaxCount++
		}
	}

	assert.Equal(t, 2, typeMismatchCount)
	assert.Equal(t, 1, syntaxCount)
}
