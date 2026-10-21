package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// wantNothingStarted fails when the refused run wrote into its out directory.
func (r result) wantNothingStarted(t *testing.T) {
	t.Helper()
	if entries, _ := os.ReadDir(r.out); len(entries) != 0 {
		t.Errorf("the refused run left %d entries in its out directory", len(entries))
	}
}

// Every mutant that breaks a rule is refused by name in one report, before
// anything is copied.
func TestRun_RefusesEveryMalformedMutantByName(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("doc/notes.txt", "notes\n")
	f.write("tagonly/t.go", "//go:build elsewhere\n\npackage tagonly\n")
	f.write(".gitignore", "ignored.txt\n")
	f.index()
	f.write("ignored.txt", "x\n")
	f.mutant("good", "m/m.go", "a + b", "a - b", "./m/")
	// A package every file of which the host's build leaves out is a package.
	f.mutant("good-tag-only-package", "m/m.go", "a + b", "a - b", "./m/ ./tagonly/")
	edit := func(id, part, content string) {
		gittree.WriteFile(t, f.mutant(id, "m/m.go", "a + b", "a - b", "./m/"), part, []byte(content), 0o600)
	}
	if err := os.Remove(filepath.Join(f.mutant("no-spell", "m/m.go", "a + b", "a - b", "./m/"), "spell")); err != nil {
		t.Fatal(err)
	}
	edit("file-with-newline", "file", "m/m.go\n")
	edit("file-outside", "file", "../co/m/m.go")
	edit("file-is-a-directory", "file", "m")
	edit("file-with-trailing-separator", "file", "m/m.go/")
	edit("file-with-nul", "file", "m/m.go\x00")
	edit("file-ignored", "file", "ignored.txt")
	edit("empty-search", "search", "")
	edit("empty-spell", "spell", " \n")
	edit("unknown-spell", "spell", "asis upper")
	edit("spell-twice", "spell", "asis\nasis")
	edit("pkgs-empty", "pkgs", "\n")
	edit("pkgs-flag", "pkgs", "./m/ -run=TestNone")
	edit("pkgs-no-directory", "pkgs", "./nosuch/")
	edit("pkgs-no-go-file", "pkgs", "./doc/")
	edit("pkgs-outside", "pkgs", "fmt")
	edit("pkgs-word-matching-nothing", "pkgs", "./m/ ./doc/...")
	if err := os.Remove(filepath.Join(f.mutant("no-package-and-no-pkgs", "doc/notes.txt", "notes", "note", "./m/"), "pkgs")); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"mutant no-spell has no spell file\n",
		`mutant file-with-newline names "m/m.go\n", which the checkout does not hold as a regular file` + "\n",
		`mutant file-outside names "../co/m/m.go", which is no path inside the checkout` + "\n",
		`mutant file-is-a-directory names "m", which the checkout does not hold as a regular file` + "\n",
		`mutant file-with-trailing-separator names "m/m.go/", which the checkout does not hold as a regular file` + "\n",
		`mutant file-with-nul names "m/m.go\x00", which is no path inside the checkout` + "\n",
		`mutant file-ignored names "ignored.txt", which git ignores or does not list in the checkout`,
		"mutant empty-search has an empty search file, which names no site\n",
		"mutant empty-spell has a spell file that names no spelling\n",
		`mutant unknown-spell names an unknown spelling "upper"` + "\n",
		"mutant spell-twice names the spelling asis twice\n",
		"mutant pkgs-empty has a pkgs file that names no package\n",
		`mutant pkgs-flag has a pkgs file that names "-run=TestNone", which go test would read as a flag` + "\n",
		"mutant pkgs-no-directory has a pkgs file that names ",
		"mutant pkgs-no-go-file has a pkgs file that names ./doc, a directory with no Go file\n",
		"mutant pkgs-outside has a pkgs file that names fmt, which is no package directory of the checkout\n",
		"mutant pkgs-word-matching-nothing has a pkgs file whose word ./doc/... matches no package\n",
		`mutant no-package-and-no-pkgs names "doc/notes.txt", whose directory is no tracked Go package`,
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		if err := os.Chmod(filepath.Join(f.mutant("unreadable-search", "m/m.go", "a + b", "a - b", "./m/"), "search"), 0); err != nil {
			t.Fatal(err)
		}
		f.mutant("a\tb", "m/m.go", "a + b", "a - b", "./m/")
		if err := os.Symlink("m.go", filepath.Join(f.checkout, "m", "link.go")); err != nil {
			t.Fatal(err)
		}
		f.index()
		f.mutant("file-is-a-link", "m/link.go", "a + b", "a - b", "./m/")
		want = append(want, `mutant file-is-a-link names "m/link.go", which the checkout does not hold as a regular file`+"\n")
		// A FIFO would block the read that opens it.
		fifo := filepath.Join(f.mutant("fifo-search", "m/m.go", "a + b", "a - b", "./m/"), "search")
		if err := os.Remove(fifo); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.CommandContext(t.Context(), "mkfifo", fifo).CombinedOutput(); err != nil {
			t.Fatalf("mkfifo: %v\n%s", err, out)
		}
		want = append(want, "mutant unreadable-search has a search file that cannot be read: ", `mutant "a\tb" has a name that is not printable text`+"\n",
			"mutant fifo-search has a search file that is not a regular file\n")
	}

	r := f.run()

	r.wantCode(t, 2)
	for _, line := range want {
		r.wantStderr(t, line)
	}
	if strings.Contains(r.stderr, "mutant good") {
		t.Errorf("a well-formed mutant is refused:\n%s", r.stderr)
	}
	r.wantNothingStarted(t)
}

