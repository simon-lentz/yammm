package scripttest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// probeToolchain writes a script that sources scripts/toolchain.sh and reports
// what it left in the environment. Sourcing is the whole contract — an exported
// variable cannot reach a caller that merely runs the file.
func probeToolchain(f *fixture) result {
	f.write("scripts/probe.sh", "#!/usr/bin/env bash\nset -euo pipefail\n. scripts/toolchain.sh\necho \"pinned=${GOTOOLCHAIN:-<unset>}\"\n")
	return f.run("probe.sh")
}

// TestToolchainScript_RefusesAToolchainThatIsNotTheModulesOwn pins the check it
// exists for: a host one release ahead of go.mod runs a different standard
// library and can pass a gate CI then fails.
func TestToolchainScript_RefusesAToolchainThatIsNotTheModulesOwn(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("go.mod", "module "+fixtureModule+"\n\ngo 1.0.0\n")

	r := probeToolchain(f)

	r.wantCode(t, 2)
	r.wantStderr(t, "go.mod pins go1.0.0")
}

// TestToolchainScript_RefusesAGoModWithNoDirective pins the other refusal: a
// module file the check cannot read stops the run rather than pinning to the
// empty string.
func TestToolchainScript_RefusesAGoModWithNoDirective(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("go.mod", "module "+fixtureModule+"\n")

	r := probeToolchain(f)

	r.wantCode(t, 2)
	r.wantStderr(t, "names no go directive")
}

// TestToolchainScript_LeavesACallersOwnChoiceAlone pins that a caller who set
// GOTOOLCHAIN keeps it, so CI's GOTOOLCHAIN=local still forbids a download and
// the check verifies local's claim instead of overriding it.
func TestToolchainScript_LeavesACallersOwnChoiceAlone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)

	r := probeToolchain(f)

	r.wantCode(t, 0)
	r.wantStdout(t, "pinned=local")
}

// TestToolchainScript_PinsWhenTheCallerChoseNothing pins the export: with no
// GOTOOLCHAIN the run takes the module's, which is what makes a local gate run
// the toolchain CI runs.
func TestToolchainScript_PinsWhenTheCallerChoseNothing(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("scripts/probe.sh", "#!/usr/bin/env bash\nset -euo pipefail\nunset GOTOOLCHAIN\n. scripts/toolchain.sh\necho \"pinned=${GOTOOLCHAIN:-<unset>}\"\n")

	r := f.run("probe.sh")

	r.wantCode(t, 0)
	if want := "pinned=go" + goDirective(); !strings.Contains(r.stdout, want) {
		t.Errorf("stdout does not hold %q\nstdout:\n%s\nstderr:\n%s", want, r.stdout, r.stderr)
	}
}

// TestToolchainScript_RefusesADirectoryWithNoModuleFile pins the refusal a
// caller outside a module gets: the run stops and names the directory.
func TestToolchainScript_RefusesADirectoryWithNoModuleFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.dir, "go.mod")); err != nil {
		t.Fatal(err)
	}

	r := probeToolchain(f)

	r.wantCode(t, 2)
	r.wantStderr(t, "toolchain: no go.mod in ")
}

// TestToolchainScript_ReadsTheWorkingDirectorysModuleAlone pins that no
// variable redirects the check: a caller working on another checkout changes
// into it, and an exported TOOLCHAIN_ROOT, which the helper once read, now
// changes nothing.
func TestToolchainScript_ReadsTheWorkingDirectorysModuleAlone(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("elsewhere/go.mod", "module other\n\ngo 1.0.0\n")
	f.env = []string{"TOOLCHAIN_ROOT=elsewhere"}

	r := probeToolchain(f)

	r.wantCode(t, 0)
	r.wantStdout(t, "pinned=local")
}

// TestToolchainScript_ReadsAModuleFileWithCRLFLineEndings pins that a checkout
// written with CRLF line endings, as git's autocrlf writes one on Windows, pins
// the version and not the version with a CR.
func TestToolchainScript_ReadsAModuleFileWithCRLFLineEndings(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("go.mod", "module "+fixtureModule+"\r\n\r\ngo "+goDirective()+"\r\n")
	f.write("scripts/probe.sh", "#!/usr/bin/env bash\nset -euo pipefail\nunset GOTOOLCHAIN\n. scripts/toolchain.sh\necho \"pinned=${GOTOOLCHAIN:-<unset>}\"\n")

	r := f.run("probe.sh")

	r.wantCode(t, 0)
	if want := "pinned=go" + goDirective() + "\n"; !strings.Contains(r.stdout, want) {
		t.Errorf("stdout does not hold %q\nstdout:\n%s\nstderr:\n%s", want, r.stdout, r.stderr)
	}
}

// TestGateScripts_RefuseAToolchainThatIsNotTheModulesOwn pins the pin in every
// gate script that builds or runs Go, so none of them judges the tree under a
// standard library CI does not run.
func TestGateScripts_RefuseAToolchainThatIsNotTheModulesOwn(t *testing.T) {
	t.Parallel()
	for _, script := range []string{"test.sh", "committest.sh", "vet.sh", "lint.sh", "lintconfig.sh", "gomodtidy.sh"} {
		t.Run(script, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.copyScript(script)
			f.write("go.mod", "module "+fixtureModule+"\n\ngo 1.0.0\n")
			f.index()

			r := f.run(script)

			r.wantCode(t, 2)
			r.wantStderr(t, "go.mod pins go1.0.0")
		})
	}
}

// TestDirectiveOf pins the spelling the fixtures write into go.mod, which the
// scripts then compare with the running toolchain.
func TestDirectiveOf(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]string{
		"go1.26.0":          "1.26.0",
		"go1.27rc1":         "1.27rc1",
		"go1.26.0 X:jsonv2": "1.26.0",
	} {
		if got := directiveOf(version); got != want {
			t.Errorf("directiveOf(%q) = %q, want %q", version, got, want)
		}
	}
}

// TestGomodtidyScript_IgnoresAnExportedCDPATH pins that a caller's CDPATH does
// not reach the tidiness verdict: with it, bash's cd prints the directory it
// chose, and the capture read that line as a diff in every nested module.
func TestGomodtidyScript_IgnoresAnExportedCDPATH(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.copyScript("gomodtidy.sh")
	f.write("sub/go.mod", "module sub\n\ngo "+goDirective()+"\n")
	f.write("sub/sub.go", "package sub\n")
	f.index()
	f.env = []string{"CDPATH=.:" + t.TempDir()}

	r := f.run("gomodtidy.sh")

	r.wantCode(t, 0)
}
