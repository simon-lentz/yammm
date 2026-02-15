package diag

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
)

func TestLSPDiagnostic_Basic(t *testing.T) {
	source := location.MustNewSourceID("test://schema.yammm")
	issue := NewIssue(Error, E_SYNTAX, "syntax error").
		WithSpan(location.Point(source, 10, 5)).
		Build()

	// Use approximate mode since we don't have byte offsets
	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))
	diag := r.LSPDiagnostic(issue)

	if diag == nil {
		t.Fatal("LSPDiagnostic should not be nil")
	}

	// Line: 10 (1-based) → 9 (0-based)
	if diag.Range.Start.Line != 9 {
		t.Errorf("Range.Start.Line = %d; want 9", diag.Range.Start.Line)
	}
	// Column: 5 (1-based) → 4 (0-based, approximate)
	if diag.Range.Start.Character != 4 {
		t.Errorf("Range.Start.Character = %d; want 4", diag.Range.Start.Character)
	}

	if diag.Severity != LSPSeverityError {
		t.Errorf("Severity = %d; want %d", diag.Severity, LSPSeverityError)
	}
	if diag.Code != "E_SYNTAX" {
		t.Errorf("Code = %q; want 'E_SYNTAX'", diag.Code)
	}
	if diag.Source != "yammm" {
		t.Errorf("Source = %q; want 'yammm'", diag.Source)
	}
	if diag.Message != "syntax error" {
		t.Errorf("Message = %q; want 'syntax error'", diag.Message)
	}
}

func TestLSPDiagnostic_SeverityMapping(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))

	tests := []struct {
		severity Severity
		want     int
	}{
		{Fatal, LSPSeverityError},
		{Error, LSPSeverityError},
		{Warning, LSPSeverityWarning},
		{Info, LSPSeverityInformation},
		{Hint, LSPSeverityHint},
	}

	for _, tt := range tests {
		t.Run(tt.severity.String(), func(t *testing.T) {
			issue := NewIssue(tt.severity, E_SYNTAX, "msg").
				WithSpan(location.Point(source, 1, 1)).
				Build()
			diag := r.LSPDiagnostic(issue)

			if diag == nil {
				t.Fatal("LSPDiagnostic should not be nil")
			}
			if diag.Severity != tt.want {
				t.Errorf("Severity = %d; want %d", diag.Severity, tt.want)
			}
		})
	}
}

func TestLSPDiagnostic_LineConversion(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))

	tests := []struct {
		line     int
		wantLine int
	}{
		{1, 0},    // First line
		{10, 9},   // Typical line
		{100, 99}, // Large line number
	}

	for _, tt := range tests {
		issue := NewIssue(Error, E_SYNTAX, "error").
			WithSpan(location.Point(source, tt.line, 1)).
			Build()
		diag := r.LSPDiagnostic(issue)

		if diag.Range.Start.Line != tt.wantLine {
			t.Errorf("Line %d → LSP Line %d; want %d",
				tt.line, diag.Range.Start.Line, tt.wantLine)
		}
	}
}

func TestLSPDiagnostic_NoSpan(t *testing.T) {
	issue := NewIssue(Error, E_SYNTAX, "no location").Build()

	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))
	diag := r.LSPDiagnostic(issue)

	if diag != nil {
		t.Error("LSPDiagnostic should be nil for issue without span")
	}
}

func TestLSPDiagnostic_UnknownPosition(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	// Span with unknown start position
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{Source: source}).
		Build()

	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))
	diag := r.LSPDiagnostic(issue)

	if diag != nil {
		t.Error("LSPDiagnostic should be nil for unknown position")
	}
}

func TestLSPDiagnostic_LSPByteFallbackOmit(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	// Issue without byte offset
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 1, Column: 5, Byte: -1}, // Unknown byte
			End:    location.Position{Line: 1, Column: 10, Byte: -1},
		}).
		Build()

	// Default is LSPByteFallbackOmit
	r := NewRenderer()
	diag := r.LSPDiagnostic(issue)

	if diag != nil {
		t.Error("LSPDiagnostic should be nil when byte offset unknown and fallback is Omit")
	}
}

