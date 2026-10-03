package scripttest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// A commit with -a runs the pre-commit hook with GIT_INDEX_FILE naming the
// commit's temporary index, and a hook can inherit GIT_DIR. A fixture's git
// must build and read the fixture's own index, and leave the other
// repository's untouched.
//
// Each variable is set for a child run of this test binary, not for this
// process: a variable the process reads is an input of go test's result cache,
// and git gives GIT_INDEX_FILE another value for each kind of commit, with its
// process ID in the name for a commit of named paths.
func TestFixture_IgnoresTheEnclosingRepositorysIndex(t *testing.T) {
	t.Parallel()
	// The child runs a test that builds a fixture, indexes it and compares the
	// packages packages.sh lists from that index.
	const target = "TestPackagesScript_ListsThePackagesHoldingATrackedGoFile"
	for _, name := range []string{"GIT_INDEX_FILE", "GIT_DIR"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			other := t.TempDir()
			gittree.WriteFile(t, other, "held.go", []byte("package held\n"), 0o600)
			gittree.Index(t, other, "held.go")
			index := filepath.Join(other, ".git", "index")
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			value := index
			if name == "GIT_DIR" {
				value = filepath.Join(other, ".git")
			}

			//nolint:gosec // runs this test binary again
			child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+target+"$", "-test.count=1", "-test.v")
			child.Env = append(gittree.WithoutRepositoryVars(t, os.Environ()), name+"="+value)
			out, err := child.CombinedOutput()
			if err != nil {
				t.Errorf("the child run failed: %v\n%s", err, out)
			}
			if !bytes.Contains(out, []byte("--- PASS: "+target)) {
				t.Errorf("the child run does not pass %s under %s\n%s", target, name, out)
			}
			after, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Errorf("the fixture wrote the index of the repository %s names\n%s", name, out)
			}
		})
	}
}
