package format_test

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/format"
	"github.com/simon-lentz/yammm/internal/gittree"
	"github.com/simon-lentz/yammm/schema"
)

// The variants perturb a corpus input in the ways the formatter has been
// measured to mishandle: operators written without spaces, a trailing comment
// carrying brackets and a quote, a blank line inside a block comment, and a
// trailing comment that ends in a brace.
var (
	variantTightOps   = regexp.MustCompile(`[ \t]*(&&|\|\|)[ \t]*`)
	variantLineEnd    = regexp.MustCompile(`(?m)([^\n])$`)
	variantBlockStart = regexp.MustCompile(`/\*`)
)

// variantsOf returns src and its perturbations, named.
func variantsOf(src string) map[string]string {
	return map[string]string{
		"original":             src,
		"tight-operators":      variantTightOps.ReplaceAllString(src, "$1"),
		"trailing-comment":     variantLineEnd.ReplaceAllString(src, "$1 // c ] { \"q\""),
		"blank-in-block":       variantBlockStart.ReplaceAllString(src, "/*\n\n"),
		"comment-ending-brace": variantLineEnd.ReplaceAllString(src, "$1 // c {"),
	}
}

// parses reports whether src has no syntax error, which is the whole of what
// Source requires of its input.
func parses(src string) bool {
	_, err := formatText(src)
	return err == nil || !strings.HasPrefix(err.Error(), "parse failed")
}

// repositorySchemas returns the text of every schema [schemaPaths] names.
func repositorySchemas(tb testing.TB) []string {
	tb.Helper()
	_, paths := schemaPaths(tb)
	srcs := make([]string, 0, len(paths))
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			tb.Fatal(err)
		}
		srcs = append(srcs, string(b))
	}
	return srcs
}

// schemaPaths returns the module root and every .yammm in the module outside
// this package's own fixtures, walked from that root. A directory holding
// another go.mod is another module, and dot directories and node_modules hold
// no tracked schema, so the walk skips them.
func schemaPaths(tb testing.TB) (root string, paths []string) {
	tb.Helper()
	pkgDir := packageDir(tb)
	root = moduleRoot(tb, pkgDir)
	own := filepath.Join(pkgDir, "testdata")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root {
			if strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || path == own {
				return filepath.SkipDir
			}
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				return filepath.SkipDir
			}
		}
		if !d.IsDir() && strings.HasSuffix(path, ".yammm") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		tb.Fatalf("walking the module: %v", err)
	}
	if len(paths) < 100 {
		tb.Fatalf("found %d schemas; the walk is not reaching the module", len(paths))
	}
	return root, paths
}

