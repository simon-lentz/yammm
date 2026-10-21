#!/usr/bin/env bash
# packages.sh — print the module's packages that hold a git-tracked Go file and
# that one build reads, one import path per line.
#
# The build is the go command's own: GOOS, GOARCH, CGO_ENABLED and GOEXPERIMENT
# come from the environment, and the arguments are go list build flags, such as
# -race or -tags=x. A caller that judges several targets asks once for each.
#
# `./...` alone also reaches gitignored trees a developer's checkout holds and
# CI's does not — an npm dependency under lsp/editors/vscode/node_modules ships
# a Go package — so a local run and CI's would cover different sets. The gate
# scripts that test, vet or lint the module take their package set from here.
# `git ls-files -z` writes each path as it is, so a file name git would quote
# still maps to its package; the script then reads one path to a line, so a
# name that holds a newline is the one it cannot map.
#
# Two kinds of package are left out, and each is named on stderr: one whose
# every Go file git ignores, as an npm dependency's, and a tracked directory
# this build reads no package from, as one whose every Go file is for another
# GOOS. With --unbuilt FILE the
# second kind is also written to FILE, one directory a line, so a caller that
# asks for each build can name a directory no build reads. A tracked directory
# is one that holds a tracked Go file go would read under some build, in a
# directory ./... walks (dirs below says which).
#
# The run prints no package and stops with go list's exit status when go list
# fails, with exit 2 for an argument it cannot use, and with exit 1 when no
# package holds a tracked Go file and in four cases more. None of the four
# depends on the build, so every caller refuses one checkout alike, and the
# run names every file or directory of the first three before it stops.
#  - an untracked Go file, ignored or not, sits in a tracked directory, its
#    name beginning with neither "." nor "_". A go command building that
#    package compiles it unless a build constraint leaves it out, and CI's
#    checkout does not hold it, so a gate would judge a tree no commit holds.
#  - an untracked Go file git does not ignore sits in another directory go
#    reads: a tracked package can import its package, with the same effect.
#  - a tracked Go file sits below a go.mod git does not track, which makes this
#    checkout's go command read it as another module's and CI's as this one's.
#  - a go.work is in effect: the go command then reads a workspace, and the
#    package list is not this module's alone.
# With --check the run stops after these refusals, lists nothing and exits 0;
# internal/mutants refuses a checkout by it.
#
# Usage: [YAMMM_PACKAGES_TRACKED=FILE] scripts/packages.sh --check
#        [YAMMM_PACKAGES_TRACKED=FILE] scripts/packages.sh [--unbuilt FILE] [go list build flags...]
set -euo pipefail

unbuilt=""
check=no
if [ "${1:-}" = "--check" ]; then
	if [ "$#" -ne 1 ]; then
		printf 'packages: --check takes no other argument\n' >&2
		exit 2
	fi
	check=yes
	shift
elif [ "${1:-}" = "--unbuilt" ]; then
	if [ "$#" -lt 2 ] || [ -z "$2" ]; then
		printf 'packages: --unbuilt needs a file\n' >&2
		exit 2
	fi
	unbuilt=$2
	shift 2
