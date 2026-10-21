#!/usr/bin/env bash
# test.sh — run the module's test suite: the full gate's test definition.
#
# The full gate (`make gate`), each CI host's test job, the test workflow's
# jsonv2 job and `make test` run this script, and the release workflow runs
# those jobs before it builds anything, so a suite that passes the full gate
# locally ran under the flags CI runs it with. The commit gate's hook runs scripts/committest.sh instead.
# CGO_ENABLED is set rather than inherited, so every host tests the build with
# cgo on: with cgo off the go command refuses -race on Linux and Windows, and on
# macOS, where the detector needs no cgo, it would test the build with cgo off.
# -shuffle=on surfaces order coupling between tests, and -count=1 runs every
# test rather than replaying a cached pass.
# -timeout states Go's own default of ten minutes per test binary, so the CI
# job's timeout can be held above it: a hung binary then prints its stack before
# the job is killed, and the summary prints that stack.
#
# The suite is scripts/packages.sh's list for the race build, then, without
# the race detector, each package only the plain build reads, as one whose every
# file is behind !race. It runs through
# `go test -json`, and internal/testsummary judges it: every package must
# report a result, and the
# summary names each package that ran no test and every skipped test with its
# reason, and, for the run under -race, lists the five packages go test timed
# longest. Each summary is handed its run's -timeout and names every package
# that took over 70% of it; in a GitHub Actions run it also prints each as a
# warning annotation. The list changes no exit status. A test skipped through
# raceskip.Skip runs again without -race, and the run fails unless it passes;
# that rerun, and the run of the packages only the plain build reads, print no
# list of the slowest packages. When TEST_DURATIONS names a file, the summary
# writes there the seconds of every package and of every test that
# reported a result in the run under -race, a relative name read from the
# repository root.
#
# Last, every benchmark runs once, without the race detector and with no
# threshold, so a benchmark that fails, or whose binary runs past
# TEST_BENCH_LIMIT, fails the run: go test compiles a benchmark but runs none.
# TEST_BENCH_LIMIT is a whole number of seconds, minutes or hours from one
# second up to 24 hours, as 90s or 2m, and two minutes when unset or empty;
# another value stops the run before any test. The race detector would slow snapshot's
# benchmarks many times over. What a failing benchmark printed is printed.
#
# Usage: [TEST_DURATIONS=FILE] [TEST_BENCH_LIMIT=LIMIT] scripts/test.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's toolchain, not the host's: the gate and CI must run one
# standard library (scripts/toolchain.sh).
. scripts/toolchain.sh

export CGO_ENABLED=1
export LC_ALL=C

# internal/mutants reads TEST_BENCH_LIMIT by the same rule (parseLimit).
bench_limit=${TEST_BENCH_LIMIT:-2m}
seconds=0
if [[ ${bench_limit} =~ ^([1-9][0-9]{0,5})([smh])$ ]]; then
	case ${BASH_REMATCH[2]} in
	s) seconds=${BASH_REMATCH[1]} ;;
	m) seconds=$((BASH_REMATCH[1] * 60)) ;;
	h) seconds=$((BASH_REMATCH[1] * 3600)) ;;
	esac
fi
if [ "${seconds}" -lt 1 ] || [ "${seconds}" -gt 86400 ]; then
	printf 'test: TEST_BENCH_LIMIT must be a whole number of seconds, minutes or hours from 1 up to 24 hours, with no leading zero, such as 90s or 2m, got "%s"\n' "${bench_limit}" >&2
	exit 2
fi

# A GitHub Actions run shows the summary's budget list as warning annotations.
annotate=false
if [ "${GITHUB_ACTIONS:-}" = true ]; then
	annotate=true
fi

