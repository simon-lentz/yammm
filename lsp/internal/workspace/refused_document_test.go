package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
)

// TestOpenDocument_PublishesARefusedPathsDiagnosticUnderTheClientsURI holds a
// document at a path the resolver refuses — under a regular file — to one
// diagnostic, published under the URI the client opened it with, however that
// URI spells its escapes.
func TestOpenDocument_PublishesARefusedPathsDiagnosticUnderTheClientsURI(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "regular"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := lsputil.PathToURI(filepath.Join(root, "regular"))

	rows := []struct {
		name string
		uri  string
	}{
		{"escapes spelled as the server spells them", base + "/caf%C3%A9.yammm"},
		{"escapes spelled in lower case", base + "/caf%c3%a9.yammm"},
		{"an escaped tilde", base + "/%7Egone.yammm"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			ws := newTestWorkspace(t, quietLogger(), Config{})
			collector := &testutil.NotificationCollector{}
			ws.SetNotifier(collector.Notify)

			ws.OpenDocument(row.uri, 1, "schema \"gone\"\n\ntype A {\n\tid String primary\n}\n")

			if got := collector.DiagnosticsFor(row.uri); len(got) != 1 {
				t.Errorf("published %d diagnostics under the client's URI %s, want 1", len(got), row.uri)
			}
		})
	}
}
