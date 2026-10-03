#!/usr/bin/env bash
# committest.sh — run the module's tests as the commit gate runs them: with go
# test's result cache and without the race detector.
#
# The pre-commit hook calls this script at each commit. With no -count flag, go
# test can replay a package's cached pass in place of running its tests again;
# `go help test` states the rule under package list mode. The verdict is go
# test's exit status.
#
# The full gate's definition is scripts/test.sh: every test run under the race
# detector, in shuffled order, with no cached result, and judged by
# internal/testsummary. `make gate` and every CI host run it.
#
# The package set is scripts/packages.sh's and the toolchain is the module's, as
# in scripts/test.sh. CGO_ENABLED and LC_ALL are set as that script sets them.
#
# Usage: scripts/committest.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's toolchain, not the host's: the gate and CI must run one
# standard library (scripts/toolchain.sh).
. scripts/toolchain.sh

export CGO_ENABLED=1
export LC_ALL=C

# git reads the index in a child process, which go test's result cache does not
# see. A test that lists the tracked files reads this digest of their names
# (internal/gittree), so its pass is not replayed once a file is added or removed.
YAMMM_TRACKED_FILES=$(git ls-files -z | git hash-object --stdin)
export YAMMM_TRACKED_FILES

list=$(scripts/packages.sh)
pkgs=()
while IFS= read -r pkg; do
	pkgs+=("${pkg}")
done <<<"${list}"

go test "${pkgs[@]}"