# The work directory is named from TMPDIR on every host: macOS mktemp -d
# takes the user's temporary directory whatever TMPDIR says.
work=$(mktemp -d "${TMPDIR:-/tmp}/test.XXXXXXXX")
trap 'rm -rf "${work}"' EXIT
# go test starts the -exec runner from each package's directory, so its path,
# which names a file in the work directory, must not be relative.
case ${work} in
/*) ;;
*) work="$(pwd)/${work}" ;;
esac

runner="${work}/mutants$(go env GOEXE | tr -d '\r')"
# go test splits -exec at white space, keeps a field that opens on a quote
# whole to the same quote, and reads a quote inside an unquoted field as itself.
case ${runner} in
*[[:space:]]*"'"*'"'* | *[[:space:]]*'"'*"'"* | *"'"*[[:space:]]*'"'* | *'"'*[[:space:]]*"'"* | *"'"*'"'*[[:space:]]* | *'"'*"'"*[[:space:]]*) exec_value="" ;;
*[[:space:]]*"'"* | *"'"*[[:space:]]*) exec_value="\"${runner}\" -supervise" ;;
*[[:space:]]*) exec_value="'${runner}' -supervise" ;;
*) exec_value="${runner} -supervise" ;;
esac
if [ -z "${exec_value}" ]; then
	printf 'test: the runner'"'"'s path %s holds white space and both quote characters, which no -exec value can hold; set TMPDIR to another directory\n' "${runner}" >&2
	exit 2
fi

# The suite runs under -race, which sets the race build tag, so its package
# list is the race build's; the benchmarks run without it, over the plain list.
# Each list's notes print once, a note being a line and the indented lines
# under it, and the plain list's only where they differ from the race list's.
status=0
YAMMM_PACKAGES_TRACKED="${work}/tracked" scripts/packages.sh -race >"${work}/race" 2>"${work}/notes" || status=$?
if [ "${status}" -eq 0 ]; then
	YAMMM_PACKAGES_TRACKED="${work}/tracked" scripts/packages.sh >"${work}/plain" 2>>"${work}/notes" || status=$?
fi
awk 'function flush() { if (note != "" && !seen[note]++) printf "%s", note; note = "" }
	!/^ / { flush() }
	{ note = note $0 "\n" }
	END { flush() }' "${work}/notes" >&2
if [ "${status}" -ne 0 ]; then
	exit "${status}"
fi
pkgs=()
while IFS= read -r pkg; do
	pkgs+=("${pkg}")
done <"${work}/race"
plain=()
while IFS= read -r pkg; do
	plain+=("${pkg}")
done <"${work}/plain"
# The packages only the plain build reads, which no run under -race tests.
only=()
while IFS= read -r pkg; do
	only+=("${pkg}")
done < <(comm -13 "${work}/race" "${work}/plain")

summary="${work}/testsummary$(go env GOEXE | tr -d '\r')"
go build -o "${summary}" ./internal/testsummary

set +e
go test -json -race -shuffle=on -count=1 -timeout=10m "${pkgs[@]}" 2>&1 |
	"${summary}" -race-skips="${work}/race-skips" -durations="${TEST_DURATIONS:-}" -slowest=5 -timeout=10m -annotate="${annotate}" "${pkgs[@]}"
codes=("${PIPESTATUS[@]}")
set -e
if [ "${codes[0]}" -ne 0 ] || [ "${codes[1]}" -ne 0 ]; then
	status=1
fi

# rerun <package> <test>... runs the package's named tests again without -race.
# -run selects by top-level test, and the summary requires each named test.
rerun() {
	local pkg=$1 name tops="" shown="" codes
	local required=()
	shift
	for name in "$@"; do
		case "|${tops}|" in *"|${name%%/*}|"*) ;; *) tops="${tops:+${tops}|}${name%%/*}" ;; esac
		shown="${shown:+${shown},}${name}"
		required+=("-require=${name}")
	done
	printf 'test: %s in %s, again without the race detector\n' "${shown}" "${pkg}"
	set +e
	go test -json -shuffle=on -count=1 -timeout=10m -run "^(${tops})\$" "${pkg}" 2>&1 |
		"${summary}" -timeout=10m -annotate="${annotate}" "${required[@]}" "${pkg}"
	codes=("${PIPESTATUS[@]}")
	set -e
	if [ "${codes[0]}" -ne 0 ] || [ "${codes[1]}" -ne 0 ]; then
		status=1
	fi
}

if [ "${#only[@]}" -gt 0 ]; then
	printf 'test: %s, which only the build without the race detector reads, without it\n' "${only[*]}"
	set +e
	go test -json -shuffle=on -count=1 -timeout=10m "${only[@]}" 2>&1 |
		"${summary}" -timeout=10m -annotate="${annotate}" "${only[@]}"
	codes=("${PIPESTATUS[@]}")
	set -e
	if [ "${codes[0]}" -ne 0 ] || [ "${codes[1]}" -ne 0 ]; then
		status=1
	fi
fi

# Each line of race-skips is a package, a tab and one test's name, and a
# package's lines are together.
if [ -s "${work}/race-skips" ]; then
	last=""
	names=()
	while IFS=$'\t' read -r -u 3 pkg name; do
		if [ -n "${last}" ] && [ "${pkg}" != "${last}" ]; then
			rerun "${last}" "${names[@]}"
			names=()
		fi
		last="${pkg}"
		names+=("${name}")
	done 3<"${work}/race-skips"
	rerun "${last}" "${names[@]}"
fi

# go test's -timeout bounds no benchmark: a test binary stops its alarm before
# its benchmarks, and go test sets no kill timeout under -bench. internal/mutants,
# as go test's -exec runner, stops each binary at the limit, asking first for
# its stacks where the host can.
printf 'test: every benchmark, once, without the race detector\n'
go build -o "${runner}" ./internal/mutants
mkdir "${work}/bench-records"
set +e
YAMMM_MUTANTS_RECORD="${work}/bench-records" YAMMM_MUTANTS_LIMIT="${bench_limit}" \
	go test -run '^$' -bench . -benchtime 1x -count=1 -timeout=2m -exec "${exec_value}" "${plain[@]}" >"${work}/bench.out" 2>&1
code=$?
set -e
if [ "${code}" -ne 0 ]; then
	cat "${work}/bench.out"
	status=1
fi

if [ "${status}" -ne 0 ]; then
	printf 'test: FAILED\n' >&2
fi
exit "${status}"
