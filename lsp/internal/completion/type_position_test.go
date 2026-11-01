package completion

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

func TestTypePositions_AFailedLoadCompletesBuiltinsAlone(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.yammm")
	text := []byte("schema \"bad\"\n\ntype A {\n\tid String primary\n\tx Missing\n}\n")
	if err := os.WriteFile(bad, text, 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := analysis.NewAnalyzer(nil).Analyze(t.Context(), bad, map[string][]byte{bad: text}, dir, lsputil.PositionEncodingUTF16)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Schema.IsZero() {
		t.Fatal("the load must fail for this test to assert anything")
	}
	if got := TypeCompletions(snap, snap.EntrySourceID); len(got) != 0 {
		t.Errorf("TypeCompletions = %d items, want none", len(got))
	}
	if got := PropertyTypeCompletions(snap, snap.EntrySourceID); len(got) != len(BuiltinTypes) {
		t.Errorf("PropertyTypeCompletions = %d items, want the built-ins", len(got))
	}
}

func TestPropertyTypeCompletions_ImportedDatatypesComeFromTheImport(t *testing.T) {
	dir := t.TempDir()
	entry := filepath.Join(dir, "main.yammm")
	text := []byte("schema \"main\"\n\nimport \"./parts\" as p\n\ntype Local = String\n\ntype A {\n\tid String primary\n\tx p.Word\n}\n")
	if err := os.WriteFile(entry, text, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "parts.yammm"), []byte("schema \"parts\"\n\ntype Word = String\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := analysis.NewAnalyzer(nil).Analyze(t.Context(), entry, map[string][]byte{entry: text}, dir, lsputil.PositionEncodingUTF16)
	if err != nil || !snap.Result.OK() {
		t.Fatal(err, snap.Result)
	}
	var labels []string
	for _, it := range PropertyTypeCompletions(snap, snap.EntrySourceID) {
		labels = append(labels, it.Label)
	}
	if !slices.Contains(labels, "p.Word") || slices.Contains(labels, "p.Local") {
		t.Errorf("labels = %q", labels)
	}
}
