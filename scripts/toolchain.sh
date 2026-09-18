#!/usr/bin/env bash
# toolchain.sh — hold a run to the module's own Go toolchain. SOURCE it; running
# it changes nothing, because an exported variable has to reach the caller.
#
# The gate and CI ran the same COMMANDS under different standard libraries. CI
# resolves go.mod's `go` directive through actions/setup-go and pins the result
# with GOTOOLCHAIN=local; a developer's `go` is whatever is on PATH. A host one
# release ahead passes a 14-of-14 gate that CI then fails, and the failure is in
# the standard library rather than in the change: encoding/json reports a
# different SyntaxError.Offset in go1.26 and go1.27, so three assertions about
# diagnostic positions passed here and failed on every CI host.
#
# Two jobs, in this order:
#
#  1. A caller that chose no toolchain gets the module's. GOTOOLCHAIN names the
#     version, so the go command fetches it once and uses it from then on.
#  2. Whatever the caller chose, the version that will actually run is checked
#     against go.mod and a mismatch stops the run. CI sets GOTOOLCHAIN=local,
#     which claims the installed toolchain is already the right one; this is
#     what checks that claim.
#
# Usage: source scripts/toolchain.sh   (from the repository root)

want=$(awk '$1 == "go" { print $2; exit }' go.mod)
if [ -z "${want}" ]; then
	echo "toolchain: go.mod names no go directive" >&2
	exit 2
fi

if [ -z "${GOTOOLCHAIN:-}" ]; then
	export GOTOOLCHAIN="go${want}"
fi

have=$(go env GOVERSION)
if [ "${have}" != "go${want}" ]; then
	echo "toolchain: go.mod pins go${want} and this run is ${have}" >&2
	echo "toolchain: GOTOOLCHAIN=${GOTOOLCHAIN:-<unset>}; unset it to take the pinned toolchain" >&2
	exit 2
fi
