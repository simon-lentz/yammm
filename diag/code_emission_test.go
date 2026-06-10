package diag_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
)

// TestCodeEmission_AllCodes verifies that every defined code can be used to
// create a valid issue that survives a Collector round-trip — the one
// property the registry guards in code_test.go cannot check. (Uniqueness,
// category membership, and specific-code existence are pinned there.)
func TestCodeEmission_AllCodes(t *testing.T) {
	t.Parallel()

	codes := diag.AllCodes()
	require.NotEmpty(t, codes, "AllCodes should return all defined codes")

	for _, code := range codes {
		t.Run(code.String(), func(t *testing.T) {
			t.Parallel()
			issue := diag.NewIssue(diag.Error, code, "test message for "+code.String()).Build()

			assert.True(t, issue.IsValid(), "Issue with %s should be valid", code.String())
			assert.Equal(t, code, issue.Code())
			assert.Contains(t, issue.Message(), code.String())

			collector := diag.NewCollector(100)
			collector.Collect(issue)

			result := collector.Result()
			assert.True(t, result.HasErrors())

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

// TestCodeEmission_ResultFilterByCode pins that same-code issues accumulate
// independently and remain distinguishable by code in the result.
func TestCodeEmission_ResultFilterByCode(t *testing.T) {
	t.Parallel()

	collector := diag.NewCollector(100)
	collector.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "type error 1").Build())
	collector.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "type error 2").Build())
	collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "syntax error").Build())

	result := collector.Result()

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
