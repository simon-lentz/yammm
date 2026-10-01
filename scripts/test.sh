#!/usr/bin/env bash
# test.sh — run the module's test suite: the full gate's test definition.
#
# The full gate (`make gate`), each CI host's job and `make test` run this
# script, and the release workflow runs those host jobs before it builds
# anything, so a suite that passes the full gate locally ran under the flags CI
# runs it with. The commit gate's hook runs scripts/committest.sh instead.
# CGO_ENABLED is set rather than inherited, so every host tests the build with
# cgo on: with cgo off the go command refuses -race on Linux and Windows, and on
# macOS, where the detector needs no cgo, it would test the build with cgo off.
# -shuffle=on surfaces order coupling between tests, and -count=1 runs every
# test rather than replaying a cached pass.
# -timeout states Go's own default of ten minutes per test binary, so the CI
# job's timeout can be held above it: a hung binary then prints its stack before
# the job is killed, and the summary prints that stack.
#
# The suite is scripts/packages.sh's. It runs through `go test -json`, and
# internal/testsummary judges it: every package must report a result, and the
# summary names each package that ran no test and every skipped test with its
# reason, and lists the five packages go test timed longest. A test skipped
# through raceskip.Skip runs again without -race, and the run fails unless it
# passes; that rerun prints no such list. When TEST_DURATIONS names a file, the
# summary writes there the seconds of every package and of every test that
# reported a result in the run under -race, a relative name read from the
# repository root.
#
# Usage: [TEST_DURATIONS=FILE] scripts/test.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's toolchain, not the host's: the gate and CI must run one
# standard library (scripts/toolchain.sh).
. scripts/toolchain.sh

export CGO_ENABLED=1
export LC_ALL=C

work=$(mktemp -d)
trap 'rm -rf "${work}"' EXIT

list=$(scripts/packages.sh)
pkgs=()
while IFS= read -r pkg; do
	pkgs+=("${pkg}")
done <<<"${list}"

summary="${work}/testsummary$(go env GOEXE | tr -d '\r')"
go build -o "${summary}" ./internal/testsummary

status=0
set +e
go test -json -race -shuffle=on -count=1 -timeout=10m "${pkgs[@]}" 2>&1 |
	"${summary}" -race-skips="${work}/race-skips" -durations="${TEST_DURATIONS:-}" -slowest=5 "${pkgs[@]}"
codes=("${PIPESTATUS[@]}")
set -e
if [ "${codes[0]}" -ne 0 ] || [ "${codes[1]}" -ne 0 ]; then
	status=1
fi

# Each line of race-skips is a package, a tab, and its test names joined by commas.
if [ -s "${work}/race-skips" ]; then
	while IFS=$'\t' read -r -u 3 pkg names; do
		printf 'test: %s in %s, again without the race detector\n' "${names}" "${pkg}"
		set +e
		go test -json -shuffle=on -count=1 -timeout=10m -run "^(${names//,/|})\$" "${pkg}" 2>&1 |
			"${summary}" -require="${names}" "${pkg}"
		codes=("${PIPESTATUS[@]}")
		set -e
		if [ "${codes[0]}" -ne 0 ] || [ "${codes[1]}" -ne 0 ]; then
			status=1
		fi
	done 3<"${work}/race-skips"
fi

if [ "${status}" -ne 0 ]; then
	printf 'test: FAILED\n' >&2
fi
exit "${status}"