fi
# A relative name is the caller's; an absolute one is kept, a Windows drive or
# UNC path among them, as Git Bash hands a Go test's path through.
case "${unbuilt}" in
"" | /* | [A-Za-z]:[\\/]* | [\\/][\\/]*) ;;
*) unbuilt="$(pwd)/${unbuilt}" ;;
esac

cd "$(git rev-parse --show-toplevel)"
export LC_ALL=C

work=$(go env GOWORK | tr -d '\r')
if [ -n "${work}" ] && [ "${work}" != off ]; then
	printf 'packages: the go command reads the workspace %s, so its package list is not this module'"'"'s alone; set GOWORK=off\n' "${work}" >&2
	exit 1
fi

tracked=$(mktemp)
untracked=$(mktemp)
listed=$(mktemp)
excluded=$(mktemp)
suite=$(mktemp)
built=$(mktemp)
modules=$(mktemp)
shadowed=$(mktemp)
trap 'rm -f "${tracked}" "${untracked}" "${listed}" "${excluded}" "${suite}" "${built}" "${modules}" "${shadowed}"' EXIT

# dirs reads NUL-separated file paths and prints, once each and "." for the
# root, the directory of each Go file go would read: not a name that begins
# with "." or "_", and not in a directory ./... passes over, which is testdata,
# one whose name begins with "." or "_", one below a vendor directory, one
# go.mod's ignore directive names (a "./x" pattern from the root, an "x"
# pattern at any depth, each read as go reads it, with a "/" added at either
# end it lacks; the root reads as ".", and when a pattern names it go reads
# nothing), or one at or below another module's root. With an argument it also writes there each directory it
# passes over for a go.mod git does not track.
dirs() {
	local patterns dir nested d out=${1:-}
	# go mod edit -json writes each pattern as a JSON string, one to a line, so
	# a newline in one is the escape "\n"; awk below splits the patterns at
	# newlines first and then decodes each, "\u0026" for "&" among the escapes.
	patterns=$(go mod edit -json | awk '
		/^\t"Ignore": \[/ { in_ignore = 1; next }
		in_ignore && /^\t\]/ { in_ignore = 0 }
		in_ignore && /"Path":/ { sub(/^[^:]*: *"/, ""); sub(/",?$/, ""); print }')
	tr '\0' '\n' | IGNORE_PATTERNS=${patterns} awk -F/ -v OFS=/ '
		function hex(h,    v, i) {
			v = 0
			for (i = 1; i <= length(h); i++) v = v * 16 + index("0123456789abcdef", substr(tolower(h), i, 1)) - 1
			return v
		}
		# utf8 writes a code point below U+10000 as its UTF-8 bytes; the run
		# exports LC_ALL=C, so %c writes one byte.
		function utf8(v) {
			if (v < 128) return sprintf("%c", v)
			if (v < 2048) return sprintf("%c%c", 192 + int(v / 64), 128 + v % 64)
			return sprintf("%c%c%c", 224 + int(v / 4096), 128 + int(v / 64) % 64, 128 + v % 64)
		}
		function unjson(s,    out, c, k) {
			out = ""
			while ((k = index(s, "\\")) > 0) {
				out = out substr(s, 1, k - 1)
				c = substr(s, k + 1, 1)
				if (c == "u") {
					out = out utf8(hex(substr(s, k + 2, 4)))
					s = substr(s, k + 6)
				} else {
					out = out (c == "t" ? "\t" : c == "n" ? "\n" : c == "r" ? "\r" : c == "b" ? "\b" : c == "f" ? "\f" : c)
					s = substr(s, k + 2)
				}
			}
			return out s
		}
		function ignored(path,    j) {
			for (j = 1; j <= n; j++) {
				if (p[j] == "") continue
				if (relative[j] ? index(path, p[j]) == 1 : index(path, p[j]) > 0) return 1
			}
			return 0
		}
		BEGIN {
			n = split(ENVIRON["IGNORE_PATTERNS"], p, "\n")
			for (j = 1; j <= n; j++) {
				if (p[j] == "") continue
				p[j] = unjson(p[j])
				relative[j] = sub(/^\.\//, "", p[j])
				if (p[j] !~ /^\//) p[j] = "/" p[j]
				if (p[j] !~ /\/$/) p[j] = p[j] "/"
			}
			# go reads the root as ".", and an ignored root ends its walk.
			root_ignored = ignored("/./")
		}
		root_ignored || $NF ~ /^[._]/ { next }
		{
			NF--
			for (i = 1; i <= NF; i++) {
				if ($i == "testdata" || $i ~ /^[._]/ || (i < NF && $i == "vendor")) next
			}
			if (NF && ignored("/" $0 "/")) next
			print (NF ? $0 : ".")
		}' | sort -u | while IFS= read -r dir; do
		nested=no
		d=${dir}
		while [ "${d}" != . ]; do
			if [ -f "${d}/go.mod" ]; then
				nested=yes
				if [ -n "${out}" ] && ! grep -qxF -- "${d}/go.mod" "${modules}"; then
					printf '%s\n' "${dir}" >>"${out}"
				fi
				break
			fi
			# The parent by parameter expansion: a process per level would cost
			# a few hundred spawns a run, which Git Bash on Windows makes slow.
			case ${d} in
			*/*) d=${d%/*} ;;
			*) d=. ;;
			esac
		done
		[ "${nested}" = yes ] || printf '%s\n' "${dir}"
	done
}

module=$(go list -m | tr -d '\r')
# refuse writes the tracked directories to ${tracked} and stops the run with
# exit 1 at any of the refusals the header names.
refuse() {
	refused=no
	git ls-files -z -- 'go.mod' '*/go.mod' | tr '\0' '\n' >"${modules}"
	git ls-files -z -- '*.go' | dirs "${shadowed}" >"${tracked}"
	if [ ! -s "${tracked}" ] && [ ! -s "${shadowed}" ]; then
		printf 'packages: no package holds a tracked Go file\n' >&2
			exit 1
	fi

	# A tracked Go file below a go.mod git does not track is in another module for
	# this checkout's go command, and in this one for CI's.
	if [ -s "${shadowed}" ]; then
		printf 'packages: %d tracked director(ies) below a go.mod git does not track, which go reads as another module and no commit holds; remove each go.mod:\n' "$(wc -l <"${shadowed}" | tr -d ' ')" >&2
		sed 's/^/  /' "${shadowed}" >&2
		refused=yes
	fi

	# An untracked Go file in a tracked directory, ignored or not: name each one.
	# Each pathspec matches one directory's own files, its name's glob
	# characters escaped.
	pathspecs=()
	while IFS= read -r dir; do
		if [ "${dir}" = . ]; then
			pathspecs+=(':(glob)*.go')
		else
			pathspecs+=(":(glob)$(printf '%s' "${dir}" | sed 's/[][*?\\]/\\&/g')/*.go")
		fi
	done <"${tracked}"
	if [ "${#pathspecs[@]}" -gt 0 ]; then
		git ls-files -z --others -- "${pathspecs[@]}" | tr '\0' '\n' | awk -F/ '$NF !~ /^[._]/' >"${untracked}"
	fi
	if [ -s "${untracked}" ]; then
		printf 'packages: %d untracked Go file(s) in a tracked package, which a go command building it compiles and no commit holds; add or remove each:\n' "$(wc -l <"${untracked}" | tr -d ' ')" >&2
		sed 's/^/  /' "${untracked}" >&2
		refused=yes
	fi

	# An untracked Go file git does not ignore, in a directory go reads and no
	# tracked Go file is in, is a package a tracked package can import.
	git ls-files -z --others --exclude-standard -- '*.go' | dirs | comm -23 - "${tracked}" >"${untracked}"
	if [ -s "${untracked}" ]; then
		printf 'packages: %d untracked package(s) that git does not ignore, which a tracked package can import and no commit holds; add or remove each:\n' "$(wc -l <"${untracked}" | tr -d ' ')" >&2
		sed 's/^/  /' "${untracked}" >&2
		refused=yes
	fi
	if [ "${refused}" = yes ]; then
			exit 1
	fi

}

