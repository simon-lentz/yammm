package schema_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// writeMarker creates a yammm.mod marker in dir with the given content.
func writeMarker(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, schema.ModuleRootMarker)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write marker %s: %v", path, err)
	}
}

// mkdirs creates a nested directory chain under root and returns the leaf.
func mkdirs(t *testing.T, root string, parts ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	return dir
}

func TestFindModuleRoot_NearestAncestorWins(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	tmp := t.TempDir()
	outer := mkdirs(t, tmp, "outer")
	inner := mkdirs(t, outer, "mid", "inner")
	writeMarker(t, outer, "")
	writeMarker(t, filepath.Dir(inner), "# the nearer marker\n")

	root, found, err := schema.FindModuleRoot(inner)
	if err != nil {
		t.Fatalf("FindModuleRoot: %v", err)
	}
	if !found {
		t.Fatal("no marker found; two sit on this chain")
	}
	if want := canonicalPath(t, filepath.Dir(inner)); root != want {
		t.Errorf("FindModuleRoot = %q, want the nearer %q", root, want)
	}
}

func TestFindModuleRoot_MarkerInStartDirectory(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	tmp := t.TempDir()
	writeMarker(t, tmp, "")

	root, found, err := schema.FindModuleRoot(tmp)
	if err != nil {
		t.Fatalf("FindModuleRoot: %v", err)
	}
	if !found {
		t.Fatal("a marker in the start directory must be found")
	}
	if want := canonicalPath(t, tmp); root != want {
		t.Errorf("FindModuleRoot = %q, want %q", root, want)
	}
}

func TestFindModuleRoot_NotFoundReachesFilesystemRoot(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	dir := mkdirs(t, t.TempDir(), "a", "b", "c")

	root, found, err := schema.FindModuleRoot(dir)
	if err != nil {
		t.Fatalf("FindModuleRoot: %v", err)
	}
	if found {
		t.Fatalf("found a marker at %q; the chain carries none", root)
	}
	if root != "" {
		t.Errorf("FindModuleRoot = %q on a miss, want \"\" so a caller can insert its own tier", root)
	}
}

func TestFindModuleRoot_LegalContent(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	for name, content := range map[string]string{
		"empty":               "",
		"comments":            "# one\n#two\n",
		"blank lines":         "\n\n\n",
		"indented comment":    "   # indented\n",
		"trailing whitespace": "# c   \n   \n",
		"crlf":                "# one\r\n\r\n",
		"no trailing newline": "# one",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeMarker(t, dir, content)
			root, found, err := schema.FindModuleRoot(dir)
			if err != nil {
				t.Fatalf("FindModuleRoot on legal content %q: %v", content, err)
			}
			if !found || root != canonicalPath(t, dir) {
				t.Errorf("FindModuleRoot = (%q, %v), want the marker's directory", root, found)
			}
		})
	}
}

func TestFindModuleRoot_MalformedContent(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	cases := map[string]struct {
		content    string
		wantLine   int
		wantReason string
	}{
		"non-comment line":        {"module foo\n", 1, "neither empty nor a comment"},
		"non-comment after blank": {"\n\nmodule foo\n", 3, "neither empty nor a comment"},
		"comment then content":    {"# ok\nnot ok\n", 2, "neither empty nor a comment"},
		// The BOM sits on a line that otherwise reads as a comment, so the
		// generic line rule would report it in terms the author cannot act on.
		"utf-8 BOM": {"\ufeff# ok\n", 1, "byte order mark"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeMarker(t, dir, tc.content)
			// The walk is canonical, so the reported path is too.
			marker := filepath.Join(canonicalPath(t, dir), schema.ModuleRootMarker)

			_, found, err := schema.FindModuleRoot(dir)
			if err == nil {
				t.Fatalf("FindModuleRoot returned no error for %q (found=%v)", tc.content, found)
			}
			mErr, ok := errors.AsType[*schema.MalformedModuleRootError](err)
			if !ok {
				t.Fatalf("error %v is not a *MalformedModuleRootError", err)
			}
			if mErr.Path != marker {
				t.Errorf("Path = %q, want %q", mErr.Path, marker)
			}
			if mErr.Line != tc.wantLine {
				t.Errorf("Line = %d, want %d", mErr.Line, tc.wantLine)
			}
			if !strings.Contains(mErr.Reason, tc.wantReason) {
				t.Errorf("Reason = %q, want it to contain %q", mErr.Reason, tc.wantReason)
			}
		})
	}
}

