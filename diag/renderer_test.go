package diag

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
)

// formatIssue renders one issue as text — the per-issue shape
// [Renderer.FormatResult] emits per line.
func formatIssue(r *Renderer, issue Issue) string {
	var sb strings.Builder
	r.formatIssueToBuilder(&sb, issue)
	return sb.String()
}

// mockSourceProvider is a test implementation of SourceProvider.
type mockSourceProvider struct {
	sources map[location.SourceID][]byte
}

func newMockSourceProvider() *mockSourceProvider {
	return &mockSourceProvider{
		sources: make(map[location.SourceID][]byte),
	}
}

func (m *mockSourceProvider) Add(source location.SourceID, content string) {
	m.sources[source] = []byte(content)
}

func (m *mockSourceProvider) Content(span location.Span) ([]byte, bool) {
	content, ok := m.sources[span.Source]
	return content, ok
}

func TestNewRenderer_Defaults(t *testing.T) {
	r := NewRenderer()

	// Test default configuration via output behavior
	issue := NewIssue(Error, E_SYNTAX, "test error").Build()
	output := formatIssue(r, issue)

	// Should have basic format without excerpts
	if !strings.Contains(output, "error") {
		t.Error("output should contain severity")
	}
	if !strings.Contains(output, "E_SYNTAX") {
		t.Error("output should contain code")
	}
	if !strings.Contains(output, "test error") {
		t.Error("output should contain message")
	}
}

func TestRenderer_WithSourceProvider_Nil(t *testing.T) {
	// WithSourceProvider(nil) should be safe
	r := NewRenderer(WithSourceProvider(nil), WithExcerpts(true))

	source := location.MustNewSourceID("test://file.yammm")
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	// Should not panic, just skip excerpts
	output := formatIssue(r, issue)
	if output == "" {
		t.Error("output should not be empty")
	}
}

func TestRenderer_WithExcerpts(t *testing.T) {
	provider := newMockSourceProvider()
	source := location.MustNewSourceID("test://file.yammm")
	provider.Add(source, "line one\nline two\nline three\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
	)

	issue := NewIssue(Error, E_SYNTAX, "error on line 2").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 2, Column: 1},
			End:    location.Position{Line: 2, Column: 5},
		}).
		Build()

	output := formatIssue(r, issue)

	// Should contain excerpt
	if !strings.Contains(output, "line two") {
		t.Errorf("output should contain source line, got: %s", output)
	}
	if !strings.Contains(output, "^^^^") {
		t.Errorf("output should contain underline, got: %s", output)
	}
}

func TestRenderer_WithExcerpts_Disabled(t *testing.T) {
	provider := newMockSourceProvider()
	source := location.MustNewSourceID("test://file.yammm")
	provider.Add(source, "source content\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(false), // Explicitly disabled
	)

	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	output := formatIssue(r, issue)

	// Should NOT contain excerpt
	if strings.Contains(output, "source content") {
		t.Error("excerpts should be disabled")
	}
}

func TestRenderer_WithColors(t *testing.T) {
	r := NewRenderer(WithColors(true))

	tests := []struct {
		severity Severity
		ansi     string
	}{
		{Fatal, "\033[1;31m"},   // Bold red
		{Error, "\033[1;31m"},   // Bold red
		{Warning, "\033[1;33m"}, // Bold yellow
		{Info, "\033[1;36m"},    // Bold cyan
		{Hint, "\033[1;32m"},    // Bold green
	}

	for _, tt := range tests {
		t.Run(tt.severity.String(), func(t *testing.T) {
			issue := NewIssue(tt.severity, E_SYNTAX, "message").Build()
			output := formatIssue(r, issue)

			if !strings.Contains(output, tt.ansi) {
				t.Errorf("output should contain ANSI code %q for %s", tt.ansi, tt.severity)
			}
			if !strings.Contains(output, "\033[0m") {
				t.Error("output should contain ANSI reset")
			}
		})
	}
}

func TestRenderer_WithColors_Disabled(t *testing.T) {
	r := NewRenderer(WithColors(false))

	issue := NewIssue(Error, E_SYNTAX, "error").Build()
	output := formatIssue(r, issue)

	if strings.Contains(output, "\033[") {
		t.Error("output should not contain ANSI codes when colors disabled")
	}
}

func TestRenderer_WithDistinguishFatal(t *testing.T) {
	issue := NewIssue(Fatal, E_INTERNAL, "limit").Build()

	// Default: Fatal renders as "error"
	r1 := NewRenderer()
	output1 := formatIssue(r1, issue)
	if !strings.Contains(output1, ": error[") {
		t.Errorf("Fatal should render as 'error' by default, got: %s", output1)
	}

	// With distinguish: Fatal renders as "fatal"
	r2 := NewRenderer(WithDistinguishFatal(true))
	output2 := formatIssue(r2, issue)
	if !strings.Contains(output2, ": fatal[") {
		t.Errorf("Fatal should render as 'fatal' when distinguished, got: %s", output2)
	}
}

