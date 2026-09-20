#!/usr/bin/env bash
# mutate.sh — kill one mutant, and prove the attempt was real.
#
# Applies one textual mutation to a tracked Go file, builds, and runs the named
# test packages expecting them to FAIL. A mutation the suite does not notice is
# a surviving mutant: nothing asserts the behaviour it changed.
#
# Two things are asserted before any verdict, because each fails silently.
# The search string MUST match: a
# pattern matching nothing rewrites nothing, and "nothing red" then reads as
# "mutant killed" when no mutant existed. The build MUST succeed: a mutation
# that does not compile makes go test exit non-zero for an unrelated reason,
# and that also reads as "killed". The build includes the named packages' test
# binaries and the vet checks go test runs, which `go build` does not reach: a
# mutant that breaks only a test build, or fails that vet, fails go test before
# any of the package's code runs. The check executes none of the binaries it
# builds: a mutant that panics in init or fails in a TestMain is a kill for the
# verdict run to report, and a TestMain that writes files runs only there.
#
# The named packages must pass before the mutation. When MUTATE_BASELINE_CACHE
# names a directory, a passing baseline is recorded there under a key over the
# packages, TMPDIR, YAMMM_* variables, the Go environment and the tree's content,
# and a later run with the same key skips the baseline. The harness restores
# each mutated file byte for byte, so the key still matches after a mutant, and
# any edit to the tree changes it.
#
# MUTATE_BASELINE_CACHE is dropped from the environment of the suite this script
# runs. A package that shells out to this script would otherwise inherit the
# cache directory and take the recorded-baseline path, which is how a run over
# internal/scripttest read its own unmutated tree as red.
#
# Usage:
#   scripts/mutate.sh <file> <search> <replace> <pkg> [pkg...]
#   scripts/mutate.sh format/wrap.go 'Threshold = 100' 'Threshold = 1000' ./format/
set -euo pipefail

usage() {
	echo "usage: $0 <file> <search> <replace> <pkg> [pkg...]" >&2
	exit 2
}

[ "$#" -ge 4 ] || usage
file="$1"
search="$2"
replace="$3"
shift 3
pkgs=("$@")

root=$(git rev-parse --show-toplevel)
cd "${root}"

# A verdict is a statement about the program CI judges, so the suite that
# produces it runs the module's toolchain (scripts/toolchain.sh). Without the
# pin a tree red under the module's Go and green under the host's yields a full
# set of verdicts about a tree CI rejects.
. scripts/toolchain.sh

if [ ! -f "${file}" ]; then
	printf 'mutate: %s does not exist\n' "${file}" >&2
	exit 2
fi

# Counted as the replacement matches, one literal that may span lines; grep -c
# reads a multi-line search as one pattern per line and counts matching lines.
hits=$(python3 - "${file}" "${search}" <<'PY'
import sys
path, search = sys.argv[1], sys.argv[2]
with open(path, encoding="utf-8", newline="") as fh:
    print(fh.read().count(search))
PY
)
if [ "${hits}" -eq 0 ]; then
	printf 'mutate: the search string matched NOTHING in %s\n' "${file}" >&2
	printf '  searched for: %s\n' "${search}" >&2
	printf '  no mutant was introduced, so a green suite would prove nothing\n' >&2
	exit 1
fi

# baseline_key hashes the packages, the environment the tests read, and the
# content of every tracked, staged, unstaged and untracked file.
baseline_key() {
	{
		printf '%s\n' "${pkgs[@]}"
		printf 'TMPDIR=%s\n' "${TMPDIR:-}"
		env | grep '^YAMMM_' | LC_ALL=C sort || true
		go env GOOS GOARCH CGO_ENABLED GOFLAGS GOEXPERIMENT
		go version
		git ls-files --stage
		git diff --binary
		git ls-files --others --exclude-standard
		git ls-files --others --exclude-standard | git hash-object --stdin-paths
	} | git hash-object --stdin
}

# The suite MUST be green before the mutation: a test that is already red
# makes every mutant read as killed.
stamp=""
if [ -n "${MUTATE_BASELINE_CACHE:-}" ]; then
	mkdir -p -- "${MUTATE_BASELINE_CACHE}"
	stamp="${MUTATE_BASELINE_CACHE}/$(baseline_key)"
fi
if [ -n "${stamp}" ] && [ -f "${stamp}" ]; then
	printf 'mutate: baseline green (recorded for this tree in %s)\n' "${MUTATE_BASELINE_CACHE}"
