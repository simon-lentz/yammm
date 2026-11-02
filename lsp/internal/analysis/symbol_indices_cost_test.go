package analysis

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

// writeStackedDiamonds writes levels diamonds, each top importing two schemas
// that both import the next top, and returns the entry path. 2^levels paths run
// through them, while the closure holds 3 schemas per level and a last top.
func writeStackedDiamonds(t *testing.T, dir string, levels int) string {
	t.Helper()
	write := func(name, imports string) {
		t.Helper()
		src := fmt.Sprintf("schema %q\n\n%s\ntype T {\n\tid String primary\n}\n", name, imports)
		if err := os.WriteFile(filepath.Join(dir, name+".yammm"), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for i := range levels {
		left, right, next := fmt.Sprintf("left%d", i), fmt.Sprintf("right%d", i), fmt.Sprintf("top%d", i+1)
		write(fmt.Sprintf("top%d", i), fmt.Sprintf("import \"./%s\"\nimport \"./%s\"\n", left, right))
		write(left, fmt.Sprintf("import \"./%s\"\n", next))
		write(right, fmt.Sprintf("import \"./%s\"\n", next))
	}
	write(fmt.Sprintf("top%d", levels), "")
	return filepath.Join(dir, "top0.yammm")
}

// Allocations stand in for visits: each visit builds one symbol index. A walk that
// follows every path allocates 2^levels indices, and the fixed levels keep such a
// walk under a second, so a failure reports a count and never a timeout.
func TestBuildSymbolIndices_ACostLinearInTheSchemasOfStackedDiamonds(t *testing.T) {
	allocations := func(levels int) float64 {
		dir := t.TempDir()
		entry := writeStackedDiamonds(t, dir, levels)
		a := NewAnalyzer(slog.New(slog.NewTextHandler(io.Discard, nil)))
		text, err := os.ReadFile(entry)
		if err != nil {
			t.Fatal(err)
		}
		snap, err := a.Analyze(t.Context(), entry, map[string][]byte{entry: text}, dir)
		if err != nil {
			t.Fatalf("analyze: %v", err)
		}
		if snap.Schema.IsZero() {
			t.Fatalf("the fixture does not load: %v", snap.Result)
		}
		if got, want := len(snap.SymbolsBySource), 3*levels+1; got != want {
			t.Fatalf("%d indices for %d levels, want %d", got, levels, want)
		}
		return testing.AllocsPerRun(1, func() {
			into := &Snapshot{SymbolsBySource: make(map[location.SourceID]*symbols.SymbolIndex)}
			a.buildSymbolIndices(into, snap.Schema, make(map[location.SourceID]struct{}))
		})
	}
	if small, large := allocations(7), allocations(14); large > 4*small {
		t.Errorf("%.0f allocations at 7 levels and %.0f at 14, want growth near 2 times", small, large)
	}
}