func TestFindModuleRoot_MalformedDirectory(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, schema.ModuleRootMarker), 0o750); err != nil {
		t.Fatalf("mkdir marker: %v", err)
	}

	_, _, err := schema.FindModuleRoot(dir)
	if _, ok := errors.AsType[*schema.MalformedModuleRootError](err); !ok {
		t.Fatalf("a directory named %s must be malformed, not skipped; got %v", schema.ModuleRootMarker, err)
	}
}

func TestFindModuleRoot_MalformedOversize(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	dir := t.TempDir()
	big := make([]byte, 65*1024)
	for i := range big {
		big[i] = '\n'
	}
	if err := os.WriteFile(filepath.Join(dir, schema.ModuleRootMarker), big, 0o600); err != nil {
		t.Fatalf("write oversize marker: %v", err)
	}

	_, _, err := schema.FindModuleRoot(dir)
	if _, ok := errors.AsType[*schema.MalformedModuleRootError](err); !ok {
		t.Fatalf("an over-size marker must be malformed by size; got %v", err)
	}
}

func TestFindModuleRoot_MalformedDanglingSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	dir := t.TempDir()
	link := filepath.Join(dir, schema.ModuleRootMarker)
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// os.Stat alone reports a dangling link as absent, which would silently
	// skip the marker the author committed.
	_, _, err := schema.FindModuleRoot(dir)
	if _, ok := errors.AsType[*schema.MalformedModuleRootError](err); !ok {
		t.Fatalf("a dangling symlink named %s must be malformed, not absent; got %v", schema.ModuleRootMarker, err)
	}
}

