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

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// rangeLine is line 5 of the fixture. Its regex literal does not compile, and
// the diagnostic starts at the literal, where bytes, runes and units disagree:
//
//	! "🎉日🎉日🎉日" title =~ /(/
//	                         ^24   UTF-16 units
//	                         ^36   bytes
//	                         ^21   runes
const rangeLine = "\t! \"🎉日🎉日🎉日\" title =~ /(/"

// rangeSource is the fixture both range tests analyze.
const rangeSource = "schema \"ranges\"\n" +
	"\n" +
	"type Doc {\n" +
	"\tid String primary\n" +
	"\ttitle String\n" +
	rangeLine + "\n" +
	"}\n"

// wantDiagnosticStarts holds the fixture's diagnostic columns in UTF-16 units.
var wantDiagnosticStarts = []uint32{24}

// A parse diagnostic's span survives the conversion into an editor range.
//
// This is the one place a parse span meets the client's UTF-16 count: hover and
// go-to-definition convert symbol spans, not diagnostics. The parser reports
// byte offsets and rune columns, so a span that is right in bytes can still land
// in the wrong column, and only a multibyte fixture shows it.
func TestAnalyze_DiagnosticRangesConvertFromParseSpans(t *testing.T) {
	t.Parallel()

	// The defect sits after four-byte emoji and three-byte kanji on its line,
	// so the byte column, the rune column and the UTF-16 column all differ.
	const src = rangeSource

	dir := t.TempDir()
	path := filepath.Join(dir, "main.yammm")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// A failing load still returns its snapshot: the diagnostics are the
	// point, and converting them is what this exercises.
	a := NewAnalyzer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	snap, _ := a.Analyze(t.Context(), path, map[string][]byte{path: []byte(src)}, dir)
	if snap == nil {
		t.Fatal("analyze returned no snapshot")
	}
	if len(snap.LSPDiagnostics) == 0 {
		t.Fatal("the fixture reports no diagnostics, so nothing is converted")
	}

	lines := strings.Split(src, "\n")
	var starts []uint32
	for _, ud := range snap.LSPDiagnostics {
		start, end := ud.Diagnostic.Range.Start, ud.Diagnostic.Range.End
		if int(start.Line) >= len(lines) {
			t.Errorf("range starts on line %d, past the %d-line source", start.Line, len(lines))
			continue
		}
		line := lines[start.Line]
		if want := len(utf16.Encode([]rune(line))); int(start.Character) > want {
			t.Errorf("range starts at character %d, past the %d units on line %d (%q)",
				start.Character, want, start.Line, line)
		}
		if end.Line == start.Line && end.Character < start.Character {
			t.Errorf("range is inverted: %d..%d", start.Character, end.Character)
		}
		starts = append(starts, start.Character)
	}
	if !slices.Equal(starts, wantDiagnosticStarts) {
		t.Errorf("starts = %v, want %v", starts, wantDiagnosticStarts)
	}
}

// TestAnalyze_DiagnosticRangesConvertAcrossSymlinkedPaths pins the conversion
// when the client's path crosses a symlink (macOS /tmp → /private): each
// diagnostic's range is counted in UTF-16 units, not in rune columns. The
// workspace path is deliberately NOT pre-resolved, so the analysis meets the
// path the client sent.
func TestAnalyze_DiagnosticRangesConvertAcrossSymlinkedPaths(t *testing.T) {
	t.Parallel()

	const src = rangeSource

	// Spell the base as the filesystem lists it, so the symlink below is the
	// only indirection in play.
	base, err := yammmtest.DiskSpelling(t.TempDir())
	if err != nil {
		t.Fatalf("spell temp base: %v", err)
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

	a := NewAnalyzer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	snap, _ := a.Analyze(t.Context(), path, map[string][]byte{path: []byte(src)}, linkDir)
	if snap == nil {
		t.Fatal("analyze returned no snapshot")
	}
	if len(snap.LSPDiagnostics) == 0 {
		t.Fatal("the fixture reports no diagnostics, so nothing is converted")
	}
	var starts []uint32
	for _, ud := range snap.LSPDiagnostics {
		starts = append(starts, ud.Diagnostic.Range.Start.Character)
	}
	if !slices.Equal(starts, wantDiagnosticStarts) {
		t.Errorf("starts = %v, want %v", starts, wantDiagnosticStarts)
	}
}

// The fixture only proves anything while its three column counts disagree.
func TestAnalyze_DiagnosticRangeFixtureDistinguishesItsCounts(t *testing.T) {
	t.Parallel()

	line := rangeLine
	bytes := len(line)
	runes := utf8.RuneCountInString(line)
	units := len(utf16.Encode([]rune(line)))

	if bytes == runes || runes == units || bytes == units {
		t.Errorf("bytes=%d runes=%d utf16=%d; the fixture must distinguish all three",
			bytes, runes, units)
	}
}
