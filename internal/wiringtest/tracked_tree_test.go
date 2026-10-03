package wiringtest

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// modulePath is this module's path.
const modulePath = "github.com/simon-lentz/yammm"

// repoRoot is the repository root relative to this package's directory, where
// go test runs the package's tests.
const repoRoot = "../.."

// fromRoot returns a slash-separated repository path relative to this package.
func fromRoot(rel string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(rel))
}

// trackedFiles returns the files git tracks under root that keep accepts,
// slash-separated, relative to root and sorted. When root lies in no work tree
// git can read, as in the module cache's copy of a release, it walks root
// instead: that copy holds the tracked tree and nothing else, and has no index.
func trackedFiles(t *testing.T, root string, keep func(rel string) bool) []string {
	t.Helper()
	var all []string
	if insideWorkTree(t, root) {
		gittree.MarkTrackedFiles()
		cmd := exec.CommandContext(t.Context(), "git", "ls-files", "-z")
		cmd.Dir = root
		cmd.Env = gitEnv(t, root)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git ls-files: %v", err)
		}
		all = strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	} else {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(root, p)
			all = append(all, filepath.ToSlash(rel))
			return err
		})
		if err != nil {
			t.Fatalf("walking %s: %v", root, err)
		}
	}
	return slices.Sorted(slices.Values(slices.DeleteFunc(all, func(rel string) bool { return rel == "" || !keep(rel) })))
}

// insideWorkTree reports whether root lies in a git work tree git can read.
func insideWorkTree(t *testing.T, root string) bool {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = root
	cmd.Env = gitEnv(t, root)
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// gitEnv is the environment git runs in when it reads root. The repository
// root keeps the repository variables, since under a hook they name the tree
// being committed; any other root is a test's own tree, with a repository of
// its own or none, and those variables would point it at the enclosing one.
func gitEnv(t *testing.T, root string) []string {
	t.Helper()
	if root == repoRoot {
		return os.Environ()
	}
	return gittree.WithoutRepositoryVars(t, os.Environ())
}

// tree is a throwaway directory a test fills and gives a git index of its own.
type tree struct {
	t   *testing.T
	dir string
}

func newTree(t *testing.T) *tree {
	t.Helper()
	return &tree{t: t, dir: t.TempDir()}
}

func (tr *tree) write(rel, content string) {
	tr.t.Helper()
	gittree.WriteFile(tr.t, tr.dir, rel, []byte(content), 0o600)
}

// index creates the tree's repository and adds every file to its index.
func (tr *tree) index() {
	tr.t.Helper()
	gittree.Index(tr.t, tr.dir)
}

// A commit with -a runs the pre-commit hook with GIT_INDEX_FILE naming the
// commit's temporary index, and a hook can inherit GIT_DIR. The repository root
// is read through the index the variable names, which under a hook is the tree
// being committed; any other tree is read through its own index or none.
//
// Not parallel: it sets each variable for the whole process, as the hook does.
func TestTrackedFiles_FollowTheRepositoryVariablesAtTheRootAlone(t *testing.T) {
	all := func(string) bool { return true }
	for _, name := range []string{"GIT_INDEX_FILE", "GIT_DIR"} {
		t.Run(name, func(t *testing.T) {
			// The enclosing run's repository variables are cleared first, so name is the one set.
			kept := gittree.WithoutRepositoryVars(t, os.Environ())
			for _, kv := range os.Environ() {
				if enclosing, _, _ := strings.Cut(kv, "="); !slices.Contains(kept, kv) {
					t.Setenv(enclosing, "")
					if err := os.Unsetenv(enclosing); err != nil {
						t.Fatal(err)
					}
				}
			}
			other := newTree(t)
			other.write("held.go", "package held\n")
			other.index()
			index := filepath.Join(other.dir, ".git", "index")
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			if name == "GIT_INDEX_FILE" {
				t.Setenv(name, index)
			} else {
				t.Setenv(name, filepath.Join(other.dir, ".git"))
			}

			if got := trackedFiles(t, repoRoot, all); !slices.Equal(got, []string{"held.go"}) {
				t.Errorf("trackedFiles read %d files at the repository root under %s, the first %q, want the named index's [held.go]", len(got), name, got[:min(len(got), 3)])
			}
			listed := newTree(t)
			listed.write("held.txt", "held\n")
			listed.index()
			if got := trackedFiles(t, listed.dir, all); !slices.Equal(got, []string{"held.txt"}) {
				t.Errorf("trackedFiles read %q under %s, want the tree's own [held.txt]", got, name)
			}
			if plain := t.TempDir(); insideWorkTree(t, plain) {
				t.Errorf("insideWorkTree reads a directory with no repository as a work tree under %s", name)
			}
			after, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("a read wrote the index of the repository %s names", name)
			}
		})
	}
}

