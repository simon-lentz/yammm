---
name: setup-lsp
description: Check yammm-lsp installation status and provide platform-specific download instructions
---

# setup-lsp

Help the user install the `yammm-lsp` binary for Claude Code plugin LSP features.

## Steps

1. **Check if already installed.** Run `command -v yammm-lsp` and `yammm-lsp --version` via Bash. If the binary is found and runs, report the version and exit — no action needed.

2. **Detect platform and architecture.** Run `uname -s` and `uname -m` via Bash to determine OS and arch. Map to release archive names:
   - `Darwin` + `arm64` → `darwin-arm64`
   - `Darwin` + `x86_64` → `darwin-amd64`
   - `Linux` + `x86_64` → `linux-amd64`
   - `Linux` + `aarch64` → `linux-arm64`
   - `MINGW*`/`MSYS*` + `x86_64` → `windows-amd64`
   - `MINGW*`/`MSYS*` + `aarch64` → `windows-arm64`

3. **Provide download instructions.** Print the exact commands the user should run to download, extract, and install. Use the latest release URL pattern:

   For Unix (macOS/Linux):
   ```
   curl -fsSL https://github.com/simon-lentz/yammm-lsp/releases/latest/download/yammm-lsp-<version>-<platform>.tar.gz -o yammm-lsp.tar.gz
   tar xzf yammm-lsp.tar.gz
   sudo mv yammm-lsp /usr/local/bin/
   rm yammm-lsp.tar.gz
   ```

   For Windows:
   ```
   Download from https://github.com/simon-lentz/yammm-lsp/releases/latest
   Extract yammm-lsp.exe and add its directory to PATH.
   ```

   Note: To determine the exact `<version>` tag for the download URL, check the latest release page at https://github.com/simon-lentz/yammm-lsp/releases/latest. The archive naming pattern is `yammm-lsp-<version>-<platform>.tar.gz` (e.g., `yammm-lsp-v0.1.4-darwin-arm64.tar.gz`).

4. **Do NOT run the download or installation commands yourself.** Print them for the user to review and run. Installation requires user confirmation since it modifies system PATH locations.
