package schema

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// TestResolveImportToRelative_UnrecordedImporterIsAnError holds the loader to
// resolving a relative import only from a host path it recorded reading: a
// file-backed identity it never read has no bytes known to name a file.
func TestResolveImportToRelative_UnrecordedImporterIsAnError(t *testing.T) {
	t.Parallel()

	sid, err := location.SourceIDFromAbsolutePath(filepath.Join(t.TempDir(), "main.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	l := newLoader(defaultLoadConfig(), "", "", diag.ModuleRootNone)
	if _, err := l.resolveImportToRelative(sid, "./dep"); !errors.Is(err, errNoHostPath) {
		t.Errorf("resolveImportToRelative of an unrecorded importer: err = %v; want errNoHostPath", err)
	}
}
