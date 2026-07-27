# CLI Reference

The `yammm` CLI provides schema validation, formatting, data checking, snapshot persistence, export, and Go code generation from the terminal.

---

## Command Reference

| Command | Description |
|---------|-------------|
| `yammm validate <schema>` | Validate a schema file |
| `yammm fmt <schema>` | Format a schema file |
| `yammm check <schema> <data>` | Validate data against a schema |
| `yammm load <schema> <data>` | Load data into graph and validate |
| `yammm export <schema> <data>` | Export data to JSON, CSV, or Cypher |
| `yammm gen --to go <schema>` | Generate Go source from a schema |
| `yammm gen --to jsonschema <schema>` | Generate a JSON Schema (draft 2020-12) for instance-data authoring |
| `yammm gen --to md <schema>` | Generate Markdown docs with a Mermaid class diagram |
| `yammm neo4j constraints <schema>` | Generate Neo4j constraint statements |
| `yammm neo4j indexes <schema>` | Generate Neo4j index statements from annotations |
| `yammm neo4j diff <schema>` | Diff schema constraints and indexes vs live database |
| `yammm neo4j introspect` | Infer schema from live Neo4j database |
| `yammm snapshot save <schema> <data>` | Save graph snapshot to `.ys` file |
| `yammm snapshot info <snapshot>` | Display snapshot metadata |
| `yammm snapshot verify <schema> <snapshot>` | Verify snapshot against schema |
| `yammm snapshot update-metadata <snapshot>` | Rewrite metadata on an existing `.ys` file |

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--format` | `text` | Diagnostic output format: `text` or `json` |
| `--no-color` | `false` | Disable ANSI color in output |

---

## Schema Development Workflow

### validate

```bash
yammm validate schema.yammm
```

Compiles a schema file and reports diagnostics. Exit code 0 on success, non-zero on errors.

### fmt

```bash
yammm fmt schema.yammm           # print formatted output to stdout
yammm fmt -w schema.yammm        # write back to source file
```

Formats a `.yammm` file (consistent indentation, ordering). Use `-w` / `--write` to modify the file in place.

### Typical development loop

```bash
yammm validate schema.yammm      # compile-check
yammm fmt -w schema.yammm        # format
yammm check schema.yammm data.json  # validate data
```

---

## Data Validation

### check

```bash
yammm check schema.yammm data.json
yammm check schema.yammm data.csv --type User
yammm check schema.yammm data.csv --type-column '$type'
yammm check --from csv schema.yammm data.tsv --type User
```

Validates data against a schema without building a full graph. Reports constraint violations, missing fields, and invariant failures.

| Flag | Description |
|------|-------------|
| `--from` | Input format override (`json` or `csv`; auto-detected if omitted) |
| `--type` | Type name for single-type CSV |
| `--type-column` | Column name containing type names (multi-type CSV) |

### load

```bash
yammm load schema.yammm data.json
```

Loads data into an in-memory graph and validates completeness (resolves associations, checks required relationships). Same flags as `check`.

---

## Data Pipeline Workflow

### snapshot save

```bash
yammm snapshot save -o output.ys schema.yammm data.json
yammm snapshot save -o output.ys schema.yammm data1.json data2.csv --type User
yammm snapshot save -o output.ys --into existing.ys schema.yammm new_data.json
yammm snapshot save -o output.ys -m env=prod -m version=2 schema.yammm data.json
```

Builds a graph snapshot from one or more data files and persists it as a `.ys` file.

| Flag | Description |
|------|-------------|
| `-o, --output` | Output path for `.ys` file (required) |
| `--from` | Input format override |
| `--type` | Type name for single-type CSV |
| `--type-column` | Column for multi-type CSV |
| `-m, --metadata` | Key=value metadata pairs (repeatable) |
| `--timestamp` | Include `created_at` timestamp (breaks determinism) |
| `--indent` | Produce indented output |
| `--into` | Existing `.ys` file to merge new data into |

### snapshot verify

```bash
yammm snapshot verify schema.yammm output.ys
```

Validates a persisted snapshot against its schema. Checks integrity hash, schema compatibility, dangling references, and structural correctness.

### snapshot info

```bash
yammm snapshot info output.ys
```

Displays metadata about a `.ys` file: schema name, version, instance counts, integrity status, timestamps, and custom metadata.

### snapshot update-metadata

```bash
yammm snapshot update-metadata --set env=prod --set version=3 output.ys
yammm snapshot update-metadata --unset env output.ys
```

Rewrites metadata on an existing `.ys` file. Uses a fast path that reuses the snapshot body when possible; when it cannot, it falls back to a full load + re-marshal and reports `W_UPDATE_METADATA_FALLBACK`.

| Flag | Description |
|------|-------------|
| `-s, --set` | `key=value` metadata pair to set (repeatable) |
| `--unset` | Metadata key to remove (repeatable) |

### Typical pipeline

```bash
yammm snapshot save -o data.ys schema.yammm data.json
yammm snapshot verify schema.yammm data.ys
yammm export --to cypher schema.yammm data.json
```

---

## Export

```bash
yammm export --to json schema.yammm data.json
yammm export --to csv --output-dir ./out schema.yammm data.json
yammm export --to cypher schema.yammm data.json > import.cypher
yammm export --to json --output result.json schema.yammm data.csv --type User
```

| Flag | Description |
|------|-------------|
| `--to` | Output format: `json`, `csv`, or `cypher` (required) |
| `--from` | Input format override |
| `--output` | Output file path (default: stdout) |
| `--output-dir` | Output directory (CSV multi-type: one file per type) |
| `--type` | Type name for single-type CSV input |
| `--type-column` | Column for multi-type CSV input |

---

## Code Generation

```bash
yammm gen --to go schema.yammm
yammm gen --to go --package models --output models_gen.go schema.yammm
yammm gen --to go --initialisms GUID,JWT --module-root . schema.yammm
yammm gen --to jsonschema schema.yammm
yammm gen --to jsonschema --schema-id https://example.com/s.json --output s.schema.json schema.yammm
yammm gen --to md schema.yammm
yammm gen --to md --no-class-diagram --output SCHEMA.md schema.yammm
```

`--to go` generates Go source via the `adapter/gogen` adapter: one struct per type, named Enum/DataType types, `EDGE_` association structs, a Graph aggregate, and an embedded `SerializedModel`. Output is stdlib-only (imports at most `time`), formatted and type-checked before being written; schemas with imports are flattened into one self-contained package.

`--to jsonschema` generates a JSON Schema draft 2020-12 document via the `adapter/jschema` adapter, describing the instance-data JSON object form `yammm check` accepts — wire it into an editor (e.g. a `# yaml-language-server: $schema=…` header or a VS Code `json.schemas` mapping) for completion, hover documentation, and validation while authoring data files. Same closure flattening; output is deterministic and self-checked before being written.

