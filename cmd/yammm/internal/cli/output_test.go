package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
)

func TestParseOutputFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    OutputFormat
		wantErr bool
	}{
		{"text", FormatText, false},
		{"json", FormatJSON, false},
		{"yaml", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseOutputFormat(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExitForResult(t *testing.T) {
	t.Parallel()

	t.Run("ok result", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, ExitOK, ExitForResult(diag.Result{}))
	})

	t.Run("error result", func(t *testing.T) {
		t.Parallel()
		c := diag.NewCollectorUnlimited()
		c.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL, "test error").Build())
		assert.Equal(t, ExitValidation, ExitForResult(c.Result()))
	})
}

func TestRenderResult_Text(t *testing.T) {
	t.Parallel()

	c := diag.NewCollectorUnlimited()
	c.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL, "something went wrong").Build())

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatText, c.Result())
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "something went wrong")
}

func TestRenderResult_JSON(t *testing.T) {
	t.Parallel()

	c := diag.NewCollectorUnlimited()
	c.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL, "json test").Build())

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatJSON, c.Result())
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"json test"`)
	assert.Contains(t, buf.String(), `"severity":"error"`)
}

func TestRenderResult_OKResultWritesNothing(t *testing.T) {
	t.Parallel()

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatText, diag.Result{})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestExitError(t *testing.T) {
	t.Parallel()

	e := &ExitError{Code: ExitValidation}
	assert.Equal(t, "validation errors found", e.Error())

	e2 := &ExitError{Code: ExitUsage}
	assert.Equal(t, "usage error", e2.Error())

	e3 := &ExitError{Code: ExitRuntime}
	assert.Equal(t, "runtime error", e3.Error())
}
