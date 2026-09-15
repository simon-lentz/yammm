package scripttest

import (
	"runtime"
	"strings"
	"testing"
)

// vetTargets is the order scripts/vet.sh vets in: this host, then every other
// target whose GOOS differs from it.
func vetTargets() []string {
	cross := []string{
		"linux/amd64", "windows/amd64", "darwin/arm64", "aix/ppc64", "android/arm64", "dragonfly/amd64",
		"freebsd/amd64", "illumos/amd64", "netbsd/amd64", "openbsd/amd64", "solaris/amd64",
	}
	targets := []string{runtime.GOOS}
	for _, target := range cross {
		if goos, _, _ := strings.Cut(target, "/"); goos != runtime.GOOS {
			targets = append(targets, target)
		}
	}
	return targets
}

func TestVetScript_VetsEveryTargetAndTheRaceBuild(t *testing.T) {
	t.Parallel()
	host := runtime.GOOS
	rows := []struct {
		name string
		args []string
		want string
	}{
		{"every target", nil, "vet: clean for " + strings.Join(vetTargets(), " ") + ", and " + host + " -race\n"},
		{"this host alone", []string{"--host"}, "vet: clean for " + host + ", and " + host + " -race\n"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			// Vet reads this file only for a solaris build with cgo enabled.
			f.write("ok/cgo_solaris.go", "//go:build cgo && solaris\n\npackage ok\n\nimport \"fmt\"\n\nfunc C() { fmt.Printf(\"%d\\n\", \"x\") }\n")
			f.index()

			r := f.run("vet.sh", row.args...)
			r.wantCode(t, 0)
			if r.stdout != row.want {
				t.Errorf("stdout = %q, want %q", r.stdout, row.want)
			}
		})
	}
}

func TestVetScript_FailsATargetWhoseSyscallPackageLacksACall(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		args     []string
		wantCode int
		stderr   string
	}{
		{"every target", nil, 1, "vet: FAILED for aix/ppc64 illumos/amd64 solaris/amd64 (of "},
		{"this host alone", []string{"--host"}, 0, ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.write("ok/fifo.go", "//go:build unix\n\npackage ok\n\nimport \"syscall\"\n\nfunc F() error { return syscall.Mkfifo(\"p\", 0o600) }\n")
			f.index()

			r := f.run("vet.sh", row.args...)
			r.wantCode(t, row.wantCode)
			r.wantStderr(t, row.stderr)
		})
	}
}

func TestVetScript_VetsTheRaceBuild(t *testing.T) {
	t.Parallel()
	host := runtime.GOOS
	f := newFixture(t)
	f.write("ok/race_test.go", "//go:build race\n\npackage ok\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc TestRaceOnly(t *testing.T) { fmt.Printf(\"%d\\n\", \"x\") }\n")
	f.index()

	r := f.run("vet.sh", "--host")
	r.wantCode(t, 1)
	r.wantStderr(t, "vet: FAILED for "+host+" -race (of "+host+", and "+host+" -race)\n")
}

func TestVetScript_ReportsEveryFailingTarget(t *testing.T) {
	t.Parallel()
	host := runtime.GOOS
	f := newFixture(t)
	f.write("ok/bad.go", "package ok\n\nimport \"fmt\"\n\nfunc Bad() { fmt.Printf(\"%d\\n\", \"x\") }\n")
	f.index()

	r := f.run("vet.sh")
	r.wantCode(t, 1)
	targets := strings.Join(vetTargets(), " ")
	r.wantStderr(t, "vet: FAILED for "+targets+" "+host+" -race (of "+targets+", and "+host+" -race)\n")
}

func TestVetScript_LeavesOutAnUntrackedPackage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.index()
	f.write("untracked/u.go", "package untracked\n\nimport \"fmt\"\n\nfunc F() { fmt.Printf(\"%d\\n\", \"x\") }\n")

	r := f.run("vet.sh", "--host")
	r.wantCode(t, 0)
	r.wantStderr(t, "packages: leaving out 1 package(s) that hold no tracked Go file:\n  "+fixtureModule+"/untracked\n")
}
