package schema_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/moduleroot"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// writeMarker creates a yammm.mod marker in dir with the given content.
func writeMarker(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, moduleroot.Marker)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write marker %s: %v", path, err)
	}
}

// TestLoad_DiscoversMarker pins the tier-2 rung of the ladder: with no
// explicit root, a marker above the entry directory becomes the module root,
// and a module-style import that reaches outside the entry directory resolves
// against it. Without discovery this load fails — it is the case the whole
// mechanism exists for.
func TestLoad_DiscoversMarker(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, moduleroot.Find)

	root, entry := writeModuleTree(t)
	writeMarker(t, root, "# project root\n")

	s, err := schema.Load(t.Context(), entry)
	if err != nil {
		t.Fatalf("load with a discovered root: %v", err)
	}
	if want := canonicalPath(t, root); s.ModuleRoot() != want {
		t.Errorf("ModuleRoot = %q, want the discovered %q", s.ModuleRoot(), want)
	}
}

// TestLoad_NoMarkerKeepsTheEntryDirectory pins tier 4: the default is exactly
// what it was before discovery existed.
func TestLoad_NoMarkerKeepsTheEntryDirectory(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, moduleroot.Find)

	dir := t.TempDir()
	path := filepath.Join(dir, "solo.yammm")
	if err := os.WriteFile(path, []byte("schema \"solo\"\n\ntype T {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatalf("write schema: %v", err)
	}

	s, res := schema.Load(t.Context(), path)
	if res != nil {
		t.Fatalf("load: %v", res)
	}
	if want := canonicalPath(t, dir); s.ModuleRoot() != want {
		t.Errorf("ModuleRoot = %q, want the entry directory %q", s.ModuleRoot(), want)
	}
}

// TestLoad_MalformedMarkerFailsTheLoad pins that a marker the author wrote and
// the loader could not honour fails loudly rather than falling through to the
// entry directory — the silent-ignore failure mode discovery exists to remove.
// The severity is Error, not Fatal: the marker is user content its author
// corrects, rather than the machine-level failure Fatal marks. The load stops
// either way, as the assertion on a zero schema below pins.
func TestLoad_MalformedMarkerFailsTheLoad(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, moduleroot.Find)

	root, entry := writeModuleTree(t)
	writeMarker(t, root, "module example.com/thing\n")

	s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.Load(t.Context(), entry, schema.WithWarnings(warn))
	})
	if !s.IsZero() {
		t.Error("a malformed marker must not produce a schema")
	}
	if diagtest.Err(res) == nil {
		t.Fatal("a malformed marker must fail the load")
	}
	if res.HasFatal() {
		t.Error("a malformed marker is Error severity, reported as an invalid schema is, not Fatal")
	}

	var found bool
	for issue := range res.Issues() {
		if issue.Code() == diag.E_LOAD_MODULE_ROOT_MALFORMED {
			found = true
			details := map[string]string{}
			for _, d := range issue.Details() {
				details[d.Key] = d.Value
			}
			if got, want := details[diag.DetailKeyModuleRoot], rootIdentity(t, root); got != want {
				t.Errorf("module_root detail = %q, want %q", got, want)
			}
			if got := details[diag.DetailKeyModuleRootOrigin]; got != diag.ModuleRootDiscovered {
				t.Errorf("module_root_origin detail = %q, want %q", got, diag.ModuleRootDiscovered)
			}
		}
	}
	if !found {
		t.Errorf("no E_LOAD_MODULE_ROOT_MALFORMED in %v", res)
	}
}

// TestLoad_ExplicitRootDoesNotReadTheMarker pins the one deliberate exception
// to "a marker is never ignored": tier 1 wins before discovery runs, so a
// caller can always override a marker it did not put there — including a
// broken one.
func TestLoad_ExplicitRootDoesNotReadTheMarker(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, moduleroot.Find)

	root, entry := writeModuleTree(t)
	writeMarker(t, root, "definitely not a comment\n")

	s, err := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
	if err != nil {
		t.Fatalf("an explicit root must win before discovery runs: %v", err)
	}
	if want := canonicalPath(t, root); s.ModuleRoot() != want {
		t.Errorf("ModuleRoot = %q, want the explicit %q", s.ModuleRoot(), want)
	}
}

// TestModuleRoot_ReportsSyntheticRoot pins that the accessor reports the
// root the load actually resolved imports against. A synthetic root stands in
// for the module root — WithSyntheticRoot's own contract — so an accessor
// returning "" denied the root the loader used, and gogen's embedded keys
// were derived from a fallback instead.
func TestModuleRoot_ReportsSyntheticRoot(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"assets/main.yammm": []byte("schema \"main\"\n\nimport \"assets/dep\" as dep\n\ntype T {\n\tid String primary\n\t--> USES (one) dep.P\n}\n"),
		"assets/dep.yammm":  []byte("schema \"dep\"\n\ntype P {\n\tpid String primary\n}\n"),
	}
	s, err := schema.LoadSourcesWithEntry(t.Context(), sources, "assets/main.yammm",
		schema.WithSyntheticRoot("embedded://app"), schema.WithSourcesOnly(true))
	if err != nil {
		t.Fatalf("synthetic load: %v", err)
	}
	if got := s.ModuleRoot(); got != "embedded://app" {
		t.Errorf("ModuleRoot = %q, want the synthetic root %q", got, "embedded://app")
	}
}
