package moduleroot

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
)

// writeMarker creates a yammm.mod marker in dir with the given content.
func writeMarker(t *testing.T, dir, content string) {
	t.Helper()
	path := filepath.Join(dir, Marker)
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

func TestFind_NearestAncestorWins(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, Find)

	tmp := t.TempDir()
	outer := mkdirs(t, tmp, "outer")
	inner := mkdirs(t, outer, "mid", "inner")
	writeMarker(t, outer, "")
	writeMarker(t, filepath.Dir(inner), "# the nearer marker\n")

	root, found, err := Find(inner)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !found {
		t.Fatal("no marker found; two sit on this chain")
	}
	if want := canonicalPath(t, filepath.Dir(inner)); root != want {
		t.Errorf("Find = %q, want the nearer %q", root, want)
	}
}

func TestFind_MarkerInStartDirectory(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, Find)

	tmp := t.TempDir()
	writeMarker(t, tmp, "")

	root, found, err := Find(tmp)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if !found {
		t.Fatal("a marker in the start directory must be found")
	}
	if want := canonicalPath(t, tmp); root != want {
		t.Errorf("Find = %q, want %q", root, want)
	}
}

func TestFind_NotFoundReachesFilesystemRoot(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, Find)

	dir := mkdirs(t, t.TempDir(), "a", "b", "c")

	root, found, err := Find(dir)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if found {
		t.Fatalf("found a marker at %q; the chain carries none", root)
	}
	if root != "" {
		t.Errorf("Find = %q on a miss, want \"\" so a caller can insert its own tier", root)
	}
}

func TestFind_LegalContent(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, Find)

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
			root, found, err := Find(dir)
			if err != nil {
				t.Fatalf("Find on legal content %q: %v", content, err)
			}
			if !found || root != canonicalPath(t, dir) {
				t.Errorf("Find = (%q, %v), want the marker's directory", root, found)
			}
		})
	}
}

func TestFind_MalformedContent(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, Find)

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
			marker := filepath.Join(canonicalPath(t, dir), Marker)

			_, found, err := Find(dir)
			if err == nil {
				t.Fatalf("Find returned no error for %q (found=%v)", tc.content, found)
			}
			mErr, ok := errors.AsType[*MalformedError](err)
			if !ok {
				t.Fatalf("error %v is not a *MalformedError", err)
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

func TestFind_MalformedDirectory(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, Find)

	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, Marker), 0o750); err != nil {
		t.Fatalf("mkdir marker: %v", err)
	}

	_, _, err := Find(dir)
	me, ok := errors.AsType[*MalformedError](err)
	if !ok {
		t.Fatalf("a directory named %s must be malformed, not skipped; got %v", Marker, err)
	}
	if me.Reason != "is a directory, not a marker file" {
		t.Errorf("Reason = %q; want the directory named as such", me.Reason)
	}
}

func TestFind_MalformedOversize(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, Find)

	dir := t.TempDir()
	big := make([]byte, 65*1024)
	for i := range big {
		big[i] = '\n'
	}
	if err := os.WriteFile(filepath.Join(dir, Marker), big, 0o600); err != nil {
		t.Fatalf("write oversize marker: %v", err)
	}

	_, _, err := Find(dir)
	if _, ok := errors.AsType[*MalformedError](err); !ok {
		t.Fatalf("an over-size marker must be malformed by size; got %v", err)
	}
	// A marker of exactly the limit is a marker.
	if err := os.WriteFile(filepath.Join(dir, Marker), big[:maxMarkerSize], 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Find(dir); err != nil {
		t.Errorf("a marker of exactly %d bytes = %v; want it read", maxMarkerSize, err)
	}
}

func TestFind_MalformedDanglingSymlink(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	yammmtest.RequireNoModuleRoot(t, Find)

	dir := t.TempDir()
	link := filepath.Join(dir, Marker)
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// os.Stat alone reports a dangling link as absent, which would silently
	// skip the marker the author committed.
	_, _, err := Find(dir)
	if _, ok := errors.AsType[*MalformedError](err); !ok {
		t.Fatalf("a dangling symlink named %s must be malformed, not absent; got %v", Marker, err)
	}
}

