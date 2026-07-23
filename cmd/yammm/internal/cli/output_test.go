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

// truncatedResult builds a Result whose collector hit its issue limit:
// `total` issues of the given severity collected against a limit of `limit`.
func truncatedResult(severity diag.Severity, total, limit int) diag.Result {
	c := diag.NewCollector(limit)
	for i := range total {
		c.Collect(diag.NewIssue(severity, diag.E_INTERNAL,
			"issue "+string(rune('a'+i%26))).Build())
	}
	return c.Result()
}

func TestRenderResult_Text_SurfacesTruncation(t *testing.T) {
	t.Parallel()

	res := truncatedResult(diag.Error, 7, 5)
	require.True(t, res.LimitReached())

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatText, res)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "2 more issue(s) dropped after reaching the 5-issue limit",
		"the text format must say how many issues fell past the limit")
}

func TestRenderResult_Text_SurfacesTruncationOnOKResult(t *testing.T) {
	t.Parallel()

	// All-warning truncation: the result is OK (no errors), and the drop must be
	// visible alongside the retained warnings.
	res := truncatedResult(diag.Warning, 7, 5)
	require.True(t, res.LimitReached())
	require.True(t, res.OK())

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatText, res)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "2 more issue(s) dropped after reaching the 5-issue limit",
		"truncation must not hide behind the no-errors early return")
}

func TestRenderResult_JSON_CarriesTruncationFields(t *testing.T) {
	t.Parallel()

	res := truncatedResult(diag.Error, 7, 5)

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatJSON, res)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"limitReached":true`)
	assert.Contains(t, buf.String(), `"droppedCount":2`)
}

func TestRenderResult_JSON_SurfacesTruncationOnOKResult(t *testing.T) {
	t.Parallel()

	// All-warning truncation: the result is OK, and JSON must emit the truncation
	// wire fields, or a machine consumer cannot distinguish a truncated result
	// from a clean one.
	res := truncatedResult(diag.Warning, 8, 6)
	require.True(t, res.LimitReached())
	require.True(t, res.OK())

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatJSON, res)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String(), "JSON output for a truncated result must not be empty")
	assert.Contains(t, buf.String(), `"limitReached":true`)
	assert.Contains(t, buf.String(), `"droppedCount":2`)
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

// A warnings-only result renders. A load warning exists because the loader chose
// not to reject, so gating rendering on error severity would make every warning
// unreachable from the CLI — including the only signal that an inherited
// annotation was silently dropped.
func TestRenderResult_Text_RendersWarningOnlyResult(t *testing.T) {
	t.Parallel()

	c := diag.NewCollectorUnlimited()
	c.Collect(diag.NewIssue(diag.Warning, diag.W_ANNOTATION_SHADOWED, "dropped an inherited annotation").Build())
	res := c.Result()
	require.True(t, res.OK(), "precondition: a warnings-only result is OK")

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatText, res)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "dropped an inherited annotation")
}

func TestRenderResult_JSON_RendersWarningOnlyResult(t *testing.T) {
	t.Parallel()

	c := diag.NewCollectorUnlimited()
	c.Collect(diag.NewIssue(diag.Warning, diag.W_ANNOTATION_SHADOWED, "dropped an inherited annotation").Build())

	renderer := diag.NewRenderer()
	var buf bytes.Buffer
	err := RenderResult(&buf, renderer, FormatJSON, c.Result())
	require.NoError(t, err)
	assert.Contains(t, buf.String(), `"severity":"warning"`)
}