func TestLSPDiagnostic_LSPByteFallbackApproximate(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	// Issue without byte offset
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 1, Column: 5, Byte: -1}, // Unknown byte
			End:    location.Position{Line: 1, Column: 10, Byte: -1},
		}).
		Build()

	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))
	diag := r.LSPDiagnostic(issue)

	if diag == nil {
		t.Fatal("LSPDiagnostic should not be nil with Approximate fallback")
	}

	// Character should be column - 1
	if diag.Range.Start.Character != 4 {
		t.Errorf("Start.Character = %d; want 4 (column 5 - 1)", diag.Range.Start.Character)
	}
	if diag.Range.End.Character != 9 {
		t.Errorf("End.Character = %d; want 9 (column 10 - 1)", diag.Range.End.Character)
	}
}

func TestLSPDiagnostic_WithRelated(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	issue := NewIssue(Error, E_TYPE_COLLISION, "collision").
		WithSpan(location.Point(source, 10, 1)).
		WithRelated(location.RelatedInfo{
			Message: "first definition here",
			Span:    location.Point(source, 5, 1),
		}).
		Build()

	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))
	diag := r.LSPDiagnostic(issue)

	if diag == nil {
		t.Fatal("LSPDiagnostic should not be nil")
	}

	if len(diag.RelatedInformation) != 1 {
		t.Fatalf("len(RelatedInformation) = %d; want 1", len(diag.RelatedInformation))
	}

	rel := diag.RelatedInformation[0]
	if rel.Message != "first definition here" {
		t.Errorf("RelatedInformation[0].Message = %q", rel.Message)
	}
	if rel.Location.URI != "test://file.yammm" {
		t.Errorf("RelatedInformation[0].Location.URI = %q", rel.Location.URI)
	}
	// Line 5 → 4 (0-based)
	if rel.Location.Range.Start.Line != 4 {
		t.Errorf("RelatedInformation[0].Location.Range.Start.Line = %d; want 4",
			rel.Location.Range.Start.Line)
	}
}

func TestLSPDiagnostic_RelatedWithoutSpan(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Point(source, 1, 1)).
		WithRelated(location.RelatedInfo{
			Message: "note without location",
			// No span
		}).
		Build()

	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))
	diag := r.LSPDiagnostic(issue)

	if diag == nil {
		t.Fatal("LSPDiagnostic should not be nil")
	}

	// Related info without span should be omitted
	if len(diag.RelatedInformation) != 0 {
		t.Errorf("RelatedInformation should be empty for related without span, got %d",
			len(diag.RelatedInformation))
	}
}

func TestLSPDiagnostics(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	c := NewCollector(0)
	c.Collect(NewIssue(Error, E_SYNTAX, "with span").
		WithSpan(location.Point(source, 1, 1)).
		Build())
	c.Collect(NewIssue(Error, E_TYPE_COLLISION, "without span").Build())
	c.Collect(NewIssue(Warning, E_INVALID_NAME, "with span 2").
		WithSpan(location.Point(source, 5, 1)).
		Build())

	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))
	diagnostics := r.LSPDiagnostics(c.Result())

	// Should only include issues with spans
	if len(diagnostics) != 2 {
		t.Errorf("len(diagnostics) = %d; want 2", len(diagnostics))
	}
}

func TestLSPDiagnostics_Empty(t *testing.T) {
	r := NewRenderer()
	diagnostics := r.LSPDiagnostics(OK())

	// Returns empty slice (not nil) for consistent JSON serialization as "[]"
	if diagnostics == nil {
		t.Error("LSPDiagnostics(OK()) should return empty slice, not nil")
	}
	if len(diagnostics) != 0 {
		t.Errorf("LSPDiagnostics(OK()) should return empty slice, got %d items", len(diagnostics))
	}
}

