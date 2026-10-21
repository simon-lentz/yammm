package scripttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// ratioTests returns a test file for package ratio that imports raceskip.
func ratioTests(body string) string {
	return "package ratio\n\nimport (\n\t\"testing\"\n\n\t\"" + fixtureModule + "/internal/raceskip\"\n)\n\n" + body
}

func TestTestScript_NamesEveryPackageWithNoTestAndEverySkip(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("notests/n.go", "package notests\n")
	f.write("skips/s.go", "package skips\n")
	f.write("skips/s_test.go", "package skips\n\nimport \"testing\"\n\nfunc TestNeedsSymlink(t *testing.T) { t.Skip(\"needs a symlink\") }\n")
	f.write("ok/nocgo_test.go", "//go:build !cgo\n\npackage ok\n\nimport \"testing\"\n\nfunc TestRunsWithCgo(t *testing.T) { t.Fatal(\"the suite ran with cgo disabled\") }\n")
	f.index()

	r := f.run("test.sh")
	r.wantCode(t, 0)
	r.wantStdout(t, "test: 5 package(s) ran no test:\n"+
		"  "+fixtureModule+"/internal/mutants\n"+
		"  "+fixtureModule+"/internal/raceskip\n"+
		"  "+fixtureModule+"/internal/testjson\n"+
		"  "+fixtureModule+"/internal/testsummary\n"+
		"  "+fixtureModule+"/notests\n")
	r.wantStdout(t, "test: 1 test(s) skipped:\n  "+fixtureModule+"/skips TestNeedsSymlink: s_test.go:5: needs a symlink\n")
	r.wantStdout(t, "test: 7 of 7 packages reported a result, and none failed\n")
}

func TestTestScript_FailsWhenATestFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("ok/boom_test.go", "package ok\n\nimport \"testing\"\n\nfunc TestBoom(t *testing.T) { t.Fatal(\"boom\") }\n")
	f.index()

	r := f.run("test.sh")
	r.wantCode(t, 1)
	r.wantStdout(t, "boom_test.go:5: boom")
	r.wantStdout(t, "-test.shuffle ")
	r.wantStderr(t, "test: 1 of 5 packages failed:\n  "+fixtureModule+"/ok\n")
	r.wantStderr(t, "test: FAILED\n")
}

// Every benchmark runs once, after the suite, and one that fails fails the run
// with what it printed, though the suite's tests all pass: go test compiles a
// benchmark and runs none.
func TestTestScript_RunsEveryBenchmarkOnce(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		bench    string
		wantCode int
		stdout   string
	}{
		{"a benchmark that runs", "func BenchmarkRuns(b *testing.B) {\n\tfor b.Loop() {\n\t}\n}\n", 0, "test: 5 of 5 packages reported a result, and none failed\n"},
		{"a benchmark that fails", "func BenchmarkFails(b *testing.B) { b.Fatal(\"the benchmark's setup is wrong\") }\n", 1, "bench_test.go:5: the benchmark's setup is wrong"},
		// The benchmarks run without the race detector, so a file only that
		// build reads holds none of them.
		{"a benchmark only the race build reads", "func BenchmarkRaceOnly(b *testing.B) { b.Fatal(\"ran under the race detector\") }\n", 0, "test: 5 of 5 packages reported a result, and none failed\n"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			src := "package ok\n\nimport \"testing\"\n\n" + row.bench
			if strings.Contains(row.name, "race build") {
				src = "//go:build race\n\n" + src
			}
			f.write("ok/bench_test.go", src)
			f.index()

			r := f.run("test.sh")
			r.wantCode(t, row.wantCode)
			r.wantStdout(t, "test: every benchmark, once, without the race detector\n")
			r.wantStdout(t, row.stdout)
			if row.wantCode != 0 {
				r.wantStderr(t, "test: FAILED\n")
			}
		})
	}
}

