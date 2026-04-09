#!/usr/bin/env bash
# validate-yammm.sh — PostToolUse hook for yammm schema validation.
#
# Invoked by Claude Code for every Write/Edit tool call. Reads the tool
# input as JSON on stdin, extracts the file path, and — if it is a
# `.yammm` file — runs `yammm validate` on it. When validation reports
# errors, emits a JSON control object with hookSpecificOutput.additionalContext
# so the errors are surfaced to Claude as context on the tool result.
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
# Silent success (clean schema, non-.yammm file, yammm not installed,
# validate-failed-with-no-output) emits no stdout, which the hook runner
# treats as "no context to inject."
#
# Independently testable without loading the plugin:
#
#     # Valid schema, silent success expected:
#     echo '{"tool_input":{"file_path":"/tmp/clean.yammm"}}' | ./validate-yammm.sh
#
#     # Invalid schema, JSON control object expected:
#     echo '{"tool_input":{"file_path":"/tmp/broken.yammm"}}' | ./validate-yammm.sh | jq .
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

# Run yammm validate and capture combined output (stdout + stderr).
# The `if cmd; then` idiom does not trip errexit on non-zero exit, so a
# failed validation falls through to the error-handling branch without
# terminating the script. Variable assignment in the command substitution
# captures the output regardless of exit status.
if validate_output=$(yammm validate "$file_path" 2>&1); then
  # Exit 0 — clean schema (or warnings-only, which yammm considers
  # non-fatal) — nothing to inject.
  exit 0
fi

# yammm validate exited non-zero. If it produced no output (unusual but
# possible — e.g., yammm crashes before emitting diagnostics), there's
# nothing useful to tell Claude, so exit silently rather than injecting
# an empty additionalContext.
if [ -z "$validate_output" ]; then
  exit 0
fi

# Emit JSON control object with the validation errors as additionalContext.
# jq -cn constructs a fresh compact JSON document; --arg safely embeds the
# captured output, handling newlines, quotes, backslashes, and all other
# special characters that would break manual JSON string construction.
jq -cn --arg ctx "$validate_output" '{
  hookSpecificOutput: {
    hookEventName: "PostToolUse",
    additionalContext: $ctx
  }
}'