func TestFindModuleRoot_WalksTheCanonicalChain(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	tmp := t.TempDir()
	real := mkdirs(t, tmp, "real", "pkg")
	writeMarker(t, filepath.Dir(real), "")
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(filepath.Join(tmp, "real"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	direct, _, err := schema.FindModuleRoot(real)
	if err != nil {
		t.Fatalf("FindModuleRoot(direct): %v", err)
	}
	viaLink, _, err := schema.FindModuleRoot(filepath.Join(link, "pkg"))
	if err != nil {
		t.Fatalf("FindModuleRoot(symlinked): %v", err)
	}
	if direct != viaLink {
		t.Errorf("symlinked path discovered %q, direct path %q; the walk must be canonical", viaLink, direct)
	}
}

func TestModuleRootIssue_Shape(t *testing.T) {
	t.Parallel()

	malformed := &schema.MalformedModuleRootError{
		Path:   filepath.Join("/tmp", "proj", schema.ModuleRootMarker),
		Line:   2,
		Reason: "line is neither empty nor a comment",
	}
	issue := schema.ModuleRootIssue(malformed)
	if issue.Severity() != diag.Error {
		t.Errorf("severity = %v, want Error: a marker is user content, and Fatal is reserved for I/O", issue.Severity())
	}
	if issue.Code() != diag.E_LOAD_MODULE_ROOT_MALFORMED {
		t.Errorf("code = %v, want E_LOAD_MODULE_ROOT_MALFORMED", issue.Code())
	}
	details := map[string]string{}
	for _, d := range issue.Details() {
		details[d.Key] = d.Value
	}
	if got, want := details[diag.DetailKeyModuleRoot], filepath.Join("/tmp", "proj"); got != want {
		t.Errorf("module_root detail = %q, want the marker's directory %q", got, want)
	}
	if got := details[diag.DetailKeyModuleRootOrigin]; got != diag.ModuleRootDiscovered {
		t.Errorf("module_root_origin detail = %q, want %q", got, diag.ModuleRootDiscovered)
	}

	other := schema.ModuleRootIssue(errors.New("permission denied"))
	if other.Severity() != diag.Fatal {
		t.Errorf("severity = %v for a read error, want Fatal", other.Severity())
	}
	if other.Code() != diag.E_LOAD_IO_FAILURE {
		t.Errorf("code = %v for a read error, want E_LOAD_IO_FAILURE", other.Code())
	}
}

// TestLoad_DiscoversMarker pins the tier-2 rung of the ladder: with no
// explicit root, a marker above the entry directory becomes the module root,
// and a module-style import that reaches outside the entry directory resolves
// against it. Without discovery this load fails — it is the case the whole
// mechanism exists for.
func TestLoad_DiscoversMarker(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root, entry := writeModuleTree(t)
	writeMarker(t, root, "# project root\n")

	s, res := schema.Load(t.Context(), entry)
	if res.HasErrors() {
		t.Fatalf("load with a discovered root: %v", res.Err())
	}
	if want := canonicalPath(t, root); s.ModuleRoot() != want {
		t.Errorf("ModuleRoot = %q, want the discovered %q", s.ModuleRoot(), want)
	}
}

// TestLoad_NoMarkerKeepsTheEntryDirectory pins tier 4: the default is exactly
// what it was before discovery existed.
func TestLoad_NoMarkerKeepsTheEntryDirectory(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	dir := t.TempDir()
	path := filepath.Join(dir, "solo.yammm")
	if err := os.WriteFile(path, []byte("schema \"solo\"\n\ntype T {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatalf("write schema: %v", err)
	}

	s, res := schema.Load(t.Context(), path)
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	if want := canonicalPath(t, dir); s.ModuleRoot() != want {
		t.Errorf("ModuleRoot = %q, want the entry directory %q", s.ModuleRoot(), want)
	}
}

// TestLoad_MalformedMarkerFailsTheLoad pins that a marker the author wrote and
// the loader could not honour fails loudly rather than falling through to the
// entry directory — the silent-ignore failure mode discovery exists to remove.
// The severity is Error, not Fatal: the marker is user content, and HasFatal
// promises I/O or cancellation.
func TestLoad_MalformedMarkerFailsTheLoad(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root, entry := writeModuleTree(t)
	writeMarker(t, root, "module example.com/thing\n")

	s, res := schema.Load(t.Context(), entry)
	if s != nil {
		t.Error("a malformed marker must not produce a schema")
	}
	if !res.HasErrors() {
		t.Fatal("a malformed marker must fail the load")
	}
	if res.HasFatal() {
		t.Error("a malformed marker is Error severity; Fatal is reserved for I/O and cancellation")
	}

	var found bool
	for issue := range res.Issues() {
		if issue.Code() == diag.E_LOAD_MODULE_ROOT_MALFORMED {
			found = true
			details := map[string]string{}
			for _, d := range issue.Details() {
				details[d.Key] = d.Value
			}
			if got, want := details[diag.DetailKeyModuleRoot], canonicalPath(t, root); got != want {
				t.Errorf("module_root detail = %q, want %q", got, want)
			}
			if got := details[diag.DetailKeyModuleRootOrigin]; got != diag.ModuleRootDiscovered {
				t.Errorf("module_root_origin detail = %q, want %q", got, diag.ModuleRootDiscovered)
			}
		}
	}
	if !found {
		t.Errorf("no E_LOAD_MODULE_ROOT_MALFORMED in %v", res.Err())
	}
}

// TestLoad_ExplicitRootDoesNotReadTheMarker pins the one deliberate exception
// to "a marker is never ignored": tier 1 wins before discovery runs, so a
// caller can always override a marker it did not put there — including a
// broken one.
func TestLoad_ExplicitRootDoesNotReadTheMarker(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root, entry := writeModuleTree(t)
	writeMarker(t, root, "definitely not a comment\n")

	s, res := schema.Load(t.Context(), entry, schema.WithModuleRoot(root))
	if res.HasErrors() {
		t.Fatalf("an explicit root must win before discovery runs: %v", res.Err())
	}
	if want := canonicalPath(t, root); s.ModuleRoot() != want {
		t.Errorf("ModuleRoot = %q, want the explicit %q", s.ModuleRoot(), want)
	}
}

// TestModuleRoot_ReportsSyntheticRoot pins A-175: the accessor reports the
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
	s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "assets/main.yammm", "",
		schema.WithSyntheticRoot("embedded://app"), schema.WithSourcesOnly(true))
	if res.HasErrors() {
		t.Fatalf("synthetic load: %v", res.Err())
	}
	if got := s.ModuleRoot(); got != "embedded://app" {
		t.Errorf("ModuleRoot = %q, want the synthetic root %q", got, "embedded://app")
	}
}