// go test bounds no benchmark, so the run stops a benchmark's binary at
// TEST_BENCH_LIMIT, with its stacks where the host can ask for them, and fails,
// though the binary exits 0 when asked for its stacks.
func TestTestScript_StopsABenchmarkAtTheLimit(t *testing.T) {
	t.Parallel()
	rows := map[string]string{
		"a benchmark that hangs":                             "func BenchmarkHangs(b *testing.B) { time.Sleep(time.Hour) }\n",
		"a benchmark that exits 0 when asked for its stacks": "func BenchmarkHangs(b *testing.B) {\n\tasked := make(chan os.Signal, 1)\n\tsignal.Notify(asked, syscall.SIGQUIT)\n\tgo func() {\n\t\t<-asked\n\t\tos.Exit(0)\n\t}()\n\ttime.Sleep(time.Hour)\n}\n",
	}
	for name, bench := range rows {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			src := "package ok\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\n" + bench
			if strings.Contains(bench, "signal") {
				src = "//go:build unix\n\npackage ok\n\nimport (\n\t\"os\"\n\t\"os/signal\"\n\t\"syscall\"\n\t\"testing\"\n\t\"time\"\n)\n\n" + bench
			}
			f.write("ok/bench_test.go", src)
			f.index()
			f.env = []string{"TEST_BENCH_LIMIT=5s"}

			r := f.run("test.sh")
			if strings.Contains(bench, "signal") && runtime.GOOS == "windows" {
				r.wantCode(t, 0)
				return
			}
			r.wantCode(t, 1)
			// The benchmark's name is in the stacks SIGQUIT prints; a host with
			// no SIGQUIT kills the binary, and only the runner names the stop.
			if runtime.GOOS != "windows" {
				r.wantStdout(t, "BenchmarkHangs")
			}
			r.wantStdout(t, "mutants: the test binary was stopped at the limit of 5s")
			r.wantStderr(t, "test: FAILED\n")
		})
	}
}

// A TEST_BENCH_LIMIT that is not a whole number of seconds, minutes or hours
// from one second up to 24 hours, with no leading zero, stops the run before
// any test, named.
func TestTestScript_RefusesABenchmarkLimitItCannotRead(t *testing.T) {
	t.Parallel()
	for _, limit := range []string{"abc", "0s", "05s", "1.5m", "2ms", "86401s", "25h", "9999999s"} {
		t.Run(limit, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.index()
			f.env = []string{"TEST_BENCH_LIMIT=" + limit}

			r := f.run("test.sh")
			r.wantCode(t, 2)
			r.wantStderr(t, "test: TEST_BENCH_LIMIT must be a whole number of seconds, minutes or hours from 1 up to 24 hours, with no leading zero, such as 90s or 2m, got \""+limit+"\"\n")
			if r.stdout != "" {
				t.Errorf("stdout = %q, want nothing", r.stdout)
			}
		})
	}
}

// go test splits -exec at white space, so a temporary directory whose path
// holds a space, a quote, or both kinds of quote and no space still reaches the
// runner whole, and one that holds a space and both kinds, which go test cannot
// be given, stops the run before any test, named.
func TestTestScript_RunsTheBenchmarksFromATemporaryDirectoryWithASpace(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows takes no quote character in a file name")
	}
	for _, name := range []string{"a dir", "it's", `a "b"`, `it's"b"`, `it's "b"`} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.write("ok/bench_test.go", "package ok\n\nimport \"testing\"\n\nfunc BenchmarkRuns(b *testing.B) {\n\tfor b.Loop() {\n\t}\n}\n")
			f.index()
			tmp := filepath.Join(t.TempDir(), name)
			if err := os.Mkdir(tmp, 0o750); err != nil {
				t.Fatal(err)
			}
			f.env = []string{"TMPDIR=" + tmp}

			r := f.run("test.sh")
			if strings.Contains(name, "'") && strings.Contains(name, `"`) && strings.Contains(name, " ") {
				r.wantCode(t, 2)
				r.wantStderr(t, "holds white space and both quote characters, which no -exec value can hold; set TMPDIR to another directory\n")
				if r.stdout != "" {
					t.Errorf("stdout = %q, want nothing", r.stdout)
				}
				return
			}
			r.wantCode(t, 0)
			r.wantStdout(t, "test: every benchmark, once, without the race detector\n")
		})
	}
}

