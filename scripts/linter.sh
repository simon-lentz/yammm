#!/usr/bin/env bash
# linter.sh — set `linter` to a build of the module's pinned golangci-lint that
# this run can execute. SOURCE it after scripts/toolchain.sh, from the
# repository root.
#
# YAMMM_GOLANGCI_LINT names a build to take, a relative path read from the
# repository root; whatever executable it names is run as the pinned linter.
# CI's check job builds the linter once and hands it to its config check.
# internal/scripttest takes the build CI's test job hands it, or else builds
# one, once a test process, and hands it to the fixtures that run it. With none named, the linter is built here, for this
# host, into a directory of this run's own that the EXIT trap removes. A path
# go keeps in a shared cache, as `go tool -n` prints, is executed while another
# run may still be writing it, which Linux refuses with "Text file busy".
#
# The build runs under no GOEXPERIMENT. An experiment is a setting of the build
# the linter reads, which the linter takes from the environment when it runs;
# built under one, it would be another program. An empty GOEXPERIMENT clears
# the caller's, and go then reads its env file, so a run whose go env file sets
# one is refused, as internal/mutants refuses it.
#
# The run stops with exit 2, before it lints, when it builds the linter and the
# go env file sets GOEXPERIMENT or go cannot build it, and when the named path
# holds no executable.
#
# Usage: . scripts/linter.sh          (sets the EXIT trap when it builds)

if [ -n "${YAMMM_GOLANGCI_LINT:-}" ]; then
	linter=${YAMMM_GOLANGCI_LINT}
else
	experiment=$(GOEXPERIMENT='' go env GOEXPERIMENT | tr -d '\r')
	if [ -n "${experiment}" ]; then
		printf 'linter: the go env file sets GOEXPERIMENT=%s, which would reach the linter'"'"'s build; run go env -u GOEXPERIMENT\n' "${experiment}" >&2
		exit 2
	fi
	linter_dir=$(mktemp -d)
	trap 'rm -rf "${linter_dir}"' EXIT
	linter="${linter_dir}/golangci-lint$(GOOS="$(go env GOHOSTOS | tr -d '\r')" go env GOEXE | tr -d '\r')"
	if ! GOEXPERIMENT='' GOOS="$(go env GOHOSTOS | tr -d '\r')" GOARCH="$(go env GOHOSTARCH | tr -d '\r')" \
		go build -o "${linter}" github.com/golangci/golangci-lint/v2/cmd/golangci-lint; then
		printf 'linter: go cannot build the pinned golangci-lint\n' >&2
		exit 2
	fi
fi
if [ ! -f "${linter}" ] || [ ! -x "${linter}" ]; then
	printf 'linter: %s holds no linter this run can execute\n' "${linter}" >&2
	exit 2
fi
