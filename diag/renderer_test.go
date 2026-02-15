package diag

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

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

// mockLineIndexProvider implements LineIndexProvider for testing.
type mockLineIndexProvider struct {
	*mockSourceProvider
	lineStarts map[location.SourceID][]int // line -> byte offset
}

func newMockLineIndexProvider() *mockLineIndexProvider {
	return &mockLineIndexProvider{
		mockSourceProvider: newMockSourceProvider(),
		lineStarts:         make(map[location.SourceID][]int),
	}
}

func (m *mockLineIndexProvider) AddWithIndex(source location.SourceID, content string) {
	m.Add(source, content)

	// Build line index
	offsets := []int{0} // Line 1 starts at byte 0
	for i := range len(content) {
		if content[i] == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	m.lineStarts[source] = offsets
}

func (m *mockLineIndexProvider) LineStartByte(source location.SourceID, line int) (int, bool) {
	offsets, ok := m.lineStarts[source]
	if !ok || line < 1 || line > len(offsets) {
		return 0, false
	}
	return offsets[line-1], true
}

func TestNewRenderer_Defaults(t *testing.T) {
	r := NewRenderer()

	// Test default configuration via output behavior
	issue := NewIssue(Error, E_SYNTAX, "test error").Build()
	output := r.FormatIssue(issue)

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
	output := r.FormatIssue(issue)
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

	output := r.FormatIssue(issue)

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

	output := r.FormatIssue(issue)

	// Should NOT contain excerpt
	if strings.Contains(output, "source content") {
		t.Error("excerpts should be disabled")
	}
}

func TestRenderer_WithMaxLineColumns(t *testing.T) {
	provider := newMockSourceProvider()
	source := location.MustNewSourceID("test://file.yammm")
	longLine := strings.Repeat("x", 200)
	provider.Add(source, longLine+"\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
		WithMaxLineColumns(50),
	)

	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	output := r.FormatIssue(issue)

	// Should be truncated
	if !strings.Contains(output, "...") {
		t.Error("long line should be truncated with indicator")
	}
	// Should not contain full 200 x's
	if strings.Contains(output, strings.Repeat("x", 100)) {
		t.Error("line should be truncated before 100 chars")
	}
}

func TestRenderer_WithTruncationIndicator(t *testing.T) {
	provider := newMockSourceProvider()
	source := location.MustNewSourceID("test://file.yammm")
	longLine := strings.Repeat("x", 200)
	provider.Add(source, longLine+"\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
		WithMaxLineColumns(50),
		WithTruncationIndicator("[...]"),
	)

	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	output := r.FormatIssue(issue)

	if !strings.Contains(output, "[...]") {
		t.Error("should use custom truncation indicator")
	}
}

func TestRenderer_WithModuleRoot(t *testing.T) {
	// Use SourceIDFromAbsolutePath to create a file-backed source for testing
	// path relativization. We need to use a path that exists or test the logic
	// directly.
	//
	// For unit testing, we use synthetic sources but test relativization
	// by verifying the logic works with the String() output.
	source := location.MustNewSourceID("file:///home/user/project/src/file.yammm")

	r := NewRenderer(WithModuleRoot("file:///home/user/project"))

	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 5, 10)).
		Build()

	output := r.FormatIssue(issue)

	// Should show relative path
	if strings.Contains(output, "file:///home/user/project/") {
		t.Errorf("should relativize path, got: %s", output)
	}
	if !strings.Contains(output, "src/file.yammm") {
		t.Errorf("should contain relative path, got: %s", output)
	}
}

