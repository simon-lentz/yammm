package spec_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Severity levels — existence and distinctness
// =============================================================================

// TestDiagnostics_SeverityLevelsExist verifies that all five severity constants
// exist and have distinct numeric values.
// Source: SPEC.md §Diagnostics — "Five levels: Fatal, Error, Warning, Info, Hint"
func TestDiagnostics_SeverityLevelsExist(t *testing.T) {
	t.Parallel()

	severities := []diag.Severity{
		diag.Fatal,
		diag.Error,
		diag.Warning,
		diag.Info,
		diag.Hint,
	}

	// All five must be distinct.
	seen := map[diag.Severity]bool{}
	for _, s := range severities {
		assert.False(t, seen[s], "severity %v duplicated", s)
		seen[s] = true
	}
	assert.Len(t, seen, 5, "expected 5 distinct severity levels")

	// Verify string representations.
	assert.Equal(t, "fatal", diag.Fatal.String())
	assert.Equal(t, "error", diag.Error.String())
	assert.Equal(t, "warning", diag.Warning.String())
	assert.Equal(t, "info", diag.Info.String())
	assert.Equal(t, "hint", diag.Hint.String())
}

// =============================================================================
// Severity → OK() interaction
// =============================================================================

// TestDiagnostics_FatalMakesNotOK verifies that a Fatal issue causes OK() to
// return false.
// Source: SPEC.md §Diagnostics — "Fatal and Error make OK() false"
func TestDiagnostics_FatalMakesNotOK(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(diag.NoLimit)
	issue := diag.NewIssue(diag.Fatal, diag.E_INTERNAL, "fatal problem").Build()
	c.Collect(issue)

	result := c.Result()
	assert.False(t, result.OK(), "Fatal issue must make OK() false")
	assert.True(t, result.HasErrors(), "Fatal issue must make HasErrors() true")
}

// TestDiagnostics_ErrorMakesNotOK verifies that an Error issue causes OK() to
// return false and HasErrors() to return true.
// Source: SPEC.md §Diagnostics — "Fatal and Error make OK() false"
func TestDiagnostics_ErrorMakesNotOK(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(diag.NoLimit)
	issue := diag.NewIssue(diag.Error, diag.E_SYNTAX, "error problem").Build()
	c.Collect(issue)

	result := c.Result()
	assert.False(t, result.OK(), "Error issue must make OK() false")
	assert.True(t, result.HasErrors(), "Error issue must make HasErrors() true")
}

// TestDiagnostics_WarningStillOK verifies that Warning issues do not cause
// OK() to return false.
// Source: SPEC.md §Diagnostics — "Warning/Info/Hint leave OK() true"
func TestDiagnostics_WarningStillOK(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(diag.NoLimit)
	issue := diag.NewIssue(diag.Warning, diag.E_INTERNAL, "warning only").Build()
	c.Collect(issue)

	result := c.Result()
	assert.True(t, result.OK(), "Warning issue must not affect OK()")
	assert.False(t, result.HasErrors(), "Warning issue must not make HasErrors() true")
}

// TestDiagnostics_InfoStillOK verifies that Info issues do not cause OK() to
// return false.
// Source: SPEC.md §Diagnostics — "Warning/Info/Hint leave OK() true"
func TestDiagnostics_InfoStillOK(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(diag.NoLimit)
	issue := diag.NewIssue(diag.Info, diag.E_INTERNAL, "info only").Build()
	c.Collect(issue)

	result := c.Result()
	assert.True(t, result.OK(), "Info issue must not affect OK()")
	assert.False(t, result.HasErrors(), "Info issue must not make HasErrors() true")
}

// TestDiagnostics_HintStillOK verifies that Hint issues do not cause OK() to
// return false.
// Source: SPEC.md §Diagnostics — "Warning/Info/Hint leave OK() true"
func TestDiagnostics_HintStillOK(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(diag.NoLimit)
	issue := diag.NewIssue(diag.Hint, diag.E_INTERNAL, "hint only").Build()
	c.Collect(issue)

	result := c.Result()
	assert.True(t, result.OK(), "Hint issue must not affect OK()")
	assert.False(t, result.HasErrors(), "Hint issue must not make HasErrors() true")
}

