#!/usr/bin/env bash
set -euo pipefail

fail=0
for d in . lsp; do
  echo "==> go test: $d"
  if ! (cd "$d" && go test ./...); then
    fail=1
  fi
done
exit $fail