else
	if ! env -u MUTATE_BASELINE_CACHE go test "${pkgs[@]}" >/dev/null 2>&1; then
		printf 'mutate: the UNMUTATED tree is already red in %s, so no verdict is possible\n' "${pkgs[*]}" >&2
		exit 1
	fi
	if [ -n "${stamp}" ]; then
		: >"${stamp}"
	fi
	printf 'mutate: baseline green\n'
fi

# Restore from a byte copy rather than from git: the harness must put the file
# back exactly as it found it without performing a git write.

backup=$(mktemp)
cp -- "${file}" "${backup}"
trap 'cp -- "${backup}" "${file}"; rm -f -- "${backup}"' EXIT

python3 - "${file}" "${search}" "${replace}" <<'PY'
import sys
path, search, replace = sys.argv[1], sys.argv[2], sys.argv[3]
# newline="" keeps a CRLF file's line endings, so only the mutation changes.
with open(path, encoding="utf-8", newline="") as fh:
    text = fh.read()
with open(path, "w", encoding="utf-8", newline="") as fh:
    fh.write(text.replace(search, replace))
PY

if cmp -s -- "${backup}" "${file}"; then
	printf 'mutate: the replacement left %s byte-identical; the mutation did not apply\n' "${file}" >&2
	exit 1
fi
printf 'mutate: applied to %s (%s occurrence(s) matched)\n' "${file}" "${hits}"

if ! build_out=$(go build ./... 2>&1); then
	printf 'mutate: the mutated tree DOES NOT BUILD, so the suite cannot judge it\n' >&2
	printf '%s\n' "${build_out}" >&2
	exit 1
fi
# -exec=true builds and vets each test binary, then runs `true` in its place.
if ! build_out=$(env -u MUTATE_BASELINE_CACHE go test -exec=true "${pkgs[@]}" 2>&1); then
	printf 'mutate: the mutated tree DOES NOT BUILD its tests or fails their vet, so the suite cannot judge it\n' >&2
	printf '%s\n' "${build_out}" >&2
	exit 1
fi
printf 'mutate: build ok\n'

set +e
test_out=$(env -u MUTATE_BASELINE_CACHE go test "${pkgs[@]}" 2>&1)
rc=$?
set -e

if [ "${rc}" -eq 0 ]; then
	printf 'mutate: MUTANT SURVIVED — %s builds and the suite is still green\n' "${file}" >&2
	printf '  %s -> %s\n' "${search}" "${replace}" >&2
	printf '  nothing in %s asserts the behaviour this mutation changed\n' "${pkgs[*]}" >&2
	exit 1
fi

# A non-zero exit is not a kill. go test gives each named package one line: the
# package and a duration when its test binary RAN — a failing test, a panic in
# init or in a test, a TestMain exit, a timeout — and the package and a
# bracketed reason ([build failed], [setup failed]) when nothing ran there.
# The pre-build above judged these same packages, so a bracketed reason here
# means the tree or the environment moved under the verdict run.
#
# One package that ran is not enough, because go test prints both forms in one
# run: a genuine failure beside a package that never built still judges a tree
# the pre-build never saw.
#
# Limit: a test binary killed from outside prints a duration like any other
# run that started, and this reads that as a kill.
ran_re='^FAIL[[:space:]]+[^[:space:]]+[[:space:]]+[0-9]+\.[0-9]+s$'
notrun=$(printf '%s\n' "${test_out}" | grep -E '^FAIL[[:space:]]' | grep -vE "${ran_re}" || true)
ran=$(printf '%s\n' "${test_out}" | grep -cE "${ran_re}" || true)

if [ -n "${notrun}" ]; then
	printf 'mutate: NO TEST RAN in a named package, so this is not a kill\n' >&2
	printf '%s\n' "${notrun}" >&2
	printf '  a package line carries a duration when its test binary ran; a bracketed reason means it did not\n' >&2
	printf '  the pre-build passed, so the tree moved under the verdict run\n' >&2
	exit 1
fi
if [ "${ran}" -eq 0 ]; then
	printf 'mutate: NO TEST RAN — the verdict run named no package that ran, so this is not a kill\n' >&2
	printf '%s\n' "${test_out}" >&2
	exit 1
fi

printf 'mutate: MUTANT KILLED (exit %d)\n' "${rc}"
printf '%s\n' "${test_out}" | grep -E '^[[:space:]]*--- FAIL|^FAIL' || true
exit 0