func TestFind_WalksTheCanonicalChain(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	yammmtest.RequireNoModuleRoot(t, Find)

	tmp := t.TempDir()
	real := mkdirs(t, tmp, "real", "pkg")
	writeMarker(t, filepath.Dir(real), "")
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(filepath.Join(tmp, "real"), link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	direct, _, err := Find(real)
	if err != nil {
		t.Fatalf("Find(direct): %v", err)
	}
	viaLink, _, err := Find(filepath.Join(link, "pkg"))
	if err != nil {
		t.Fatalf("Find(symlinked): %v", err)
	}
	if direct != viaLink {
		t.Errorf("symlinked path discovered %q, direct path %q; the walk must be canonical", viaLink, direct)
	}
}

// TestFind_SpellsTheRootAsItsDirectoryListsIt holds discovery to the
// directory a start path names rather than to the bytes it was typed with:
// where the filesystem finds one directory by two spellings, the root is the
// spelling its parent lists.
func TestFind_SpellsTheRootAsItsDirectoryListsIt(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, Find)

	base := t.TempDir()
	if !yammmtest.CaseFoldingFilesystem(t, base) {
		t.Skip("the filesystem is case-sensitive, so two spellings name two directories")
	}
	root := mkdirs(t, base, "Proj")
	mkdirs(t, root, "Sub")
	writeMarker(t, root, "")

	typed := filepath.Join(base, "proj", "sub")
	got, found, err := Find(typed)
	if err != nil {
		t.Fatalf("Find(%q): %v", typed, err)
	}
	if !found {
		t.Fatalf("Find(%q) found no marker; one sits in %q", typed, root)
	}
	if want := canonicalPath(t, filepath.Dir(typed)); got != want {
		t.Errorf("Find(%q) = %q, want %q", typed, got, want)
	}
}

// TestFind_RefusesAPathTheResolverRefuses holds discovery to the
// resolver's refusals: an empty path and a path that is not valid UTF-8 name no
// directory, so no walk starts from the working directory in their place.
func TestFind_RefusesAPathTheResolverRefuses(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		dir  string
		want error
	}{
		{"an empty path", "", location.ErrEmptyPath},
		{"a path that is not valid UTF-8", "caf\xff", location.ErrInvalidUTF8Path},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root, found, err := Find(row.dir)
			if !errors.Is(err, row.want) {
				t.Errorf("Find(%q) error = %v, want %v", row.dir, err, row.want)
			}
			if found || root != "" {
				t.Errorf("Find(%q) = (%q, %v), want (\"\", false)", row.dir, root, found)
			}
		})
	}
}

func TestIssue_Shape(t *testing.T) {
	t.Parallel()

	malformed := &MalformedError{
		Path:   filepath.Join("/tmp", "proj", Marker),
		Line:   2,
		Reason: "line is neither empty nor a comment",
	}
	issue := Issue(malformed)
	if issue.Severity() != diag.Error {
		t.Errorf("severity = %v, want Error: a marker is user content, reported as an invalid schema is", issue.Severity())
	}
	if issue.Code() != diag.E_LOAD_MODULE_ROOT_MALFORMED {
		t.Errorf("code = %v, want E_LOAD_MODULE_ROOT_MALFORMED", issue.Code())
	}
	details := map[string]string{}
	for _, d := range issue.Details() {
		details[d.Key] = d.Value
	}
	root, err := location.SourceIDFromPath(filepath.Join("/tmp", "proj"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := details[diag.DetailKeyModuleRoot], root.String(); got != want {
		t.Errorf("module_root detail = %q, want the marker's directory %q", got, want)
	}
	if got := details[diag.DetailKeyModuleRootOrigin]; got != diag.ModuleRootDiscovered {
		t.Errorf("module_root_origin detail = %q, want %q", got, diag.ModuleRootDiscovered)
	}

	other := Issue(errors.New("permission denied"))
	if other.Severity() != diag.Fatal {
		t.Errorf("severity = %v for a read error, want Fatal", other.Severity())
	}
	if other.Code() != diag.E_LOAD_IO_FAILURE {
		t.Errorf("code = %v for a read error, want E_LOAD_IO_FAILURE", other.Code())
	}
}

// canonicalPath is the existing path as its directories list it, read by
// [yammmtest.DiskSpelling], which shares no code with the resolver.
func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	spelled, err := yammmtest.DiskSpelling(path)
	if err != nil {
		t.Fatal(err)
	}
	return spelled
}