func TestRenderer_WithModuleRoot_EdgeCases(t *testing.T) {
	// Note: SourceID.String() always returns forward-slash paths for file-backed sources.
	// For testing the relativization logic, we use synthetic sources with file:// prefix
	// which produces the same String() output format as CanonicalPath-based sources.
	tests := []struct {
		name       string
		source     string
		moduleRoot string
		wantPath   string
	}{
		{
			name:       "exact match returns dot",
			source:     "file:///home/user/project",
			moduleRoot: "file:///home/user/project",
			wantPath:   ".:1:1",
		},
		{
			name:       "nested path is relativized",
			source:     "file:///home/user/project/src/file.yammm",
			moduleRoot: "file:///home/user/project",
			wantPath:   "src/file.yammm:1:1",
		},
		{
			name:       "non-matching path unchanged",
			source:     "file:///home/user/other/file.yammm",
			moduleRoot: "file:///home/user/project",
			wantPath:   "file:///home/user/other/file.yammm:1:1",
		},
		{
			name:       "trailing slash on root is normalized",
			source:     "file:///home/user/project/src/file.yammm",
			moduleRoot: "file:///home/user/project/",
			wantPath:   "src/file.yammm:1:1",
		},
		{
			name:       "Windows-style canonical path",
			source:     "file://C:/Users/project/src/file.yammm",
			moduleRoot: "file://C:/Users/project",
			wantPath:   "src/file.yammm:1:1",
		},
		{
			name:       "Windows root exact match",
			source:     "file://C:/Users/project",
			moduleRoot: "file://C:/Users/project",
			wantPath:   ".:1:1",
		},
		{
			name:       "synthetic source not relativized",
			source:     "test://unit/person.yammm",
			moduleRoot: "file:///home/user/project",
			wantPath:   "test://unit/person.yammm:1:1",
		},
		{
			name:       "prefix but not path segment",
			source:     "file:///home/user/project-other/file.yammm",
			moduleRoot: "file:///home/user/project",
			wantPath:   "file:///home/user/project-other/file.yammm:1:1",
		},
		{
			name:       "empty module root does nothing",
			source:     "file:///home/user/project/file.yammm",
			moduleRoot: "",
			wantPath:   "file:///home/user/project/file.yammm:1:1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := location.MustNewSourceID(tt.source)
			r := NewRenderer(WithModuleRoot(tt.moduleRoot))

			issue := NewIssue(Error, E_SYNTAX, "error").
				WithSpan(location.Point(source, 1, 1)).
				Build()

			output := r.FormatIssue(issue)

			if !strings.Contains(output, tt.wantPath) {
				t.Errorf("output should contain %q, got: %s", tt.wantPath, output)
			}
		})
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
			output := r.FormatIssue(issue)

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
	output := r.FormatIssue(issue)

	if strings.Contains(output, "\033[") {
		t.Error("output should not contain ANSI codes when colors disabled")
	}
}

func TestRenderer_WithDistinguishFatal(t *testing.T) {
	issue := NewIssue(Fatal, E_LIMIT_REACHED, "limit").Build()

	// Default: Fatal renders as "error"
	r1 := NewRenderer()
	output1 := r1.FormatIssue(issue)
	if !strings.Contains(output1, ": error[") {
		t.Errorf("Fatal should render as 'error' by default, got: %s", output1)
	}

	// With distinguish: Fatal renders as "fatal"
	r2 := NewRenderer(WithDistinguishFatal(true))
	output2 := r2.FormatIssue(issue)
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
			output := r.FormatIssue(tt.issue)
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
	output := r.FormatIssue(issue)

	if !strings.Contains(output, "hint: try doing X instead") {
		t.Errorf("output should contain hint, got: %s", output)
	}
}

func TestRenderer_FormatIssue_Related(t *testing.T) {
	source := location.MustNewSourceID("test://related.yammm")
	issue := NewIssue(Error, E_TYPE_COLLISION, "type collision").
		WithRelated(location.RelatedInfo{
			Message: "first definition here",
			Span:    location.Point(source, 5, 1),
		}).
		Build()

	r := NewRenderer()
	output := r.FormatIssue(issue)

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
	c.Collect(NewIssue(Error, E_TYPE_COLLISION, "second error").Build())

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

func TestRenderer_FormatIssues(t *testing.T) {
	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "first").Build(),
		NewIssue(Error, E_SYNTAX, "second").Build(),
	}

	r := NewRenderer()
	output := r.FormatIssues(issues)

	if !strings.Contains(output, "first") || !strings.Contains(output, "second") {
		t.Errorf("output should contain both issues, got: %s", output)
	}
	// Should be separated by newline
	lines := strings.Split(output, "\n")
	if len(lines) < 2 {
		t.Errorf("issues should be on separate lines, got: %s", output)
	}
}

func TestRenderer_FormatIssues_Empty(t *testing.T) {
	r := NewRenderer()
	output := r.FormatIssues(nil)

	if output != "" {
		t.Errorf("FormatIssues(nil) should be empty, got: %q", output)
	}
}