// The suite runs over the race build's package list, then without the race
// detector over each package only the plain build reads: each runs once, none
// is handed to a go test whose build leaves it out, and each list's note on
// what its build leaves out prints once.
func TestTestScript_RunsTheRaceBuildsPackagesAndThenThePlainBuildsOwn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("raceonly/r.go", "//go:build race\n\npackage raceonly\n")
	f.write("raceonly/r_test.go", "//go:build race\n\npackage raceonly\n\nimport \"testing\"\n\nfunc TestRaceOnly(t *testing.T) { t.Fatal(\"the race build's package ran\") }\n")
	f.write("norace/n.go", "//go:build !race\n\npackage norace\n")
	f.write("norace/n_test.go", "//go:build !race\n\npackage norace\n\nimport \"testing\"\n\nfunc TestNoRace(t *testing.T) { t.Fatal(\"the plain build's package ran\") }\n")
	f.index()

	r := f.run("test.sh")
	r.wantCode(t, 1)
	r.wantStdout(t, "the race build's package ran")
	r.wantStdout(t, "the plain build's package ran")
	r.wantStdout(t, "test: "+fixtureModule+"/norace, which only the build without the race detector reads, without it\n")
	r.wantStderr(t, "test: 1 of 6 packages failed:\n  "+fixtureModule+"/raceonly\n")
	r.wantStderr(t, "test: 1 of 1 packages failed:\n  "+fixtureModule+"/norace\n")
	if strings.Contains(r.stdout+r.stderr, "build constraints exclude all Go files") {
		t.Errorf("a package a build leaves out was handed to go test\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
	}
	host := hostTarget()
	r.wantStderr(t, "packages: this build ("+host+" -race) reads no package from 1 tracked director(ies):\n  norace\n")
	r.wantStderr(t, "packages: this build ("+host+") reads no package from 1 tracked director(ies):\n  raceonly\n")
	if n := strings.Count(r.stderr, "reads no package from"); n != 2 {
		t.Errorf("%d notes name what a build leaves out, want 2:\n%s", n, r.stderr)
	}
}

func TestTestScript_RunsUnderTheRaceDetector(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("ok/race_test.go", "package ok\n\nimport \"testing\"\n\n"+
		"func TestRace(t *testing.T) {\n\tx := 0\n\tdone := make(chan bool)\n\tgo func() { x++; done <- true }()\n\tx++\n\t<-done\n\t_ = x\n}\n")
	f.index()

	r := f.run("test.sh")
	r.wantCode(t, 1)
	r.wantStdout(t, "WARNING: DATA RACE")
	r.wantStderr(t, "test: 1 of 5 packages failed:\n  "+fixtureModule+"/ok\n")
}

