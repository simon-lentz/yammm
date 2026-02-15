package instance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
)

func TestValidationFailure_Error(t *testing.T) {
	t.Run("with_errors", func(t *testing.T) {
		collector := diag.NewCollector(0)
		collector.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "first error").Build())
		collector.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "second error").Build())

		raw := instance.RawInstance{Properties: map[string]any{"id": int64(1)}}
		failure := instance.NewValidationFailure(raw, collector.Result())

		// Should return the first error message
		assert.Equal(t, "first error", failure.Error())
	})

	t.Run("with_only_warning", func(t *testing.T) {
		collector := diag.NewCollector(0)
		collector.Collect(diag.NewIssue(diag.Warning, diag.E_TYPE_MISMATCH, "just a warning").Build())

		raw := instance.RawInstance{Properties: map[string]any{"id": int64(1)}}
		failure := instance.NewValidationFailure(raw, collector.Result())

		// Warning is not a failure severity, so Error() returns empty
		assert.Equal(t, "", failure.Error())
	})

	t.Run("empty_result", func(t *testing.T) {
		collector := diag.NewCollector(0)
		raw := instance.RawInstance{Properties: map[string]any{"id": int64(1)}}
		failure := instance.NewValidationFailure(raw, collector.Result())

		assert.Equal(t, "", failure.Error())
	})
}

func TestValidationFailure_HasErrors(t *testing.T) {
	t.Run("with_errors", func(t *testing.T) {
		collector := diag.NewCollector(0)
		collector.Collect(diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "an error").Build())

		raw := instance.RawInstance{Properties: map[string]any{"id": int64(1)}}
		failure := instance.NewValidationFailure(raw, collector.Result())

		assert.True(t, failure.HasErrors())
	})

	t.Run("no_errors", func(t *testing.T) {
		collector := diag.NewCollector(0)

		raw := instance.RawInstance{Properties: map[string]any{"id": int64(1)}}
		failure := instance.NewValidationFailure(raw, collector.Result())

		assert.False(t, failure.HasErrors())
	})

	t.Run("with_warnings_only", func(t *testing.T) {
		collector := diag.NewCollector(0)
		collector.Collect(diag.NewIssue(diag.Warning, diag.E_TYPE_MISMATCH, "just a warning").Build())

		raw := instance.RawInstance{Properties: map[string]any{"id": int64(1)}}
		failure := instance.NewValidationFailure(raw, collector.Result())

		// Warnings only - OK() returns true, so HasErrors() returns false
		assert.False(t, failure.HasErrors())
	})
}