func TestRenderer_extractLine(t *testing.T) {
	r := NewRenderer()

	tests := []struct {
		name    string
		content string
		lineNum int
		want    string
	}{
		{
			name:    "first line",
			content: "line one\nline two\nline three",
			lineNum: 1,
			want:    "line one",
		},
		{
			name:    "middle line",
			content: "line one\nline two\nline three",
			lineNum: 2,
			want:    "line two",
		},
		{
			name:    "last line with newline",
			content: "line one\nline two\nline three\n",
			lineNum: 3,
			want:    "line three",
		},
		{
			name:    "last line without newline",
			content: "line one\nline two\nline three",
			lineNum: 3,
			want:    "line three",
		},
		{
			name:    "CRLF line endings",
			content: "line one\r\nline two\r\nline three",
			lineNum: 2,
			want:    "line two",
		},
		{
			name:    "CR only line endings",
			content: "line one\rline two\rline three",
			lineNum: 2,
			want:    "line two",
		},
		{
			name:    "line out of range",
			content: "line one\nline two",
			lineNum: 5,
			want:    "",
		},
		{
			name:    "line zero",
			content: "line one",
			lineNum: 0,
			want:    "",
		},
		{
			name:    "negative line",
			content: "line one",
			lineNum: -1,
			want:    "",
		},
		{
			name:    "empty content",
			content: "",
			lineNum: 1,
			want:    "",
		},
		{
			name:    "single line no newline",
			content: "only line",
			lineNum: 1,
			want:    "only line",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.extractLine([]byte(tt.content), tt.lineNum)
			if got != tt.want {
				t.Errorf("extractLine() = %q; want %q", got, tt.want)
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

	// Point span (start == end)
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 1, 3)).
		Build()

	output := r.FormatIssue(issue)

	// Should have single caret for point
	if !strings.Contains(output, "^") {
		t.Error("point span should have underline")
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

	output := r.FormatIssue(issue)

	// Should have 5 carets (columns 3-7 inclusive)
	if !strings.Contains(output, "^^^^^") {
		t.Errorf("range span should have 5 carets, got: %s", output)
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

	output := r.FormatIssue(issue)

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

	output := r.FormatIssue(issue)

	// Should gracefully omit excerpt
	if output == "" {
		t.Error("should still produce basic output")
	}
}

func TestWithLSPByteFallback(t *testing.T) {
	// Test that the option is accepted (actual LSP output tested in lsp_test.go)
	r1 := NewRenderer(WithLSPByteFallback(LSPByteFallbackOmit))
	r2 := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))

	// Both should produce valid output
	issue := NewIssue(Error, E_SYNTAX, "test").Build()
	if r1.FormatIssue(issue) == "" {
		t.Error("r1 should produce output")
	}
	if r2.FormatIssue(issue) == "" {
		t.Error("r2 should produce output")
	}
}

func TestRenderer_CompleteOutput(t *testing.T) {
	provider := newMockSourceProvider()
	source := location.MustNewSourceID("file:///project/src/schema.yammm")
	provider.Add(source, "type User {\n  name: String\n  age: Int\n}\n")

	r := NewRenderer(
		WithSourceProvider(provider),
		WithExcerpts(true),
		WithModuleRoot("file:///project"),
	)

	issue := NewIssue(Error, E_TYPE_COLLISION, "type 'User' is already defined").
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

	output := r.FormatIssue(issue)

	// Verify all components are present
	expected := []string{
		"src/schema.yammm:1:6",           // Relativized location
		"error",                          // Severity
		"E_TYPE_COLLISION",               // Code
		"type 'User' is already defined", // Message
		"hint: consider renaming",        // Hint
		"note: first definition here",    // Related
		"type User {",                    // Source excerpt
		"^^^^",                           // Underline
	}

	for _, s := range expected {
		if !strings.Contains(output, s) {
			t.Errorf("output should contain %q, got:\n%s", s, output)
		}
	}
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

	output := r.FormatIssue(issue)

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
			output := r.FormatIssue(tt.issue)
			if !strings.HasPrefix(output, tt.expected) {
				t.Errorf("expected output to start with %q, got:\n%s", tt.expected, output)
			}
		})
	}
}
