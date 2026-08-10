package analysis

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// A parse diagnostic's span survives the conversion into an editor range.
//
// This is the one place a parse span meets the negotiated position encoding:
// hover and go-to-definition convert symbol spans, not diagnostics. The parser
// reports byte offsets and rune columns; an editor wants UTF-16 code units on
// UTF-16 clients, so a span that is right in bytes can still land in the wrong
// column, and only a multibyte fixture shows it.
func TestAnalyze_DiagnosticRangesConvertFromParseSpans(t *testing.T) {
	t.Parallel()

	// The defect sits after a four-byte emoji and a three-byte kanji on its own
	// line, so the byte column, the rune column and the UTF-16 column all differ.
	const src = "schema \"ranges\"\n" +
		"\n" +
		"type Doc {\n" +
		"\tid String primary\n" +
		"\ttitle Enum[\"🎉日\", \"🎉日\"]\n" +
		"}\n"

	// Compared across encodings below: a pass-through conversion would make the
	// two identical, which the bounds check alone would not notice.
	starts := map[lsputil.PositionEncoding][]uint32{}

	for _, enc := range []lsputil.PositionEncoding{
		lsputil.PositionEncodingUTF16,
		lsputil.PositionEncodingUTF8,
	} {
		// The loader canonicalises the source path, so an unresolved temp root
		// would miss the registry and fall back to the rune column.
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatalf("resolve temp dir: %v", err)
		}
		path := filepath.Join(dir, "main.yammm")
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		// A failing load still returns its snapshot: the diagnostics are the
		// point, and converting them is what this exercises.
		a := NewAnalyzer(slog.New(slog.NewTextHandler(io.Discard, nil)))
		snap, _ := a.Analyze(t.Context(), path, map[string][]byte{path: []byte(src)}, dir, enc)
		if snap == nil {
			t.Fatalf("%s: analyze returned no snapshot", enc)
		}
		if len(snap.LSPDiagnostics) == 0 {
			t.Fatalf("%s: the fixture reports no diagnostics, so nothing is converted", enc)
		}

		lines := strings.Split(src, "\n")
		for _, ud := range snap.LSPDiagnostics {
			start, end := ud.Diagnostic.Range.Start, ud.Diagnostic.Range.End
			if int(start.Line) >= len(lines) {
				t.Errorf("%s: range starts on line %d, past the %d-line source", enc, start.Line, len(lines))
				continue
			}
			line := lines[start.Line]
			if want := columnUnits(line, enc); int(start.Character) > want {
				t.Errorf("%s: range starts at character %d, past the %d units on line %d (%q)",
					enc, start.Character, want, start.Line, line)
			}
			if end.Line == start.Line && end.Character < start.Character {
				t.Errorf("%s: range is inverted: %d..%d", enc, start.Character, end.Character)
			}
			starts[enc] = append(starts[enc], start.Character)
		}
	}

	utf8Cols, utf16Cols := starts[lsputil.PositionEncodingUTF8], starts[lsputil.PositionEncodingUTF16]
	if len(utf8Cols) != len(utf16Cols) {
		t.Fatalf("diagnostic counts differ by encoding: %d and %d", len(utf8Cols), len(utf16Cols))
	}
	if slices.Equal(utf8Cols, utf16Cols) {
		t.Errorf("every column is identical under both encodings (%v); the span was "+
			"passed through rather than converted", utf8Cols)
	}
}

// TestAnalyze_DiagnosticRangesConvertAcrossSymlinkedPaths pins the conversion
// when the client's path crosses a symlink (macOS /tmp → /private): the
// analyzer must canonicalise its own registry keys, because on a failed load
// no back-fill re-keys them and a miss falls back to rune columns — identical
// under both encodings, which is what the final assertion catches. The
// workspace path is deliberately NOT pre-resolved; resolving it is what hides
// the bug.
func TestAnalyze_DiagnosticRangesConvertAcrossSymlinkedPaths(t *testing.T) {
	t.Parallel()

	const src = "schema \"ranges\"\n" +
		"\n" +
		"type Doc {\n" +
		"\tid String primary\n" +
		"\ttitle Enum[\"🎉日\", \"🎉日\"]\n" +
		"}\n"

	// Resolve the base so the symlink below is the only indirection in play.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp base: %v", err)
	}
	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	linkDir := filepath.Join(base, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	path := filepath.Join(linkDir, "main.yammm")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	starts := map[lsputil.PositionEncoding][]uint32{}
	for _, enc := range []lsputil.PositionEncoding{
		lsputil.PositionEncodingUTF16,
		lsputil.PositionEncodingUTF8,
	} {
		a := NewAnalyzer(slog.New(slog.NewTextHandler(io.Discard, nil)))
		snap, _ := a.Analyze(t.Context(), path, map[string][]byte{path: []byte(src)}, linkDir, enc)
		if snap == nil {
			t.Fatalf("%s: analyze returned no snapshot", enc)
		}
		if len(snap.LSPDiagnostics) == 0 {
			t.Fatalf("%s: the fixture reports no diagnostics, so nothing is converted", enc)
		}
		for _, ud := range snap.LSPDiagnostics {
			starts[enc] = append(starts[enc], ud.Diagnostic.Range.Start.Character)
		}
	}

	utf8Cols, utf16Cols := starts[lsputil.PositionEncodingUTF8], starts[lsputil.PositionEncodingUTF16]
	if len(utf8Cols) != len(utf16Cols) {
		t.Fatalf("diagnostic counts differ by encoding: %d and %d", len(utf8Cols), len(utf16Cols))
	}
	if slices.Equal(utf8Cols, utf16Cols) {
		t.Errorf("every column is identical under both encodings (%v); the registry "+
			"missed the symlinked path and fell back to rune columns", utf8Cols)
	}
}

// columnUnits returns the length of a line in the units the encoding counts.
func columnUnits(line string, enc lsputil.PositionEncoding) int {
	if enc == lsputil.PositionEncodingUTF8 {
		return len(line)
	}
	return len(utf16.Encode([]rune(line)))
}

// The fixture only proves anything while its three column counts disagree.
func TestAnalyze_DiagnosticRangeFixtureDistinguishesEncodings(t *testing.T) {
	t.Parallel()

	const line = "\ttitle Enum[\"🎉日\", \"🎉日\"]"
	bytes := len(line)
	runes := utf8.RuneCountInString(line)
	units := len(utf16.Encode([]rune(line)))

	if bytes == runes || runes == units || bytes == units {
		t.Errorf("bytes=%d runes=%d utf16=%d; the fixture must distinguish all three",
			bytes, runes, units)
	}
}