func TestTestScript_RunsARaceSkippedTestAgainWithoutRace(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		tests    string
		files    map[string]string
		wantCode int
		stdout   []string
		stderr   []string
	}{
		{
			name:  "each plain run passes",
			tests: "func TestFloorA(t *testing.T) { raceskip.Skip(t) }\n\nfunc TestFloorB(t *testing.T) { raceskip.Skip(t) }\n",
			// The plain run must match the skipped tests' names exactly, not every name they begin.
			files: map[string]string{
				"ratio/extra_test.go": "//go:build !race\n\npackage ratio\n\nimport \"testing\"\n\nfunc TestFloorAExtra(t *testing.T) { t.Fatal(\"the plain run ran a test it did not name\") }\n",
			},
			wantCode: 0,
			stdout: []string{
				"test: TestFloorA,TestFloorB in " + fixtureModule + "/ratio, again without the race detector\n",
				"test: 1 of 1 packages reported a result, and none failed\n",
			},
		},
		{
			name:     "the plain run fails",
			tests:    "func TestFloor(t *testing.T) {\n\traceskip.Skip(t)\n\tt.Fatal(\"the floor does not hold\")\n}\n",
			wantCode: 1,
			stdout: []string{
				"test: TestFloor in " + fixtureModule + "/ratio, again without the race detector\n",
				"the floor does not hold",
				"-test.shuffle ",
			},
			stderr: []string{"test: 1 of 1 packages failed:\n  " + fixtureModule + "/ratio\n", "test: FAILED\n"},
		},
		{
			name:     "the plain run skips",
			tests:    "func TestFloor(t *testing.T) {\n\traceskip.Skip(t)\n\tt.Skip(\"no input on this host\")\n}\n",
			wantCode: 1,
			stdout:   []string{"test: TestFloor in " + fixtureModule + "/ratio, again without the race detector\n"},
			stderr:   []string{"test: TestFloor did not run and pass\n", "test: FAILED\n"},
		},
		{
			// The subtest is required by its own name: its parent passes in both runs.
			name: "a subtest's plain run skips",
			tests: "func TestRatios(t *testing.T) {\n\tt.Run(\"floor\", func(t *testing.T) {\n\t\traceskip.Skip(t)\n\t\tt.Skip(\"no input on this host\")\n\t})\n" +
				"\tt.Run(\"ceiling\", func(t *testing.T) { raceskip.Skip(t) })\n}\n",
			wantCode: 1,
			stdout:   []string{"test: TestRatios/ceiling,TestRatios/floor in " + fixtureModule + "/ratio, again without the race detector\n"},
			stderr:   []string{"test: TestRatios/floor did not run and pass\n", "test: FAILED\n"},
		},
		{
			// Each named test is required, the first as well as the last.
			name: "the first subtest's plain run skips",
			tests: "func TestRatios(t *testing.T) {\n\tt.Run(\"ceiling\", func(t *testing.T) {\n\t\traceskip.Skip(t)\n\t\tt.Skip(\"no input on this host\")\n\t})\n" +
				"\tt.Run(\"floor\", func(t *testing.T) { raceskip.Skip(t) })\n}\n",
			wantCode: 1,
			stdout:   []string{"test: TestRatios/ceiling,TestRatios/floor in " + fixtureModule + "/ratio, again without the race detector\n"},
			stderr:   []string{"test: TestRatios/ceiling did not run and pass\n", "test: FAILED\n"},
		},
		{
			// Each package's tests run again in a run of that package alone.
			name:  "tests skipped in two packages",
			tests: "func TestFloor(t *testing.T) { raceskip.Skip(t) }\n",
			files: map[string]string{
				"other/o.go":      "package other\n",
				"other/o_test.go": "package other\n\nimport (\n\t\"testing\"\n\n\t\"" + fixtureModule + "/internal/raceskip\"\n)\n\nfunc TestOther(t *testing.T) { raceskip.Skip(t) }\n",
			},
			wantCode: 0,
			stdout: []string{
				"test: TestOther in " + fixtureModule + "/other, again without the race detector\n",
				"test: TestFloor in " + fixtureModule + "/ratio, again without the race detector\n",
			},
		},
		{
			name:     "a subtest's plain run passes",
			tests:    "func TestRatios(t *testing.T) {\n\tt.Run(\"floor\", func(t *testing.T) { raceskip.Skip(t) })\n}\n\nfunc TestOther(t *testing.T) { raceskip.Skip(t) }\n",
			wantCode: 0,
			stdout: []string{
				"test: TestOther,TestRatios/floor in " + fixtureModule + "/ratio, again without the race detector\n",
				"test: 1 of 1 packages reported a result, and none failed\n",
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.write("ratio/r.go", "package ratio\n")
			f.write("ratio/r_test.go", ratioTests(row.tests))
			for rel, content := range row.files {
				f.write(rel, content)
			}
			f.index()

			r := f.run("test.sh")
			r.wantCode(t, row.wantCode)
			for _, want := range row.stdout {
				r.wantStdout(t, want)
			}
			for _, want := range row.stderr {
				r.wantStderr(t, want)
			}
		})
	}
}