func TestRenderer_FormatIssue_Location(t *testing.T) {
	tests := []struct {
		name     string
		issue    Issue
		contains string
	}{
		{
			name: "with span",
			issue: NewIssue(Error, E_SYNTAX, "msg").
				WithSpan(location.Point(location.MustNewSourceID("test://a.yammm"), 10, 5)).
				Build(),
			contains: "test://a.yammm:10:5",
		},
		{
			name: "with path only",
			issue: NewIssue(Error, E_SYNTAX, "msg").
				WithPath("data.json", "$.items[0]").
				Build(),
			contains: "data.json", // path is shown, sourceName prefix comes first if present
		},
		{
			name:     "unknown location",
			issue:    NewIssue(Error, E_SYNTAX, "msg").Build(),
			contains: "<unknown>",
		},
	}

	r := NewRenderer()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := formatIssue(r, tt.issue)
			if !strings.Contains(output, tt.contains) {
				t.Errorf("output should contain %q, got: %s", tt.contains, output)
			}
		})
	}
}

func TestRenderer_FormatIssue_Hint(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "error message").
		WithHint("try doing X instead").
		Build()

	r := NewRenderer()
	output := formatIssue(r, issue)

	if !strings.Contains(output, "hint: try doing X instead") {
		t.Errorf("output should contain hint, got: %s", output)
	}
}

func TestRenderer_FormatIssue_Related(t *testing.T) {
	source := location.MustNewSourceID("test://related.yammm")
	issue := NewIssue(Error, E_DUPLICATE_TYPE, "type collision").
		WithRelated(location.RelatedInfo{
			Message: "first definition here",
			Span:    location.Point(source, 5, 1),
		}).
		Build()

	r := NewRenderer()
	output := formatIssue(r, issue)

	if !strings.Contains(output, "note: first definition here") {
		t.Errorf("output should contain related note, got: %s", output)
	}
	if !strings.Contains(output, "test://related.yammm:5:1") {
		t.Errorf("output should contain related location, got: %s", output)
	}
}

func TestRenderer_FormatResult(t *testing.T) {
	c := NewCollector(0)
	c.Collect(NewIssue(Error, E_SYNTAX, "first error").Build())
	c.Collect(NewIssue(Warning, E_INVALID_NAME, "warning").Build())
	c.Collect(NewIssue(Error, E_DUPLICATE_TYPE, "second error").Build())

	r := NewRenderer()
	output := r.FormatResult(c.Result())

	// Should contain all issues separated by newlines
	if !strings.Contains(output, "first error") {
		t.Error("output should contain first error")
	}
	if !strings.Contains(output, "warning") {
		t.Error("output should contain warning")
	}
	if !strings.Contains(output, "second error") {
		t.Error("output should contain second error")
	}
}

func TestRenderer_FormatResult_Empty(t *testing.T) {
	r := NewRenderer()
	output := r.FormatResult(OK())

	if output != "" {
		t.Errorf("FormatResult(OK()) should be empty, got: %q", output)
	}
}

// TestRenderer_extractLine holds a line's text and whether it exists apart: a
// blank line and the line after a final line ending exist and are empty.
func TestRenderer_extractLine(t *testing.T) {
	tests := []struct {
		name    string
		content string
		lineNum int
		want    string
		wantOK  bool
	}{
		{"first line", "line one\nline two\nline three", 1, "line one", true},
		{"middle line", "line one\nline two\nline three", 2, "line two", true},
		{"last line with newline", "line one\nline two\nline three\n", 3, "line three", true},
		{"last line without newline", "line one\nline two\nline three", 3, "line three", true},
		{"CRLF line endings", "line one\r\nline two\r\nline three", 2, "line two", true},
		{"CR only line endings", "line one\rline two\rline three", 2, "line two", true},
		{"a blank line", "line one\n\nline three", 2, "", true},
		{"the line after a final newline", "line one\n", 2, "", true},
		{"the line after a final CRLF", "line one\r\n", 2, "", true},
		{"empty content has one empty line", "", 1, "", true},
		{"single line no newline", "only line", 1, "only line", true},
		{"line out of range", "line one\nline two", 5, "", false},
		{"two past a final newline", "line one\n", 3, "", false},
		{"line zero", "line one", 0, "", false},
		{"negative line", "line one", -1, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractLine([]byte(tt.content), tt.lineNum)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("extractLine(%q, %d) = %q, %t; want %q, %t", tt.content, tt.lineNum, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestRenderer_Excerpt_PointSpan(t *testing.T) {
	provider := newMockSourceProvider()
	source := location.MustNewSourceID("test://file.yammm")
	provider.Add(source, "  token here\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
	)

	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 1, 3)).
		Build()

	output := formatIssue(r, issue)

	// A point is one caret, under the column it names.
	if want := "\n1 |   token here\n  |   ^"; !strings.HasSuffix(output, want) {
		t.Errorf("point span excerpt\n got %q\nwant suffix %q", output, want)
	}
}

func TestRenderer_Excerpt_RangeSpan(t *testing.T) {
	provider := newMockSourceProvider()
	source := location.MustNewSourceID("test://file.yammm")
	provider.Add(source, "  token here\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
	)

	// Range span
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 1, Column: 3},
			End:    location.Position{Line: 1, Column: 8},
		}).
		Build()

	output := formatIssue(r, issue)

	// Five carets, under columns 3 to 7; the end column is exclusive.
	if want := "\n  |   ^^^^^"; !strings.HasSuffix(output, want) {
		t.Errorf("range span excerpt\n got %q\nwant suffix %q", output, want)
	}
}

