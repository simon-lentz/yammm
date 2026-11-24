#!/usr/bin/env bash
# lint.sh — lint the module for this host and for windows/amd64 with the pinned
# linter.
#
# golangci-lint type-checks one GOOS and GOARCH per run, so a file behind the
# windows constraint is never read on a darwin or Linux machine, and its first
# lint is CI's Windows job. This script gives a developer that read before a
# push, for windows/amd64, the pair CI's Windows job lints; each CI host lints
# its own build. This host is the go command's GOHOSTOS and GOHOSTARCH, linted
# with the caller's CGO_ENABLED; windows/amd64 is linted with cgo off, since
# its standard library cannot be compiled with cgo from another host.
#
# Each target lints the package list scripts/packages.sh gives for that target,
# so it reads no untracked package, as scripts/vet.sh and scripts/test.sh read
# none. The linter is scripts/linter.sh's: the build YAMMM_GOLANGCI_LINT names,
# or one made for this run. The script stops before it lints, and prints no
# summary, when it has no linter to execute. Every target runs even after an
# earlier one's lint fails, and the summary names each failure; a target whose
# package list cannot be made stops the run with packages.sh's message.
# Arguments are flags of `golangci-lint run`, passed to every run before the
# target's package list, so a path among them adds to the list and does not
# narrow it.
#
# With --host as the first argument, only this host's build is linted. The
# commit gate's hook and the test workflow's jsonv2 job pass it; the full
# gate's hook reads the windows/amd64 build too.
#
# Usage: scripts/lint.sh [--host] [run arguments...]
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's toolchain, not the host's: the gate and CI must run one
# standard library (scripts/toolchain.sh).
. scripts/toolchain.sh

host="$(go env GOHOSTOS | tr -d '\r')/$(go env GOHOSTARCH | tr -d '\r')"
targets=("${host}")
if [ "${1:-}" = "--host" ]; then
	shift
elif [ "${host}" != windows/amd64 ]; then
	targets+=(windows/amd64)
fi

. scripts/linter.sh

module=$(go list -m | tr -d '\r')
failed=()
for target in "${targets[@]}"; do
	cgo=0
	[ "${target}" != "${host}" ] || cgo=${CGO_ENABLED:-}
	list=$(GOOS="${target%%/*}" GOARCH="${target##*/}" CGO_ENABLED=${cgo} scripts/packages.sh)
	# golangci-lint reads a bare argument as a directory under ./, so each
	# package goes as its directory.
	dirs=()
	while IFS= read -r pkg; do
		if [ "${pkg}" = "${module}" ]; then
			dirs+=(.)
		else
			dirs+=("./${pkg#"${module}"/}")
		fi
	done <<<"${list}"
	if [ "${target}" = "${host}" ]; then
		GOOS="${target%%/*}" GOARCH="${target##*/}" "${linter}" run "$@" "${dirs[@]}" || failed+=("${target}")
	elif ! GOOS="${target%%/*}" GOARCH="${target##*/}" CGO_ENABLED=0 "${linter}" run "$@" "${dirs[@]}"; then
		failed+=("${target}")
	fi
done

if [ "${#failed[@]}" -gt 0 ]; then
	printf 'lint: FAILED for %s (of %s)\n' "${failed[*]}" "${targets[*]}" >&2
	exit 1
fi
printf 'lint: clean for %s\n' "${targets[*]}"