// TestUTF16OffsetFromByte_ASCII tests UTF-16 offset computation for ASCII text.
func TestUTF16OffsetFromByte_ASCII(t *testing.T) {
	// ASCII: 1 byte = 1 UTF-16 code unit
	content := []byte("hello world")
	//                  01234567890

	tests := []struct {
		lineStart  int
		targetByte int
		want       int
	}{
		{0, 0, 0},   // Start of line
		{0, 5, 5},   // "hello" (5 chars)
		{0, 11, 11}, // Full line
		{6, 11, 5},  // "world" from offset 6
	}

	for _, tt := range tests {
		got := utf16OffsetFromByte(content, tt.lineStart, tt.targetByte)
		if got != tt.want {
			t.Errorf("utf16OffsetFromByte(%q, %d, %d) = %d; want %d",
				content, tt.lineStart, tt.targetByte, got, tt.want)
		}
	}
}

// TestUTF16OffsetFromByte_BMP tests UTF-16 offset for BMP characters.
func TestUTF16OffsetFromByte_BMP(t *testing.T) {
	// BMP characters (U+0000-U+FFFF): 1-3 bytes = 1 UTF-16 code unit
	// "héllo" has: h(1), é(2), l(1), l(1), o(1) = 6 bytes, 5 chars
	content := []byte("héllo")

	tests := []struct {
		targetByte int
		want       int
	}{
		{0, 0}, // Start
		{1, 1}, // After 'h'
		{3, 2}, // After 'é' (2 bytes)
		{4, 3}, // After first 'l'
		{6, 5}, // End
	}

	for _, tt := range tests {
		got := utf16OffsetFromByte(content, 0, tt.targetByte)
		if got != tt.want {
			t.Errorf("utf16OffsetFromByte(%q, 0, %d) = %d; want %d",
				content, tt.targetByte, got, tt.want)
		}
	}
}

// TestUTF16OffsetFromByte_NonBMP tests UTF-16 offset for characters above BMP.
func TestUTF16OffsetFromByte_NonBMP(t *testing.T) {
	// Non-BMP characters (U+10000+): 4 bytes = 2 UTF-16 code units (surrogate pair)
	// "a😀b" has: a(1), 😀(4), b(1) = 6 bytes
	// UTF-16: a(1), 😀(2 surrogates), b(1) = 4 code units
	content := []byte("a😀b")

	tests := []struct {
		targetByte int
		want       int
	}{
		{0, 0}, // Start
		{1, 1}, // After 'a'
		{5, 3}, // After 😀 (4 bytes, 2 UTF-16 code units)
		{6, 4}, // After 'b'
	}

	for _, tt := range tests {
		got := utf16OffsetFromByte(content, 0, tt.targetByte)
		if got != tt.want {
			t.Errorf("utf16OffsetFromByte(\"a😀b\", 0, %d) = %d; want %d",
				tt.targetByte, got, tt.want)
		}
	}
}

// TestUTF16OffsetFromByte_MixedContent tests various Unicode scenarios.
func TestUTF16OffsetFromByte_MixedContent(t *testing.T) {
	// Mix of ASCII, BMP, and non-BMP
	// "aéb😀c" = a(1) + é(2) + b(1) + 😀(4) + c(1) = 9 bytes
	// UTF-16:    a(1) + é(1) + b(1) + 😀(2) + c(1) = 6 code units
	content := []byte("aéb😀c")

	tests := []struct {
		targetByte int
		want       int
		desc       string
	}{
		{0, 0, "start"},
		{1, 1, "after 'a'"},
		{3, 2, "after 'é'"},
		{4, 3, "after 'b'"},
		{8, 5, "after '😀'"},
		{9, 6, "end"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := utf16OffsetFromByte(content, 0, tt.targetByte)
			if got != tt.want {
				t.Errorf("utf16OffsetFromByte(..., 0, %d) = %d; want %d",
					tt.targetByte, got, tt.want)
			}
		})
	}
}

