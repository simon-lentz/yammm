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
