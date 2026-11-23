package analysis

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/lsp/internal/protocol"
)

// TestAnalyze_PublishesTheLoadsWarningsUnderTheCap pins that a load's warnings
// reach the editor as Warning diagnostics, and that past 100 the
// analysis publishes 100 of them.
func TestAnalyze_PublishesTheLoadsWarningsUnderTheCap(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 120} {
		var src strings.Builder
		src.WriteString("schema \"main\"\ntype T {\n\tid String primary\n")
		for i := range n {
			fmt.Fprintf(&src, "\tn%d Integer[-_, 5]\n", i)
		}
		src.WriteString("}\n")
		snap, err := NewAnalyzer(slog.New(slog.DiscardHandler)).Analyze(t.Context(), "/probe/main.yammm",
			map[string][]byte{"/probe/main.yammm": []byte(src.String())}, "", true)
		if err != nil {
			t.Fatalf("%d warnings: Analyze: %v", n, err)
		}
		want := min(n, 100)
		warnings := 0
		for _, d := range snap.LSPDiagnostics {
			if sev := d.Diagnostic.Severity; sev != nil && *sev == protocol.DiagnosticSeverityWarning {
				warnings++
			}
		}
		if len(snap.LSPDiagnostics) != want || warnings != want {
			t.Errorf("%d warnings: %d diagnostics, %d of them warnings; want %d warnings", n, len(snap.LSPDiagnostics), warnings, want)
		}
	}
}
