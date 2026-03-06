#!/usr/bin/env bash
set -euo pipefail

fail=0
for d in . lsp; do
  echo "==> golangci-lint: $d"
  if ! (cd "$d" && go tool golangci-lint run); then
    fail=1
  fi
done
exit $fail
