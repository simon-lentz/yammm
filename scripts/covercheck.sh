#!/usr/bin/env bash
# covercheck.sh — statement-coverage floor for a package subtree.
#
# Records a package subtree's total statement coverage, then verifies a
# later measurement has not regressed. The total is `go tool cover -func`'s
# "total:" line over an atomic-mode profile.
#
# Usage:
#   scripts/covercheck.sh before ./adapter/neo4j   # record the baseline
#   ... make test changes ...
#   scripts/covercheck.sh after  ./adapter/neo4j   # compare; exit 1 on drop
#
# Profiles and totals are stored under $COVERCHECK_DIR (default
# /tmp/covercheck). An intended drop (deleting a genuine duplicate whose
# coverage another package's tests provide) must be justified in the PR
# description — this script only detects, it does not approve.
set -euo pipefail

usage() {
  echo "usage: $0 before|after <pkg>   (e.g. $0 before ./adapter/neo4j)" >&2
  exit 2
}

mode="${1:-}"
pkg="${2:-}"
[[ "$mode" == "before" || "$mode" == "after" ]] || usage
[[ -n "$pkg" ]] || usage

# Accept exactly the documented input shape: a ./-prefixed package directory
# without a /... suffix (the script appends /... itself). Anything else is
# rejected here rather than producing a confusing go-test pattern error or —
# worse — silently measuring the wrong package set ('./x/.../...' matches
# only subpackages, not ./x itself).
case "$pkg" in
*/...) echo "covercheck: pass a package directory without '/...' (got $pkg)" >&2; usage ;;
./*) ;;
*) echo "covercheck: package must be ./-prefixed (got $pkg)" >&2; usage ;;
esac

# One '_' per path segment separator and a distinct '-' for '.', so distinct
# subtree arguments cannot collide on one baseline filename (tr's set-to-set
# mapping would fold './a/b' and './a_b' together).
slug="${pkg#./}"
slug="${slug//\//_}"
slug="${slug//./-}"
dir="${COVERCHECK_DIR:-/tmp/covercheck}"
mkdir -p "$dir"
profile="$dir/$slug.$mode.out"

go test "${pkg%/}/..." -covermode=atomic -coverprofile="$profile" >/dev/null

total="$(go tool cover -func="$profile" | awk '/^total:/ {print $3}')"
[[ -n "$total" ]] || { echo "covercheck: no total in $profile" >&2; exit 2; }
printf '%s\n' "$total" >"$dir/$slug.$mode.total"
echo "covercheck[$mode] $pkg total=$total"

if [[ "$mode" == "after" ]]; then
  before_file="$dir/$slug.before.total"
  if [[ ! -f "$before_file" ]]; then
    echo "covercheck: no baseline for $pkg — run '$0 before $pkg' first" >&2
    exit 2
  fi
  before="$(cat "$before_file")"
  if awk -v a="${total%\%}" -v b="${before%\%}" 'BEGIN { exit (a + 0 >= b + 0) ? 0 : 1 }'; then
    echo "covercheck: OK (before=$before after=$total)"
  else
    echo "covercheck: REGRESSION (before=$before after=$total)" >&2
    exit 1
  fi
fi
