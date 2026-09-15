package schema

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// TestResolveImportToRelative_UnrecordedImporterIsAnError holds the loader to
// resolving a relative import only from a host path it recorded reading: a
// file-backed identity it never read has no bytes known to name a file.
func TestResolveImportToRelative_UnrecordedImporterIsAnError(t *testing.T) {
	t.Parallel()

	sid, err := location.SourceIDFromPath(filepath.Join(t.TempDir(), "main.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	l := newLoader(defaultLoadConfig(), "", "", diag.ModuleRootNone)
	if _, err := l.resolveImportToRelative(sid, "./dep"); !errors.Is(err, errNoHostPath) {
		t.Errorf("resolveImportToRelative of an unrecorded importer: err = %v; want errNoHostPath", err)
	}
}

// TestLoadImport_UnrecordedImporterIsInternal holds a relative import from an
// importer with no recorded host path to one E_INTERNAL at the declaration,
// never the author's E_IMPORT_RESOLVE, and binds its alias as failed.
func TestLoadImport_UnrecordedImporterIsInternal(t *testing.T) {
	t.Parallel()

	sid, err := location.SourceIDFromPath(filepath.Join(t.TempDir(), "main.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	l := newLoader(defaultLoadConfig(), "", "", diag.ModuleRootNone)
	imp := &importDecl{
		Path:  "./dep",
		Alias: "dep",
		Span: location.Span{
			Source: sid,
			Start:  location.Position{Line: 3, Column: 1, Byte: 16},
			End:    location.Position{Line: 3, Column: 22, Byte: 37},
		},
	}
	if err := l.loadImport(t.Context(), sid, imp); err != nil {
		t.Fatalf("loadImport: %v", err)
	}

	var issues []diag.Issue
	for issue := range l.collector.Result().Issues() {
		issues = append(issues, issue)
	}
	if len(issues) != 1 {
		t.Fatalf("got %d issues %v; want one E_INTERNAL", len(issues), issues)
	}
	if got := issues[0]; got.Code() != diag.E_INTERNAL || got.Span() != imp.Span || !strings.Contains(got.Message(), errNoHostPath.Error()) {
		t.Errorf("got %s at %v: %q; want E_INTERNAL at %v naming %q", got.Code(), got.Span(), got.Message(), imp.Span, errNoHostPath)
	}
	if b, ok := l.imports[imp.Alias]; !ok || !b.failed || !b.sourceID.IsZero() || b.decl != imp {
		t.Errorf("binding for %q = %+v, bound %v; want a failed binding of the declaration with no SourceID", imp.Alias, b, ok)
	}
}
