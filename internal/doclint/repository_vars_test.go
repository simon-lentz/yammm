package doclint_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// repositoryVars returns the variables `git rev-parse --local-env-vars` names:
// each sets the repository, index or configuration git uses, whatever the
// working directory. A commit with -a runs the pre-commit hook with
// GIT_INDEX_FILE naming the commit's temporary index.
func repositoryVars(t *testing.T) []string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "rev-parse", "--local-env-vars")
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse --local-env-vars: %v", err)
	}
	return strings.Fields(string(out))
}

// isolateFromEnclosingRepository unsets every repository variable for the rest
// of the test and restores it at cleanup, so a test that builds its own
// repository, and the gate it runs over that repository, read that repository
// alone. A test that reads a fixture the enclosing repository tracks keeps the
// variables: under a hook the index they name is the tree being committed.
// t.Setenv refuses a parallel test, whose environment another test shares.
func isolateFromEnclosingRepository(t *testing.T) {
	t.Helper()
	for _, name := range repositoryVars(t) {
		value, ok := os.LookupEnv(name)
		if !ok {
			continue
		}
		t.Logf("cleared %s", name)
		t.Setenv(name, value) // restores the value at cleanup
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
}

// TestFixtureRepositories_IgnoreTheEnclosingRepositorysIndex runs a test that
// builds its own repository in a child of this test binary, with
// GIT_INDEX_FILE naming another repository's index, as a hook would.
func TestFixtureRepositories_IgnoreTheEnclosingRepositorysIndex(t *testing.T) {
	t.Parallel()
	vars := repositoryVars(t)
	clean := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(vars, name)
	})
	other := t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "held.go"), []byte("package held\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"add", "held.go"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = other
		cmd.Env = clean
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	index := filepath.Join(other, ".git", "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}

	const target = "TestAssertCitedCodesExist_ReportsAFileItCannotRead"
	//nolint:gosec // runs this test binary again
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+target+"$", "-test.count=1", "-test.v")
	child.Env = slices.Concat(clean, []string{"GIT_INDEX_FILE=" + index})
	out, err := child.CombinedOutput()
	if err != nil {
		t.Errorf("the child run failed: %v\n%s", err, out)
	}
	for _, want := range []string{"cleared GIT_INDEX_FILE", "--- PASS: " + target} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the child run does not show %q\n%s", want, out)
		}
	}
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("a fixture's git wrote the index GIT_INDEX_FILE names\n%s", out)
	}
}
