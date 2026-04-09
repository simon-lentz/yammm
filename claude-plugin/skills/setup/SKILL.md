---
name: setup
description: >-
  Install and configure yammm toolchain (CLI, LSP, jq).
  Use when setting up yammm for the first time or when
  a session warning reports missing tools.
disable-model-invocation: true
user-invocable: true
allowed-tools: Bash
argument-hint: "[tool-name]"
---

# yammm Toolchain Setup

## Current Environment

```!
uname -s
uname -m
command -v yammm && yammm --version 2>&1 || echo "yammm: not installed"
command -v yammm-lsp && yammm-lsp --version 2>&1 || echo "yammm-lsp: not installed"
command -v jq && jq --version 2>&1 || echo "jq: not installed"
```

Based on the detected platform and installed tools above, provide installation instructions for any missing tools. Skip tools that are already installed.

If `$ARGUMENTS` specifies a tool name, provide instructions for that tool only.

---

## yammm CLI

The `yammm` CLI provides schema validation, formatting, data checking, snapshot persistence, and export.

### Option 1: Go install (if Go is available)

```bash
go install github.com/simon-lentz/yammm/cmd/yammm@latest
```

### Option 2: Pre-built binary

Download the archive for your platform from the latest GitHub release. The naming pattern is `yammm-<version>-<platform>.tar.gz`.

Platforms: `darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`, `windows-amd64`, `windows-arm64`.

```bash
# Example for macOS ARM:
curl -fsSL https://github.com/simon-lentz/yammm/releases/latest/download/yammm-<version>-darwin-arm64.tar.gz -o yammm.tar.gz
tar xzf yammm.tar.gz
sudo mv yammm /usr/local/bin/
rm yammm.tar.gz
```

### Verify

```bash
yammm version
```

---

## yammm-lsp

The LSP server provides diagnostics, completions, hover, and go-to-definition for `.yammm` files.

### Option 1: Go install

```bash
go install github.com/simon-lentz/yammm/cmd/yammm-lsp@latest
```

### Option 2: Pre-built binary

Archive naming: `yammm-lsp-<version>-<platform>.tar.gz`. Same platform matrix as the CLI.

```bash
# Example for macOS ARM:
curl -fsSL https://github.com/simon-lentz/yammm/releases/latest/download/yammm-lsp-<version>-darwin-arm64.tar.gz -o yammm-lsp.tar.gz
tar xzf yammm-lsp.tar.gz
sudo mv yammm-lsp /usr/local/bin/
rm yammm-lsp.tar.gz
```

### Verify

```bash
yammm-lsp --version
```

---

## jq

Required for the automatic schema validation hook (PostToolUse). Without it, the hook silently skips validation.

| Platform | Command |
|----------|---------|
| macOS | `brew install jq` |
| Debian/Ubuntu | `sudo apt-get install jq` |
| Windows | `winget install jqlang.jq` |

---

## What Each Tool Enables

| Tool | Plugin features unlocked |
|------|--------------------------|
| `yammm` | Schema compilation, formatting, export, PostToolUse auto-validation, `Bash(yammm *)` in skills/agents |
| `yammm-lsp` | Editor diagnostics, completions, hover, go-to-definition |
| `jq` | PostToolUse auto-validation hook (parses stdin JSON to extract file paths) |

---

## Safety

Print installation commands for the user to review and run. Do NOT execute `sudo`, `mv` to system directories, or any command that modifies PATH without user confirmation.