// failingGoTest is a go that runs the real one and then exits 1 for a go test
// whose arguments hold SHIM_FAILS, whatever the real run's status.
const failingGoTest = `#!/usr/bin/env bash
"$SHIM_REAL" "$@"
rc=$?
if [ "$1" = test ]; then
	for a in "$@"; do
		if [ "$a" = "$SHIM_FAILS" ]; then exit 1; fi
	done
fi
exit "$rc"
`

// Each pipeline is go test and the summary, and the script fails when either
// program of either pipeline fails. Each row fails one program alone in a
// suite whose every test passes.
func TestTestScript_FailsWhenOneProgramOfAPipelineFailsAlone(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name      string
		shimFails string // the go test argument whose run the shim fails
		durations bool   // name a durations file the summary cannot write
		stdout    string
	}{
		{name: "the summary of the run under -race", durations: true, stdout: "test: 6 of 6 packages reported a result, and none failed\n"},
		{name: "go test under -race", shimFails: "-race", stdout: "test: 6 of 6 packages reported a result, and none failed\n"},
		{name: "go test in the run without -race", shimFails: "-run", stdout: "test: 1 of 1 packages reported a result, and none failed\n"},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.write("ratio/r.go", "package ratio\n")
			f.write("ratio/r_test.go", ratioTests("func TestFloor(t *testing.T) { raceskip.Skip(t) }\n"))
			f.index()
			if row.durations {
				f.env = append(f.env, "TEST_DURATIONS="+filepath.Join(t.TempDir(), "missing", "durations.tsv"))
			}
			if row.shimFails != "" {
				real, err := exec.LookPath("go")
				if err != nil {
					t.Fatal(err)
				}
				shim := t.TempDir()
				gittree.WriteFile(t, shim, "go", []byte(failingGoTest), 0o700)
				f.env = append(f.env, "PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"), "SHIM_REAL="+real, "SHIM_FAILS="+row.shimFails)
			}

			r := f.run("test.sh")

			r.wantCode(t, 1)
			r.wantStdout(t, row.stdout)
			r.wantStdout(t, "again without the race detector")
			r.wantStderr(t, "test: FAILED\n")
		})
	}
}

// slowGoTest is a go that runs the real one and, in a go test -json run,
// rewrites to 541.2 s the seconds of every package that passes, over 70% of a
// ten-minute -timeout; a failed or skipped package keeps its own.
const slowGoTest = `#!/usr/bin/env bash
if [ "$1" != test ]; then
	exec "$SHIM_REAL" "$@"
fi
"$SHIM_REAL" "$@" | sed -E 's/("Action":"pass","Package":"[^"]*","Elapsed":)[0-9.eE+-]+}/\1541.2}/'
exit "${PIPESTATUS[0]}"
`

