# CLI Reference

The `yammm` CLI provides schema validation, formatting, data checking, snapshot persistence, export, and Go code generation from the terminal.

---

## Command Reference

| Command | Description |
| ------- | ----------- |
| `yammm validate <schema>` | Validate a schema file |
| `yammm fmt <schema>...` | Format schema files, or check them with `--check` |
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
| ---- | ------- | ----------- |
| `--format` | `text` | Diagnostic output format: `text` or `json` |
| `--no-color` | `false` | Disable ANSI color in output |

### Writing files

Every command that writes a file you name — `--output`, `--output-dir`, `-o`,
`fmt -w`, `snapshot update-metadata` — writes it the same way. The path's
symlinks are followed, so a link survives and the file it names is written. A
regular file, or one that does not exist yet, is replaced atomically: the
content is staged beside it and renamed over it, so an interrupted write leaves
the previous file. A new file is created at `0600`; an existing one keeps its
mode. A FIFO, a device or a path under `/dev/` — `--output /dev/stdout`, say —
is written through, continuing its stream: behind `>> log` the bytes are
appended and the log keeps its history. Anything else is refused at exit 3, naming the path: a
read-only file, a directory, a looping link, or a file whose directory cannot
hold the staging file, which `gofmt -w` refuses too. Omit `--output` to write
to stdout.

---

## Schema Development Workflow

### validate

```bash
yammm validate schema.yammm
yammm validate --module-root . pipelines/x/schema.yammm   # module-style imports resolve against the root
```

Compiles a schema file and reports diagnostics. Exit code 0 on success, non-zero on errors.

### fmt

```bash
yammm fmt schema.yammm                 # print formatted output to stdout
yammm fmt -w schema.yammm              # write back to source file
yammm fmt --check schema.yammm a.yammm # list unformatted files, exit 1 if any
```

Formats `.yammm` files (consistent indentation, ordering). Use `-w` / `--write` to
modify files in place.

`--check` writes nothing. It prints the path of every unformatted file, one per
line, and exits 1 when the list is not empty — the shape `gofmt -l` uses, so it
drops into a pre-commit hook without a shell loop. Every path is checked, so one
run reports every offender. `--check` and `--write` together are a usage error
(exit 2). A file that does not parse is reported as the positioned `E_SYNTAX`
diagnostic `validate` reports, naming the file, and exits 1.

When formatting would change a schema's tokens or comments, `fmt` refuses. The
defect is the formatter's, never the schema's: it writes nothing, leaves the
file unchanged, names the file on stderr and exits 3. A pre-commit hook running
`fmt --write` therefore blocks the commit rather than committing the rewrite.

The check normalizes line endings before formatting, so a CRLF file is reported
as unformatted even when nothing else differs. That is what `-w` already does to
the same file.

### Typical development loop