func TestRun_RefusesARunItCannotOwn(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	for _, row := range []struct {
		name    string
		arrange func(f *fixture, args []string) []string
		stderr  string
	}{
		{name: "too few arguments", arrange: func(_ *fixture, args []string) []string { return args[:2] }, stderr: "usage: mutants <checkout> <mutants dir> <out dir> [workers]"},
		{name: "too many arguments", arrange: func(_ *fixture, args []string) []string { return append(args, "more") }, stderr: "usage: mutants <checkout> <mutants dir> <out dir> [workers]"},
		{name: "a checkout that is a file", arrange: func(f *fixture, args []string) []string {
			return append([]string{filepath.Join(f.checkout, "go.mod")}, args[1:]...)
		}, stderr: "go.mod is not a directory"},
		{name: "a checkout that tracks no file", arrange: func(f *fixture, args []string) []string {
			if err := os.Remove(filepath.Join(f.checkout, ".git", "index")); err != nil {
				f.t.Fatal(err)
			}
			return args
		}, stderr: " tracks no file"},
		{name: "a module file go list cannot read", arrange: func(f *fixture, args []string) []string {
			f.write("go.mod", "module example.com/fx\n\ngo "+goDirective()+"\n\nrequire (\n")
			f.index()
			return args
		}, stderr: "has a pkgs file go list cannot read"},
		{name: "an experiment the go env file sets", arrange: func(f *fixture, args []string) []string {
			goenv := filepath.Join(f.t.TempDir(), "go.env")
			if err := os.WriteFile(goenv, []byte("GOEXPERIMENT=jsonv2\n"), 0o600); err != nil {
				f.t.Fatal(err)
			}
			f.env = append(f.env, "GOENV="+goenv)
			return args
		}, stderr: "the go env file sets GOEXPERIMENT=jsonv2, which would reach every configuration"},
		{name: "no worker", arrange: func(_ *fixture, args []string) []string { return append(args[:3], "0") }, stderr: `workers must be a positive integer, got "0"`},
		{name: "a worker count with a leading zero", arrange: func(_ *fixture, args []string) []string { return append(args[:3], "08") }, stderr: `workers must be a positive integer, got "08"`},
		{name: "a worker count that is no number", arrange: func(_ *fixture, args []string) []string { return append(args[:3], "two") }, stderr: `workers must be a positive integer, got "two"`},
		{name: "a malformed limit", arrange: func(f *fixture, args []string) []string {
			f.env = append(f.env, limitVar+"=5")
			return args
		}, stderr: limitVar + ` must be a whole number of seconds, minutes or hours from 1 up to 24 hours, with no leading zero, such as 900s or 15m, got "5"`},
		{name: "no mutant", arrange: func(f *fixture, args []string) []string {
			if err := os.RemoveAll(filepath.Join(f.root, "mutants", "m01")); err != nil {
				f.t.Fatal(err)
			}
			return args
		}, stderr: "no mutants in "},
		{name: "an unstaged change", arrange: func(f *fixture, args []string) []string {
			f.write("m/m.go", addSource+"\n// edited\n")
			return args
		}, stderr: "the checkout's unstaged tree is not clean"},
		{name: "another toolchain than the module's", arrange: func(f *fixture, args []string) []string {
			f.write("go.mod", "module example.com/fx\n\ngo 1.0.0\n")
			f.index()
			return args
		}, stderr: "pins go1.0.0 and this run is "},
		{name: "a go line with no version before the directive", arrange: func(f *fixture, args []string) []string {
			f.write("go.mod", "module example.com/fx\n\ngo\ngo "+goDirective()+"\n")
			f.index()
			return args
		}, stderr: "names no go directive"},
		{name: "a nested repository", arrange: func(f *fixture, args []string) []string {
			f.write("sub/x.txt", "x\n")
			gittree.Index(f.t, filepath.Join(f.checkout, "sub"))
			return args
		}, stderr: "the checkout holds sub/ as a submodule or a nested repository, which a worker's copy cannot hold"},
		{name: "a module file with no go directive", arrange: func(f *fixture, args []string) []string {
			f.write("go.mod", "module example.com/fx\n")
			f.index()
			return args
		}, stderr: "names no go directive"},
		{name: "the checkout as the out directory", arrange: func(f *fixture, args []string) []string { return []string{args[0], args[1], f.checkout} }, stderr: " is not empty"},
		{name: "an out directory whose name is not text", arrange: func(f *fixture, args []string) []string {
			return []string{args[0], args[1], filepath.Join(f.root, "o\xffut")}
		}, stderr: "is not named in UTF-8"},
		{name: "an out directory a run has used", arrange: func(f *fixture, args []string) []string {
			gittree.WriteFile(f.t, args[2], "results.tsv", []byte("id\n"), 0o600)
			return args
		}, stderr: " is not empty: a run takes a directory no run has used"},
		{name: "an out directory that holds a tracked file", arrange: func(f *fixture, args []string) []string {
			f.write("runs/out/kept.txt", "kept\n")
			f.index()
			return []string{args[0], args[1], filepath.Join(f.checkout, "runs", "out")}
		}, stderr: " is not empty"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
			args := row.arrange(f, f.args("1"))

			r := f.run(args...)

			r.wantCode(t, 2)
			r.wantStderr(t, row.stderr)
			if _, err := os.Stat(filepath.Join(r.out, "work")); r.out != "" && err == nil {
				t.Errorf("the refused run made %s", filepath.Join(r.out, "work"))
			}
		})
	}
}

