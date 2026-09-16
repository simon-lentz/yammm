#!/usr/bin/env bash
# test.sh — run the module's test suite: the one test definition every gate uses.
#
# The pre-commit hook, each CI host's job and `make test` call this script, and
# the release workflow runs those host jobs before it builds anything, so a suite
# that passes locally ran under the flags CI runs it with. -race needs cgo, so
# CGO_ENABLED is set rather than inherited: a host with no C compiler fails here
# instead of testing without the detector. -shuffle=on surfaces order coupling
# between tests, and -count=1 runs every test rather than replaying a cached pass.
#
# The suite is scripts/packages.sh's. It runs through `go test -json`, and
# internal/testsummary judges it: every package must report a result, and the
# summary names each package that ran no test and every skipped test with its
# reason. A test skipped through raceskip.Skip runs again without -race, and the
# run fails unless it passes.
#
# Usage: scripts/test.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

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
go test -json -race -shuffle=on -count=1 "${pkgs[@]}" 2>&1 |
	"${summary}" -race-skips="${work}/race-skips" "${pkgs[@]}"
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
		go test -json -shuffle=on -count=1 -run "^(${names//,/|})\$" "${pkg}" 2>&1 |
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