`--to md` (alias `markdown`) generates a Markdown reference document via the `adapter/markdown` adapter: a Mermaid class diagram of the whole import closure plus per-type sections (flattened property tables with `from <Owner>` inherited-row markers, relation bullets with edge-property sub-tables, invariant source fences) and data-type tables. Same closure flattening; output is deterministic and structurally self-checked before being written.

| Flag | Description |
|------|-------------|
| `--to` | Target: `go`, `jsonschema`, or `md` (required) |
| `--package` | go target: generated package name (default: derived from schema name) |
| `--output` | Output file path (default: stdout) |
| `--initialisms` | go target: extra acronyms to upper-case in generated names, e.g. `GUID,JWT` |
| `--module-root` | Root directory for module-style imports (default: the schema's directory) |
| `--schema-id` | jsonschema target: value for the emitted `"$id"` (omitted when unset) |
| `--no-class-diagram` | md target: omit the Mermaid class-diagram section |

Per-target flags are enforced with a usage error: `--package`/`--initialisms` apply only to `--to go`, `--schema-id` only to `--to jsonschema`, `--no-class-diagram` only to `--to md`.

---

## Neo4j Workflow

All `neo4j` subcommands share connection flags (also available via environment variables):

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--uri` | `$YAMMM_NEO4J_URI` | -- | Neo4j bolt URI |
| `--username` | `$YAMMM_NEO4J_USERNAME` | `neo4j` | Username |
| `--password` | `$YAMMM_NEO4J_PASSWORD` | -- | Password |
| `--database` | `$YAMMM_NEO4J_DATABASE` | `neo4j` | Database name |

### neo4j constraints

```bash
yammm neo4j constraints schema.yammm
yammm neo4j constraints --edition community schema.yammm
yammm neo4j constraints --named=false schema.yammm
```

Generates `CREATE CONSTRAINT IF NOT EXISTS` Cypher statements from a schema.

| Flag | Default | Description |
|------|---------|-------------|
| `--edition` | `enterprise` | `enterprise` or `community` |
| `--named` | `true` | Generate named constraints |
| `--node-keys` | `false` | Emit NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Neo4j 5.7+, Enterprise) |
| `--scalar-types` | `true` | Emit `IS :: <TYPE>` constraints for scalar properties |
| `--required-only-types` | `false` | Restrict type constraints to required properties |
| `--separator` | `__` | Label separator (schema__Type) |
| `--prefix` | *(none)* | Global label prefix, if the target graph was generated with one |

### neo4j indexes

```bash
yammm neo4j indexes schema.yammm
```

Generates `CREATE INDEX` / `CREATE VECTOR INDEX IF NOT EXISTS` Cypher statements from a schema's `@index` / `@@index` / `@vector` annotations. Index names are always emitted and indexes apply to every edition, so this command takes the label flags but none of the constraint-shape flags (`--edition`, `--named`, `--node-keys`, `--scalar-types`, `--required-only-types`).

| Flag | Default | Description |
|------|---------|-------------|
| `--separator` | `__` | Label separator (schema__Type) |
| `--prefix` | *(none)* | Global label prefix, if the target graph was generated with one |

### neo4j diff

```bash
yammm neo4j diff --uri bolt://localhost:7687 schema.yammm
yammm neo4j diff --uri bolt://localhost:7687 --edition community --separator __ schema.yammm
```

Compares desired schema constraints **and indexes** against the live database (index diffing is on by default; `--indexes=false` restores the pre-v0.9.0 constraints-only behaviour, exit code included). Reports constraints and indexes to create, drop, and those already present. A schema-owned remote index with no declaration surfaces as a drop until it is annotated.

`diff` computes its desired side exactly as `constraints` and `indexes` emit it, so it takes **the same flags** — set every one of them to match how the target graph was generated. A flag left at its default when the graph was built with another value makes the desired side disagree with the database by construction, and the plan reports drift the operator never introduced.

| Flag | Default | Description |
|------|---------|-------------|
| `--indexes` | `true` | Include index drift in the diff and the exit code; `--indexes=false` is constraints-only |
| `--edition` | `enterprise` | `enterprise` or `community` (governs which constraints are diffed, on **both** sides) |
| `--named` | `true` | Named constraints; with `false` every pairing falls through to semantic identity |
| `--node-keys` | `false` | Emit NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Neo4j 5.7+, Enterprise) |
| `--scalar-types` | `true` | Emit `IS :: <TYPE>` constraints for scalar properties |
| `--required-only-types` | `false` | Restrict type constraints to required properties |
| `--separator` | `__` | Label separator (schema__Type) |
| `--prefix` | *(none)* | Global label prefix, if the target graph was generated with one |

Exit codes: `0` only when everything compared came back in sync. Drift, creates, or drops (constraints or indexes) exit `1`. A definition that could **not** be verified exits `3` — an index whose configuration the server did not report, a TYPE constraint whose enforced type it did not report, or an index introspection that failed outright and degraded the run to constraints-only. None is reported as success: a drift gate must not read "no drift" from a comparison that never ran. Drift outranks unverified, so a run with both exits `1`.

### neo4j introspect

```bash
yammm neo4j introspect --uri bolt://localhost:7687
yammm neo4j introspect --uri bolt://localhost:7687 --schema inventory --output inferred.yammm
```

Infers a `.yammm` schema from a live Neo4j database by reading constraints and relationships.

| Flag | Description |
|------|-------------|
| `--schema` | Filter to specific schema name prefix |
| `--output` | Output file (default: stdout) |

### Typical Neo4j workflow

```bash
yammm neo4j constraints schema.yammm > constraints.cypher
yammm neo4j diff --uri bolt://localhost:7687 schema.yammm
yammm export --to cypher schema.yammm data.json | cypher-shell -u neo4j
```

---

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success (no errors) |
| 1 | Errors in input (validation failures, constraint violations) |
| 2 | Usage error (bad flags, missing arguments) |
| 3 | Runtime error (connection failure, I/O error) |

---

## Diagnostics Go to stderr, at Every Severity

Since v0.9.0 every command prints whatever its run produced — errors, warnings,
info, hints — to **stderr**. Before v0.9.0 a warnings-only result printed
nothing at all, in any command, `yammm validate` included.

Two consequences worth knowing:

- **Exit codes are unchanged.** A warning is not a failure: a command that
  prints warnings and nothing else still exits `0`. Scripting that gates on the
  exit code is unaffected; scripting that gates on "did it print anything" is
  not. Each command's stdout contract (generated Cypher, Go source, exported
  data) is untouched — diagnostics have always been separate from it.
- **An unchanged, still-passing schema may emit new stderr output.** Warnings
  that already existed but that no command printed are now visible. The
  `-_`-in-a-constraint-bound warning (`E_INVALID_CONSTRAINT`, "minus sign before
  `_` (unbounded) has no effect") is the common one; `yammm snapshot verify`,
  `yammm export`, and `yammm snapshot save --into` additionally surface the
  snapshot decoder's `E_SNAPSHOT_PATH_FALLBACK` and
  `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` on otherwise unchanged `.ys` files.

This is what makes `W_ANNOTATION_SHADOWED` reachable — the only signal that a
subtype's property re-declaration dropped an inherited `@writeOnce` or `@index`
annotation. **Do not read a zero exit code as "no diagnostics."** Read the
output.

With `--format json`, each invocation writes exactly one JSON document to
stderr, so a warnings-only run now produces a wire object where it previously
produced nothing.