// TestUTF16OffsetFromByte_EdgeCases tests boundary conditions.
func TestUTF16OffsetFromByte_EdgeCases(t *testing.T) {
	content := []byte("hello")

	tests := []struct {
		lineStart  int
		targetByte int
		want       int
		desc       string
	}{
		{0, 100, 5, "target beyond content"},
		{10, 5, 0, "target before lineStart"},
		{0, 0, 0, "zero offset"},
		{5, 5, 0, "lineStart equals target"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := utf16OffsetFromByte(content, tt.lineStart, tt.targetByte)
			if got != tt.want {
				t.Errorf("utf16OffsetFromByte(..., %d, %d) = %d; want %d",
					tt.lineStart, tt.targetByte, got, tt.want)
			}
		})
	}
}

// TestLSPDiagnostic_WithLineIndexProvider tests exact UTF-16 computation
// when a LineIndexProvider is available.
func TestLSPDiagnostic_WithLineIndexProvider(t *testing.T) {
	provider := newMockLineIndexProvider()
	source := location.MustNewSourceID("test://utf16.yammm")
	// "héllo😀" on line 1
	provider.AddWithIndex(source, "héllo😀\n")

	r := NewRenderer(WithSourceProvider(provider))

	// Position pointing to the emoji (byte 6 = after "héllo")
	// "héllo" = 6 bytes (h=1, é=2, l=1, l=1, o=1)
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 1, Column: 6, Byte: 6},
			End:    location.Position{Line: 1, Column: 7, Byte: 10},
		}).
		Build()

	diag := r.LSPDiagnostic(issue)

	if diag == nil {
		t.Fatal("LSPDiagnostic should not be nil")
	}

	// UTF-16 offset of position at byte 6:
	// h(1) + é(1) + l(1) + l(1) + o(1) = 5 UTF-16 code units
	if diag.Range.Start.Character != 5 {
		t.Errorf("Start.Character = %d; want 5", diag.Range.Start.Character)
	}

	// UTF-16 offset of position at byte 10 (after emoji):
	// h(1) + é(1) + l(1) + l(1) + o(1) + 😀(2) = 7 UTF-16 code units
	if diag.Range.End.Character != 7 {
		t.Errorf("End.Character = %d; want 7", diag.Range.End.Character)
	}
}

func TestLSPDiagnostic_Range(t *testing.T) {
	source := location.MustNewSourceID("test://file.yammm")
	issue := NewIssue(Error, E_SYNTAX, "error").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 10, Column: 5, Byte: -1},
			End:    location.Position{Line: 10, Column: 15, Byte: -1},
		}).
		Build()

	r := NewRenderer(WithLSPByteFallback(LSPByteFallbackApproximate))
	diag := r.LSPDiagnostic(issue)

	if diag == nil {
		t.Fatal("LSPDiagnostic should not be nil")
	}

	// Start and end should be on same line (0-based: line 9)
	if diag.Range.Start.Line != 9 || diag.Range.End.Line != 9 {
		t.Errorf("Range lines = %d-%d; want 9-9",
			diag.Range.Start.Line, diag.Range.End.Line)
	}

	// Characters: 5-1=4, 15-1=14
	if diag.Range.Start.Character != 4 || diag.Range.End.Character != 14 {
		t.Errorf("Range characters = %d-%d; want 4-14",
			diag.Range.Start.Character, diag.Range.End.Character)
	}
}

