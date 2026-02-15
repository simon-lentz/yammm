# YAMMM Language Support for VS Code

This extension provides language support for YAMMM (Yet Another Meta-Meta Model) schema files.

## Features

- **Syntax Highlighting**: Full TextMate grammar for `.yammm` files
- **IntelliSense**: Completions for keywords, types, and snippets
- **Diagnostics**: Real-time error checking as you type
- **Go to Definition**: Navigate to type definitions
- **Hover Information**: View type details and documentation
- **Document Symbols**: Outline view and breadcrumbs
- **Formatting**: Automatic code formatting
- **Snippets**: Quick templates for common patterns

## Requirements

The extension requires the `yammm-lsp` language server binary. You can either:

1. **Use bundled binary**: The extension includes pre-built binaries for common platforms
2. **Configure custom path**: Set `yammm.lsp.serverPath` in settings

## Extension Settings

- `yammm.lsp.serverPath`: Path to the yammm-lsp binary (optional)
- `yammm.lsp.logLevel`: Log level for the language server (`error`, `warn`, `info`, `debug`, `trace`)
- `yammm.lsp.moduleRoot`: Override the module root for import resolution
- `yammm.trace.server`: Trace communication between VS Code and the server

## Supported Platforms

Pre-built binaries are included for:

- macOS (Apple Silicon / arm64)
- macOS (Intel / amd64)
- Linux (amd64)
- Linux (arm64)
- Windows (amd64)
- Windows (arm64)

## Snippets

| Prefix | Description |
| ------ | ----------- |
| `schema` | Schema declaration |
| `import` | Import statement |
| `type` | Type declaration |
| `abstract` | Abstract type |
| `part` | Part type |
| `propstring` | String property |
| `propint` | Integer property |
| `assocone` | One-to-one association |
| `assocmany` | One-to-many association |
| `compone` | Composition (one) |
| `compmany` | Composition (many) |
| `invariant` | Invariant constraint |

## Development

```bash
# Install dependencies
npm install

# Compile TypeScript
npm run compile

# Package extension
npm run package
```

## License

MIT
