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
	l := newLoader(newLoadConfig(nil), "", "", diag.ModuleRootNone)
	if _, err := l.resolveImportToRelative(sid, "./dep"); !errors.Is(err, errNoHostPath) {
		t.Errorf("resolveImportToRelative of an unrecorded importer: err = %v; want errNoHostPath", err)
	}
}

// TestLoadImport_UnrecordedImporterIsInternal holds a relative import from an
// importer with no recorded host path to one Fatal E_INTERNAL at the declaration,
// never the author's E_IMPORT_RESOLVE, and binds its alias as failed.
func TestLoadImport_UnrecordedImporterIsInternal(t *testing.T) {
	t.Parallel()

	sid, err := location.SourceIDFromPath(filepath.Join(t.TempDir(), "main.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	var warnings []diag.Issue
	l := newLoader(newLoadConfig([]Option{WithWarnings(func(issue diag.Issue) { warnings = append(warnings, issue) })}), "", "", diag.ModuleRootNone)
	imp := &importDecl{
		Path:  "./dep",
		Alias: "dep",
		Span: location.Span{
			Source: sid,
			Start:  location.Position{Line: 3, Column: 1, Offset: 16},
			End:    location.Position{Line: 3, Column: 22, Offset: 37},
		},
	}
	if err := l.loadImport(t.Context(), sid, imp); err != nil {
		t.Fatalf("loadImport: %v", err)
	}

	var issues []diag.Issue
	for issue := range l.collector.Result().Issues() {
		issues = append(issues, issue)
	}
	if len(issues) != 1 || len(warnings) != 0 {
		t.Fatalf("got %d issues %v and warnings %v; want one E_INTERNAL and no warning", len(issues), issues, warnings)
	}
	if got := issues[0]; got.Code() != diag.E_INTERNAL || got.Severity() != diag.Fatal || got.Span() != imp.Span || !strings.Contains(got.Message(), errNoHostPath.Error()) {
		t.Errorf("got %s %s at %v: %q; want a Fatal E_INTERNAL at %v naming %q", got.Severity(), got.Code(), got.Span(), got.Message(), imp.Span, errNoHostPath)
	}
	if b, ok := l.imports[imp.Alias]; !ok || !b.failed || !b.sourceID.IsZero() || b.decl != imp {
		t.Errorf("binding for %q = %+v, bound %v; want a failed binding of the declaration with no SourceID", imp.Alias, b, ok)
	}
}
