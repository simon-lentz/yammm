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
| `yammm neo4j constraints <schema>` | Generate Neo4j constraint statements |
| `yammm neo4j diff <schema>` | Diff schema constraints vs live database |
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
```

Generates Go source from a schema via the `adapter/gogen` adapter: one struct per type, named Enum/DataType types, `EDGE_` association structs, a Graph aggregate, and an embedded `SerializedModel`. Output is stdlib-only (imports at most `time`), formatted and type-checked before being written; schemas with imports are flattened into one self-contained package.

| Flag | Description |
|------|-------------|
| `--to` | Target: `go` (required) |
| `--package` | Generated Go package name (default: derived from schema name) |
| `--output` | Output file path (default: stdout) |
| `--initialisms` | Extra acronyms to upper-case in generated names, e.g. `GUID,JWT` |
| `--module-root` | Root directory for module-style imports (default: the schema's directory) |

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
| `--separator` | `__` | Label separator (schema__Type) |

### neo4j diff

```bash
yammm neo4j diff --uri bolt://localhost:7687 schema.yammm
```

Compares desired schema constraints against the live database. Reports constraints to create, drop, and those already present.

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
