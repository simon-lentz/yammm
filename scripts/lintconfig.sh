#!/usr/bin/env bash
# lintconfig.sh — check .golangci.yml against the pinned linter's own schema.
#
# The pre-commit hook, every CI host's job and, through them, the release run
# this, so a config key the schema refuses fails locally as it fails in CI. The
# schema comes from the module cache rather than golangci-lint.run: a fetch there
# failed CI on transient network errors, and the module already holds the schema
# its source builds against. `next` is the file a source build of the linter
# asks for, and the newest versioned schema lags the module's own version.
#
# Usage: scripts/lintconfig.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

# The module's own toolchain (scripts/toolchain.sh) resolves the linter's
# module and its schema, as CI's does.
. scripts/toolchain.sh

schema="$(go list -m -f '{{.Dir}}' github.com/golangci/golangci-lint/v2)/jsonschema/golangci.next.jsonschema.json"
if [ ! -f "${schema}" ]; then
	printf 'lintconfig: schema not found at %s\n' "${schema}" >&2
	exit 1
fi
go tool golangci-lint config verify --config=.golangci.yml --schema "${schema}"
