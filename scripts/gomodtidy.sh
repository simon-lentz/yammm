#!/usr/bin/env bash
# gomodtidy.sh — assert every git-tracked module is tidy.
#
# Runs `go mod tidy -diff` in each directory holding a tracked go.mod, covering
# the testdata fixture modules as well as the root one. Tracked is the rule:
# the gitignored .claude/ tree holds throwaway probe modules whose replace
# directives name export directories that no longer exist.
#
# The exit code is the verdict: `-diff` exits non-zero when the diff is not
# empty. Stderr is download progress on a cold cache, not a diff.
#
# Usage: scripts/gomodtidy.sh
set -euo pipefail

status=0
errfile=$(mktemp)
trap 'rm -f "${errfile}"' EXIT

while IFS= read -r modfile; do
	dir=$(dirname "${modfile}")
	if ! diff=$(cd "${dir}" && go mod tidy -diff 2>"${errfile}"); then
		printf '%s: go mod tidy would change this module\n%s\n' "${dir}" "${diff}"
		cat "${errfile}" >&2
		status=1
	fi
done < <(git ls-files '*go.mod')

exit "${status}"