// Each of the three runs hands its summary the ten-minute -timeout, and a
// package over 70% of it is named; in a GitHub Actions run each is also a
// warning annotation. The run still passes.
func TestTestScript_NamesEachPackageNearItsTimeout(t *testing.T) {
	t.Parallel()
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name    string
		actions string
	}{
		{"in a GitHub Actions run", "true"},
		{"elsewhere", ""},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.write("ratio/r.go", "package ratio\n")
			f.write("ratio/r_test.go", ratioTests("func TestFloor(t *testing.T) { raceskip.Skip(t) }\n"))
			f.write("norace/n.go", "//go:build !race\n\npackage norace\n")
			f.write("norace/n_test.go", "//go:build !race\n\npackage norace\n\nimport \"testing\"\n\nfunc TestNoRace(t *testing.T) {}\n")
			f.index()
			shim := t.TempDir()
			gittree.WriteFile(t, shim, "go", []byte(slowGoTest), 0o700)
			f.env = append(f.env, "PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"), "SHIM_REAL="+real, "GITHUB_ACTIONS="+row.actions)

			r := f.run("test.sh")

			r.wantCode(t, 0)
			for _, pkg := range []string{"ok", "ratio", "norace"} {
				r.wantStdout(t, "   541.2s  "+fixtureModule+"/"+pkg+"\n")
			}
			if n := strings.Count(r.stdout, "package(s) took over 70% of the 10m0s -timeout:\n"); n != 3 {
				t.Errorf("%d budget lists, want one for each run:\n%s", n, r.stdout)
			}
			annotation := func(pkg string) string {
				return "::warning title=Test budget::" + fixtureModule + "/" + pkg + " took 541.2s, over 70% of the 10m0s -timeout\n"
			}
			want := map[string]int{"ok": 1, "ratio": 2, "norace": 1}
			for pkg, n := range want {
				if row.actions == "" {
					n = 0
				}
				if got := strings.Count(r.stdout, annotation(pkg)); got != n {
					t.Errorf("%d annotations name %s, want %d:\n%s", got, pkg, n, r.stdout)
				}
			}
		})
	}
}

func TestTestScript_RunsEveryPackageHoldingATrackedGoFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("doc.go", "package yammm\n")
	f.write("doc_test.go", "package yammm\n\nimport \"testing\"\n\nfunc TestRoot(t *testing.T) { t.Fatal(\"root reached\") }\n")
	f.write("cafe/café.go", "package cafe\n")
	f.write("cafe/café_test.go", "package cafe\n\nimport \"testing\"\n\nfunc TestCafe(t *testing.T) { t.Fatal(\"cafe reached\") }\n")
	f.write(".gitignore", "untracked/\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n")
	f.write("untracked/u_test.go", "package untracked\n\nimport \"testing\"\n\nfunc TestU(t *notatype) {}\n")

	r := f.run("test.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: leaving out 1 package(s) that git ignores:\n  "+fixtureModule+"/untracked\n")
	r.wantStderr(t, "test: 2 of 7 packages failed:\n  "+fixtureModule+"\n  "+fixtureModule+"/cafe\n")
}

func TestTestScript_LeavesOutAnUntrackedPackage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write(".gitignore", "untracked/\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n")
	f.write("untracked/u_test.go", "package untracked\n\nimport \"testing\"\n\nfunc TestU(t *notatype) {}\n")

	r := f.run("test.sh")
	r.wantCode(t, 0)
	r.wantStderr(t, "packages: leaving out 1 package(s) that git ignores:\n  "+fixtureModule+"/untracked\n")
	// Both package lists write this note, and it prints once.
	if n := strings.Count(r.stderr, "packages: leaving out"); n != 1 {
		t.Errorf("the note prints %d times, want once:\n%s", n, r.stderr)
	}
	r.wantStdout(t, "test: 5 of 5 packages reported a result, and none failed\n")
}

func TestTestScript_RunsEveryTestRatherThanReplayingACachedResult(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("ratio/r.go", "package ratio\n")
	f.write("ratio/r_test.go", ratioTests("func TestFloor(t *testing.T) { raceskip.Skip(t) }\n"))
	f.index()

	f.run("test.sh").wantCode(t, 0)
	r := f.run("test.sh")
	r.wantCode(t, 0)
	if strings.Contains(r.stdout, "(cached)") {
		t.Errorf("the second run replayed a cached result\nstdout:\n%s", r.stdout)
	}
}