```bash
yammm validate schema.yammm      # compile-check
yammm fmt --check schema.yammm   # gate: is it already canonical?
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
| ---- | ----------- |
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
| ---- | ----------- |
| `-o, --output` | Output path for `.ys` file (required) |
| `--from` | Input format override |
| `--type` | Type name for single-type CSV |
| `--type-column` | Column for multi-type CSV |
| `-m, --metadata` | Key=value metadata pairs (repeatable) |
| `--timestamp` | Stamp the current time as `created_at` (breaks determinism) |
| `--indent` | Produce indented output |
| `--into` | Existing `.ys` file to merge new data into. Its `created_at` and metadata are carried forward: `--timestamp` replaces the first, and `-m` overlays the second key by key |

Output is byte-for-byte deterministic unless a `created_at` is written, which happens only through `--timestamp` or `--into`. The summary line's type count is the written document's type table, the count `snapshot info` reports. `W_SNAPSHOT_PATH_EXTENSION` is raised only after the file is written, located at the output path.

### snapshot verify

```bash
yammm snapshot verify schema.yammm output.ys
```

Validates a persisted snapshot against its schema. Checks integrity hash, schema compatibility, dangling references, and structural correctness.

| Flag | Description |
| ---- | ----------- |
| `--skip-integrity-check` | Skip integrity hash verification (hand-edited files) |
| `--value-conformance` | Report stored `Timestamp`/`Date`/`UUID` values that do not conform to their constraints |
| `--revalidate` | Run every instance back through the validator; findings reported as warnings (v0.15+) |

### snapshot info

```bash
yammm snapshot info output.ys
```

Displays metadata about a `.ys` file: schema name, version, instance counts, integrity status, timestamps, custom metadata, and the header's attestation (the writer's validity claim, v0.15+).

`--header-only` reads the header alone and reports the file's size. `--dir <path>` scans every `.ys` file in a directory, header-only. The text and `--format json` modes render one structure, so a field one reports the other reports too, and absent `metadata` renders as `{}`. Under `--format json`, each `--dir` entry carries its result under `diagnostics` in the wire the diagnostic stream uses: `issues`, plus `limitReached` and `droppedCount` when the entry's issues were truncated. In text, a `warn` row names its first warning.

### snapshot update-metadata

```bash
yammm snapshot update-metadata --set env=prod --set version=3 output.ys
yammm snapshot update-metadata --unset env output.ys
```

Rewrites metadata on an existing `.ys` file. It reuses the snapshot body verbatim and recomputes only the integrity hash. There is no fallback: the command calls the strict primitive, so a document the fast path cannot rewrite is reported and the command exits non-zero rather than re-serializing. `created_at` is preserved byte for byte.

| Flag | Description |
| ---- | ----------- |
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
| ---- | ----------- |
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

`--to go` generates Go source via the `adapter/gogen` adapter: one struct per type, named Enum/DataType types, generated `Date` and per-layout `Timestamp` types, `EDGE_` association structs, a Graph aggregate, and the embedded schema source reachable through `SerializedSources()` / `SerializedEntry`. Output is stdlib-only (imports at most `time` and `encoding/json`), formatted and type-checked before being written; schemas with imports are flattened into one self-contained package.

`--to jsonschema` generates a JSON Schema draft 2020-12 document via the `adapter/jschema` adapter, describing the instance-data JSON object form `yammm check` accepts — wire it into an editor (e.g. a `# yaml-language-server: $schema=…` header or a VS Code `json.schemas` mapping) for completion, hover documentation, and validation while authoring data files. Same closure flattening; output is deterministic and self-checked before being written.

`--to md` (alias `markdown`) generates a Markdown reference document via the `adapter/markdown` adapter: a Mermaid class diagram of the whole import closure plus per-type sections (flattened property tables with `from <Owner>` inherited-row markers, relation bullets with edge-property sub-tables, invariant source fences) and data-type tables. Same closure flattening; output is deterministic and structurally self-checked before being written.

| Flag | Description |
| ---- | ----------- |
| `--to` | Target: `go`, `jsonschema`, or `md` (required) |
| `--package` | go target: generated package name (default: derived from schema name) |
| `--output` | Output file path (default: stdout) |
| `--initialisms` | go target: extra acronyms to upper-case in generated names, e.g. `GUID,JWT` |
| `--module-root` | Root directory for module-style imports (default: the nearest ancestor holding a `yammm.mod` marker, else the schema's directory). Shared by every command that loads a schema: `validate`, `check`, `load`, `export`, `gen`, `snapshot save`, `snapshot verify`, `neo4j constraints`, `neo4j diff`, `neo4j indexes` |
| `--schema-id` | jsonschema target: value for the emitted `"$id"` (omitted when unset) |
| `--no-class-diagram` | md target: omit the Mermaid class-diagram section |
| `--no-class-members` | md target: keep the diagram and omit the member lines inside each class |

Per-target flags are enforced with a usage error: `--package`/`--initialisms` apply only to `--to go`, `--schema-id` only to `--to jsonschema`, `--no-class-diagram` and `--no-class-members` only to `--to md`.

---

## Neo4j Workflow

All `neo4j` subcommands share connection flags (also available via environment variables):

| Flag | Env Var | Default | Description |
| ---- | ------- | ------- | ----------- |
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
| ---- | ------- | ----------- |
| `--edition` | `enterprise` | `enterprise` or `community` |
| `--named` | `true` | Generate named constraints |
| `--node-keys` | `false` | Emit NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Neo4j 5.7+, Enterprise; degrades to UNIQUE with `W_NEO4J_NODE_KEY_UNSUPPORTED` under `--edition community`) |
| `--scalar-types` | `true` | Emit `IS :: <TYPE>` constraints for scalar properties |
| `--required-only-types` | `false` | Restrict type constraints to required properties |
| `--separator` | `__` | Label separator (schema__Type) |
| `--prefix` | *(none)* | Global label prefix, if the target graph was generated with one |

