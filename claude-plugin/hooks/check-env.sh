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

# --- Version staleness (warn-only) ------------------------------------------
# The plugin version tracks the library version in lockstep, so a binary whose
# reported version differs from plugin.json is stale on one side or the other.
# Both binaries fall back to their Go build-info version, so plain
# `go install module/cmd/...@tag` builds self-report the tag; only a non-VCS
# source build still says "dev".

# version_lt compares release triples numerically; pre-release and
# pseudo-version suffixes are stripped by the caller.
version_lt() {
  awk -v a="$1" -v b="$2" 'BEGIN {
    split(a, x, "."); split(b, y, ".")
    for (i = 1; i <= 3; i++) {
      if (x[i]+0 < y[i]+0) exit 0
      if (x[i]+0 > y[i]+0) exit 1
    }
    exit 1
  }'
}

# check_binary_version prints a warning line when the binary's reported version
# and the plugin's disagree. The direction matters because the remediations
# differ: a stale binary rejects grammar the plugin documents (reinstall it); a
# stale plugin documents less than the binary accepts (update the plugin).
check_binary_version() {
  bin="$1"
  install_path="$2"
  command -v "$bin" >/dev/null 2>&1 || return 0
  reported="$("$bin" --version 2>/dev/null | head -1 | awk '{print $NF}')" || return 0
  reported="${reported#v}"
  [ -n "$reported" ] || return 0
  if [ "$reported" = "dev" ]; then
    echo "  $bin reports version 'dev' (non-VCS source build); cannot verify it against plugin v$plugin_version"
    return 0
  fi
  rep_cmp="${reported%%-*}"
  plug_cmp="${plugin_version%%-*}"
  [ "$rep_cmp" = "$plug_cmp" ] && return 0
  if version_lt "$rep_cmp" "$plug_cmp"; then
    echo "  $bin v$reported is OLDER than this plugin (v$plugin_version); newer grammar features may be rejected as syntax errors."
    echo "    Update: go install github.com/simon-lentz/yammm/$install_path@v$plugin_version"
  else
    echo "  $bin v$reported is NEWER than this plugin (v$plugin_version); update the installed yammm plugin to match."
  fi
}

plugin_root="${CLAUDE_PLUGIN_ROOT:-$(cd "$(dirname "$0")/.." && pwd)}"
manifest="$plugin_root/.claude-plugin/plugin.json"

if command -v jq >/dev/null 2>&1 && [ -f "$manifest" ]; then
  plugin_version="$(jq -r '.version // empty' "$manifest")"
  if [ -n "$plugin_version" ]; then
    version_notes="$(
      check_binary_version yammm cmd/yammm
      check_binary_version yammm-lsp cmd/yammm-lsp
    )"
    if [ -n "$version_notes" ]; then
      echo ""
      echo "yammm version check (plugin v$plugin_version):"
      echo "$version_notes"
    fi
  fi
fi
