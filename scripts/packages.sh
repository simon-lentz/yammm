#!/usr/bin/env bash
# packages.sh — print the module's packages that hold a git-tracked Go file,
# one import path per line.
#
# `./...` alone also reaches gitignored trees a developer's checkout holds and
# CI's does not — an npm dependency under lsp/editors/vscode/node_modules ships
# a Go package — so a local run and CI's would cover different sets. Every
# script that tests or vets the module takes its package set from here. Each
# package left out is named on stderr. `git ls-files -z` writes each path as it
# is, so a file name git would quote still maps to its package.
#
# Usage: scripts/packages.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
export LC_ALL=C

tracked=$(mktemp)
listed=$(mktemp)
excluded=$(mktemp)
suite=$(mktemp)
trap 'rm -f "${tracked}" "${listed}" "${excluded}" "${suite}"' EXIT

module=$(go list -m)
git ls-files -z -- '*.go' | tr '\0' '\n' | awk -F/ -v OFS=/ '{ NF--; print (NF ? $0 : ".") }' | sort -u >"${tracked}"
go list ./... | tr -d '\r' >"${listed}"

# A package's directory is its import path relative to the module, "." for the root.
awk -v module="${module}" -v excluded="${excluded}" '
	NR == FNR { tracked[$0] = 1; next }
	{
		rel = ($0 == module) ? "." : substr($0, length(module) + 2)
		if (rel in tracked) print; else print > excluded
	}' "${tracked}" "${listed}" | sort -u >"${suite}"

if [ -s "${excluded}" ]; then
	printf 'packages: leaving out %d package(s) that hold no tracked Go file:\n' "$(wc -l <"${excluded}" | tr -d ' ')" >&2
	sed 's/^/  /' "${excluded}" >&2
fi
if [ ! -s "${suite}" ]; then
	printf 'packages: no package holds a tracked Go file\n' >&2
	exit 1
fi
cat "${suite}"
