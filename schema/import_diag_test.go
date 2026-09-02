package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// resolutionIssue returns the first issue in the import-resolution family
// carrying the given code, and its details as a map.
func resolutionIssue(t *testing.T, res diag.Result, code diag.Code) (diag.Issue, map[string]string) {
	t.Helper()
	for issue := range res.Issues() {
		if issue.Code() != code {
			continue
		}
		if !diag.IsImportResolutionCode(issue.Code().String()) {
			t.Fatalf("%s is not in the import-resolution family", code)
		}
		details := map[string]string{}
		for _, d := range issue.Details() {
			details[d.Key] = d.Value
		}
		return issue, details
	}
	t.Fatalf("no %s issue in %v", code, res.Err())
	return diag.Issue{}, nil
}

// assertProvenance checks that an issue in the resolution family carries both
// detail keys and states the provenance in its message. Both are required:
// the structured keys serve --format json and library callers, and the message
// serves the text renderer and the plugin hook, neither of which prints
// details.
func assertProvenance(t *testing.T, issue diag.Issue, details map[string]string, wantRoot, wantOrigin, wantMessagePart string) {
	t.Helper()
	root, ok := details[diag.DetailKeyModuleRoot]
	if !ok {
		t.Errorf("issue %s carries no %s detail", issue.Code(), diag.DetailKeyModuleRoot)
	}
	if root != wantRoot {
		t.Errorf("%s = %q, want %q", diag.DetailKeyModuleRoot, root, wantRoot)
	}
	origin, ok := details[diag.DetailKeyModuleRootOrigin]
	if !ok {
		t.Errorf("issue %s carries no %s detail", issue.Code(), diag.DetailKeyModuleRootOrigin)
	}
	if origin != wantOrigin {
		t.Errorf("%s = %q, want %q", diag.DetailKeyModuleRootOrigin, origin, wantOrigin)
	}
	if !strings.Contains(issue.Message(), wantMessagePart) {
		t.Errorf("message %q does not state the provenance in words (want %q)", issue.Message(), wantMessagePart)
	}
}

// writeBrokenImportTree builds a tree whose entry imports a module-style path
// that does not exist, so every root arrangement reaches the same failure.
func writeBrokenImportTree(t *testing.T) (root, entry string) {
	t.Helper()
	root = t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	entry = filepath.Join(sub, "entry.yammm")
	src := "schema \"entry\"\n\nimport \"lib/missing\" as m\n\ntype T {\n\tid String primary\n\t--> USES (one) m.P\n}\n"
	if err := os.WriteFile(entry, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, entry
}

func TestImportResolveProvenance_Default(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	_, entry := writeBrokenImportTree(t)

	_, res := schema.Load(t.Context(), entry)
	issue, details := resolutionIssue(t, res, diag.E_IMPORT_RESOLVE)
	assertProvenance(t, issue, details,
		canonicalPath(t, filepath.Dir(entry)), diag.ModuleRootDefault, "defaulted to the entry schema's directory")

	// The remedy clause is the sentence that would have saved the consumer's
	// CLI failure: it names the marker, not just the root.
	if !strings.Contains(issue.Message(), schema.ModuleRootMarker) {
		t.Errorf("the default clause must name %s as the remedy; message: %q", schema.ModuleRootMarker, issue.Message())
	}
}

func TestImportResolveProvenance_Discovered(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root, entry := writeBrokenImportTree(t)
	writeMarker(t, root, "")

	_, res := schema.Load(t.Context(), entry)
	issue, details := resolutionIssue(t, res, diag.E_IMPORT_RESOLVE)
	assertProvenance(t, issue, details, canonicalPath(t, root), diag.ModuleRootDiscovered, "discovered from")
}

func TestImportResolveProvenance_Explicit(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root, entry := writeBrokenImportTree(t)

	_, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
	issue, details := resolutionIssue(t, res, diag.E_IMPORT_RESOLVE)
	assertProvenance(t, issue, details, canonicalPath(t, root), diag.ModuleRootExplicit, "given explicitly")
}

func TestImportResolveProvenance_Synthetic(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"assets/main.yammm": []byte("schema \"main\"\n\nimport \"assets/missing\" as m\n\ntype T {\n\tid String primary\n\t--> USES (one) m.P\n}\n"),
	}
	_, res := schema.LoadSourcesWithEntry(t.Context(), sources, "assets/main.yammm", "",
		schema.WithSyntheticRoot("embedded://app"), schema.WithSourcesOnly(true))
	issue, details := resolutionIssue(t, res, diag.E_IMPORT_RESOLVE)
	assertProvenance(t, issue, details, "embedded://app", diag.ModuleRootSynthetic, "synthetic root")
}

// TestImportResolveProvenance_None reaches the loader's no-root site: an
// in-memory load with an empty root and no synthetic root has no directory to
// default to, so the origin is "none" and the root is empty. A load that has
// no directory must not claim one.
func TestImportResolveProvenance_None(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"/main.yammm": []byte("schema \"main\"\n\nimport \"lib/missing\" as m\n\ntype T {\n\tid String primary\n\t--> USES (one) m.P\n}\n"),
	}
	_, res := schema.LoadSourcesWithEntry(t.Context(), sources, "/main.yammm", "")
	issue, details := resolutionIssue(t, res, diag.E_IMPORT_RESOLVE)
	assertProvenance(t, issue, details, "", diag.ModuleRootNone, "no module root in play")
}

func TestPathEscapeProvenance(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(sub, "entry.yammm")
	src := "schema \"entry\"\n\nimport \"../../outside/dep\" as d\n\ntype T {\n\tid String primary\n\t--> USES (one) d.P\n}\n"
	if err := os.WriteFile(entry, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	_, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
	issue, details := resolutionIssue(t, res, diag.E_PATH_ESCAPE)
	assertProvenance(t, issue, details, canonicalPath(t, root), diag.ModuleRootExplicit, "given explicitly")

	// One code, one shape: the escape site has the declaration in hand and
	// must carry the same details every sibling in the family carries.
	if details[diag.DetailKeyAlias] != "d" {
		t.Errorf("alias detail = %q, want %q", details[diag.DetailKeyAlias], "d")
	}
}

func TestImportCycleProvenance(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root := t.TempDir()
	a := filepath.Join(root, "a.yammm")
	b := filepath.Join(root, "b.yammm")
	if err := os.WriteFile(a, []byte("schema \"a\"\n\nimport \"b\" as b\n\ntype A {\n\tid String primary\n\t--> USES (one) b.B\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("schema \"b\"\n\nimport \"a\" as a\n\ntype B {\n\tid String primary\n\t--> USES (one) a.A\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, res := schema.Load(t.Context(), a, schema.WithModuleRoot(root))
	issue, details := resolutionIssue(t, res, diag.E_IMPORT_CYCLE)
	assertProvenance(t, issue, details, canonicalPath(t, root), diag.ModuleRootExplicit, "given explicitly")
}

// TestBuilderImportResolve_SameShape pins that the Builder's front door builds
// the code the same way the loader does. A family that carries one shape at
// one site and another at its sibling is the defect one builder per code
// exists to prevent. The Builder resolves through a registry and never through
// a root, so its origin is "none".
func TestBuilderImportResolve_SameShape(t *testing.T) {
	t.Parallel()

	b := schema.NewBuilder().
		WithName("main").
		WithSourceID(location.MustNewSourceID("builder://main"))
	b.AddImport("other", "other")
	_, res := b.Build()

	issue, details := resolutionIssue(t, res, diag.E_IMPORT_RESOLVE)
	assertProvenance(t, issue, details, "", diag.ModuleRootNone, "no module root in play")
}
