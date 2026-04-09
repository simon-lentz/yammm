# yammm Claude Code Plugin

Claude Code plugin for the yammm schema DSL, Go library, and CLI. Provides a holistic knowledge surface, schema authoring, quality review, and automatic validation hooks.

## Prerequisites (Optional)

All tools are optional. The plugin provides full knowledge and review capabilities without them.

| Tool | Enables | Install |
| ---- | ------- | ------- |
| `yammm` CLI | Schema compilation, formatting, export, auto-validation | `/yammm:setup` |
| `yammm-lsp` | Editor diagnostics, completions, hover, go-to-definition | `/yammm:setup` |
| `jq` | PostToolUse auto-validation hook | `/yammm:setup` |

## Installation

### Primary: install from GitHub (recommended)

Inside any Claude Code session:

```txt
/plugin marketplace add simon-lentz/yammm
/plugin install yammm@yammm
```

The first command registers this repo as a marketplace via GitHub — Claude Code clones the repo, reads `.claude-plugin/marketplace.json` from the repo root, and caches the plugin locally. The second command installs the `yammm` plugin from that marketplace. After installation, the plugin is enabled in `~/.claude/settings.json` as `yammm@yammm` and available in every Claude Code session.

To pin to a specific release rather than tracking the default branch, append `@<ref>` to the marketplace source:

```txt
/plugin marketplace add simon-lentz/yammm@v0.2.0
```

Refs can be branches, tags, or commit SHAs. Omit the ref to track the default branch.

Updates come via `/plugin marketplace update yammm` inside a Claude Code session, or automatically if you have plugin auto-updates enabled.

### Alternative: install from a local clone

If you already have a local clone of the repo — for example, when working on a fork or in an offline environment — you can add the marketplace from the local path instead:

```txt
/plugin marketplace add /absolute/path/to/yammm
/plugin install yammm@yammm
```

Same installation result, but Claude Code reads the marketplace and plugin directly from your local working tree rather than cloning from GitHub. Useful when you need to test against a non-default branch you've checked out locally, or when GitHub is unreachable from your environment.

### Dev iteration: per-session load without installing

For iterating on the plugin itself without installing, start Claude Code with `--plugin-dir` pointing at the plugin directory:

```bash
claude --plugin-dir /absolute/path/to/yammm/claude-plugin
```

This loads the plugin only for that session, additively alongside your installed plugins. Per the official docs, if a `--plugin-dir` plugin has the same name as an installed one, the local copy takes precedence for that session — useful for testing changes to an already-installed version. The flag is repeatable for loading multiple plugins at once.

### Reloading changes

After edits to skill, hook, or LSP files, run `/reload-plugins` to pick up changes without restarting Claude Code.

## Skills

### `yammm` (Primary)

Holistic knowledge surface covering the full yammm ecosystem. Auto-triggers on any yammm-related question or when working with `.yammm` files. Includes an orientation layer and 9 reference files for progressive depth.

### `/yammm:author-schema [description]`

Designs and writes `.yammm` schema files from requirements. Follows a 4-step process: understand, design, write, verify (compile-check). Also triggers on natural-language requests like "write me a schema" or "model this dataset."

### `/yammm:review-schema [path]`

Structured schema quality review. Compiles the schema, applies a 10-item review checklist, and produces an Errors/Warnings/Suggestions/Summary report. Also triggers on natural-language requests like "review my schema."

### `/yammm:setup [tool-name]`

Platform-aware toolchain installation guide. Detects your OS and currently installed tools, then provides download instructions for missing components.

## Hooks

### PostToolUse: Auto-validation

After any Edit or Write to a `.yammm` file, automatically runs `yammm validate` and feeds diagnostics back into the conversation. Requires `yammm` CLI and `jq`. Non-blocking (silently skips if tools are missing).

### SessionStart: Environment check

On session start, checks for `yammm`, `yammm-lsp`, and `jq`. Reports missing tools as warnings (never blocks) and directs to `/yammm:setup` for installation.

## Reference Files

| File | Topic |
| ---- | ----- |
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

**Skill not triggering**: Ensure the plugin is installed and enabled (`/plugin` to inspect, or check `enabledPlugins` in `~/.claude/settings.json` for `yammm@yammm`). The `yammm` skill should trigger on any question mentioning yammm, `.yammm` files, or yammm-related Go packages.

**PostToolUse hook not running**: Verify `yammm` and `jq` are installed (`command -v yammm && command -v jq`). The hook silently skips if either is missing.

**LSP not working**: Verify `yammm-lsp` is on PATH (`command -v yammm-lsp`). The `.lsp.json` configures `yammm-lsp --stdio` for `.yammm` files.

**Reload after changes**: Use `/reload-plugins` to pick up plugin modifications without restarting Claude Code.
