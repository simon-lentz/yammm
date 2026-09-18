package scripttest

import (
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

// TestToolchainScript_ReadsTheRootItIsGiven pins TOOLCHAIN_ROOT, which
// scripts/run_mutants.sh needs: it reads one checkout and mutates copies of it,
// so the module file it must agree with is not its working directory's.
func TestToolchainScript_ReadsTheRootItIsGiven(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("elsewhere/go.mod", "module other\n\ngo 1.0.0\n")
	f.write("scripts/probe.sh", "#!/usr/bin/env bash\nset -euo pipefail\nTOOLCHAIN_ROOT=elsewhere\n. scripts/toolchain.sh\n")

	r := f.run("probe.sh")

	r.wantCode(t, 2)
	r.wantStderr(t, "elsewhere/go.mod pins go1.0.0")
}

// TestToolchainScript_RefusesARootWithNoModuleFile pins the other TOOLCHAIN_ROOT
// arm: a root naming no module file stops the run rather than reading the
// working directory's by accident.
func TestToolchainScript_RefusesARootWithNoModuleFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("scripts/probe.sh", "#!/usr/bin/env bash\nset -euo pipefail\nTOOLCHAIN_ROOT=nosuchdir\n. scripts/toolchain.sh\n")

	r := f.run("probe.sh")

	r.wantCode(t, 2)
	r.wantStderr(t, "no go.mod at nosuchdir/go.mod")
}

// TestMutateScript_RefusesAToolchainThatIsNotTheModulesOwn pins the reason the
// mutation harness pins at all: without it a tree red under the module's Go and
// green under the host's yields a full set of verdicts about a tree CI rejects.
func TestMutateScript_RefusesAToolchainThatIsNotTheModulesOwn(t *testing.T) {
	t.Parallel()
	f, _ := mutateFixture(t, "3")
	f.write("go.mod", "module "+fixtureModule+"\n\ngo 1.0.0\n")

	r := f.mutate()

	r.wantCode(t, 2)
	r.wantStderr(t, "pins go1.0.0")
}

// TestRunMutantsScript_RefusesTheToolchainBeforeAnyWorkerStarts pins that the
// check runs once, at the top, rather than once per mutant inside each worker.
func TestRunMutantsScript_RefusesTheToolchainBeforeAnyWorkerStarts(t *testing.T) {
	t.Parallel()
	f := runMutantsFixture(t)
	f.write("go.mod", "module "+fixtureModule+"\n\ngo 1.0.0\n")
	f.index() // the checkout must be clean, which the rewritten go.mod undid
	f.writeMutant("m01", "a - b")

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	r.wantCode(t, 2)
	r.wantStderr(t, "pins go1.0.0")
}
