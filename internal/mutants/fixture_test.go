package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// Variables that make the test binary stand in for another program: the
// harness itself, or a test binary the runner supervises.
const (
	asProgramVar = "MUTANTS_TEST_AS_PROGRAM" // run the harness over os.Args
	seedVar      = "MUTANTS_TEST_SEED"       // with this directory as its cache seed
	childVar     = "MUTANTS_TEST_CHILD"      // act as a supervised binary: exit3, sleep, deaf, exit0 or interleave
)

func TestMain(m *testing.M) {
	switch {
	// The runner's own process carries childVar to the binary it starts.
	case len(os.Args) > 1 && os.Args[1] == superviseFlag:
		os.Exit(mainStatus(nil))
	case os.Getenv(childVar) != "":
		child(os.Getenv(childVar))
	case os.Getenv(asProgramVar) != "":
		os.Exit(mainStatus(os.DirFS(os.Getenv(seedVar))))
	}
	os.Exit(m.Run())
}

// child behaves as a test binary under the runner.
func child(mode string) {
	if os.Getenv(recordVar) != "" || os.Getenv(superviseLimitVar) != "" {
		fmt.Println("the runner's variables reached its child")
		os.Exit(9)
	}
	switch mode {
	case "exit3":
		os.Exit(3)
	case "deaf":
		ignoreStackRequest()
	case "exit0":
		exitAtStackRequest()
	case "interleave":
		for i := range 200 {
			fmt.Fprintf(os.Stdout, "out %03d\n", i)
			fmt.Fprintf(os.Stderr, "err %03d\n", i)
		}
		os.Exit(0)
	}
	time.Sleep(time.Hour)
}

const (
	addSource = "package m\n\nfunc Add(a, b int) int { return a + b }\n"
	addTest   = "package m\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"Add(1, 2) is wrong\")\n\t}\n}\n"
)

// fixture is a throwaway checkout beside a mutants directory.
type fixture struct {
	t        *testing.T
	root     string
	checkout string
	env      []string // added to the environment of every run
}

type result struct {
	code           int
	stdout, stderr string
	out            string // the run's out directory
}

// goDirective spells the running toolchain as a go.mod go directive, so the
// program's pin accepts the toolchain that runs the test.
func goDirective() string {
	v := strings.TrimPrefix(runtime.Version(), "go")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	return v
}

// skipJudgingOnWindows skips a test of the program's judging run, which no
// Windows job of CI makes: there CI runs the program only as scripts/test.sh's
// benchmark runner (-supervise), whose tests run on Windows. On Windows the
// judging tests were nearly all of this package's time, in a binary near its
// -timeout. TestWorkflows_RunNoModeUntestedOnWindows in internal/wiringtest
// fails when a Windows job runs the program otherwise, so the change that adds
// that job must also change that check and remove the skip.
func skipJudgingOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no Windows job of CI runs the judging run, only scripts/test.sh's runner (TestWorkflows_RunNoModeUntestedOnWindows)")
	}
}

// newFixture returns an indexed checkout of one package, m, whose test passes.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{t: t, root: root, checkout: filepath.Join(root, "co")}
	f.write("go.mod", "module example.com/fx\n\ngo "+goDirective()+"\n")
	f.write("m/m.go", addSource)
	f.write("m/m_test.go", addTest)
	f.index()
	return f
}

func (f *fixture) write(rel, content string) {
	f.t.Helper()
	gittree.WriteFile(f.t, f.checkout, rel, []byte(content), 0o600)
}

func (f *fixture) index() {
	f.t.Helper()
	gittree.Index(f.t, f.checkout)
}

// mutant writes one mutant directory. An empty pkgs writes no pkgs file.
func (f *fixture) mutant(id, file, search, replace, pkgs string) string {
	f.t.Helper()
	dir := filepath.Join(f.root, "mutants", id)
	parts := map[string]string{"file": file, "search": search, "replace": replace, "spell": "asis"}
	if pkgs != "" {
		parts["pkgs"] = pkgs
	}
	for name, content := range parts {
		gittree.WriteFile(f.t, dir, name, []byte(content), 0o600)
	}
	return dir
}

// environment keeps a run's go commands inside the fixture and off the
// network, whatever the enclosing run sets.
func (f *fixture) environment() []string {
	dropped := []string{"GOFLAGS", "GOPROXY", "GOTOOLCHAIN", "GOWORK", "GODEBUG", limitVar, benchLimitVar}
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.ContainsFunc(dropped, func(d string) bool { return strings.EqualFold(d, name) })
	})
	return append(append(env, "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off", limitVar+"=120s"), f.env...)
}

// args returns a run's arguments: the fixture's checkout and mutants, a new
// out directory, and workers.
func (f *fixture) args(workers string) []string {
	f.t.Helper()
	return []string{f.checkout, filepath.Join(f.root, "mutants"), filepath.Join(f.t.TempDir(), "out"), workers}
}

