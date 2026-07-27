# YAMMM for Visual Studio Code

Language support for [YAMMM](https://github.com/simon-lentz/yammm) schema files (`.yammm`) — a compact DSL for defining typed data models with relationships, constraints, and invariants, backed by the `yammm` Go library and CLI.

## Features

- **Syntax highlighting** for `.yammm` files, including fenced ` ```yammm ` code blocks in markdown and `@` / `@@` annotations.
- **Diagnostics** as you type — parse errors and schema-rule violations with stable `E_*` codes and precise source locations.
- **Completions** for keywords, types, schema members, and annotations — annotation names and their arguments, offered per placement (`@` on a property, `@@` in a type body).
- **Hover** documentation for types, properties, and annotations.
- **Go to definition** for type references and imports.
- **Document outline** (symbols) for navigating larger schemas.
- **Formatting** in the canonical `yammm fmt` style.

The language server also analyzes fenced yammm blocks inside markdown documents.

## Requirements

- Visual Studio Code **1.91** or later.
- The `yammm-lsp` language server. The extension ships platform binaries (macOS arm64/x64, Linux x64/arm64, Windows x64/arm64), so no setup is needed. To use a different build, set `yammm.lsp.serverPath` or put `yammm-lsp` on your `PATH`.

## Extension Settings

| Setting | Default | Description |
| --- | --- | --- |
| `yammm.lsp.serverPath` | `""` | Path to the `yammm-lsp` binary. If empty, uses the bundled binary. |
| `yammm.lsp.logFile` | `""` | Path to a log file for the language server. If empty, logs to stderr. Useful for debugging. |
| `yammm.lsp.logLevel` | `"info"` | Log level for the language server: `error`, `warn`, `info`, `debug`, or `trace`. |
| `yammm.lsp.moduleRoot` | `""` | Override the module root for import resolution. If empty, uses the workspace root. |
| `yammm.trace.server` | `"off"` | Trace communication between VS Code and the YAMMM language server: `off`, `messages`, or `verbose`. |

Changing any of these prompts to restart the language server.

## Notes

- Graph snapshot files (`*.ys`) are associated with JSON for convenient inspection.
- The **YAMMM** output channel carries both extension and server logs; use its log-level dropdown to filter. To capture protocol traffic, set the channel's log level to *Trace* and `yammm.trace.server` to `messages` or `verbose`.
- Schema language reference: [docs/SPEC.md](https://github.com/simon-lentz/yammm/blob/main/docs/SPEC.md).

## License

[MIT](https://github.com/simon-lentz/yammm/blob/main/LICENSE)
