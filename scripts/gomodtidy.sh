#!/usr/bin/env bash
# gomodtidy.sh — assert every git-tracked module is tidy.
#
# Runs `go mod tidy -diff` in each directory holding a tracked go.mod, covering
# the testdata fixture modules as well as the root one. Tracked is the rule:
# the gitignored .claude/ tree holds throwaway probe modules whose replace
# directives name export directories that no longer exist.
#
# Usage: scripts/gomodtidy.sh
set -euo pipefail

status=0

while IFS= read -r modfile; do
	dir=$(dirname "${modfile}")
	if ! output=$(cd "${dir}" && go mod tidy -diff 2>&1); then
		printf '%s: go mod tidy would change this module\n%s\n' "${dir}" "${output}"
		status=1
	elif [ -n "${output}" ]; then
		printf '%s:\n%s\n' "${dir}" "${output}"
		status=1
	fi
done < <(git ls-files '*go.mod')

exit "${status}"