### neo4j indexes

```bash
yammm neo4j indexes schema.yammm
```

Generates `CREATE INDEX` / `CREATE VECTOR INDEX` / `CREATE FULLTEXT INDEX ... IF NOT EXISTS` Cypher statements from a schema's `@index` / `@@index` / `@vector` / `@fulltext` / `@@fulltext` annotations. Index names are always emitted and indexes apply to every edition, so this command takes the label flags but none of the constraint-shape flags (`--edition`, `--named`, `--node-keys`, `--scalar-types`, `--required-only-types`).

| Flag | Default | Description |
| ---- | ------- | ----------- |
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
| ---- | ------- | ----------- |
| `--indexes` | `true` | Include index drift in the diff and the exit code; `--indexes=false` is constraints-only |
| `--edition` | `enterprise` | `enterprise` or `community` (governs which constraints are diffed, on **both** sides) |
| `--named` | `true` | Named constraints; with `false` every pairing falls through to semantic identity |
| `--node-keys` | `false` | Emit NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Neo4j 5.7+, Enterprise; degrades to UNIQUE with `W_NEO4J_NODE_KEY_UNSUPPORTED` under `--edition community`) |
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
| ---- | ----------- |
| `--schema` | Infer only this schema's types; compared as a label writes it, so `book-catalog` matches `book_catalog`'s labels |
| `--separator` | Label separator (schema__Type), as the graph was written |
| `--prefix` | Global label prefix, if the graph was written with one |
| `--output` | Output file (default: stdout) |

Each label is read as the label flags compose it: the prefix is stripped and the
separator splits schema from type, so a label another configuration wrote is not
read as this schema's type. A `--schema` that matches none of the constraints
read leaves a TODO line in the scaffold saying so.

Every command that takes the label flags — the four `neo4j` commands and
`export --to cypher` — refuses an empty `--separator`, and a `--prefix` or
`--separator` whose composed label is not a Neo4j identifier, at exit 2 before
it loads a schema or opens a connection.

### Typical Neo4j workflow

```bash
yammm neo4j constraints schema.yammm > constraints.cypher
yammm neo4j diff --uri bolt://localhost:7687 schema.yammm
yammm export --to cypher schema.yammm data.json --output load.cypher
```

The `--to cypher` output is parameterized — `UNWIND`/`MERGE` with `$key_id`,
`$props` and `$rows` placeholders — so it is written for inspection and for
integration into application or migration code. It is **not** directly
executable in `cypher-shell` or Neo4j Browser; running it there fails on the
unbound parameters.

---

## Exit Codes

| Code | Meaning |
| ---- | ------- |
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
  snapshot decoder's `W_SNAPSHOT_PATH_FALLBACK` on otherwise unchanged `.ys`
  files. From v0.15.0, `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` is an Error on
  these body-reading commands — the document is refused, not warned about;
  only header-only reads (`yammm snapshot info --header-only`, `--dir`) keep
  it a Warning.

This is what makes `W_ANNOTATION_SHADOWED` reachable — the only signal that a
subtype's property re-declaration dropped an inherited `@writeOnce` or `@index`
annotation. **Do not read a zero exit code as "no diagnostics."** Read the
output.

With `--format json`, each invocation writes exactly one JSON document to
stderr, so a warnings-only run now produces a wire object where it previously
produced nothing. A failure that is not itself a diagnostic — a bad flag, an
unreadable path, a lost connection — is inside that document as an
`E_COMMAND_FAILED` error whose `exit_code` detail is the process exit code.