func TestParseLimit(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]time.Duration{
		"": 300 * time.Second, "1s": time.Second, "900s": 900 * time.Second, "15m": 15 * time.Minute, "86400s": 24 * time.Hour, "1440m": 24 * time.Hour, "24h": 24 * time.Hour,
	} {
		if got, err := parseLimit(limitVar, value, 300*time.Second); err != nil || got != want {
			t.Errorf("parseLimit(%q) = %v, %v; want %v", value, got, err, want)
		}
	}
	for _, value := range []string{"5", "5x", "0s", "-1s", "+1s", "1.5m", "5ms", "1xs", "05s", "86401s", "1441m", "25h", "9999999999s", "99999999999999999999s", "s", " 5s"} {
		if got, err := parseLimit(limitVar, value, 300*time.Second); err == nil {
			t.Errorf("parseLimit(%q) = %v, want a refusal", value, got)
		}
	}
}

// TEST_BENCH_LIMIT is read by the rule of MUTATE_TIMEOUT, and a refusal names
// the variable that holds the value.
func TestNewRun_ReadsTheBenchmarkLimitScriptsTestShReads(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.index()
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
	f.env = append(f.env, benchLimitVar+"=0s")

	r := f.run()

	r.wantCode(t, 2)
	r.wantStderr(t, benchLimitVar+" must be a whole number of seconds, minutes or hours")
}

