package scripttest

import "testing"

func TestPackagesScript_ListsThePackagesHoldingATrackedGoFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("doc.go", "package yammm\n")
	f.write("cafe/café.go", "package cafe\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n")
	f.write("untracked2/u.go", "package untracked2\n")

	r := f.run("packages.sh")
	r.wantCode(t, 0)
	want := fixtureModule + "\n" +
		fixtureModule + "/cafe\n" +
		fixtureModule + "/internal/raceskip\n" +
		fixtureModule + "/internal/testsummary\n" +
		fixtureModule + "/ok\n"
	if r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if wantErr := "packages: leaving out 2 package(s) that hold no tracked Go file:\n  " + fixtureModule + "/untracked\n  " + fixtureModule + "/untracked2\n"; r.stderr != wantErr {
		t.Errorf("stderr = %q, want %q", r.stderr, wantErr)
	}
}

func TestPackagesScript_RefusesAModuleWithNoTrackedGoFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.index("go.mod", "scripts")

	r := f.run("packages.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: no package holds a tracked Go file\n")
	if r.stdout != "" {
		t.Errorf("stdout = %q, want nothing", r.stdout)
	}
}
