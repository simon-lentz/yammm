#!/usr/bin/env bash
# validate-yammm.sh — PostToolUse hook for yammm schema validation.
#
# Invoked by Claude Code for every Write/Edit tool call. Reads the tool
# input as JSON on stdin, extracts the file path, and — if it is a
# `.yammm` file — runs `yammm validate` on it. When validation reports
# anything at all, emits a JSON control object with
# hookSpecificOutput.additionalContext so the diagnostics are surfaced to
# Claude as context on the tool result.
#
# Injection is gated on OUTPUT, not on exit status. `yammm validate` exits 0
# for a warnings-only schema while still printing the warnings to stderr, and
# some of those warnings report a silent regression the author needs to see —
# W_ANNOTATION_SHADOWED, for one, is the only signal that a subtype's property
# re-declaration dropped an inherited @writeOnce or @index annotation. Gating
# on exit status would discard exactly those.
#
# Per the Claude Code hooks reference (code.claude.com/docs/en/hooks),
# plain-text stdout from a PostToolUse command hook is written to the
# debug log only and is NOT visible to Claude. To inject context, the
# hook must emit JSON with the documented shape:
#
#     { "hookSpecificOutput": {
#         "hookEventName": "PostToolUse",
#         "additionalContext": "..."
#       } }
#
# This script exits 0 on every path so it never blocks the tool call.
# Silent success (diagnostic-free schema, non-.yammm file, yammm not installed,
# validate-failed-with-no-output) emits no stdout, which the hook runner
# treats as "no context to inject."
#
# Independently testable without loading the plugin:
#
#     # Diagnostic-free schema, silent success expected:
#     echo '{"tool_input":{"file_path":"/tmp/clean.yammm"}}' | ./validate-yammm.sh
#
#     # Invalid schema, JSON control object expected:
#     echo '{"tool_input":{"file_path":"/tmp/broken.yammm"}}' | ./validate-yammm.sh | jq .
#
#     # Warnings-only schema (exits 0), JSON control object still expected:
#     echo '{"tool_input":{"file_path":"/tmp/warns.yammm"}}' | ./validate-yammm.sh | jq .
#
#     # Non-yammm file, silent exit expected:
#     echo '{"tool_input":{"file_path":"/tmp/foo.go"}}' | ./validate-yammm.sh
#
#     # Missing file_path, silent exit expected:
#     echo '{"tool_input":{}}' | ./validate-yammm.sh
#
#     # yammm not on PATH, silent exit expected:
#     PATH=/usr/bin:/bin echo '{"tool_input":{"file_path":"/tmp/foo.yammm"}}' | ./validate-yammm.sh

set -euo pipefail

# Bail silently if jq is not installed. The hook depends on jq for parsing
# stdin JSON, so we must check this before the first jq call. Without this
# guard, `set -e` would cause the script to fail with a "jq: command not
# found" error rather than silently skipping, which would contradict the
# README's "silently skips if either is missing" documentation and violate
# this script's stated "exits 0 on every path" contract. SessionStart's
# check-env.sh is responsible for warning the user about missing jq.
if ! command -v jq >/dev/null 2>&1; then
  exit 0
fi

# Read tool input JSON from stdin. Claude Code passes the full tool
# invocation payload here; we only need tool_input.file_path.
tool_input=$(cat)
file_path=$(printf '%s' "$tool_input" | jq -r '.tool_input.file_path // empty')

# Bail silently if file_path is missing or not a .yammm file.
# The matcher in hooks.json is "Write|Edit" (regex on tool name), which
# matches every Write or Edit — including edits to .go, .md, .json, etc.
# Path-based filtering must happen here because the matcher field does
# not filter by file extension.
case "$file_path" in
  *.yammm) ;;
  *)       exit 0 ;;
esac

# Bail silently if yammm CLI is not installed. SessionStart's check-env.sh
# is responsible for warning the user about this; we do not duplicate
# that warning here on every Write/Edit.
if ! command -v yammm >/dev/null 2>&1; then
  exit 0
fi

# The VCS root stands in for the module root so module-style imports resolve;
# outside a checkout, validate's own default (the schema's directory) applies.
module_root_args=()
if module_root=$(git -C "$(dirname "$file_path")" rev-parse --show-toplevel 2>/dev/null) && [ -n "$module_root" ]; then
  module_root_args=(--module-root "$module_root")
fi

# Run yammm validate and capture combined output (stdout + stderr).
# Diagnostics go to stderr at every severity, so both streams are captured.
#
# `|| true` keeps errexit from terminating the script on a non-zero exit; the
# status is deliberately discarded, because what decides whether there is
# something to report is whether validate SAID anything, not how it exited.
validate_output=$(yammm validate "${module_root_args[@]}" "$file_path" 2>&1) || true

# Nothing printed — either the schema is diagnostic-free, or validate failed
# before emitting anything (unusual, but possible). Either way there is nothing
# useful to tell Claude, so exit silently rather than injecting empty context.
if [ -z "$validate_output" ]; then
  exit 0
fi

# Emit JSON control object with the validation diagnostics as additionalContext.
# jq -cn constructs a fresh compact JSON document; --arg safely embeds the
# captured output, handling newlines, quotes, backslashes, and all other
# special characters that would break manual JSON string construction.
jq -cn --arg ctx "$validate_output" '{
  hookSpecificOutput: {
    hookEventName: "PostToolUse",
    additionalContext: $ctx
  }
}'
