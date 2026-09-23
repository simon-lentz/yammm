package scripttest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A commit with -a runs the pre-commit hook with GIT_INDEX_FILE naming the
// commit's temporary index, and a hook can inherit GIT_DIR. A fixture's git
// must build and read the fixture's own index, and leave the other
// repository's untouched.
//
// Not parallel: it sets each variable for the whole process, as the hook does.
func TestFixture_IgnoresTheEnclosingRepositorysIndex(t *testing.T) {
	for _, name := range []string{"GIT_INDEX_FILE", "GIT_DIR"} {
		t.Run(name, func(t *testing.T) { ignoresTheEnclosingRepository(t, name) })
	}
}

func ignoresTheEnclosingRepository(t *testing.T, name string) {
	t.Helper()
	other := t.TempDir()
	for _, args := range [][]string{{"init", "-q"}, {"add", "held.go"}} {
		if args[0] == "add" {
			if err := os.WriteFile(filepath.Join(other, "held.go"), []byte("package held\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = other
		cmd.Env = withoutRepositoryVars(t, os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	index := filepath.Join(other, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if name == "GIT_INDEX_FILE" {
		t.Setenv(name, index)
	} else {
		t.Setenv(name, filepath.Join(other, ".git"))
	}

	f := newFixture(t)
	f.index()
	r := f.run("packages.sh")

	r.wantCode(t, 0)
	r.wantStdout(t, fixtureModule+"/ok\n")
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("the fixture wrote the index of the repository %s names", name)
	}
}
