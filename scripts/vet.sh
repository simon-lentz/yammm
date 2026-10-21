#!/usr/bin/env bash
# vet.sh — vet the module's packages for this host, for every target in its
# cross list, and this host's build once more under -race.
#
# A developer's machine executes one host's build; CI executes three. Vet
# type-checks each target's GOOS- and GOARCH-gated files and tests, so a call a
# target lacks fails here before a push. The cross list names every GOOS and
# GOARCH pair the Makefile ships, and each member of the unix family save ios
# and hurd, since a file behind the unix constraint builds for every member and the members
# differ in the syscalls they declare (solaris and aix have no syscall.Mkfifo).
# ios is left out, since its build links through cgo, and hurd, which this
# toolchain does not build. Each target vets the package list scripts/packages.sh
# gives for that target, so a package whose every file is for one GOOS is vetted
# there. In a full run, a tracked directory that no build of the test
# workflow reads a package from fails the run and is named. Those builds are
# the cross-vet job's, every target but linux/amd64 with cgo off; on each host
# the workflow tests on (linux/amd64, windows/amd64 and darwin/arm64), with cgo
# on, the plain and race builds and the build with .golangci.yml's tags,
# mermaid and neo4j_integration, which its check job lints; linux/amd64 with
# the mermaid tag for adapter/markdown and with the neo4j_integration tag for
# adapter/neo4j/integration, the one package each of those jobs tests; and
# linux/amd64 under GOEXPERIMENT=jsonv2, plain and race. The check lists each
# with its own cgo setting and no GOFLAGS from any host alike; --host asks no
# other target and does not judge it.
# scripts/test.sh runs its suite with -race, which sets the race build tag, so
# the files behind that tag are vetted too. Every target runs even after an
# earlier one's vet fails, and the summary names each failure; a target whose
# package list cannot be made stops the run with packages.sh's message.
#
# This host is the go command's GOHOSTOS and GOHOSTARCH, whatever GOOS or
# GOARCH the caller exports; its build is vetted with the caller's CGO_ENABLED,
# and every other target with cgo off.
#
# With --host, only this host's build is vetted, plain and under -race: each
# host's check job of the test workflow vets its own build, its jsonv2 job vets
# the Linux build under GOEXPERIMENT=jsonv2, and one job of that workflow vets
# every target. The commit gate's hook passes --host too, and the full gate's
# hook vets every target.
#
# Usage: scripts/vet.sh [--host]
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's toolchain, not the host's: the gate and CI must run one
# standard library (scripts/toolchain.sh).
. scripts/toolchain.sh

host="$(go env GOHOSTOS | tr -d '\r')/$(go env GOHOSTARCH | tr -d '\r')"
cross=(darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64
	aix/ppc64 android/arm64 dragonfly/amd64 freebsd/amd64 illumos/amd64 netbsd/amd64
	openbsd/amd64 solaris/amd64)
targets=("${host}")
if [ "${1:-}" != "--host" ]; then
	for target in "${cross[@]}"; do
		[ "${target}" = "${host}" ] || targets+=("${target}")
	done
fi

work=$(mktemp -d)
trap 'rm -rf "${work}"' EXIT

# packages <unbuilt> <target> [go list build flags...] prints the target's
# package list, keeping packages.sh's notes, and writes the directories it
# reads nothing from to unbuilt.<unbuilt>.
packages() {
	local unbuilt=$1 target=$2
	shift 2
	GOOS="${target%%/*}" GOARCH="${target##*/}" YAMMM_PACKAGES_TRACKED="${work}/tracked" \
		scripts/packages.sh --unbuilt "${work}/unbuilt.${unbuilt}" "$@" 2>>"${work}/notes"
}

# notes prints each of packages.sh's notes once, in the order first written,
# and none an earlier call printed: a note is a line and the indented lines
# under it, and every target repeats the notes that do not depend on it.
printed=0
notes() {
	awk -v from="${printed}" '
		function flush() { if (note != "" && !seen[note]++ && start > from) printf "%s", note; note = "" }
		!/^ / { flush(); start = NR }
		{ note = note $0 "\n" }
		END { flush() }' "${work}/notes" >&2
	printed=$(wc -l <"${work}/notes" | tr -d ' ')
}

