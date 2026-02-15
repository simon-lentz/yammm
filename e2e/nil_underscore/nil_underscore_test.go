package nil_underscore_test

import (
	"context"
	"os"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/load"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadTestData reads and parses the shared JSON test data file.
func loadTestData(t *testing.T) []instance.RawInstance {
	t.Helper()

	dataPath := "data.json"
	dataBytes, err := os.ReadFile(dataPath)
	require.NoError(t, err, "read test data")

	adapter, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err, "create JSON adapter")

	sourceID := location.NewSourceID("test://data.json")
	parsed, parseResult := adapter.ParseObject(sourceID, dataBytes)
	require.True(t, parseResult.OK(), "JSON parse failed: %v", parseResult.Messages())

	records := parsed["Record"]
	require.Len(t, records, 3, "expected 3 records in test data")
	return records
}

// TestE2E_NilUnderscore tests that _ and nil behave identically as nil
// literals in invariant expressions. This is an end-to-end test that:
//  1. Loads .yammm schema files from testdata
//  2. Parses JSON instance data via the adapter
//  3. Validates instances against the schema (triggering invariant evaluation)
//  4. Compares behavior between the two syntactic forms
func TestE2E_NilUnderscore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t)

	// Both schema variants define the same invariant using only operators:
	//   ! "description_when_present" description == <nil_syntax> || description != ""
	//
	// Expected behavior per instance:
	//   valid-with-desc:    description="A non-empty description" → invariant passes
	//   valid-no-desc:      description absent (nil)              → invariant passes (nil == nil)
	//   invalid-empty-desc: description=""                        → invariant FAILS ("" != nil, "" == "")
	schemas := []struct {
		name string
		file string
	}{
		{name: "underscore (_)", file: "underscore_nil.yammm"},
		{name: "keyword (nil)", file: "keyword_nil.yammm"},
	}

	for _, sc := range schemas {
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()

			schemaPath := sc.file
			s, result, err := load.Load(ctx, schemaPath)
			require.NoError(t, err, "load schema %s", sc.file)
			require.True(t, result.OK(), "schema %s has errors: %v", sc.file, result.Messages())

			validator := instance.NewValidator(s)

			t.Run("valid_with_description", func(t *testing.T) {
				t.Parallel()
				valid, failure, err := validator.ValidateOne(ctx, "Record", records[0])
				require.NoError(t, err)
				assert.Nil(t, failure, "expected valid: description is non-empty")
				assert.NotNil(t, valid)
			})

			t.Run("valid_nil_description", func(t *testing.T) {
				t.Parallel()
				valid, failure, err := validator.ValidateOne(ctx, "Record", records[1])
				require.NoError(t, err)
				assert.Nil(t, failure, "expected valid: description is nil (absent)")
				assert.NotNil(t, valid)
			})

			t.Run("invalid_empty_description", func(t *testing.T) {
				t.Parallel()
				valid, failure, err := validator.ValidateOne(ctx, "Record", records[2])
				require.NoError(t, err)
				assert.Nil(t, valid, "expected invalid: empty description should fail invariant")
				require.NotNil(t, failure, "expected validation failure for empty description")

				hasInvariantFailure := false
				for issue := range failure.Result.Issues() {
					if issue.Code() == diag.E_INVARIANT_FAIL {
						hasInvariantFailure = true
						break
					}
				}
				assert.True(t, hasInvariantFailure,
					"expected E_INVARIANT_FAIL in diagnostics, got: %v", failure.Result.Messages())
			})
		})
	}
}

// TestE2E_BuiltinLen_Bug1 reproduces Issue 9 Bug 1: VisitFcall body normalization
// breaks callBuiltin validation. The Len builtin called via pipe operator
// (description -> Len) fails with "Len does not accept a lambda expression"
// because VisitFcall wraps the missing body as NewLiteral(nil) (non-nil pointer).
//
// This test documents the CURRENT BROKEN behavior. When Bug 1 is fixed,
// the assertions marked "BUG 1" should be inverted (see inline comments).
func TestE2E_BuiltinLen_Bug1(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	records := loadTestData(t)

	schemaPath := "builtin_len.yammm"
	s, result, err := load.Load(ctx, schemaPath)
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	validator := instance.NewValidator(s)

	t.Run("non_nil_description_triggers_bug", func(t *testing.T) {
		t.Parallel()
		// Record with description="A non-empty description".
		// The invariant: description == _ || description -> Len > 0
		// description != nil, so the LHS of || is false.
		// The RHS evaluates: description -> Len > 0
		// Bug 1: Len receives a non-nil body (NewLiteral(nil)) and errors.
		_, failure, err := validator.ValidateOne(ctx, "Record", records[0])
		require.NoError(t, err)

		// BUG 1: Should pass (Len("A non-empty description") == 24 > 0),
		// but fails with E_EVAL_ERROR due to body normalization.
		require.NotNil(t, failure, "Bug 1: Len rejects the phantom body")

		hasEvalError := false
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_EVAL_ERROR {
				hasEvalError = true
				break
			}
		}
		assert.True(t, hasEvalError,
			"expected E_EVAL_ERROR from Len body rejection, got: %v", failure.Result.Messages())
		assert.Contains(t, failure.Result.Messages()[0], "does not accept a lambda expression",
			"Bug 1 signature error message")
	})

	t.Run("nil_description_short_circuits", func(t *testing.T) {
		t.Parallel()
		// Record with no description field (nil).
		// The invariant: description == _ || description -> Len > 0
		// description == nil → LHS is true → || short-circuits.
		// Len is never called, so Bug 1 is not triggered.
		valid, failure, err := validator.ValidateOne(ctx, "Record", records[1])
		require.NoError(t, err)
		assert.Nil(t, failure, "nil description short-circuits past Len")
		assert.NotNil(t, valid)
	})

	t.Run("empty_description_triggers_bug", func(t *testing.T) {
		t.Parallel()
		// Record with description="".
		// description != nil → LHS of || is false.
		// RHS evaluates: "" -> Len > 0
		// Bug 1: Len errors with "does not accept a lambda expression".
		// Note: The error is E_EVAL_ERROR, NOT E_INVARIANT_FAIL.
		_, failure, err := validator.ValidateOne(ctx, "Record", records[2])
		require.NoError(t, err)

		// BUG 1: Should fail with E_INVARIANT_FAIL (Len("") == 0, not > 0),
		// but instead fails with E_EVAL_ERROR.
		require.NotNil(t, failure, "failure expected (either bug or correct invariant)")

		hasEvalError := false
		for issue := range failure.Result.Issues() {
			if issue.Code() == diag.E_EVAL_ERROR {
				hasEvalError = true
				break
			}
		}
		assert.True(t, hasEvalError,
			"Bug 1: expected E_EVAL_ERROR, got: %v", failure.Result.Messages())
	})
}