# A caller that asks for several builds of one tree, as scripts/vet.sh does,
# names a file of its own in YAMMM_PACKAGES_TRACKED: the first run writes the
# tracked directories there once its refusals pass, and each later run reads
# them and makes neither the refusals nor the walk again.
if [ -n "${YAMMM_PACKAGES_TRACKED:-}" ] && [ -s "${YAMMM_PACKAGES_TRACKED}" ]; then
	cp "${YAMMM_PACKAGES_TRACKED}" "${tracked}"
else
	refuse
	if [ -n "${YAMMM_PACKAGES_TRACKED:-}" ]; then
		cp "${tracked}" "${YAMMM_PACKAGES_TRACKED}"
	fi
fi

if [ "${check}" = yes ]; then
	exit 0
fi

go list "$@" ./... | tr -d '\r' >"${listed}"

# A package's directory is its import path relative to the module, "." for the root.
awk -v module="${module}" -v excluded="${excluded}" -v built="${built}" '
	NR == FNR { tracked[$0] = 1; next }
	{
		rel = ($0 == module) ? "." : substr($0, length(module) + 2)
		print rel > built
		if (rel in tracked) print; else print > excluded
	}' "${tracked}" "${listed}" | sort -u >"${suite}"

# What is left of a listed package with no tracked Go file is files git
# ignores, as an npm dependency's are.
if [ -s "${excluded}" ]; then
	printf 'packages: leaving out %d package(s) that git ignores:\n' "$(wc -l <"${excluded}" | tr -d ' ')" >&2
	sed 's/^/  /' "${excluded}" >&2
fi
missing=$(sort -u "${built}" | comm -23 "${tracked}" -)
if [ -n "${missing}" ]; then
	printf 'packages: this build (%s/%s%s) reads no package from %d tracked director(ies):\n' \
		"$(go env GOOS | tr -d '\r')" "$(go env GOARCH | tr -d '\r')" "${*:+ $*}" "$(printf '%s\n' "${missing}" | wc -l | tr -d ' ')" >&2
	printf '%s\n' "${missing}" | sed 's/^/  /' >&2
	if [ -n "${unbuilt}" ]; then
		printf '%s\n' "${missing}" >>"${unbuilt}"
	fi
fi
if [ ! -s "${suite}" ]; then
	printf 'packages: no package holds a tracked Go file\n' >&2
	exit 1
fi
cat "${suite}"
