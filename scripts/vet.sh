#!/usr/bin/env bash
# vet.sh — vet the module's packages for this host, for Windows and for every
# member of the unix family, and this host's build once more under -race.
#
# A developer's machine executes one host's build; CI executes three. Vet
# type-checks each target's GOOS-gated files and tests, so a call a target lacks
# fails here before a push. A file behind the unix constraint builds for every
# member of the family, and the members differ in the syscalls they declare
# (solaris and aix have no syscall.Mkfifo), so each member is vetted. ios is
# left out: its build links through cgo. scripts/test.sh
# always runs with -race, which sets the race build tag, so the files behind that
# tag are vetted too. The package set is scripts/packages.sh's. Every target runs
# even after an earlier one fails, and the summary names each failure.
#
# With --host, only this host's build is vetted, plain and under -race: each
# host job of the test workflow vets its own build, and one job of that
# workflow vets every target.
#
# Usage: scripts/vet.sh [--host]
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's toolchain, not the host's: the gate and CI must run one
# standard library (scripts/toolchain.sh).
. scripts/toolchain.sh

host=$(go env GOOS | tr -d '\r')
cross=(linux/amd64 windows/amd64 darwin/arm64 aix/ppc64 android/arm64 dragonfly/amd64
	freebsd/amd64 illumos/amd64 netbsd/amd64 openbsd/amd64 solaris/amd64)
targets=("${host}")
if [ "${1:-}" != "--host" ]; then
	for target in "${cross[@]}"; do
		[ "${target%%/*}" = "${host}" ] || targets+=("${target}")
	done
fi

list=$(scripts/packages.sh)
pkgs=()
while IFS= read -r pkg; do
	pkgs+=("${pkg}")
done <<<"${list}"

failed=()
for target in "${targets[@]}"; do
	if [ "${target}" = "${host}" ]; then
		go vet "${pkgs[@]}" || failed+=("${target}")
	elif ! GOOS="${target%%/*}" GOARCH="${target##*/}" CGO_ENABLED=0 go vet "${pkgs[@]}"; then
		failed+=("${target}")
	fi
done
if ! CGO_ENABLED=1 go vet -race "${pkgs[@]}"; then
	failed+=("${host} -race")
fi

if [ "${#failed[@]}" -gt 0 ]; then
	printf 'vet: FAILED for %s (of %s, and %s -race)\n' "${failed[*]}" "${targets[*]}" "${host}" >&2
	exit 1
fi
printf 'vet: clean for %s, and %s -race\n' "${targets[*]}" "${host}"
