#!/usr/bin/env bash
# stagedtree.sh — refuse a working tree that is not the index.
#
# `make gate` runs this before the full gate. With --all-files pre-commit does
# not stash, so every hook reads the index's file list and the working tree's
# bodies. A green full gate is a statement about the tree a commit will hold
# only when the two are one, so a tracked file the working tree changes,
# deletes or re-modes against the index, and an untracked file git does not
# ignore, are each named and the run stops with exit 1. A commit does not run
# this: pre-commit stashes the unstaged changes to tracked files, and
# scripts/packages.sh refuses an untracked Go file in a tracked package, but an
# untracked file of another kind stays, and a test that walks the disk reads it.
# The check is git's own comparison: a file git is told not to compare, as one
# marked assume-unchanged or skip-worktree, is not named.
#
# Usage: scripts/stagedtree.sh
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
export LC_ALL=C

work=$(mktemp -d)
trap 'rm -rf "${work}"' EXIT
git diff --name-only --no-renames -z >"${work}/changed"
git ls-files -z --others --exclude-standard >"${work}/untracked"
if [ ! -s "${work}/changed" ] && [ ! -s "${work}/untracked" ]; then
	exit 0
fi
# count reads NUL-separated names, so a name that holds a newline is one.
count() { tr -cd '\0' <"$1" | wc -c | tr -d ' '; }
if [ -s "${work}/changed" ]; then
	printf 'stagedtree: the working tree differs from the index in %d tracked file(s):\n' "$(count "${work}/changed")" >&2
	tr '\0' '\n' <"${work}/changed" | sed 's/^/  /' >&2
fi
if [ -s "${work}/untracked" ]; then
	printf 'stagedtree: %d untracked file(s) git does not ignore:\n' "$(count "${work}/untracked")" >&2
	tr '\0' '\n' <"${work}/untracked" | sed 's/^/  /' >&2
fi
printf 'stagedtree: the full gate judges the index; stage each file, or stash it with git stash --include-untracked\n' >&2
exit 1