// fakeModule writes a module under a temporary directory and
// returns its resolved root: 100 schemas the walk reads, one in each tree it skips (a
// nested module, a dot directory, node_modules and the package's own
// testdata), and the package directory internal/format.
func fakeModule(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/fake\n")
	for i := range 100 {
		write(fmt.Sprintf("schemas/s%03d.yammm", i), "schema \"s\"\n")
	}
	write("nested/go.mod", "module example.com/nested\n")
	write("nested/n.yammm", "schema \"n\"\n")
	write(".scratch/d.yammm", "schema \"d\"\n")
	write("node_modules/m.yammm", "schema \"m\"\n")
	write("internal/format/testdata/own.yammm", "schema \"o\"\n")
	// The walk reads the resolved path, and a temporary directory can lie below
	// a link, as /var does on macOS.
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// TestSchemaPaths_WalkOneModule pins the walk's skips: from the package
// directory it reads the module's 100 schemas and none in a nested module, a
// dot directory, node_modules or the package's own testdata.
//
// Not parallel: it changes the working directory.
func TestSchemaPaths_WalkOneModule(t *testing.T) {
	root := fakeModule(t)
	t.Chdir(filepath.Join(root, "internal", "format"))
	_, paths := schemaPaths(t)
	for _, path := range paths {
		if rel, _ := filepath.Rel(root, path); !strings.HasPrefix(filepath.ToSlash(rel), "schemas/") {
			t.Errorf("the walk read %s", rel)
		}
	}
	if len(paths) != 100 {
		t.Errorf("the walk read %d schemas, want 100", len(paths))
	}
}

// TestSchemaPaths_ReachTheModuleThroughALink pins the walk from a package
// directory reached through a symbolic link, which is the working directory go
// test gives a checkout opened by a link's path.
//
// Not parallel: it changes the working directory.
func TestSchemaPaths_ReachTheModuleThroughALink(t *testing.T) {
	root := fakeModule(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("this host makes no symbolic link: %v", err)
	}
	t.Chdir(filepath.Join(link, "internal", "format"))
	if _, paths := schemaPaths(t); len(paths) != 100 {
		t.Errorf("through the link the walk read %d schemas, want 100", len(paths))
	}
}

// TestSchemaPaths_HoldEveryTrackedSchema pins the seed walk's reach: every
// .yammm git tracks and the work tree holds, outside this package's own
// fixtures, is a path the walk returns. The walk may hold more, as a
// checkout's untracked schemas.
func TestSchemaPaths_HoldEveryTrackedSchema(t *testing.T) {
	t.Parallel()
	root, paths := schemaPaths(t)
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git to list the tracked tree")
	}
	gittree.MarkTrackedFiles()
	cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z", "--", "*.yammm")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("%s is no git work tree to list: %v", root, err)
	}
	walked := make(map[string]bool, len(paths))
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		walked[filepath.ToSlash(rel)] = true
	}
	own, err := filepath.Rel(root, filepath.Join(packageDir(t), "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	own = filepath.ToSlash(own) + "/"
	tracked := 0
	for rel := range strings.SplitSeq(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		if rel == "" || strings.HasPrefix(rel, own) {
			continue
		}
		// The index can name a file the work tree has deleted and not staged.
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		tracked++
		if !walked[rel] {
			t.Errorf("the walk misses the tracked schema %s", rel)
		}
	}
	if tracked == 0 {
		t.Error("git lists no schema outside this package's fixtures")
	}
	if srcs := repositorySchemas(t); len(srcs) != len(paths) {
		t.Errorf("the seeds hold %d texts for %d walked paths", len(srcs), len(paths))
	}
}

// packageDir returns the package directory with its symbolic links resolved.
// filepath.WalkDir does not follow a link, and go test sets the working
// directory to the logical path, which names one when the checkout is reached
// through a link.
func packageDir(tb testing.TB) string {
	tb.Helper()
	wd, err := os.Getwd()
	if err != nil {
		tb.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(wd)
	if err != nil {
		tb.Fatal(err)
	}
	return dir
}

// moduleRoot returns the nearest directory at or above dir that holds a go.mod.
// That go.mod is the package's own module's, so moving the package changes
// nothing the walk reads.
func moduleRoot(tb testing.TB, dir string) string {
	tb.Helper()
	for {
		_, err := os.Stat(filepath.Join(dir, "go.mod"))
		if err == nil {
			return dir
		}
		if !errors.Is(err, fs.ErrNotExist) {
			tb.Fatal(err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatalf("no go.mod above the package directory")
		}
		dir = parent
	}
}

// formatOutcome runs one input through the formatter and returns the first
// property it fails, or empty: the output holds the input's tokens and
// comments, is a fixed point, and obeys the documented layout.
func formatOutcome(src string) string {
	out, err := formatText(src)
	if err != nil {
		return "Source: " + err.Error()
	}
	if err := format.Preserves(src, out); err != nil {
		return "output does not preserve the source: " + err.Error()
	}
	again, err := formatText(out)
	if err != nil {
		return "Source on its own output: " + err.Error()
	}
	if again != out {
		return "not idempotent"
	}
	if v := format.LayoutViolations(out); len(v) > 0 {
		return "layout: " + strings.Join(v, "; ")
	}
	return ""
}

// TestSource_CorpusVariants runs every corpus input and its variants
// through the formatter's three properties. A variant that no longer parses is
// skipped: the formatter owes nothing to a source that has a syntax error.
func TestSource_CorpusVariants(t *testing.T) {
	t.Parallel()

	inputs := discoverInputs(t)
	checked := 0
	for _, path := range inputs {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		variants := variantsOf(string(src))
		names := make([]string, 0, len(variants))
		for name := range variants {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			v := variants[name]
			if !parses(v) {
				continue
			}
			checked++
			key := path + "#" + name
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				checkRepairState(t, key, formatOutcome(v))
			})
		}
	}
	if checked < 100 {
		t.Fatalf("checked %d inputs; the corpus walk is not reaching the fixtures", checked)
	}
}

// TestSource_FailsClosedOnEveryVariant asserts, over every corpus input
// and variant, that Source returns output preserving the source or
// refuses with ErrNotPreserved and returns nothing — whichever defects are live.
func TestSource_FailsClosedOnEveryVariant(t *testing.T) {
	t.Parallel()

	checked, refused := 0, 0
	for _, path := range discoverInputs(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for name, v := range variantsOf(string(src)) {
			out, err := formatText(v)
			switch {
			case errors.Is(err, format.ErrNotPreserved):
				refused++
				if out != "" {
					t.Errorf("%s#%s: a refusal returned output", path, name)
				}
			case err != nil:
				if !strings.HasPrefix(err.Error(), "parse failed") {
					t.Errorf("%s#%s: an error that is neither a refusal nor a parse failure: %v", path, name, err)
				}
			default:
				checked++
				if perr := format.Preserves(v, out); perr != nil {
					t.Errorf("%s#%s: output that does not preserve the source: %v", path, name, perr)
				}
			}
		}
	}
	if checked < 100 {
		t.Fatalf("checked %d outputs; the corpus walk is not reaching the fixtures", checked)
	}
	t.Logf("%d outputs preserve their source; %d variants refused", checked, refused)
}

// FuzzSource is the formatter's property under fuzzing: for any input
// that parses, formatting keeps its tokens and comments, is a fixed point,
// and — when the input loads — keeps its structural hash. The seeds are the
// repository's schemas; this package's fixtures, built to reproduce defects,
// are the corpus test's.
func FuzzSource(f *testing.F) {
	for _, src := range repositorySchemas(f) {
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src string) {
		if !parses(src) {
			return
		}
		if failure := formatOutcome(src); failure != "" {
			t.Fatal(failure)
		}
		s, result := schema.LoadString(context.Background(), src, "fuzz.yammm")
		if result.Err() != nil || s.IsZero() {
			return
		}
		out, _ := formatText(src)
		after, ok := loadHash(t, out)
		if !ok {
			t.Fatalf("a loading schema no longer loads after formatting:\n%s", out)
		}
		if before := schema.StructuralHash(s); after != before {
			t.Fatalf("formatting changed the schema's meaning: %s -> %s\n%s", before, after, out)
		}
	})
}
