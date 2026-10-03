#!/usr/bin/env bash
# lint.sh — lint the module for this host and for Windows with the pinned linter.
#
# golangci-lint type-checks one GOOS per run, so a file behind the windows
# constraint is never read on a darwin or Linux machine, and its first lint is
# CI's Windows job. This script gives a developer that read before a push; each
# CI host lints its own build. The linter is this host's build of the module's
# tool: under GOOS=windows, `go tool golangci-lint` builds the tool itself for
# Windows and cannot execute it. `go tool -n` builds it when the build cache
# does not hold it and prints where the cache keeps it, so a later run links
# nothing. The script stops before it lints, and prints no summary, when go
# cannot build the tool or the path it prints holds no executable. Every target
# runs even after an earlier one fails, and the summary names each failure.
# Arguments pass to every `golangci-lint run`.
#
# With --host as the first argument, only this host's build is linted. The
# commit gate's hook and the test workflow's jsonv2 job pass it; the full
# gate's hook and CI's Windows job read the Windows build.
#
# Usage: scripts/lint.sh [--host] [run arguments...]
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's toolchain, not the host's: the gate and CI must run one
# standard library (scripts/toolchain.sh).
. scripts/toolchain.sh

host=$(go env GOOS | tr -d '\r')
targets=("${host}")
if [ "${1:-}" = "--host" ]; then
	shift
elif [ "${host}" != windows ]; then
	targets+=(windows)
fi

linter=$(go tool -n golangci-lint | tr -d '\r')
# Where the build cache keeps no executable, as a GOCACHEPROG cache does not,
# go tool -n prints the binary's path in go's work directory, which go removes
# as it exits.
if [ ! -x "${linter}" ]; then
	printf 'lint: go tool -n names no linter this run can execute: %s\n' "${linter}" >&2
	exit 2
fi

failed=()
for target in "${targets[@]}"; do
	GOOS="${target}" "${linter}" run "$@" || failed+=("${target}")
done

if [ "${#failed[@]}" -gt 0 ]; then
	printf 'lint: FAILED for %s (of %s)\n' "${failed[*]}" "${targets[*]}" >&2
	exit 1
fi
printf 'lint: clean for %s\n' "${targets[*]}"