func TestExecValue(t *testing.T) {
	t.Parallel()
	for self, want := range map[string]string{
		"/bin/mutants":       "/bin/mutants -supervise",
		"/a dir/mutants":     "'/a dir/mutants' -supervise",
		"/it's here/mutants": `"/it's here/mutants" -supervise`,
		`/a"b/mutants`:       `/a"b/mutants -supervise`,
		`/it's"b"/mutants`:   `/it's"b"/mutants -supervise`,
		`/a "b/mutants`:      `'/a "b/mutants' -supervise`,
		"/a\tb/mutants":      "'/a\tb/mutants' -supervise",
		"/a\nb/mutants":      "'/a\nb/mutants' -supervise",
		"/a\rb/mutants":      "'/a\rb/mutants' -supervise",
	} {
		if got, err := execValue(self); err != nil || got != want {
			t.Errorf("execValue(%q) = %q, %v; want %q", self, got, err, want)
		}
	}
	if got, err := execValue(`/it's "here"/mutants`); err == nil {
		t.Errorf("a path with white space and both quote characters is spelled %q, want a refusal", got)
	}
}

func TestWords_SplitsAsAShellDoes(t *testing.T) {
	t.Parallel()
	if got := strings.Join(words([]byte(" ./a\t./b\n./c d\r\n")), "|"); got != "./a|./b|./c d\r" {
		t.Errorf("words split to %q", got)
	}
}

// A caller that chose no toolchain gets the module's, by name, so the go
// command runs it wherever the run's commands start.
func TestPinToolchain_NamesTheModulesToolchainWhenTheCallerChoseNone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	env, err := pinToolchain(t.Context(), f.checkout, []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GOTOOLCHAIN="})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := envValue(env, "GOTOOLCHAIN"), "go"+goDirective(); got != want {
		t.Errorf("GOTOOLCHAIN is %q, want %q", got, want)
	}
	env, err = pinToolchain(t.Context(), f.checkout, []string{"PATH=" + os.Getenv("PATH"), "HOME=" + os.Getenv("HOME"), "GOTOOLCHAIN=local"})
	if err != nil || envValue(env, "GOTOOLCHAIN") != "local" {
		t.Errorf("a caller's own choice is %q, %v; want it kept", envValue(env, "GOTOOLCHAIN"), err)
	}
}

// With gotestjsonbuildtext=1 the go command names no failed build in its
// events, so the run sets it to 0 over a caller's own setting.
func TestCleanEnv_DropsWhatBindsARunAndKeepsBuildFailuresInTheEvents(t *testing.T) {
	t.Parallel()
	env, err := cleanEnv(t.Context(), []string{
		"PATH=" + os.Getenv("PATH"), "GODEBUG=http2debug=1,gotestjsonbuildtext=1", "GIT_DIR=/elsewhere/.git", "GIT_INDEX_FILE=/elsewhere/index",
		limitVar + "=5s", benchLimitVar + "=5s", recordVar + "=/r", superviseLimitVar + "=1s", "KEPT=1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := envValue(env, "GODEBUG"); got != "http2debug=1,gotestjsonbuildtext=0" {
		t.Errorf("GODEBUG is %q", got)
	}
	for _, name := range []string{"GIT_DIR", "GIT_INDEX_FILE", limitVar, benchLimitVar, recordVar, superviseLimitVar} {
		if strings.Contains(strings.Join(env, "\n"), name+"=") {
			t.Errorf("the run's environment holds %s", name)
		}
	}
	if envValue(env, "KEPT") != "1" || envValue(env, "PATH") == "" {
		t.Errorf("the run's environment lost a caller's variable: %q", env)
	}
}
