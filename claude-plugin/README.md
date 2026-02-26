# yammm - Claude Code Plugin

Yammm DSL schema language support for Claude Code. Provides LSP intelligence, DSL knowledge, schema authoring and review agents, and utility commands.

## Prerequisites

- **Claude Code** CLI
- **VS Code** (strongly recommended) — the `yammm-lsp` is optimized for and designed with VS Code in mind

## Installation

### 1. Install the yammm-lsp language server

**Recommended: VS Code Marketplace extension**

The yammm-lsp binary is bundled with the VS Code extension. Install it from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=simon-lentz.yammm).

**Alternative: Build the standalone binary**

If you need the LSP binary outside of VS Code, clone the repo and build it:

```bash
git clone https://github.com/simon-lentz/yammm.git
cd yammm
make build-lsp
```

Verify the binary is in your PATH:

```bash
yammm-lsp --version
```

**Local extension development**

To develop the VS Code extension or yammm-lsp locally, follow the [LSP Quickstart](https://github.com/simon-lentz/yammm#lsp-quickstart) in the yammm README.

### 2. Install the plugin

```bash
claude plugin add /path/to/yammm/claude-plugin
```

## Features

### LSP Intelligence

The plugin integrates the `yammm-lsp` language server, giving Claude native support for:

- **Diagnostics** -- real-time parse and semantic error reporting
- **Hover** -- type and property details, constraints, documentation
- **Completion** -- context-aware suggestions for keywords, types, properties, imports
- **Go-to-definition** -- navigate to type definitions (local and imported)
- **Document symbols** -- hierarchical outline of schemas, types, and properties
- **Formatting** -- canonical yammm style formatting

**Supported files:** `.yammm` files and markdown files (`.md`, `.markdown`) containing fenced yammm code blocks.

### Schema Authoring Agent

Helps design and write new `.yammm` schemas from scratch. Understands data modeling, relationships, constraints, and invariants. Triggers when you ask to create or design a schema.

### Schema Review Agent

Analyzes existing `.yammm` files against a 10-item review checklist covering syntax, primary keys, field modifiers, constraint bounds, multiplicity, invariants, part types, imports, inheritance, and common anti-patterns. Triggers when you ask to review or validate a schema.

### Commands

| Command | Description |
| ------- | ----------- |
| `/new-schema` | Scaffold a new `.yammm` file with boilerplate type definitions |

### DSL Knowledge

The plugin includes a comprehensive yammm DSL skill with:

- Core syntax reference (schema structure, properties, relationships, invariants, imports, inheritance)
- Expression language reference (operators, pipeline, lambdas, all built-in functions)
- Type system reference (constraint types, bounds, aliases, abstract/part types, inheritance narrowing)
- Common schema patterns (audit fields, soft delete, normalization, relationship idioms)

### Settings

The plugin supports per-project configuration via `.claude/yammm.local.md`. Copy the template to get started:

```bash
cp /path/to/yammm/claude-plugin/settings-template.local.md .claude/yammm.local.md
```

| Setting | Type | Default | Description |
| ------- | ---- | ------- | ----------- |
| `default_model` | `sonnet` \| `haiku` | `sonnet` | Model used by schema-author and schema-reviewer agents |
| `auto_review` | `bool` | `false` | Automatically run schema-reviewer after schema-author writes a file |
| `scaffold_audit_fields` | `bool` | `false` | Include an `Auditable` abstract type stub when `/new-schema` scaffolds a file |

## Usage Examples

**Create a new schema:**
> "Create a yammm schema for a library system with books, authors, and borrowing records"

**Review an existing schema:**
> "Review my schema.yammm file for issues"

**Scaffold and iterate:**
> `/new-schema` to create the boilerplate, then ask the schema-author agent to fill in the details

**Check for errors:**
> Check your editor's LSP diagnostics panel after editing, or ask the schema-reviewer agent to audit a file

## Troubleshooting

### "yammm-lsp binary not found in PATH"

The plugin checks for `yammm-lsp` at startup. If you see this warning:

1. Install the [VS Code extension](https://marketplace.visualstudio.com/items?itemName=simon-lentz.yammm) (recommended — the LSP binary is bundled)
2. Or build the standalone binary: `git clone https://github.com/simon-lentz/yammm.git && cd yammm && make build-lsp`
3. Verify it is in your PATH: `which yammm-lsp`

### No LSP diagnostics appearing

- Confirm `yammm-lsp` runs: `yammm-lsp --version`
- Check that the file has a `.yammm` extension
- For markdown files, ensure yammm code is in fenced blocks with the `yammm` language identifier

### Import resolution fails

The LSP resolves imports relative to the module root. If imports fail:

- Ensure you're running Claude Code from the project root
- Use the `--module-root` flag if needed: configure in your editor or LSP settings

## More Information

- [yammm repository](https://github.com/simon-lentz/yammm)
- [Language specification](https://github.com/simon-lentz/yammm/blob/main/docs/SPEC.md)
- [VS Code extension](https://github.com/simon-lentz/yammm/tree/main/lsp/editors/vscode)
