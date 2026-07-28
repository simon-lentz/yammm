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
argument-hint: "[question about yammm]"
---

# yammm

YAMMM is a schema DSL, Go library, and CLI for typed data validation, graph construction, multi-format data export (JSON, CSV, Neo4j Cypher), and schema-driven artifact generation (Go source, JSON Schema, Markdown). Define schemas once in a compact DSL, validate data at runtime with structured diagnostics, build integrity-checked instance graphs, persist snapshots, and export to databases.

```text
data path:
  .yammm file --> schema.Load() --> instance.Validate() --> graph.Add()
                                                                |
                    adapter.Write() <-- snapshot.Marshal() <-- graph.Check()
                    (JSON / CSV / Neo4j)

generator path:
  .yammm file --> schema.Load() --> gogen | jschema | markdown --> bytes
                                    (Go source / JSON Schema / Markdown)
```

The two paths are independent: the data path carries instances, while the
generators are schema-in, bytes-out and never touch instance data.

Every operation returns `(value, diag.Result)` with stable error codes and precise source locations. Loaded schemas, validated instances, and snapshots are immutable and thread-safe.

---

## Three Ways to Use yammm

### Write Schemas

Define types, properties, relationships, invariants, annotations, and imports in `.yammm` files. The LSP (`yammm-lsp`) provides diagnostics, completions, hover, and go-to-definition.

### Use the Go Library

Load schemas, validate raw data, build instance graphs, persist snapshots, and export via adapters. The public API lives in packages `schema`, `instance`, `graph`, `snapshot`, and `adapter/{json,csv,neo4j}`. All results are immutable and thread-safe.

### Use the CLI

`yammm validate`, `yammm fmt`, `yammm check`, `yammm load`, `yammm snapshot`, `yammm export`, `yammm gen`, `yammm neo4j`. Schema development, data pipelines, Go / JSON Schema / Markdown generation, and database setup from the terminal.

---

## Where to Start

- **"I want to model a dataset"** -- Read `references/dsl-syntax.md` and `references/patterns.md`
- **"I want to validate data in a Go application"** -- Read `references/api-pipeline.md`
- **"I want to export data to Neo4j / JSON / CSV"** -- Read `references/adapters.md` and `references/cli.md`
- **"I want to understand an error"** -- Read `references/diagnostics.md`
- **"I want feedback on my schema"** -- Use `/yammm:review-schema`
- **"I want to write a new schema from scratch"** -- Use `/yammm:author-schema`
- **"I need to install the toolchain"** -- Run `/yammm:setup`

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
| `references/dsl-syntax.md` | Full grammar: types, properties, relationships, annotations, imports | Writing or modifying `.yammm` schemas |
| `references/expressions.md` | Operators, pipeline, lambdas, all built-in functions | Writing invariants or understanding expression evaluation |
| `references/type-system.md` | Constraint types, aliases, abstract/part, inheritance | Type system questions, narrowing rules, PK restrictions |
| `references/patterns.md` | Common modeling patterns with examples | Looking for schema design patterns |
| `references/api-pipeline.md` | Go API: load -> validate -> graph -> snapshot | Writing Go code that uses yammm packages |
| `references/adapters.md` | JSON/CSV/Neo4j/gogen/jschema/markdown adapter usage | Exporting, importing, or generating Go / JSON Schema / Markdown from a schema |
| `references/diagnostics.md` | Error codes, troubleshooting | Understanding or fixing errors |
| `references/cli.md` | CLI commands and workflows | Using yammm from the terminal |

### Canonical upstream documentation (requires network)

For edge cases and ambiguities that the reference files above don't cover, the canonical sources live in the yammm repository on GitHub. These are **fetched** (via `WebFetch`), not read from disk — the URLs are plain HTTP, so they require network access at fetch time but work identically from any install path (dev-load, marketplace install, or otherwise).

- **DSL specification** — `https://raw.githubusercontent.com/simon-lentz/yammm/main/docs/SPEC.md` — use when resolving grammar ambiguities or edge cases not covered by `references/dsl-syntax.md`, `references/type-system.md`, or `references/expressions.md`.
- **Go library API** — `https://raw.githubusercontent.com/simon-lentz/yammm/main/docs/API.md` — use when detailed API semantics go beyond what `references/api-pipeline.md` or `references/adapters.md` covers.

Both URLs track the `main` branch so the skill always reflects the latest canonical documentation without requiring a per-release URL update. Raw URLs (`raw.githubusercontent.com`) are used instead of the GitHub blob viewer (`github.com/.../blob/...`) so that `WebFetch` receives the markdown content directly rather than GitHub's HTML page chrome.

---

## Examples

Before/after transformation examples: see `examples/` directory.

- `examples/schema-improvements.md` -- Common quality improvements (bare types, missing invariants, abstract extraction)
- `examples/modeling-patterns.md` -- Complete mini-schemas for different domains (e-commerce, org hierarchy, CMS)

---

## Quick Pre-Merge Checklist

- [ ] `yammm validate` clean on all modified `.yammm` files -- and **read its stderr**: warnings do not change the exit code
- [ ] No `W_ANNOTATION_SHADOWED` warnings (a re-declaration silently dropped an inherited `@index` / `@writeOnce`)
- [ ] `yammm fmt` applied (deterministic formatting)
- [ ] `yammm check` passes if instance data is available
- [ ] Every concrete type has at least one `primary` field (one or more -- composite keys allowed)
- [ ] Imported types use qualified references (`alias.TypeName`)
- [ ] Optional fields guarded with nil checks in invariants
- [ ] Constraint bounds explicit where the domain is known (no bare `String` for bounded fields)
- [ ] Annotations reviewed: each `@index` / `@@index` serves a real lookup, `@@index` argument order matches it, `@fulltext` / `@@fulltext` target genuinely searchable text (not identifiers), and immutable-after-creation fields carry `@writeOnce`
