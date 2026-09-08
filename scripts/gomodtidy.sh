#!/usr/bin/env bash
# gomodtidy.sh — assert every git-tracked module is tidy.
#
# Runs `go mod tidy -diff` in each directory holding a tracked go.mod, covering
# the testdata fixture modules as well as the root one. Tracked is the rule:
# the gitignored .claude/ tree holds throwaway probe modules whose replace
# directives name export directories that no longer exist.
#
# The EVIDENCE is the verdict, not the exit code. `-diff` documents no exit-code
# contract, and a non-zero status with nothing on stdout is a command that
# failed to RUN — a network error, a missing toolchain — which reported as
# "this module is untidy" sends an operator to look for a diff that is not
# there. So: output means untidy; no output and a non-zero status means the
# command failed, reported as such with its stderr.
#
# Usage: scripts/gomodtidy.sh
set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "${root}"

status=0
errfile=$(mktemp)
trap 'rm -f "${errfile}"' EXIT

modules=$(git ls-files --full-name -- 'go.mod' '*/go.mod') || {
	printf 'gomodtidy: git ls-files failed; the module list is unknown\n' >&2
	exit 1
}
if [ -z "${modules}" ]; then
	printf 'gomodtidy: no tracked go.mod found under %s; the gate would pass over nothing\n' "${root}" >&2
	exit 1
fi

while IFS= read -r modfile; do
	dir=$(dirname "${modfile}")
	set +e
	output=$(cd "${dir}" && go mod tidy -diff 2>"${errfile}")
	rc=$?
	set -e
	if [ -n "${output}" ]; then
		printf '%s: go mod tidy would change this module\n%s\n' "${dir}" "${output}"
		status=1
	elif [ "${rc}" -ne 0 ]; then
		printf '%s: go mod tidy -diff failed to run (exit %d)\n' "${dir}" "${rc}" >&2
		cat "${errfile}" >&2
		status=1
	fi
done <<<"${modules}"

# An untracked go.mod beside a tracked one is a module the gate would skip in
# CI and read locally, which is the divergence class this script exists to
# close. Refused rather than silently excluded.
untracked=$(git ls-files --others --exclude-standard -- 'go.mod' '*/go.mod')
if [ -n "${untracked}" ]; then
	printf 'gomodtidy: untracked go.mod, which CI will not see:\n%s\n' "${untracked}" >&2
	status=1
fi

exit "${status}"
