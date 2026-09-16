package scripttest

import (
	"strings"
	"testing"
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
	r.wantStdout(t, "test: 3 package(s) ran no test:\n"+
		"  "+fixtureModule+"/internal/raceskip\n"+
		"  "+fixtureModule+"/internal/testsummary\n"+
		"  "+fixtureModule+"/notests\n")
	r.wantStdout(t, "test: 1 test(s) skipped:\n  "+fixtureModule+"/skips TestNeedsSymlink: s_test.go:5: needs a symlink\n")
	r.wantStdout(t, "test: 5 of 5 packages reported a result, and none failed\n")
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
	r.wantStderr(t, "test: 1 of 3 packages failed:\n  "+fixtureModule+"/ok\n")
	r.wantStderr(t, "test: FAILED\n")
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
	r.wantStderr(t, "test: 1 of 3 packages failed:\n  "+fixtureModule+"/ok\n")
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

func TestTestScript_RunsEveryPackageHoldingATrackedGoFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("doc.go", "package yammm\n")
	f.write("doc_test.go", "package yammm\n\nimport \"testing\"\n\nfunc TestRoot(t *testing.T) { t.Fatal(\"root reached\") }\n")
	f.write("cafe/café.go", "package cafe\n")
	f.write("cafe/café_test.go", "package cafe\n\nimport \"testing\"\n\nfunc TestCafe(t *testing.T) { t.Fatal(\"cafe reached\") }\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n")
	f.write("untracked/u_test.go", "package untracked\n\nimport \"testing\"\n\nfunc TestU(t *notatype) {}\n")

	r := f.run("test.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: leaving out 1 package(s) that hold no tracked Go file:\n  "+fixtureModule+"/untracked\n")
	r.wantStderr(t, "test: 2 of 5 packages failed:\n  "+fixtureModule+"\n  "+fixtureModule+"/cafe\n")
}

func TestTestScript_LeavesOutAnUntrackedPackage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.index()
	f.write("untracked/u.go", "package untracked\n")
	f.write("untracked/u_test.go", "package untracked\n\nimport \"testing\"\n\nfunc TestU(t *notatype) {}\n")

	r := f.run("test.sh")
	r.wantCode(t, 0)
	r.wantStderr(t, "packages: leaving out 1 package(s) that hold no tracked Go file:\n  "+fixtureModule+"/untracked\n")
	r.wantStdout(t, "test: 3 of 3 packages reported a result, and none failed\n")
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
