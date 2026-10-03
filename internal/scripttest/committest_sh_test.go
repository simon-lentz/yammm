package scripttest

import (
	"path/filepath"
	"strings"
	"testing"
)

// The commit gate's test run takes the suite and the build of scripts/test.sh,
// less the race detector, and replays a pass from go test's result cache. The
// fixture's environment sets CGO_ENABLED=0 and LC_ALL=POSIX, so only the
// script's own settings give the tests cgo and LC_ALL=C.
func TestCommitTestScript_ReplaysAPassWithoutTheRaceDetector(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("ok/race_test.go", "//go:build race\n\npackage ok\n\nimport \"testing\"\n\nfunc TestRunsUnderRace(t *testing.T) { t.Fatal(\"the suite ran under the race detector\") }\n")
	f.write("ok/nocgo_test.go", "//go:build !cgo\n\npackage ok\n\nimport \"testing\"\n\nfunc TestRunsWithCgo(t *testing.T) { t.Fatal(\"the suite ran with cgo disabled\") }\n")
	f.write("ok/short_test.go", "package ok\n\nimport \"testing\"\n\nfunc TestRunsEveryTest(t *testing.T) {\n\tif testing.Short() {\n\t\tt.Fatal(\"the suite ran with -short\")\n\t}\n}\n")
	f.write("ok/locale_test.go", "package ok\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\n"+
		"func TestRunsInTheCLocale(t *testing.T) {\n\tif got := os.Getenv(\"LC_ALL\"); got != \"C\" {\n\t\tt.Fatalf(\"LC_ALL is %q\", got)\n\t}\n}\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n")
	f.write("untracked/u_test.go", "package untracked\n\nimport \"testing\"\n\nfunc TestU(t *notatype) {}\n")
	f.env = []string{"CGO_ENABLED=0", "LC_ALL=POSIX"}
	const ran = "ok  \t" + fixtureModule + "/ok\t"

	first := f.run("committest.sh")
	first.wantCode(t, 0)
	first.wantStderr(t, "packages: leaving out 1 package(s) that hold no tracked Go file:\n  "+fixtureModule+"/untracked\n")
	first.wantStdout(t, ran)
	if strings.Contains(first.stdout, "(cached)") {
		t.Errorf("the first run replayed a result\nstdout:\n%s", first.stdout)
	}

	// The script finds the repository root from any directory inside it.
	second := f.runFrom(filepath.Join(f.dir, "ok"), "committest.sh")
	second.wantCode(t, 0)
	second.wantStdout(t, ran+"(cached)\n")
}

// A step that fails stops the script: with no package to test it runs no go test.
func TestCommitTestScript_StopsWhenThePackageListFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.index("go.mod", "scripts")

	r := f.run("committest.sh")
	r.wantCode(t, 1)
	if want := "packages: no package holds a tracked Go file\n"; r.stderr != want || r.stdout != "" {
		t.Errorf("stdout %q, stderr %q, want no output but %q", r.stdout, r.stderr, want)
	}
}

func TestCommitTestScript_FailsWhenATestFails(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("ok/boom_test.go", "package ok\n\nimport \"testing\"\n\nfunc TestBoom(t *testing.T) { t.Fatal(\"boom\") }\n")
	f.index()

	r := f.run("committest.sh")
	r.wantCode(t, 1)
	r.wantStdout(t, "boom_test.go:5: boom")
	r.wantStdout(t, "FAIL\t"+fixtureModule+"/ok\t")
}

// A test that lists the tracked files through git calls
// gittree.MarkTrackedFiles, so the commit gate's test run does not replay its
// pass once a file is added to the index. The file is on disk from the start:
// adding it changes the index and no directory the test reads.
func TestCommitTestScript_RunsATrackedTreeTestAgainAfterTheIndexChanges(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.copyPackage("internal/gittree")
	f.write("tracked/tracked.go", "package tracked\n")
	f.write("tracked/tracked_test.go", "package tracked\n\nimport (\n\t\"os/exec\"\n\t\"strings\"\n\t\"testing\"\n\n\t\""+fixtureModule+"/internal/gittree\"\n)\n\n"+
		"func TestTracksNoRefusedFile(t *testing.T) {\n"+
		"\tgittree.MarkTrackedFiles()\n"+
		"\tcmd := exec.CommandContext(t.Context(), \"git\", \"ls-files\")\n\tcmd.Dir = \"..\"\n"+
		"\tout, err := cmd.Output()\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n"+
		"\tif strings.Contains(string(out), \"refused.txt\") {\n\t\tt.Fatal(\"refused.txt is tracked\")\n\t}\n}\n")
	f.write("refused.txt", "refused\n")
	f.index("go.mod", "scripts", "internal", "ok", "tracked")

	f.run("committest.sh").wantCode(t, 0)
	replayed := f.run("committest.sh")
	replayed.wantCode(t, 0)
	replayed.wantStdout(t, "ok  \t"+fixtureModule+"/tracked\t(cached)\n")

	f.index("refused.txt")
	r := f.run("committest.sh")
	r.wantCode(t, 1)
	r.wantStdout(t, "refused.txt is tracked")
}
