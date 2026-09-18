#!/usr/bin/env bash
# lint.sh — lint the module for this host and for Windows with the pinned linter.
#
# golangci-lint type-checks one GOOS per run, so a file behind the windows
# constraint is never read on a darwin or Linux machine, and its first lint is
# CI's Windows job. This script gives a developer that read before a push; each
# CI host lints its own build. The linter is built once, for this host: under
# GOOS=windows, `go tool golangci-lint` builds the tool itself for Windows and
# cannot execute it. Every target runs even after an earlier one fails, and the
# summary names each failure. Arguments pass to every `golangci-lint run`.
#
# Usage: scripts/lint.sh [run arguments...]
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's toolchain, not the host's: the gate and CI must run one
# standard library (scripts/toolchain.sh).
. scripts/toolchain.sh

host=$(go env GOOS | tr -d '\r')
targets=("${host}")
[ "${host}" = windows ] || targets+=(windows)

bin=$(mktemp -d)
trap 'rm -rf "${bin}"' EXIT
go build -o "${bin}/" github.com/golangci/golangci-lint/v2/cmd/golangci-lint
linter="${bin}/golangci-lint"
[ -x "${linter}" ] || linter="${linter}.exe"

failed=()
for target in "${targets[@]}"; do
	GOOS="${target}" "${linter}" run "$@" || failed+=("${target}")
done

if [ "${#failed[@]}" -gt 0 ]; then
	printf 'lint: FAILED for %s (of %s)\n' "${failed[*]}" "${targets[*]}" >&2
	exit 1
fi
printf 'lint: clean for %s\n' "${targets[*]}"
