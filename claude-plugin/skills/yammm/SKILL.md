---
name: yammm
description: >-
  YAMMM schema DSL, Go library, and CLI. ALWAYS activate when
  working with .yammm files, writing Go code that imports yammm
  packages, running yammm CLI commands, or any question about
  yammm's type system, validation, graph construction, adapters,
  diagnostics, or expression language -- even for quick lookups.
  Triggers on "yammm schema", "yammm validate", ".yammm file",
  "yammm API", "yammm export", "yammm invariant".
allowed-tools: Read Grep Glob Bash(yammm *)
paths: "**/*.yammm"
argument-hint: "[question about yammm]"
---

# yammm

YAMMM (Yet Another Meta-Meta Model) is a schema DSL, Go library, and CLI for typed data validation, graph construction, and multi-format export (JSON, CSV, Neo4j Cypher). Define schemas once in a compact DSL, validate data at runtime with structured diagnostics, build integrity-checked instance graphs, persist snapshots, and export to databases.

```text
.yammm file --> schema.Load() --> instance.Validate() --> graph.Add()
                                                              |
               adapter.Write() <-- snapshot.Marshal() <-- graph.Check()
               (JSON/CSV/Neo4j)
```

Every operation returns `(value, diag.Result)` with stable error codes and precise source locations. Loaded schemas, validated instances, and snapshots are immutable and thread-safe.

---

## Three Ways to Use yammm

### Write Schemas

Define types, properties, relationships, invariants, and imports in `.yammm` files. The LSP (`yammm-lsp`) provides diagnostics, completions, hover, and go-to-definition.

### Use the Go Library

Load schemas, validate raw data, build instance graphs, persist snapshots, and export via adapters. The public API lives in packages `schema`, `instance`, `graph`, `snapshot`, and `adapter/{json,csv,neo4j}`. All results are immutable and thread-safe.

### Use the CLI

`yammm validate`, `yammm fmt`, `yammm check`, `yammm snapshot`, `yammm export`, `yammm neo4j`. Schema development, data pipelines, and database setup from the terminal.

---

## Where to Start

- **"I want to model a dataset"** -- Read `references/dsl-syntax.md` and `references/patterns.md`
- **"I want to validate data in a Go application"** -- Read `references/api-pipeline.md`
- **"I want to export data to Neo4j / JSON / CSV"** -- Read `references/adapters.md` and `references/cli.md`
- **"I want to understand an error"** -- Read `references/diagnostics.md`
- **"I want feedback on my schema"** -- Use `/yammm:review-schema`
- **"I want to write a new schema from scratch"** -- Use `/yammm:author-schema`
- **"I need to install the toolchain"** -- Run `/yammm:setup`
- **"I want to traverse a graph programmatically"** -- Read `references/graph-traversal.md`

---

## Key Design Principles

- **Immutability**: Loaded schemas, validated instances, and snapshots are immutable and thread-safe.
- **Structured diagnostics**: Every operation returns `(value, diag.Result)`. Stable error codes (`E_*`). Precise source locations.
- **Layer discipline**: Foundation (`location`, `diag`, `immutable`) -> Primary API (`schema`, `instance`, `graph`, `snapshot`) -> Adapters (`json`, `csv`, `neo4j`). Adapters import the library; the library never imports adapters.
- **Deterministic output**: Snapshots, graph traversal, and diagnostic ordering are deterministic and reproducible.

---

## Reference Files

| File | Covers | Consult when... |
|------|--------|-----------------|
| `references/quick-reference.md` | Compact syntax cheat sheet | Quick DSL syntax lookup |
| `references/common-mistakes.md` | 20 wrong/right patterns | Checking or fixing common errors |
| `references/dsl-syntax.md` | Full grammar: types, properties, relationships, imports | Writing or modifying `.yammm` schemas |
| `references/expressions.md` | Operators, pipeline, lambdas, all built-in functions | Writing invariants or understanding expression evaluation |
| `references/type-system.md` | Constraint types, aliases, abstract/part, inheritance | Type system questions, narrowing rules, PK restrictions |
| `references/patterns.md` | Common modeling patterns with examples | Looking for schema design patterns |
| `references/api-pipeline.md` | Go API: load -> validate -> graph -> snapshot | Writing Go code that uses yammm packages |
| `references/graph-traversal.md` | `graph/walk` API: Visitor, callbacks, ordering | Programmatic graph traversal |
| `references/adapters.md` | JSON/CSV/Neo4j adapter usage | Exporting or importing data |
| `references/diagnostics.md` | Error codes, troubleshooting | Understanding or fixing errors |
| `references/cli.md` | CLI commands and workflows | Using yammm from the terminal |
| `../../docs/SPEC.md` | Canonical DSL specification | Resolving ambiguities or edge cases not covered by reference files |
| `../../docs/API.md` | Canonical Go library API documentation | Detailed API semantics beyond what `api-pipeline.md` covers |

---

## Examples

Before/after transformation examples: see `examples/` directory.

- `examples/schema-improvements.md` -- Common quality improvements (bare types, missing invariants, abstract extraction)
- `examples/modeling-patterns.md` -- Complete mini-schemas for different domains (e-commerce, org hierarchy, CMS)

---

## Quick Pre-Merge Checklist

- [ ] `yammm validate` clean on all modified `.yammm` files
- [ ] `yammm fmt` applied (deterministic formatting)
- [ ] `yammm check` passes if instance data is available
- [ ] Every concrete type has exactly one `primary` field
- [ ] Imported types use qualified references (`alias.TypeName`)
- [ ] Optional fields guarded with nil checks in invariants
- [ ] Constraint bounds explicit where the domain is known (no bare `String` for bounded fields)