// git gives a commit hook GIT_INDEX_FILE, so the commit gate runs this package
// under it, and a run from a terminal does not. A child run of this test binary
// repeats every other test with the variable naming the index git reads at the
// repository root.
func TestPackage_PassesUnderACommitHooksIndexVariable(t *testing.T) {
	t.Parallel()
	if !insideWorkTree(t, repoRoot) {
		t.Skip("no repository to commit to: the tree is not a git work tree")
	}
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "--path-format=absolute", "--git-path", "index")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse --git-path index: %v", err)
	}
	//nolint:gosec // runs this test binary again
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.count=1", "-test.skip=^"+t.Name()+"$")
	child.Env = append(os.Environ(), "GIT_INDEX_FILE="+strings.TrimSpace(string(out)))
	if out, err := child.CombinedOutput(); err != nil {
		t.Errorf("the package fails under GIT_INDEX_FILE: %v\n%s", err, out)
	}
}

// plainTree returns a tree git reads as no work tree, which holds only while
// the temporary directory lies outside every repository.
func plainTree(t *testing.T) *tree {
	t.Helper()
	tr := newTree(t)
	if insideWorkTree(t, tr.dir) {
		t.Fatalf("the temporary directory %s lies inside a git work tree; set TMPDIR outside one", tr.dir)
	}
	return tr
}

// Both readings return the files sorted by path: git lists a-b before a/x, and
// a walk reaches the directory a first.
func TestTrackedFiles_SortsTheFilesOfEitherReading(t *testing.T) {
	t.Parallel()
	all := func(string) bool { return true }
	want := []string{"a-b", "a/x"}
	indexed, walked := newTree(t), plainTree(t)
	for _, tr := range []*tree{indexed, walked} {
		tr.write("a/x", "x\n")
		tr.write("a-b", "b\n")
	}
	indexed.index()
	for name, tr := range map[string]*tree{"the index": indexed, "a walk": walked} {
		if got := trackedFiles(t, tr.dir, all); !slices.Equal(got, want) {
			t.Errorf("trackedFiles from %s = %q, want %q", name, got, want)
		}
	}
}

// An index that holds no file yields no file, not the empty name git's output
// splits into.
func TestTrackedFiles_AnEmptyIndexHoldsNoFile(t *testing.T) {
	t.Parallel()
	empty := newTree(t)
	empty.index()
	if got := trackedFiles(t, empty.dir, func(string) bool { return true }); len(got) != 0 {
		t.Errorf("trackedFiles of an empty index = %q, want none", got)
	}
}

// throwawayListers are the files that run git ls-files on a tree of their own
// and never on the repository, each with what it lists there.
var throwawayListers = map[string]string{
	"internal/gittree/gittree_test.go":    "a temporary directory's index",
	"internal/scripttest/test_sh_test.go": "a fixture's untracked files",
}

// A file that has git list the tracked files calls gittree.MarkTrackedFiles:
// go test's result cache cannot see git read the index, and without the call
// the commit gate replays a pass after a file is added.
func TestTrackedFileListers_MarkTheListForTheResultCache(t *testing.T) {
	t.Parallel()
	const marker = modulePath + "/internal/gittree"
	fset := token.NewFileSet()
	listers := 0
	for _, rel := range trackedFiles(t, repoRoot, func(rel string) bool { return strings.HasSuffix(rel, ".go") }) {
		if slices.Contains(strings.Split(rel, "/"), "testdata") {
			continue
		}
		src, err := os.ReadFile(fromRoot(rel))
		if errors.Is(err, fs.ErrNotExist) {
			// The index still lists a file the working tree has deleted.
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
		if err != nil {
			t.Errorf("parse %s: %v", rel, err)
			continue
		}
		lists, marks := false, false
		name := importName(file, marker)
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if v, err := strconv.Unquote(n.Value); n.Kind == token.STRING && err == nil && v == "ls-files" {
					lists = true
				}
			case *ast.CallExpr:
				if sel, ok := selectorOf(n.Fun, name); ok && sel == "MarkTrackedFiles" {
					marks = true
				}
			}
			return true
		})
		if !lists {
			continue
		}
		listers++
		if _, throwaway := throwawayListers[rel]; !throwaway && !marks {
			t.Errorf("%s runs git ls-files and does not call gittree.MarkTrackedFiles", rel)
		}
	}
	if listers < 4 {
		t.Errorf("%d tracked Go files run git ls-files, want the four known readers at least", listers)
	}
}
