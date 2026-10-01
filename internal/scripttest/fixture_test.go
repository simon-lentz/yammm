package scripttest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// fixtureModule is the module path each fixture declares, so the copies of
// internal/testsummary and internal/raceskip build unchanged.
const fixtureModule = "github.com/simon-lentz/yammm"

// repoRoot is the repository root relative to this package's directory, where
// go test runs the package's tests.
const repoRoot = "../.."

// pinGoDirective rewrites the fixture's copied go.mod to declare the RUNNING
// toolchain. A fixture that copies the repository's module file inherits its
// pin, and the scripts refuse a toolchain that is not the module's, so a copy
// would refuse every run under another release.
func (f *fixture) pinGoDirective() {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "go.mod"))
	if err != nil {
		f.t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "go ") {
			lines[i] = "go " + goDirective()
			break
		}
	}
	f.write("go.mod", strings.Join(lines, "\n"))
}

// goDirective returns the running toolchain's version as a go.mod go directive
// spells it: "1.26.0" or "1.27rc1", never "go1.26.0", and never a suffix.
func goDirective() string {
	return directiveOf(runtime.Version())
}

// directiveOf spells a toolchain version as a go.mod go directive: the version
// less its "go" prefix and any experiment or build suffix. scripts/toolchain.sh
// compares "go" and the directive with `go env GOVERSION`, which spells a
// release and a prerelease the same way.
func directiveOf(version string) string {
	v := strings.TrimPrefix(version, "go")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	return v
}

// inputs are the tracked files this package's tests read: a file by its path
// from the repository root, a directory's files by that path and a slash. go
// test's result cache replays a pass of the package after a change to any other
// tracked file outside the package, which keeps these slow tests out of a commit.
var inputs = []string{".golangci.yml", "go.mod", "go.sum", "internal/gittree/", "internal/raceskip/", "internal/testsummary/", "scripts/"}

// isInput reports whether rel, a slash-separated repository path in its
// cleaned form, is a listed file, a listed directory, or a file under one.
func isInput(rel string) bool {
	return path.Clean(rel) == rel && slices.ContainsFunc(inputs, func(in string) bool {
		return rel == in || rel+"/" == in || strings.HasSuffix(in, "/") && strings.HasPrefix(rel, in)
	})
}

// failer is the slice of testing.T that fromRoot uses, which lets a test see
// it refuse a path.
type failer interface {
	Helper()
	Fatalf(format string, args ...any)
}

// fromRoot returns the path of the input rel relative to this package, and
// fails the test when rel is outside inputs.
func fromRoot(t failer, rel string) string {
	t.Helper()
	if !isInput(rel) {
		t.Fatalf("%s is not one of this package's inputs %q: a test that reads another tracked file belongs in internal/wiringtest", rel, inputs)
	}
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
	// The go directive is the RUNNING toolchain's version, not a literal: the
	// scripts hold a run to go.mod's toolchain and refuse a mismatch, and a
	// fixture pinned to one release would refuse every run under another.
	f.write("go.mod", "module "+fixtureModule+"\n\ngo "+goDirective()+"\n")
	for _, name := range []string{"test.sh", "committest.sh", "vet.sh", "packages.sh", "lintconfig.sh", "toolchain.sh"} {
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
	gittree.WriteFile(f.t, f.dir, rel, content, mode)
}

// copyScript copies a script from scripts/, executable, because the scripts
// call each other by path.
func (f *fixture) copyScript(name string) {
	f.t.Helper()
	b, err := os.ReadFile(fromRoot(f.t, "scripts/"+name))
	if err != nil {
		f.t.Fatal(err)
	}
	f.writeMode("scripts/"+name, b, 0o700)
}

// copyFile copies a file from the repository root to the same path in the fixture.
func (f *fixture) copyFile(rel string) {
	f.t.Helper()
	b, err := os.ReadFile(fromRoot(f.t, rel))
	if err != nil {
		f.t.Fatal(err)
	}
	f.writeMode(rel, b, 0o600)
}

// copyPackage copies a repository package's non-test Go files.
func (f *fixture) copyPackage(dir string) {
	f.t.Helper()
	entries, err := os.ReadDir(fromRoot(f.t, dir))
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
	gittree.Index(f.t, f.dir, paths...)
}

func (f *fixture) run(script string, args ...string) result {
	f.t.Helper()
	return f.runFrom(f.dir, script, args...)
}

// runFrom runs one of the fixture's scripts with dir as the working directory.
func (f *fixture) runFrom(dir, script string, args ...string) result {
	f.t.Helper()
	//nolint:gosec // runs one of the repository's scripts, copied into the fixture's own module
	cmd := exec.CommandContext(f.t.Context(), "bash", append([]string{filepath.Join(f.dir, "scripts", script)}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(gittree.WithoutRepositoryVars(f.t, fixtureEnv()), f.env...)
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
// the network, whatever the enclosing run sets. It drops what an enclosing
// mutation or gate run sets for itself: a recorded baseline, a go test timeout,
// and a durations file that every fixture's test.sh would write.
func fixtureEnv() []string {
	dropped := []string{"GOFLAGS", "GOPROXY", "GOTOOLCHAIN", "GOWORK", "MUTATE_BASELINE_CACHE", "MUTATE_TIMEOUT", "TEST_DURATIONS"}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.ContainsFunc(dropped, func(p string) bool { return strings.EqualFold(p, name) })
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

// refusal records a Fatalf call in place of a test.
type refusal struct{ message string }

func (r *refusal) Helper() {}

func (r *refusal) Fatalf(format string, args ...any) { r.message = fmt.Sprintf(format, args...) }

func TestFromRoot_FailsATestThatReadsOutsideTheInputs(t *testing.T) {
	t.Parallel()
	var r refusal
	if fromRoot(&r, "scripts/test.sh"); r.message != "" {
		t.Errorf("fromRoot refuses an input: %s", r.message)
	}
	if fromRoot(&r, "graph/doc.go"); !strings.Contains(r.message, "graph/doc.go is not one of this package's inputs") {
		t.Errorf("fromRoot's refusal of graph/doc.go is %q", r.message)
	}
}

func TestIsInput_AdmitsTheListedFilesAndDirectoriesAlone(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]bool{
		"go.mod":                         true,
		".golangci.yml":                  true,
		"scripts":                        true,
		"scripts/test.sh":                true,
		"internal/testsummary":           true,
		"internal/testsummary/main.go":   true,
		"":                               false,
		"./go.mod":                       false,
		"scripts/":                       false,
		"scripts/../graph/doc.go":        false,
		"internal/raceskip/../../go.mod": false,
		"go.model":                       false,
		"scripts2/test.sh":               false,
		"internal/testsummaries/a.go":    false,
		"internal/scripttest/tree.go":    false,
		"graph/doc.go":                   false,
		".github/workflows/ci.yaml":      false,
	} {
		if got := isInput(rel); got != want {
			t.Errorf("isInput(%q) = %v, want %v", rel, got, want)
		}
	}
}