// The seconds are those of the run under -race: a race-skipped test's plain
// rerun writes nothing over the file and prints no list of the slowest packages.
func TestTestScript_ReportsTheSecondsOfTheRunUnderRace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	for _, name := range []string{"p1", "p2", "ratio"} {
		f.write(name+"/p.go", "package "+name+"\n")
	}
	f.write("ratio/r_test.go", ratioTests("func TestFloor(t *testing.T) { raceskip.Skip(t) }\n"))
	f.index()
	file := filepath.Join(t.TempDir(), "durations.tsv")
	f.env = append(f.env, "TEST_DURATIONS="+file)

	r := f.run("test.sh")
	r.wantCode(t, 0)
	r.wantStdout(t, "again without the race detector")

	t.Run("the file TEST_DURATIONS names holds them", func(t *testing.T) {
		t.Parallel()
		got, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read durations: %v", err)
		}
		for _, want := range []string{
			"package\ttest\tresult\tseconds\tscheduling\n",
			fixtureModule + "/ok\tTestOK\tpass\t",
			fixtureModule + "/ok\t\tpass\t",
			fixtureModule + "/ratio\tTestFloor\tskip\t",
		} {
			if !strings.Contains(string(got), want) {
				t.Errorf("durations do not hold %q:\n%s", want, got)
			}
		}
	})
	t.Run("the five slowest packages are listed once", func(t *testing.T) {
		t.Parallel()
		list := regexp.MustCompile(`(?m)^test: the 5 slowest of 8 packages:\n(?: +[0-9]+\.[0-9]s  ` + regexp.QuoteMeta(fixtureModule) + `[^\n]*\n){5}test: `)
		if got := list.FindAllString(r.stdout, -1); len(got) != 1 {
			t.Errorf("the run printed %d lists of its five slowest packages, want 1\nstdout:\n%s", len(got), r.stdout)
		}
		if n := strings.Count(r.stdout, "slowest of"); n != 1 {
			t.Errorf("the run named its slowest packages %d times, want once\nstdout:\n%s", n, r.stdout)
		}
	})
}

func TestTestScript_WritesNoDurationsUnlessNamed(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.index()

	f.run("test.sh").wantCode(t, 0)
	for _, args := range [][]string{{"ls-files", "--others"}, {"diff", "--name-only"}} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = f.dir
		cmd.Env = gittree.WithoutRepositoryVars(t, os.Environ())
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
		if len(out) != 0 {
			t.Errorf("a run with no TEST_DURATIONS wrote into the module (git %v):\n%s", args, out)
		}
	}
}

// Not parallel: it sets each variable for the whole process.
func TestFixtureEnv_DropsWhatTheEnclosingRunSetsForItself(t *testing.T) {
	enclosing := map[string]string{
		"TEST_DURATIONS":      filepath.Join(t.TempDir(), "enclosing.tsv"),
		"GITHUB_ACTIONS":      "true",
		"YAMMM_GOLANGCI_LINT": filepath.Join(t.TempDir(), "golangci-lint"),
	}
	for name, value := range enclosing {
		t.Setenv(name, value)
	}
	for _, kv := range fixtureEnv() {
		if name, _, _ := strings.Cut(kv, "="); enclosing[name] != "" {
			t.Errorf("a fixture inherits %s", kv)
		}
	}
}

// A relative TMPDIR names the work directory from the repository root, and the
// runner's path is made absolute, since go test starts it from each package's
// directory.
func TestTestScript_RunsTheBenchmarksUnderARelativeTMPDIR(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("ok/bench_test.go", "package ok\n\nimport \"testing\"\n\nfunc BenchmarkRuns(b *testing.B) {\n\tfor b.Loop() {\n\t}\n}\n")
	f.write(".gitignore", "rt/\n")
	f.index()
	if err := os.Mkdir(filepath.Join(f.dir, "rt"), 0o750); err != nil {
		t.Fatal(err)
	}
	f.env = []string{"TMPDIR=rt"}

	r := f.run("test.sh")
	r.wantCode(t, 0)
	r.wantStdout(t, "test: every benchmark, once, without the race detector\n")
}