failed=()
n=0
for target in "${targets[@]}"; do
	cgo=0
	[ "${target}" != "${host}" ] || cgo=${CGO_ENABLED:-}
	n=$((n + 1))
	if ! list=$(CGO_ENABLED=${cgo} packages "${n}" "${target}"); then
		notes
		exit 1
	fi
	pkgs=()
	while IFS= read -r pkg; do
		pkgs+=("${pkg}")
	done <<<"${list}"
	if [ "${target}" = "${host}" ]; then
		GOOS="${target%%/*}" GOARCH="${target##*/}" go vet "${pkgs[@]}" || failed+=("${target}")
	elif ! GOOS="${target%%/*}" GOARCH="${target##*/}" CGO_ENABLED=0 go vet "${pkgs[@]}"; then
		failed+=("${target}")
	fi
done
if ! list=$(CGO_ENABLED=1 packages host-race "${host}" -race); then
	notes
	exit 1
fi
pkgs=()
while IFS= read -r pkg; do
	pkgs+=("${pkg}")
done <<<"${list}"
if ! GOOS="${host%%/*}" GOARCH="${host##*/}" CGO_ENABLED=1 go vet -race "${pkgs[@]}"; then
	failed+=("${host} -race")
fi

# A tracked directory no build of the test workflow reads a package from is
# read by none of them. Each line is a build: its target, CGO_ENABLED, GOEXPERIMENT ("-" for
# none), go list's build flags ("-" for none) and the directory the job hands go
# test ("-" for the module's every package). Only these lists are counted: the
# vet lists above follow this host. GOFLAGS is cleared, as CI sets none.
unread=""
if [ "${1:-}" != "--host" ]; then
	builds=()
	for target in "${cross[@]}"; do
		# the cross-vet job, on linux/amd64, vets every other target with cgo off
		[ "${target}" = linux/amd64 ] || builds+=("${target} 0 - - -")
	done
	for target in linux/amd64 windows/amd64 darwin/arm64; do
		# each test host's check job, with cgo on: vet, and golangci-lint over
		# every package under .golangci.yml's build tags; and its test job
		builds+=("${target} 1 - - -" "${target} 1 - -tags=mermaid,neo4j_integration -" "${target} 1 - -race -")
	done
	builds+=(
		"linux/amd64 1 - -tags=mermaid adapter/markdown"
		"linux/amd64 1 - -tags=neo4j_integration adapter/neo4j/integration"
		"linux/amd64 1 jsonv2 - -"
		"linux/amd64 1 jsonv2 -race -"
	)
	module=$(GOFLAGS='' go list -m | tr -d '\r')
	: >"${work}/read"
	for build in "${builds[@]}"; do
		read -r target cgo experiment flags dir <<<"${build}"
		[ "${experiment}" != - ] || experiment=""
		args=()
		[ "${flags}" = - ] || args=("${flags}")
		n=$((n + 1))
		CGO_ENABLED=${cgo} GOEXPERIMENT=${experiment} GOFLAGS='' packages "ci.${n}" "${target}" "${args[@]+"${args[@]}"}" >"${work}/list" || {
			notes
			exit 1
		}
		# Each import path to its directory, "." for the root, kept when the
		# job reads it.
		awk -v module="${module}" -v dir="${dir}" '{
			d = ($0 == module) ? "." : substr($0, length(module) + 2)
			if (dir == "-" || d == dir) print d
		}' "${work}/list" >>"${work}/read"
	done
	# packages.sh wrote the tracked directories at its first run.
	unread=$(sort -u "${work}/read" | comm -13 - <(sort -u "${work}/tracked"))
fi
notes

if [ -n "${unread}" ]; then
	printf 'vet: no build of the test workflow reads a package from these tracked directories:\n' >&2
	printf '%s\n' "${unread}" | sed 's/^/  /' >&2
	failed+=("every target")
fi

if [ "${#failed[@]}" -gt 0 ]; then
	printf 'vet: FAILED for %s (of %s, and %s -race)\n' "${failed[*]}" "${targets[*]}" "${host}" >&2
	exit 1
fi
printf 'vet: clean for %s, and %s -race\n' "${targets[*]}" "${host}"
