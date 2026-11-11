# CLI Reference

The `yammm` CLI provides schema validation, formatting, data checking, snapshot persistence, Go and Markdown generation, Neo4j DDL, and a diff against a live Neo4j database from the terminal.

---

## Command Reference

| Command | Description |
| ------- | ----------- |
| `yammm validate <schema>` | Validate a schema file |
| `yammm fmt <schema>...` | Format schema files, or check them with `--check` |
| `yammm check <schema> <data>...` | Validate data files against a schema |
| `yammm gen --to go <schema>` | Generate Go source from a schema |
| `yammm gen --to md <schema>` | Generate Markdown docs with a Mermaid class diagram |
| `yammm neo4j constraints <schema>` | Generate Neo4j constraint statements |
| `yammm neo4j indexes <schema>` | Generate Neo4j index statements from annotations |
| `yammm neo4j diff <schema>` | Diff schema constraints and indexes vs live database |
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

Every command that writes a file you name — `--output`, `-o`, `snapshot save
--into`, `fmt -w`, `snapshot update-metadata` — writes it the same way. A `..` in the
path is evaluated on its text first, as for every path yammm reads, so
`link/../f` is the `f` beside `link` and the file `yammm check` reads for that
spelling, where on Unix the shell's `cat link/../f` reads the one beside the
directory `link` reaches. The path's symlinks are then followed, each link's own target
as the kernel follows it, so a link survives and the file it names is written. A
regular file, or one that does not exist yet, is replaced atomically: the
content is staged beside it and renamed over it, so an interrupted write leaves
the previous file. A new file is created at `0600` on Unix (Windows honours
only a mode's write bit); an existing one keeps its permission bits, and loses setuid, setgid and sticky. A FIFO, a device,
`/dev/stdin`, `/dev/stdout`, `/dev/stderr` or `/dev/fd/N` is written through, continuing its
stream, however the path reaches that directory: through a link to `/dev`, or
on Linux through any descriptor table under `/proc`, such as `/proc/self/fd/N`.
Behind `>> log` the bytes are
appended and the log keeps its history. Any other path
under `/dev/`, such as a file under Linux's `/dev/shm`, is decided by the file
it reaches. Anything else is refused at exit 3, naming the path: a
read-only file, a directory, a looping link, or a file whose directory cannot
hold the staging file, which `gofmt -w` refuses too. A path the process cannot
stat, a refused rename and a write-through target that will not open exit 3 the
same way. `gen` writes to stdout when `--output` is omitted; `snapshot save`
needs `-o` or `--into`.

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
line, as `gofmt -l` does, and exits 1 when the list is not empty, which `gofmt -l`
does not. A pre-commit hook needs only that exit code. Every path is checked, so one
run reports every offender. `--check` and `--write` together are a usage error
(exit 2). A file the grammar does not match, or that holds an escape the
language does not have outside an invariant expression or an annotation
argument, is reported as the positioned `E_SYNTAX` diagnostic `validate`
reports, naming the file, and exits 1. Any other diagnostic `validate` reports
does not stop `fmt`.

When formatting would change a schema's tokens or comments, `fmt` refuses. The
defect is the formatter's, never the schema's: it writes nothing, leaves the
file unchanged, names the file on stderr and exits 3. A pre-commit hook running
`fmt --write` therefore blocks the commit rather than committing the rewrite.

The check normalizes line endings before formatting, so a CRLF file is reported
as unformatted even when nothing else differs. That is what `-w` already does to
the same file. A file that starts with a UTF-8 byte order mark is reported the
same way, and `-w` removes the mark.

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
yammm check schema.yammm people.json cars.json
```

Validates data against a schema and writes nothing. Several data files are one document, as `snapshot save` reads them. It checks every instance, primary-key uniqueness, and every required association's target. Reports instance and graph diagnostics, the validator's at most 100 for one instance, among them constraint violations, missing and unknown fields, invariant failures, duplicate primary keys and unresolved required associations. The verdict is about the document the data files form: a target the document holds but the validator refuses is reported by that refusal and not as a missing target, and a key two instances state is a duplicate even where one of them is refused. Every command that reads data reads each data file as JSON or JSONC, whatever its name, so `/dev/stdin` serves on Linux and macOS, on a pipe or a redirected file. A file that is not well formed, is not one object, holds a type name whose value is not an array of objects, states one member name twice in one object, or nests deeper than the decoder's 10,000-level limit draws `E_DATA_PARSE`. A JSON object is read as that document, so a `.ys` snapshot draws `E_INVALID_TYPE_TAG` for each of its top-level keys, here and at `snapshot save`.

---

## Data Pipeline Workflow

### snapshot save

```bash
yammm snapshot save -o output.ys schema.yammm data.json
yammm snapshot save -o output.ys schema.yammm data1.json data2.json
yammm snapshot save -o output.ys --into existing.ys schema.yammm new_data.json
yammm snapshot save -o output.ys -m env=prod -m version=2 schema.yammm data.json
```

Builds a graph snapshot from one or more data files and persists it as a `.ys` file. The data files are validated as one document, as `check` reads them, and under `--into` the merged file's instances hold their keys too. Every data file's path is refused if it is empty or not UTF-8 before the schema or any file is read. So a data usage error names nothing about the data, and a data file that cannot be read keeps the parse diagnostics the files before it drew.

| Flag | Description |
| ---- | ----------- |
| `-o, --output` | Output path for the `.ys` file; required unless `--into` is given, which defaults it to the merged file |
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
| `--revalidate` (v0.15+) | Run each root instance the reader walks, with its composed children, its edges and its `target_missing` records, back through the validator; findings are reported as warnings, and a run with warnings alone exits 0. The reader walks no root of an instances entry that repeats a types-table row or names none. A duplicate record's instance is not run, and a root is not run where a structural check refused its type, its tree, one of its edges, one of its unresolved records, or one of its required or `(one)` associations, where another root the reader walks stands at its address, or where it stands at the key of a record whose types-table row the reader refused; an edge that names no root does not stop a root, unless it stands beside another record under a `(one)` association, and draws no finding for a target key component its key property refuses, nor does an edge whose target a refused root stands at, or a `target_missing` record whose target a refused unit stands at or states, after which the root's composed children and invariants are not run; a listing that repeats a target no sound root holds alone is left out of the run, the target's own issue standing for the repeat |

### snapshot info

```bash
yammm snapshot info output.ys
```

Displays metadata about a `.ys` file: schema name, version, instance counts, integrity status, timestamps, custom metadata, and the header's attestation (the writer's validity claim, v0.15+).

`--header-only` reads the header and the types table alone and reports the file's size. `--dir <path>` scans the `.ys` files of a directory that `snapshot.ScanDir`'s filtering admits, header-only. For one file, the text and `--format json` modes render one structure, so a field one reports the other reports too, and absent `metadata` reads `none` in text and `{}` in JSON; under `--dir` a JSON entry carries the name, path, size, modification time and the header, `null` where it did not parse, where a text row names the file and its status, then the schema, version and `created_at` of a file that read or the first error of one that did not. Under `--format json`, each `--dir` entry carries its result under `diagnostics` in the wire the diagnostic stream uses: `issues`, plus `limitReached` and `droppedCount` when the entry's issues were truncated. In text, a `warn` row names its first warning.

### snapshot update-metadata

```bash
yammm snapshot update-metadata --set env=prod --set version=3 output.ys
yammm snapshot update-metadata --unset env output.ys
```

Rewrites metadata on an existing `.ys` file. It reuses the snapshot body verbatim and recomputes only the integrity hash. There is no fallback: the command calls the strict primitive, so a document the fast path cannot rewrite is reported and the command exits non-zero rather than re-serializing. `created_at` keeps its text, and its JSON escapes are written anew.

| Flag | Description |
| ---- | ----------- |
| `-s, --set` | `key=value` metadata pair to set (repeatable) |
| `--unset` | Metadata key to remove (repeatable) |

### Typical pipeline

```bash
yammm snapshot save -o data.ys schema.yammm data.json
yammm snapshot verify schema.yammm data.ys
```

---

## Code Generation

```bash
yammm gen --to go schema.yammm
yammm gen --to go --package models --output models_gen.go schema.yammm
yammm gen --to go --initialisms GUID,JWT --module-root . schema.yammm
yammm gen --to md schema.yammm
yammm gen --to md --no-class-diagram --output SCHEMA.md schema.yammm
```

`--to go` generates Go source via the `schema/gogen` package: one struct per type, named Enum/DataType types, generated `Date` and per-layout `Timestamp` types, `EDGE_` association structs, a Graph aggregate, and the embedded schema source reachable through `SerializedSources()` / `SerializedEntry`. Output is stdlib-only (imports at most `time` and `encoding/json`), formatted and type-checked before being written; schemas with imports are flattened into one self-contained package.

`--to md` (alias `markdown`) generates a Markdown reference document via the `schema/markdown` package: a Mermaid class diagram of the whole import closure plus per-type sections (flattened property tables with `from <Owner>` inherited-row markers, relation bullets with edge-property sub-tables, invariant source fences) and data-type tables. Same closure flattening; output is deterministic and structurally self-checked before being written.

| Flag | Description |
| ---- | ----------- |
| `--to` | Target: `go` or `md` (required) |
| `--package` | go target: generated package name (default: derived from schema name). A value that is not a Go identifier, or is a keyword or `_`, an empty one included, is a usage error (exit 2), judged before the schema is loaded |
| `--output` | Output file path (default: stdout) |
| `--initialisms` | go target: extra acronyms to upper-case in generated names, e.g. `GUID,JWT` |
| `--module-root` | Root directory for module-style imports (default: the nearest ancestor holding a `yammm.mod` marker, else the schema's directory). Shared by every command that loads a schema: `validate`, `check`, `gen`, `snapshot save`, `snapshot verify`, `neo4j constraints`, `neo4j diff`, `neo4j indexes`. A root that is not a directory the loader can open exits 3 before the schema is parsed, whether or not the schema imports anything |
| `--no-class-diagram` | md target: omit the Mermaid class-diagram section |
| `--no-class-members` | md target: keep the diagram and omit the member lines inside each class |

Per-target flags are enforced with a usage error: `--package`/`--initialisms` apply only to `--to go`, `--no-class-diagram` and `--no-class-members` only to `--to md`.

---

## Neo4j Workflow

`neo4j diff`, the one `neo4j` command that connects, takes the connection flags (also available via environment variables); `constraints` and `indexes` take none:

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
| `--node-keys` | `false` | Emit NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Enterprise; under `--edition community` it degrades to UNIQUE with `W_NEO4J_NODE_KEY_UNSUPPORTED`) |
| `--scalar-types` | `true` | Emit `IS :: <TYPE>` constraints for scalar, List and Vector properties |
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
| `--node-keys` | `false` | Compare against NODE KEY instead of separate UNIQUE + NOT NULL for primary keys (Enterprise; under `--edition community` the desired side degrades to UNIQUE, and `diff` prints `W_NEO4J_NODE_KEY_UNSUPPORTED` only beside a generation error) |
| `--scalar-types` | `true` | Emit `IS :: <TYPE>` constraints for scalar, List and Vector properties |
| `--required-only-types` | `false` | Restrict type constraints to required properties |
| `--separator` | `__` | Label separator (schema__Type) |
| `--prefix` | *(none)* | Global label prefix, if the target graph was generated with one |

Exit codes: `0` only when everything compared came back in sync. Drift, creates, or drops (constraints or indexes) exit `1`. A definition that could **not** be verified exits `3` — an index whose configuration the server did not report, a TYPE constraint whose enforced type it did not report, or an index introspection that failed outright and degraded the run to constraints-only. None is reported as success: a drift gate must not read "no drift" from a comparison that never ran. Drift outranks unverified, so a run with both exits `1`.

Every command that takes the label flags — the three `neo4j` commands —
refuses a `--prefix` or `--separator` whose composed label is not a Neo4j
identifier, at exit 2 before it loads a schema or opens a connection.

### Typical Neo4j workflow

```bash
yammm neo4j constraints schema.yammm > constraints.cypher
yammm neo4j diff --uri bolt://localhost:7687 schema.yammm
```

---

## Exit Codes

| Code | Meaning |
| ---- | ------- |
| 0 | Success (no errors) |
| 1 | An error in the input (a validation failure, a constraint violation), drift `neo4j diff` finds, or a file `fmt --check` would reformat |
| 2 | Usage error (bad flags, missing or extra arguments, an unknown command or help topic, an empty path operand, a schema, data or module-root path that is not valid UTF-8) |
| 3 | Runtime error (connection failure, I/O error, a path the filesystem refuses, a `neo4j diff` comparison that could not be verified, a database object `neo4j diff` cannot classify, an internal fault, a failure the command did not classify) |

---

## Diagnostics Go to stderr, at Every Severity

A diagnostic a command prints goes to **stderr**, whatever its severity. One
report, named below, also states diagnostics on stdout. The list says what a
command prints, and names what some commands find and do not print.

- **A warning is not a failure.** A run that meets none of the Exit Codes
  table's conditions for `1`, `2` or `3` exits `0`, whatever warnings it
  prints. Scripting that gates on the exit code sees no warning; scripting
  that gates on "did it print anything" does.
- **stdout holds each command's own output**: generated Cypher, Go source,
  Markdown, formatted schema text and `fmt --check`'s path list, a report, a
  shell completion script, help text, `--version`'s line.
- **One report carries diagnostics on stdout.** `yammm snapshot info --dir`
  sends the diagnostics of the directory read to stderr, and puts each file's
  result in its stdout report: in text a row's status with its first error or
  first warning and no other issue of that file, and under `--format json`
  each entry's `diagnostics`. The exit code is read from every file's result.
- **`yammm neo4j diff` prints a generation warning only beside an error.** It
  generates the constraints it compares, and the indexes unless
  `--indexes=false`. It prints what a generation reports only when that
  generation's own report holds an error. So for a schema whose constraints
  generate with no error, `neo4j diff --edition community` prints no
  `W_NEO4J_EDITION_CONSTRAINT_OMITTED` where
  `yammm neo4j constraints --edition community` prints one. The diagnostics
  of the schema load are printed at every severity.
- **`yammm fmt` judges the grammar alone.** For a file the grammar does not
  match, it reports the first place that does not match. A file the grammar
  matches is formatted, and `fmt` prints none of the diagnostics
  `yammm validate` reports for it, such as inverted bounds.
- **`yammm check` and `yammm snapshot save` leave out one consequence.** When
  the document holds a root the pipeline refused, the `E_UNRESOLVED_REQUIRED`
  that names that root as a missing target is not printed. The refusal of the
  root is.
- **A schema that passes can still print.** The `-_`-in-a-constraint-bound
  warning (`E_INVALID_CONSTRAINT`, "minus sign before
  '_' (unbounded) has no effect") is one. `yammm snapshot verify`,
  `yammm snapshot info` without `--header-only` or `--dir`, and
  `yammm snapshot save --into` print the
  snapshot decoder's `W_SNAPSHOT_PATH_FALLBACK`.
  `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` is an Error on
  these body-reading commands: the document is refused, not warned about.
  `yammm snapshot info --header-only` and `--dir` read the header and the types
  table alone and report it as a Warning. `yammm snapshot update-metadata` reports that Warning
  from its header read, then refuses the document with the Error.

`W_ANNOTATION_SHADOWED` is a warning, and it is the only signal that a
subtype's property re-declaration dropped an inherited property annotation.
**Do not read a zero exit code as "no diagnostics."** Read the
output.

With `--format json`, each command writes exactly one JSON document to stderr,
a clean run included, so a consumer parses stderr unconditionally. A `help`,
`completion` or `--version` that succeeds writes no document; `help` given a
topic no command answers, and `completion` given no shell or one it does not
know, exit 2 and write the refusal as any usage error. A failure that is not itself a diagnostic — a bad flag, an
unreadable path, a lost connection — is inside that document as an
`E_COMMAND_FAILED` error whose `exit_code` detail is the process exit code.
