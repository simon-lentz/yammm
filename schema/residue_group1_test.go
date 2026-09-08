package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// TestLoadString_ImportDoesNotReadTheWorkingDirectory pins that an import
// with no module root in play resolves against the pre-registered sources
// alone. The file this test places at the import's own relative path is in
// the process's working directory and nowhere else, so a loader that fell
// back to a working-directory read would find it and load clean. Restoring
// that read turns this red.
func TestLoadString_ImportDoesNotReadTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	const imported = "shared.yammm"
	// A relative-path import is the form that reaches readImportFile; a bare
	// name is refused a tier earlier as module-style.
	const importSpec = "./" + imported
	if err := os.WriteFile(filepath.Join(dir, imported), []byte(`schema "shared"

type Shared {
	id String primary
}
`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Go 1.24: the working directory is the process's, restored at cleanup.
	t.Chdir(dir)

	// An EMPTY module root, no synthetic root and no WithSourcesOnly: every
	// tier of the resolution ladder is absent except the in-memory set, which
	// holds the entry file alone.
	sources := map[string][]byte{"main.yammm": []byte(`schema "main"

import "` + importSpec + `"

type Main {
	id String primary
}
`)}
	_, res := schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", "")
	if res.Err() == nil {
		t.Fatal("the import resolved with no module root in play: the working directory was read")
	}
	if got := res.Err().Error(); !strings.Contains(got, "is not among the pre-registered sources and no module root is in play") {
		t.Errorf("want readImportFile's no-module-root refusal; got %v", got)
	}
}