func TestRenderer_Excerpt_UnknownPosition(t *testing.T) {
	provider := newMockSourceProvider()
	source := location.MustNewSourceID("test://file.yammm")
	provider.Add(source, "content\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
	)

	// Span with unknown position
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{Source: source}).
		Build()

	output := formatIssue(r, issue)

	// Should not contain excerpt (position unknown)
	if strings.Contains(output, "content") {
		t.Error("should not show excerpt when position is unknown")
	}
}

func TestRenderer_Excerpt_SourceNotAvailable(t *testing.T) {
	provider := newMockSourceProvider()
	// Don't add source content

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
	)

	source := location.MustNewSourceID("test://missing.yammm")
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	output := formatIssue(r, issue)

	// Should gracefully omit excerpt
	if output == "" {
		t.Error("should still produce basic output")
	}
}

func TestRenderer_CompleteOutput(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, err := location.SourceIDFromAbsolutePath(filepath.Join(root, "src", "schema.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	provider := newMockSourceProvider()
	provider.Add(source, "type User {\n  name: String\n  age: Int\n}\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
		WithModuleRoot(root),
	)

	issue := NewIssue(Error, E_DUPLICATE_TYPE, "type 'User' is already defined").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 1, Column: 6},
			End:    location.Position{Line: 1, Column: 10},
		}).
		WithHint("consider renaming one of the types").
		WithRelated(location.RelatedInfo{
			Message: "first definition here",
			Span:    location.Point(source, 1, 6),
		}).
		Build()

	// The golden pins the complete rendered form: relativized location,
	// severity, code, message, hint, related note, source excerpt, and the
	// underline — including their exact layout and ordering.
	yammmtest.Golden(t, "renderer_complete_output", []byte(formatIssue(r, issue)))
}

// TestRenderer_WriteLocation_SourceNameOnly verifies that issues with only
// SourceName (no Span or Path) render the SourceName instead of "<unknown>".
func TestRenderer_WriteLocation_SourceNameOnly(t *testing.T) {
	r := NewRenderer()

	// Issue with only SourceName (no span, no path)
	issue := Issue{
		severity:   Error,
		code:       E_TYPE_MISMATCH,
		message:    "type error",
		sourceName: "data.json",
		// No span or path
	}

	output := formatIssue(r, issue)

	// Should use sourceName as location, not "<unknown>"
	if !strings.HasPrefix(output, "data.json:") {
		t.Errorf("output should start with SourceName 'data.json:', got:\n%s", output)
	}
	if strings.Contains(output, "<unknown>") {
		t.Errorf("output should not contain '<unknown>' when SourceName is set:\n%s", output)
	}
}

// TestRenderer_WriteLocation_Precedence verifies location rendering precedence:
// Span > Path > SourceName > "<unknown>"
func TestRenderer_WriteLocation_Precedence(t *testing.T) {
	r := NewRenderer()
	source := location.MustNewSourceID("test://schema.yammm")

	tests := []struct {
		name     string
		issue    Issue
		expected string
	}{
		{
			name: "span takes precedence",
			issue: Issue{
				severity:   Error,
				code:       E_SYNTAX,
				message:    "test",
				span:       location.Point(source, 10, 5),
				sourceName: "data.json",
				path:       "$.foo",
			},
			expected: "test://schema.yammm:10:5",
		},
		{
			name: "path takes precedence over sourceName alone",
			issue: Issue{
				severity:   Error,
				code:       E_SYNTAX,
				message:    "test",
				sourceName: "data.json",
				path:       "$.foo",
			},
			expected: "data.json $.foo",
		},
		{
			name: "sourceName alone",
			issue: Issue{
				severity:   Error,
				code:       E_SYNTAX,
				message:    "test",
				sourceName: "data.json",
			},
			expected: "data.json:",
		},
		{
			name: "unknown when nothing set",
			issue: Issue{
				severity: Error,
				code:     E_SYNTAX,
				message:  "test",
			},
			expected: "<unknown>:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := formatIssue(r, tt.issue)
			if !strings.HasPrefix(output, tt.expected) {
				t.Errorf("expected output to start with %q, got:\n%s", tt.expected, output)
			}
		})
	}
}
