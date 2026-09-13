#!/usr/bin/env bash
# test.sh — run the module's test suite: the one test definition every gate uses.
#
# The pre-commit hook, each CI host's job, the release workflow and `make test`
# all call this script, so a suite that passes locally ran under the flags CI
# runs it with. -race needs cgo, so CGO_ENABLED is set rather than inherited: a
# host with no C compiler fails here instead of testing without the detector.
# -shuffle=on surfaces order coupling between tests, and -count=1 runs every
# test rather than replaying a cached pass.
#
# The suite is the packages `./...` names that hold a git-tracked Go file.
# `./...` alone also reaches gitignored trees a developer's checkout holds and
# CI's does not — an npm dependency under lsp/editors/vscode/node_modules ships
# a Go package — so the local run and CI's would test different sets. Each
# package left out is named.
#
# A green exit is believed only with its count: every package in the suite must
# report a result line, and a package that reports none fails the run.
#
# Usage: scripts/test.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

export CGO_ENABLED=1
export LC_ALL=C

log=$(mktemp)
tracked=$(mktemp)
listed=$(mktemp)
suite=$(mktemp)
excluded=$(mktemp)
reported=$(mktemp)
trap 'rm -f "${log}" "${tracked}" "${listed}" "${suite}" "${excluded}" "${reported}"' EXIT

module=$(go list -m)
git ls-files -- '*.go' | awk -F/ -v OFS=/ '{ NF--; print (NF ? $0 : ".") }' | sort -u >"${tracked}"
go list ./... | tr -d '\r' >"${listed}"

# A package's directory is its import path relative to the module, "." for the root.
awk -v module="${module}" -v excluded="${excluded}" '
	NR == FNR { tracked[$0] = 1; next }
	{
		rel = ($0 == module) ? "." : substr($0, length(module) + 2)
		if (rel in tracked) print; else print > excluded
	}' "${tracked}" "${listed}" | sort -u >"${suite}"

if [ -s "${excluded}" ]; then
	printf 'test: leaving out %d package(s) that hold no tracked Go file:\n' "$(wc -l <"${excluded}" | tr -d ' ')"
	sed 's/^/  /' "${excluded}"
fi

total=$(wc -l <"${suite}" | tr -d ' ')
if [ "${total}" -eq 0 ]; then
	printf 'test: no package holds a tracked Go file; the suite would pass over nothing\n' >&2
	exit 1
fi

set +e
xargs go test -race -shuffle=on -count=1 <"${suite}" 2>&1 | tee "${log}"
status=${PIPESTATUS[0]}
set -e

# A result line is "ok", "FAIL" or "?", a tab, then the package; a build or
# setup failure appends " [build failed]" or " [setup failed]" to the package.
tr -d '\r' <"${log}" |
	awk -F'\t' '$1 ~ /^(ok|FAIL|\?) *$/ && NF >= 2 { sub(/ \[[a-z ]+\]$/, "", $2); print $2 }' |
	sort -u >"${reported}"

missing=$(comm -23 "${suite}" "${reported}")
if [ -n "${missing}" ]; then
	printf 'test: %d of %d packages reported no result:\n%s\n' \
		"$(printf '%s\n' "${missing}" | wc -l | tr -d ' ')" "${total}" "${missing}" >&2
	[ "${status}" -ne 0 ] || status=1
fi

if [ "${status}" -ne 0 ]; then
	printf 'test: FAILED (exit %d) over %d packages\n' "${status}" "${total}" >&2
	exit "${status}"
fi
printf 'test: %d of %d packages reported a result, and every one passed\n' "${total}" "${total}"