// TestUTF16OffsetFromByte_MidRuneByteOffset tests that mid-rune byte offsets
// floor to the containing rune's UTF-16 offset (not the next rune).
func TestUTF16OffsetFromByte_MidRuneByteOffset(t *testing.T) {
	// Japanese characters: each 3 bytes, 1 UTF-16 code unit
	// "日本語" = 日[0,1,2] + 本[3,4,5] + 語[6,7,8]
	content := []byte("日本語")

	tests := []struct {
		targetByte int
		want       int
		desc       string
	}{
		{0, 0, "start of first rune"},
		{1, 0, "mid first rune (byte 1) - should floor to 0"},
		{2, 0, "mid first rune (byte 2) - should floor to 0"},
		{3, 1, "start of second rune"},
		{4, 1, "mid second rune - should floor to 1"},
		{5, 1, "mid second rune (byte 5) - should floor to 1"},
		{6, 2, "start of third rune"},
		{7, 2, "mid third rune - should floor to 2"},
		{8, 2, "mid third rune (byte 8) - should floor to 2"},
		{9, 3, "end of content"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := utf16OffsetFromByte(content, 0, tt.targetByte)
			if got != tt.want {
				t.Errorf("utf16OffsetFromByte(日本語, 0, %d) = %d; want %d",
					tt.targetByte, got, tt.want)
			}
		})
	}
}

// TestUTF16OffsetFromByte_MidRuneEmoji tests mid-rune behavior for non-BMP
// characters (emoji) which use 4 bytes and 2 UTF-16 code units (surrogate pair).
func TestUTF16OffsetFromByte_MidRuneEmoji(t *testing.T) {
	// "a😀b" = a[0] + 😀[1,2,3,4] + b[5]
	// UTF-16: a(1) + 😀(2 surrogates) + b(1) = 4 code units
	content := []byte("a😀b")

	tests := []struct {
		targetByte int
		want       int
		desc       string
	}{
		{0, 0, "before 'a'"},
		{1, 1, "start of emoji"},
		{2, 1, "mid emoji (byte 2) - should floor to 1"},
		{3, 1, "mid emoji (byte 3) - should floor to 1"},
		{4, 1, "mid emoji (byte 4) - should floor to 1"},
		{5, 3, "start of 'b' (after emoji's 2 UTF-16 units)"},
		{6, 4, "end of content"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := utf16OffsetFromByte(content, 0, tt.targetByte)
			if got != tt.want {
				t.Errorf("utf16OffsetFromByte(a😀b, 0, %d) = %d; want %d",
					tt.targetByte, got, tt.want)
			}
		})
	}
}

// TestUTF16OffsetFromByte_BoundaryConditions tests exact rune boundaries.
func TestUTF16OffsetFromByte_BoundaryConditions(t *testing.T) {
	// "é" is 2 bytes [0,1], "😀" is 4 bytes
	// "aéb😀c" = a[0] + é[1,2] + b[3] + 😀[4,5,6,7] + c[8]
	// UTF-16: a(1) + é(1) + b(1) + 😀(2) + c(1) = 6 code units
	content := []byte("aéb😀c")

	tests := []struct {
		targetByte int
		want       int
		desc       string
	}{
		// Exact rune start boundaries
		{0, 0, "start of 'a'"},
		{1, 1, "start of 'é'"},
		{3, 2, "start of 'b'"},
		{4, 3, "start of '😀'"},
		{8, 5, "start of 'c'"},
		{9, 6, "end of content"},

		// Mid-rune positions
		{2, 1, "mid 'é' - should floor to 1"},
		{5, 3, "mid '😀' (byte 5) - should floor to 3"},
		{6, 3, "mid '😀' (byte 6) - should floor to 3"},
		{7, 3, "mid '😀' (byte 7) - should floor to 3"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := utf16OffsetFromByte(content, 0, tt.targetByte)
			if got != tt.want {
				t.Errorf("utf16OffsetFromByte(aéb😀c, 0, %d) = %d; want %d",
					tt.targetByte, got, tt.want)
			}
		})
	}
}

