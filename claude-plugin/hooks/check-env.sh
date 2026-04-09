#!/usr/bin/env bash
set -euo pipefail

# All checks are warnings — no tool blocks the session.
# The plugin provides substantial value (full knowledge surface, read-only
# review, schema authoring guidance) without any of these tools installed.
missing=""
warn=""

command -v yammm     >/dev/null 2>&1 || missing="$missing yammm"
command -v yammm-lsp >/dev/null 2>&1 || warn="$warn yammm-lsp"
command -v jq        >/dev/null 2>&1 || warn="$warn jq"

if [ -n "$missing" ]; then
  echo "yammm CLI not found. Schema compilation, formatting, and export"
  echo "commands will not work. Knowledge, review, and schema authoring"
  echo "are still available."
fi

if [ -n "$warn" ]; then
  echo "Optional tools not found:$warn"
  command -v yammm-lsp >/dev/null 2>&1 || echo "  yammm-lsp: LSP diagnostics in your editor"
  command -v jq        >/dev/null 2>&1 || echo "  jq: automatic schema validation on write"
fi

if [ -n "$missing" ] || [ -n "$warn" ]; then
  echo ""
  echo "Run /yammm:setup for installation instructions."
fi
