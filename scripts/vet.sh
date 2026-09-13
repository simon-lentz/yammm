#!/usr/bin/env bash
# vet.sh — vet every package for this host, for Linux and for Windows.
#
# A developer's machine executes one host's build; CI executes the others. Vet
# type-checks each target's GOOS-gated files and tests, so a call a target
# lacks fails here before a push rather than in that target's CI job. Every
# target runs even after an earlier one fails, and the summary names each
# failure.
#
# Usage: scripts/vet.sh
set -euo pipefail

cd "$(dirname "$0")/.."

host=$(go env GOOS)
targets=("${host}")
for goos in linux windows; do
	[ "${goos}" = "${host}" ] || targets+=("${goos}")
done

failed=()
for goos in "${targets[@]}"; do
	if ! GOOS="${goos}" go vet ./...; then
		failed+=("${goos}")
	fi
done

if [ "${#failed[@]}" -gt 0 ]; then
	printf 'vet: FAILED for %s (of %s)\n' "${failed[*]}" "${targets[*]}" >&2
	exit 1
fi
printf 'vet: clean for %s\n' "${targets[*]}"
