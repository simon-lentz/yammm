#!/usr/bin/env bash
# mutate.sh — kill one mutant, and prove the attempt was real.
#
# Applies one textual mutation to a tracked Go file, builds, and runs the named
# test packages expecting them to FAIL. A mutation the suite does not notice is
# a surviving mutant: nothing asserts the behaviour it changed.
#
# Two things are asserted before any verdict, because both have gone wrong here
# in one fix pass and neither was caught. The search string MUST match: a
# pattern matching nothing rewrites nothing, and "nothing red" then reads as
# "mutant killed" when no mutant existed. The build MUST succeed: a mutation
# that does not compile makes go test exit non-zero for an unrelated reason,
# and that also reads as "killed".
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

# Restore from a byte copy rather than from git: the harness must put the file
# back exactly as it found it without performing a git write.
# The suite MUST be green before the mutation: a test that is already red
# makes every mutant read as killed, which voided two sweeps in one fix pass.
if ! go test "${pkgs[@]}" >/dev/null 2>&1; then
	printf 'mutate: the UNMUTATED tree is already red in %s, so no verdict is possible\n' "${pkgs[*]}" >&2
	exit 1
fi
printf 'mutate: baseline green\n'

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
printf 'mutate: build ok\n'

set +e
test_out=$(go test "${pkgs[@]}" 2>&1)
rc=$?
set -e

if [ "${rc}" -eq 0 ]; then
	printf 'mutate: MUTANT SURVIVED — %s builds and the suite is still green\n' "${file}" >&2
	printf '  %s -> %s\n' "${search}" "${replace}" >&2
	printf '  nothing in %s asserts the behaviour this mutation changed\n' "${pkgs[*]}" >&2
	exit 1
fi

printf 'mutate: MUTANT KILLED (exit %d)\n' "${rc}"
printf '%s\n' "${test_out}" | grep -E '^(---|\s+---)? *(FAIL|ok)' | head -20
exit 0