// =============================================================================
// Result methods
// =============================================================================

// TestDiagnostics_ResultOK_Empty verifies that an empty collector produces an
// OK result.
// Source: SPEC.md §Diagnostics — "result.OK() → no fatal or error issues"
func TestDiagnostics_ResultOK_Empty(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(diag.NoLimit)
	result := c.Result()
	assert.True(t, result.OK(), "empty result must be OK")
	assert.False(t, result.HasErrors(), "empty result must not have errors")
	assert.False(t, result.LimitReached(), "empty result must not have reached limit")
	assert.Equal(t, 0, result.Len(), "empty result must have 0 issues")
}

// TestDiagnostics_ResultLimitReached verifies that LimitReached() returns true
// when the collector's issue limit is exceeded.
// Source: SPEC.md §Diagnostics — "result.LimitReached() → issue limit was reached"
func TestDiagnostics_ResultLimitReached(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(2) // limit of 2

	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "first error").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "second error").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "third error (dropped)").Build())

	result := c.Result()
	assert.True(t, result.LimitReached(), "limit of 2 with 3 issues must trigger LimitReached()")
	assert.Equal(t, 2, result.Len(), "only 2 issues should be stored")
	assert.Equal(t, 1, result.DroppedCount(), "1 issue should have been dropped")
}

// TestDiagnostics_ResultIssuesIterator verifies that Issues() yields all
// collected issues.
// Source: SPEC.md §Diagnostics — "result.Issues() → iterate over all issues"
func TestDiagnostics_ResultIssuesIterator(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "error one").Build())
	c.Collect(diag.NewIssue(diag.Warning, diag.E_INTERNAL, "warning one").Build())
	c.Collect(diag.NewIssue(diag.Info, diag.E_INTERNAL, "info one").Build())

	result := c.Result()

	// Count all issues via iterator.
	var count int
	for range result.Issues() {
		count++
	}
	assert.Equal(t, 3, count, "Issues() should yield all 3 issues")

	// Verify Errors() only yields failure-level issues.
	var errorCount int
	for range result.Errors() {
		errorCount++
	}
	assert.Equal(t, 1, errorCount, "Errors() should yield only the 1 error-level issue")

	// Verify Messages() returns only error/fatal messages.
	msgs := result.Messages()
	assert.Len(t, msgs, 1, "Messages() returns only error/fatal messages")
	assert.Equal(t, "error one", msgs[0])
}

// =============================================================================
// Diagnostic codes — triggered by real operations
// =============================================================================

// TestDiagnostics_Code_E_SYNTAX verifies that a syntax error in a schema file
// produces an E_SYNTAX diagnostic code.
// Source: SPEC.md §Diagnostics — "Each issue carries a stable Code"
func TestDiagnostics_Code_E_SYNTAX(t *testing.T) {
	t.Parallel()

	result := loadSchemaExpectError(t, "testdata/diagnostics/syntax_error.yammm")
	assertDiagHasCode(t, result, diag.E_SYNTAX)
}

// TestDiagnostics_Code_E_SYNTAX_Inline verifies E_SYNTAX via inline schema
// string with broken syntax.
func TestDiagnostics_Code_E_SYNTAX_Inline(t *testing.T) {
	t.Parallel()

	result := loadSchemaStringExpectError(t, `schema "Bad"
type { broken`, "inline_syntax_error.yammm")
	assertDiagHasCode(t, result, diag.E_SYNTAX)
}

// TestDiagnostics_Code_E_MISSING_REQUIRED verifies that missing a required
// field produces an E_MISSING_REQUIRED diagnostic code.
// Source: SPEC.md §Diagnostics — "Diagnostic code categories"
func TestDiagnostics_Code_E_MISSING_REQUIRED(t *testing.T) {
	t.Parallel()

	v := loadSchemaString(t, `schema "DiagRequired"

type Item {
    id String primary
    name String required
}
`, "diag_required.yammm")

	ctx := context.Background()
	// Missing the required "name" field (and "id" primary key)
	_, failure, err := v.ValidateOne(ctx, "Item", raw(map[string]any{
		"extra": "value",
	}))
	require.NoError(t, err)
	require.NotNil(t, failure, "expected validation failure for missing required field")
	assertDiagHasCode(t, failure.Result, diag.E_MISSING_REQUIRED)
}