// TestSourceIDToURI verifies that sourceIDToURI correctly converts SourceIDs
// to LSP-compatible URIs.
func TestSourceIDToURI(t *testing.T) {
	t.Run("synthetic source passes through", func(t *testing.T) {
		source := location.MustNewSourceID("test://schema.yammm")
		uri := sourceIDToURI(source)
		if uri != "test://schema.yammm" {
			t.Errorf("sourceIDToURI(synthetic) = %q; want %q", uri, "test://schema.yammm")
		}
	})

	t.Run("file-backed source becomes file:// URI", func(t *testing.T) {
		// SourceIDFromAbsolutePath creates a file-backed SourceID
		source, err := location.SourceIDFromAbsolutePath("/foo/bar/schema.yammm")
		if err != nil {
			t.Skipf("skipping file-backed test: %v", err)
		}

		uri := sourceIDToURI(source)
		if uri != "file:///foo/bar/schema.yammm" {
			t.Errorf("sourceIDToURI(file-backed) = %q; want %q",
				uri, "file:///foo/bar/schema.yammm")
		}
	})

	t.Run("path with spaces is percent-encoded", func(t *testing.T) {
		// Paths with spaces must be percent-encoded in URIs
		source, err := location.SourceIDFromAbsolutePath("/path/with spaces/schema.yammm")
		if err != nil {
			t.Skipf("skipping file-backed test: %v", err)
		}

		uri := sourceIDToURI(source)
		// Spaces should be encoded as %20
		want := "file:///path/with%20spaces/schema.yammm"
		if uri != want {
			t.Errorf("sourceIDToURI(path with spaces) = %q; want %q", uri, want)
		}
	})
}

// TestFindLineStartByte verifies that findLineStartByte correctly locates
// line starts in content.
func TestFindLineStartByte(t *testing.T) {
	content := []byte("line1\nline2\nline3\n")

	tests := []struct {
		lineNum int
		want    int
		desc    string
	}{
		{1, 0, "line 1 starts at byte 0"},
		{2, 6, "line 2 starts after first newline"},
		{3, 12, "line 3 starts after second newline"},
		{4, 18, "line 4 starts after trailing newline (empty line)"},
		{5, -1, "line 5 doesn't exist"},
		{0, -1, "line 0 is invalid"},
		{-1, -1, "negative line is invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			got := findLineStartByte(content, tt.lineNum)
			if got != tt.want {
				t.Errorf("findLineStartByte(content, %d) = %d; want %d",
					tt.lineNum, got, tt.want)
			}
		})
	}
}

// mockContentProvider implements SourceProvider but NOT LineIndexProvider,
// to test the slow path in computeUTF16Character.
type mockContentProvider struct {
	content map[location.SourceID][]byte
}

func (p *mockContentProvider) Content(span location.Span) ([]byte, bool) {
	c, ok := p.content[span.Source]
	return c, ok
}

// TestComputeUTF16Character_SlowPath verifies that computeUTF16Character
// uses content scanning when LineIndexProvider is unavailable.
func TestComputeUTF16Character_SlowPath(t *testing.T) {
	source := location.MustNewSourceID("test://slow-path")
	content := []byte("line1\nline2\nline3\n")

	provider := &mockContentProvider{
		content: map[location.SourceID][]byte{source: content},
	}

	r := NewRenderer(WithSourceProvider(provider))

	// Create a position on line 2, byte offset 8 (letter 'n' in "line2")
	span := location.Range(source, 2, 3, 2, 4)
	span.Start.Byte = 8
	span.End.Byte = 9

	// Should compute UTF-16 offset using slow path (no LineIndexProvider)
	diag := r.LSPDiagnostic(
		NewIssue(Error, E_SYNTAX, "test").WithSpan(span).Build(),
	)

	if diag == nil {
		t.Fatal("LSPDiagnostic returned nil; expected slow path to succeed")
	}

	// Line 2 starts at byte 6, position byte 8 is offset 2 into the line
	// For ASCII content, UTF-16 offset == byte offset within line
	if diag.Range.Start.Character != 2 {
		t.Errorf("Character = %d; want 2 (computed via slow path)",
			diag.Range.Start.Character)
	}
}
