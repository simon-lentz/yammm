package wiringtest

import (
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

// trackedFiles returns the files of the tree at root that keep accepts,
// slash-separated, relative to root and sorted, as [gittree.Files] reads them.
func trackedFiles(t *testing.T, root string, keep func(rel string) bool) []string {
	t.Helper()
	all, err := gittree.Files(root)
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(all, func(rel string) bool { return !keep(rel) })
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

// git gives a commit hook GIT_INDEX_FILE, so the commit gate runs this package
// under it, and a run from a terminal does not. A child run of this test binary
// repeats every other test with the variable naming the index git reads at the
// repository root.
func TestPackage_PassesUnderACommitHooksIndexVariable(t *testing.T) {
	t.Parallel()
	if _, err := os.Lstat(fromRoot(".git")); errors.Is(err, fs.ErrNotExist) {
		t.Skip("no repository to commit to: the tree is a copy, as gittree.Files reads one")
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

// throwawayListers are the files that run git ls-files on a tree of their own,
// or on one a caller names, and never on the repository as a test's input, each
// with what it lists there.
var throwawayListers = map[string]string{
	"internal/gittree/gittree_test.go":    "a temporary directory's index",
	"internal/mutants/tree.go":            "the checkout a run is given, and a worker's copy of it",
	"internal/mutants/tree_test.go":       "a fixture's checkout and a worker's copy of it",
	"internal/scripttest/test_sh_test.go": "a fixture's untracked files",
}

// treeReader is the one file that lists a tree's tracked files, and
// listerChecker is this file, which names the command it looks for.
const (
	treeReader    = "internal/gittree/gittree.go"
	listerChecker = "internal/wiringtest/tracked_tree_test.go"
)

// Only gittree.Files lists the repository's tracked set, and it calls
// MarkTrackedFiles: go test's result cache cannot see git read the index, and
// without the call the commit gate replays a pass after a file is added. A
// second reader would judge "no tracked set" by a rule of its own.
func TestTrackedFileListers_ReadThroughGittree(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	readerMarks := false
	listers := map[string]bool{}
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
		ast.Inspect(file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.BasicLit:
				if v, err := strconv.Unquote(n.Value); n.Kind == token.STRING && err == nil && v == "ls-files" {
					listers[rel] = true
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && rel == treeReader && id.Name == "MarkTrackedFiles" {
					readerMarks = true
				}
			}
			return true
		})
	}
	for rel := range listers {
		if _, throwaway := throwawayListers[rel]; !throwaway && rel != treeReader && rel != listerChecker {
			t.Errorf("%s runs git ls-files; read the tree through gittree.Files", rel)
		}
	}
	if !listers[treeReader] || !readerMarks {
		t.Errorf("%s lists the tracked set: %v, and calls MarkTrackedFiles: %v; want both", treeReader, listers[treeReader], readerMarks)
	}
	for rel := range throwawayListers {
		if !listers[rel] {
			t.Errorf("%s runs no git ls-files; remove it from throwawayListers", rel)
		}
	}
}