// TestDiagnostics_Code_E_CONSTRAINT_FAIL verifies that a value violating a
// constraint produces an E_CONSTRAINT_FAIL diagnostic code.
// Source: SPEC.md §Diagnostics — "Diagnostic code categories"
func TestDiagnostics_Code_E_CONSTRAINT_FAIL(t *testing.T) {
	t.Parallel()

	v := loadSchemaString(t, `schema "DiagConstraint"

type Score {
    id String primary
    value Integer[0, 100]
}
`, "diag_constraint.yammm")

	ctx := context.Background()
	// value=200 exceeds the Integer[0,100] constraint
	_, failure, err := v.ValidateOne(ctx, "Score", raw(map[string]any{
		"id":    "s1",
		"value": 200,
	}))
	require.NoError(t, err)
	require.NotNil(t, failure, "expected validation failure for constraint violation")
	assertDiagHasCode(t, failure.Result, diag.E_CONSTRAINT_FAIL)
}

// TestDiagnostics_Code_E_INVARIANT_FAIL verifies that a failing invariant
// produces an E_INVARIANT_FAIL diagnostic code.
// Source: SPEC.md §Diagnostics — "Diagnostic code categories"
func TestDiagnostics_Code_E_INVARIANT_FAIL(t *testing.T) {
	t.Parallel()

	v := loadSchemaString(t, `schema "DiagInvariant"

type Range {
	id String primary
	lo Integer required
	hi Integer required
	! "lo_lt_hi" lo < hi
}
`, "diag_invariant.yammm")

	ctx := context.Background()
	// lo=10, hi=5 violates "lo < hi"
	_, failure, err := v.ValidateOne(ctx, "Range", raw(map[string]any{
		"id": "r1",
		"lo": 10,
		"hi": 5,
	}))
	require.NoError(t, err)
	require.NotNil(t, failure, "expected validation failure for invariant violation")
	assertDiagHasCode(t, failure.Result, diag.E_INVARIANT_FAIL)
}

// =============================================================================
// Renderer
// =============================================================================

// TestDiagnostics_RendererFormatResult verifies that FormatResult produces
// non-empty output for a result containing errors.
// Source: SPEC.md §Diagnostics — "Render() produces formatted text"
// Note: SPEC says Render() but actual API is FormatResult().
func TestDiagnostics_RendererFormatResult(t *testing.T) {
	t.Parallel()

	c := diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "unexpected token").Build())
	c.Collect(diag.NewIssue(diag.Warning, diag.E_INTERNAL, "something suspicious").Build())

	renderer := diag.NewRenderer()
	output := renderer.FormatResult(c.Result())
	assert.NotEmpty(t, output, "FormatResult must produce non-empty output")
	assert.Contains(t, output, "E_SYNTAX", "output should contain the error code")
	assert.Contains(t, output, "unexpected token", "output should contain the error message")
}

// TestDiagnostics_RendererFormatIssue verifies that FormatIssue produces
// non-empty output for a single issue.
// Source: SPEC.md §Diagnostics — "Renderer formats individual issues"
func TestDiagnostics_RendererFormatIssue(t *testing.T) {
	t.Parallel()

	issue := diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "expected String, got Integer").
		WithHint("check the property type").
		Build()

	renderer := diag.NewRenderer()
	output := renderer.FormatIssue(issue)
	assert.NotEmpty(t, output, "FormatIssue must produce non-empty output")
	assert.Contains(t, output, "E_TYPE_MISMATCH", "output should contain the code")
	assert.Contains(t, output, "expected String, got Integer", "output should contain the message")
	assert.Contains(t, output, "hint:", "output should contain the hint")
}
