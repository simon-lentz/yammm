package schema

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// A source the registry already holds with other bytes is a loader fault, never
// the author's: each of the three sites that register a source raises one Fatal
// E_INTERNAL for it.
func TestLoad_ARegistryCollisionIsAnInternalFault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mainPath, depPath := filepath.Join(dir, "main.yammm"), filepath.Join(dir, "dep.yammm")
	mainContent := []byte("schema \"main\"\nimport \"./dep\" as dep\ntype A {\n id String primary\n}\n")
	depContent := []byte("schema \"dep\"\ntype B {\n id String primary\n}\n")
	for path, content := range map[string][]byte{mainPath: mainContent, depPath: depContent} {
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mainID, err := location.SourceIDFromPath(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	depID, err := location.SourceIDFromPath(depPath)
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		held location.SourceID
		want string
		load func(l *loader) error
	}{
		// The message quotes the path as %q does, so a Windows path's separators read escaped.
		"a file": {held: mainID, want: "register source " + strconv.Quote(mainPath), load: func(l *loader) error {
			_, _, err := l.loadFile(t.Context(), mainID, mainPath, mainContent)
			return err
		}},
		"a source": {held: mainID, want: "register source " + mainID.String(), load: func(l *loader) error {
			_, _, err := l.loadSource(t.Context(), mainID, mainContent)
			return err
		}},
		"an import": {held: depID, want: "register import source", load: func(l *loader) error {
			_, _, err := l.loadFile(t.Context(), mainID, mainPath, mainContent)
			return err
		}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l := newLoader(defaultLoadConfig(), dir, "", diag.ModuleRootDefault)
			defer l.Close()
			if err := l.sourceRegistry.Register(c.held, []byte("other bytes")); err != nil {
				t.Fatal(err)
			}
			if err := c.load(l); err != nil {
				t.Fatal(err)
			}
			var internal []diag.Issue
			for issue := range l.collector.Result().Issues() {
				if issue.Code() == diag.E_INTERNAL {
					internal = append(internal, issue)
				}
			}
			if len(internal) != 1 || internal[0].Severity() != diag.Fatal || !strings.HasPrefix(internal[0].Message(), c.want) {
				t.Errorf("E_INTERNAL issues %v, want one Fatal starting %q: %s", internal, c.want, l.collector.Result())
			}
		})
	}
}