// run judges the fixture's mutants in this process with one worker, or as
// args say, with the test binary as go test's -exec runner.
func (f *fixture) run(args ...string) result {
	f.t.Helper()
	if len(args) == 0 {
		args = f.args("1")
	}
	r := result{}
	if len(args) >= 3 {
		r.out = args[2]
	}
	self, err := os.Executable()
	if err != nil {
		f.t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	r.code = judgeSet(f.t.Context(), args, f.environment(), self, seed(f.t), &stdout, &stderr)
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// seed returns the build cache that building and testing the fixture's module
// fills. A run keeps a cache of its own, so each would otherwise compile the
// standard library packages a test binary links. It is held in memory: nothing
// removes a directory when a test binary is killed.
func seed(t *testing.T) fs.FS {
	t.Helper()
	files, err := seedFiles()
	if err != nil {
		t.Fatalf("build the cache seed: %v", err)
	}
	return files
}

var seedFiles = sync.OnceValues(func() (fstest.MapFS, error) {
	dir, err := os.MkdirTemp("", "mutants-seed")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	files := map[string]string{"go.mod": "module example.com/fx\n\ngo " + goDirective() + "\n", "m/m.go": addSource, "m/m_test.go": addTest}
	for name, content := range files {
		path := filepath.Join(dir, "co", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			return nil, err
		}
	}
	cache := filepath.Join(dir, "cache")
	for _, args := range [][]string{{"build", "./..."}, {"test", "-count=1", "./..."}} {
		cmd := exec.CommandContext(context.Background(), "go", args...)
		cmd.Dir = filepath.Join(dir, "co")
		// The run drops GOEXPERIMENT (cleanEnv), so a seed built under the
		// caller's, as in the jsonv2 job, would match none of its builds.
		env := slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "GOEXPERIMENT=") })
		cmd.Env = append(env, "GOFLAGS=-mod=mod", "GOPROXY=off", "GOTOOLCHAIN=local", "GOWORK=off", "GOCACHE="+cache)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	held := fstest.MapFS{}
	err = fs.WalkDir(os.DirFS(cache), ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := os.ReadFile(filepath.Join(cache, filepath.FromSlash(name)))
		held[name] = &fstest.MapFile{Data: content, Mode: 0o600}
		return err
	})
	return held, err
})

func (r result) wantCode(t *testing.T, want int) {
	t.Helper()
	if r.code != want {
		t.Fatalf("exit %d, want %d\nstdout:\n%s\nstderr:\n%s", r.code, want, r.stdout, r.stderr)
	}
}

func (r result) wantStderr(t *testing.T, want string) {
	t.Helper()
	if !strings.Contains(r.stderr, want) {
		t.Errorf("stderr does not hold %q\nstdout:\n%s\nstderr:\n%s", want, r.stdout, r.stderr)
	}
}

// rows returns results.tsv's rows by id and spelling, each a map from the
// header's names.
func (r result) rows(t *testing.T) map[string]map[string]string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(r.out, "results.tsv"))
	if err != nil {
		t.Fatalf("read results.tsv: %v\nstdout:\n%s\nstderr:\n%s", err, r.stdout, r.stderr)
	}
	reader := csv.NewReader(bytes.NewReader(content))
	reader.Comma = '\t'
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("results.tsv: %v", err)
	}
	if !slices.IsSortedFunc(records[1:], slices.Compare) {
		t.Errorf("results.tsv's rows are not in order:\n%s", content)
	}
	if !slices.Equal(records[0], header()) {
		t.Fatalf("results.tsv's header is %q, want %q", records[0], header())
	}
	rows := map[string]map[string]string{}
	for _, record := range records[1:] {
		row := map[string]string{}
		for i, name := range records[0] {
			row[name] = record[i]
		}
		rows[row["id"]+"/"+row["spelling"]] = row
	}
	return rows
}

// wantVerdicts fails unless results.tsv holds exactly want, a map from each
// mutant's id to its verdict under the asis spelling.
func (r result) wantVerdicts(t *testing.T, want map[string]string) map[string]map[string]string {
	t.Helper()
	rows := r.rows(t)
	got := map[string]string{}
	for key, row := range rows {
		got[strings.TrimSuffix(key, "/asis")] = row["verdict"]
	}
	for id, verdict := range want {
		if got[id] != verdict {
			t.Errorf("mutant %s reads %q, want %s\n%s", id, got[id], verdict, r.log(t, id))
		}
	}
	if len(got) != len(want) {
		t.Errorf("results.tsv holds %d rows (%v), want %d", len(got), got, len(want))
	}
	return rows
}

// log returns a mutant's log under the asis spelling, or why it cannot be read.
func (r result) log(t *testing.T, id string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(r.out, "logs", id+".asis.log"))
	if err != nil {
		return err.Error()
	}
	return string(content)
}
