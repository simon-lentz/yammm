# yammm Claude Code Plugin

Claude Code plugin for the yammm schema DSL, Go library, and CLI. Provides a holistic knowledge surface, schema authoring, quality review, and automatic validation hooks.

## Prerequisites (Optional)

All tools are optional. The plugin provides full knowledge and review capabilities without them.

| Tool | Enables | Install |
|------|---------|---------|
| `yammm` CLI | Schema compilation, formatting, export, auto-validation | `/setup` |
| `yammm-lsp` | Editor diagnostics, completions, hover, go-to-definition | `/setup` |
| `jq` | PostToolUse auto-validation hook | `/setup` |

## Installation

```bash
claude plugin add ./claude-plugin
```

Or load for a single session:

```bash
claude --plugin-dir ./claude-plugin
```

## Skills

### `yammm` (Primary)

Holistic knowledge surface covering the full yammm ecosystem. Auto-triggers on any yammm-related question or when working with `.yammm` files. Includes an orientation layer and 9 reference files for progressive depth.

### `/author-schema [description]`

Designs and writes `.yammm` schema files from requirements. Follows a 4-step process: understand, design, write, verify (compile-check). Also triggers on natural-language requests like "write me a schema" or "model this dataset."

### `/review-schema [path]`

Structured schema quality review. Compiles the schema, applies a 10-item review checklist, and produces an Errors/Warnings/Suggestions/Summary report. Also triggers on natural-language requests like "review my schema."

### `/setup [tool-name]`

Platform-aware toolchain installation guide. Detects your OS and currently installed tools, then provides download instructions for missing components.

## Hooks

### PostToolUse: Auto-validation

After any Edit or Write to a `.yammm` file, automatically runs `yammm validate` and feeds diagnostics back into the conversation. Requires `yammm` CLI and `jq`. Non-blocking (silently skips if tools are missing).

### SessionStart: Environment check

On session start, checks for `yammm`, `yammm-lsp`, and `jq`. Reports missing tools as warnings (never blocks) and directs to `/setup` for installation.

## Reference Files

| File | Topic |
|------|-------|
| `dsl-syntax.md` | Full grammar: types, properties, relationships, imports |
| `expressions.md` | Operators, pipeline, lambdas, built-in functions |
| `type-system.md` | Constraint types, aliases, abstract/part, inheritance |
| `patterns.md` | Common modeling patterns with examples |
| `api-pipeline.md` | Go API: load, validate, graph, snapshot |
| `graph-traversal.md` | graph/walk API: Visitor, callbacks, ordering |
| `adapters.md` | JSON/CSV/Neo4j adapter usage |
| `diagnostics.md` | Error codes and troubleshooting |
| `cli.md` | CLI commands and workflows |

## Troubleshooting

**Skill not triggering**: Ensure the plugin is loaded (`claude --plugin-dir ./claude-plugin`). The `yammm` skill should trigger on any question mentioning yammm, `.yammm` files, or yammm-related Go packages.

**PostToolUse hook not running**: Verify `yammm` and `jq` are installed (`command -v yammm && command -v jq`). The hook silently skips if either is missing.

**LSP not working**: Verify `yammm-lsp` is on PATH (`command -v yammm-lsp`). The `.lsp.json` configures `yammm-lsp --stdio` for `.yammm` files.

**Reload after changes**: Use `/reload-plugins` to pick up plugin modifications without restarting Claude Code.
