package scripttest

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixtureModule is the module path each fixture declares, so the copies of
// internal/testsummary and internal/raceskip build unchanged.
const fixtureModule = "github.com/simon-lentz/yammm"

// repoRoot is the repository root relative to this package's directory, where
// go test runs the package's tests.
const repoRoot = "../.."

// fromRoot returns a slash-separated repository path relative to this package.
func fromRoot(rel string) string {
	return filepath.Join(repoRoot, filepath.FromSlash(rel))
}

// fixture is a throwaway module that runs copies of the repository's scripts.
type fixture struct {
	t   *testing.T
	dir string
	env []string // added to fixtureEnv for this fixture's runs
}

type result struct {
	code   int
	stdout string
	stderr string
}

// newFixture returns a module holding the scripts, the two packages
// scripts/test.sh builds, and one package with a passing test. Nothing is
// indexed until the caller calls index.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir()}
	f.write("go.mod", "module "+fixtureModule+"\n\ngo 1.26.0\n")
	for _, name := range []string{"test.sh", "vet.sh", "packages.sh", "lintconfig.sh"} {
		f.copyScript(name)
	}
	for _, dir := range []string{"internal/testsummary", "internal/raceskip"} {
		f.copyPackage(dir)
	}
	f.write("ok/ok.go", "package ok\n")
	f.write("ok/ok_test.go", "package ok\n\nimport \"testing\"\n\nfunc TestOK(t *testing.T) {}\n")
	return f
}

func (f *fixture) write(rel, content string) {
	f.t.Helper()
	f.writeMode(rel, []byte(content), 0o600)
}

func (f *fixture) writeMode(rel string, content []byte, mode os.FileMode) {
	f.t.Helper()
	path := filepath.Join(f.dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(path, content, mode); err != nil {
		f.t.Fatal(err)
	}
}

// copyScript copies a script from scripts/, executable, because the scripts
// call each other by path.
func (f *fixture) copyScript(name string) {
	f.t.Helper()
	b, err := os.ReadFile(fromRoot("scripts/" + name))
	if err != nil {
		f.t.Fatal(err)
	}
	f.writeMode("scripts/"+name, b, 0o700)
}

// copyFile copies a file from the repository root to the same path in the fixture.
func (f *fixture) copyFile(rel string) {
	f.t.Helper()
	b, err := os.ReadFile(fromRoot(rel))
	if err != nil {
		f.t.Fatal(err)
	}
	f.writeMode(rel, b, 0o600)
}

// copyPackage copies a repository package's non-test Go files.
func (f *fixture) copyPackage(dir string) {
	f.t.Helper()
	entries, err := os.ReadDir(fromRoot(dir))
	if err != nil {
		f.t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f.copyFile(dir + "/" + name)
	}
}

// index creates the fixture's repository and adds paths, or every file when
// none are named. The scripts list packages through git ls-files, which reads
// the index, so nothing is committed.
func (f *fixture) index(paths ...string) {
	f.t.Helper()
	f.git("init", "-q")
	if len(paths) == 0 {
		paths = []string{"-A"}
	}
	f.git(append([]string{"add"}, paths...)...)
}

func (f *fixture) git(args ...string) {
	f.t.Helper()
	cmd := exec.CommandContext(f.t.Context(), "git", args...)
	cmd.Dir = f.dir
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func (f *fixture) run(script string, args ...string) result {
	f.t.Helper()
	//nolint:gosec // runs one of the repository's scripts, copied into the fixture's own module
	cmd := exec.CommandContext(f.t.Context(), "bash", append([]string{"scripts/" + script}, args...)...)
	cmd.Dir = f.dir
	cmd.Env = append(fixtureEnv(), f.env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var r result
	var exit *exec.ExitError
	switch err := cmd.Run(); {
	case err == nil:
	case errors.As(err, &exit):
		r.code = exit.ExitCode()
	default:
		f.t.Fatalf("run scripts/%s: %v", script, err)
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// fixtureEnv keeps a fixture's go commands inside the fixture module and off
// the network, whatever the enclosing run sets.
func fixtureEnv() []string {
	pinned := []string{"GOFLAGS", "GOPROXY", "GOTOOLCHAIN", "GOWORK"}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.ContainsFunc(pinned, func(p string) bool { return strings.EqualFold(p, name) })
	})
	return append(env, "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off")
}

func (r result) wantCode(t *testing.T, want int) {
	t.Helper()
	if r.code != want {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, want, r.stdout, r.stderr)
	}
}

func (r result) wantStdout(t *testing.T, want string) {
	t.Helper()
	if !strings.Contains(r.stdout, want) {
		t.Errorf("stdout does not hold %q\nstdout:\n%s\nstderr:\n%s", want, r.stdout, r.stderr)
	}
}

func (r result) wantStderr(t *testing.T, want string) {
	t.Helper()
	if !strings.Contains(r.stderr, want) {
		t.Errorf("stderr does not hold %q\nstdout:\n%s\nstderr:\n%s", want, r.stdout, r.stderr)
	}
}
