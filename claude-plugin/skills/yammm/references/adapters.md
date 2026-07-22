# Adapter Reference

Adapters parse raw data into `RawInstance` values for validation and serialize validated data for export. Six adapters are provided: the three data adapters (JSON, CSV, Neo4j) and the three gen-family generators (gogen, jschema, markdown).

All adapters live in `adapter/{json,csv,neo4j,gogen,jschema,markdown}` packages. The library never imports adapters -- adapters import the library. The data adapters parse and serialize instance data; the generators are schema-in/bytes-out and never touch instance data.

---

## JSON Adapter

```go
import jsonAdapter "github.com/simon-lentz/yammm/adapter/json"
```

### Constructor

```go
adapter, err := jsonAdapter.New(registry,
    jsonAdapter.WithStrictJSON(false),        // allow JSONC (comments, trailing commas)
    jsonAdapter.WithTrackLocations(true),     // track source positions for diagnostics
    jsonAdapter.WithTypeField("$type"),       // custom type discriminator field
)
```

The `registry` is a `location.PositionRegistry` for tracking source positions. Pass `nil` unless `WithTrackLocations(true)` is set (a nil registry with tracking enabled is a construction error).

### Parsing

```go
// Parse a JSON object keyed by type name: {"User": [...], "Product": [...]}
parsed, result := adapter.ParseObject(ctx, sourceID, data)

// Parse a JSON array of typed objects (each has $type field)
parsed, result := adapter.ParseArray(ctx, sourceID, data)

// Parse a JSON array where all objects are the same type
raws, result := adapter.ParseTypedArray(ctx, sourceID, "User", data)

// Parse a single JSON object
raw, result := adapter.ParseOne(ctx, sourceID, "User", data)
```

All parse methods return `RawInstance` values ready for `instance.Validator.Validate()`.

### Writing

```go
// Marshal snapshot to JSON bytes
data, err := adapter.MarshalObject(ctx, snapshot,
    jsonAdapter.WithIndent("  "),
    jsonAdapter.WithDiagnostics(true),       // include $diagnostics section
)

// Write snapshot to io.Writer
n, err := adapter.WriteObject(ctx, writer, snapshot)

// Marshal a single validated instance
data, err := adapter.MarshalInstance(ctx, validInst, schemaType)
```

---

## CSV Adapter

```go
import csvAdapter "github.com/simon-lentz/yammm/adapter/csv"
```

### Constructor

```go
adapter := csvAdapter.New(
    csvAdapter.WithDelimiter(','),            // field delimiter (use '\t' for TSV)
    csvAdapter.WithHeader(true),              // first row is header
    csvAdapter.WithTypeColumn("$type"),       // column for type discrimination
    csvAdapter.WithNullValue(""),             // string representing nil
    csvAdapter.WithListSeparator("|"),        // list element separator
)
```

### Parsing

```go
// Parse CSV where all rows are the same type
raws, result := adapter.ParseTyped(ctx, sourceID, "User", reader, schemaType)

// Parse CSV with a type column (multi-type)
parsed, result := adapter.ParseWithTypeColumn(ctx, sourceID, reader, typeResolver)

// Parse a single row
raw, result := adapter.ParseOne(ctx, sourceID, "User", columns, row, schemaType)
```

Type coercion: CSV values are strings. The adapter coerces them to the schema's expected types (integers, floats, booleans, timestamps, UUIDs, lists). Coercion failures produce `E_CSV_COERCE` diagnostics.

BOM stripping: UTF-8 BOM bytes at the start of input are automatically stripped.

### Writing

```go
// Marshal instances of one type to CSV bytes
data, err := adapter.MarshalTyped(ctx, instances, schemaType,
    csvAdapter.WithWriteHeader(true),
    csvAdapter.WithWriteNullString(""),
)

// Write instances to an io.Writer
n, err := adapter.WriteTyped(ctx, writer, instances, schemaType)

// Marshal entire snapshot (one CSV per type)
files, err := adapter.MarshalSnapshot(ctx, snapshot)
// files is map[string][]byte: type name -> CSV bytes

// Write snapshot to per-type writers
err := adapter.WriteSnapshot(ctx, writerFactory, snapshot)
```

---

## Neo4j Adapter

```go
import neo4jAdapter "github.com/simon-lentz/yammm/adapter/neo4j"
```

The Neo4j adapter produces Cypher statements and parameters. It does **not** manage a database connection -- use a Neo4j driver to execute the generated queries.

### Constructor

```go
adapter := neo4jAdapter.New(
    neo4jAdapter.WithEdition(neo4jAdapter.Enterprise),  // or Community
    neo4jAdapter.WithLabelSeparator("__"),               // schema__TypeName
    neo4jAdapter.WithLabelPrefix(""),                    // optional global prefix
    neo4jAdapter.WithNamedConstraints(true),             // generate named constraints
    neo4jAdapter.WithScalarTypeConstraints(true),        // property type constraints
    neo4jAdapter.WithNodeKeyConstraints(false),          // NODE KEY instead of UNIQUE+NOT NULL
)
```

**Edition differences:**
- `Enterprise`: Supports UNIQUE, NOT NULL, property type, and NODE KEY constraints
- `Community`: UNIQUE constraints only

### Constraint Generation

```go
// Generate constraint Cypher statements
statements, result := adapter.ConstraintsForSchema(ctx, s)
// statements is []string of CREATE CONSTRAINT IF NOT EXISTS ...

// Structured constraint objects
constraints, result := adapter.ConstraintsStructured(ctx, s)
// Each Constraint has: Name, Kind, Label, Properties, TypeExpr, Statement
```

### Index Generation

```go
// Generate index Cypher from @index / @@index / @vector annotations
statements, result := adapter.IndexesForSchema(ctx, s)
// statements is []string of CREATE INDEX / CREATE VECTOR INDEX IF NOT EXISTS ...

// Structured index objects
indexes, result := adapter.IndexesStructured(ctx, s)
// Each Index has: Name, Kind (IndexRange|IndexVector), Label, Properties,
// VectorDimensions, VectorSimilarity, Statement
```

Property-level `@index` emits a single-property range index; type-level `@@index(a, b)` a composite range index (declared order significant); property-level `@vector(cosine|euclidean)` a vector index (dimension from the `Vector[N]` constraint). Index names are always emitted; a schema-wide emitted-name collision reports `E_NEO4J_INDEX_NAME_COLLISION`. Indexes emit for every edition (unlike constraints); the `CREATE VECTOR INDEX ... OPTIONS` form requires Neo4j 5.15+. `DiffIndexes(desired, actual, schemaName)` returns an `*IndexDiffResult` (Match/Drift/Create/Drop); composite property order is significant, and a schema-owned remote index with no declaration reports as a drop.

### Graph Shape Introspection

```go
shape, result := adapter.ShapeForSchema(ctx, s)
// shape.Types maps type names to NodeShape (Label, PrimaryKeys, RequiredFields)
```

### Query Generation

```go
// Batch node queries for an entire snapshot
nodeQueries, err := adapter.BatchNodeQueries(ctx, snapshot, shape,
    neo4jAdapter.WithImmutableKeys("first_seen_at"), // properties set only on creation
    neo4jAdapter.WithNodeChunkSize(5000),             // max nodes per UNWIND batch
)

// Batch edge queries
edgeQueries, err := adapter.BatchEdgeQueries(ctx, snapshot, shape,
    neo4jAdapter.WithEdgeChunkSize(5000),
)
```

Each `BatchNodeQuery` / `BatchEdgeQuery` contains a Cypher statement and parameters map ready for driver execution.

`WithImmutableKeys` unions with the immutable keys derived from a type's `@writeOnce` annotations (`ImmutableKeysFor(t)` returns a type's `@writeOnce` properties, own and inherited); the effective set per type drives the ON CREATE / ON MATCH split, selected per type in a batch. Only the explicitly-passed keys are validated against the schema at query generation: every one must name a declared property (own or inherited) of a node type being written, otherwise the call errors — a mistyped key would silently defeat the write-once guarantee. The option affects node merges only (relationship merges have no ON CREATE / ON MATCH split).

### Parameter Coercion (Direct-Cypher Path)

Values passing through `BatchNodeQueries` / `NodeQueryFor` are coerced to driver-native types automatically. Consumers hand-building Cypher parameters use the same chokepoint directly:

```go
v, err := neo4jAdapter.Coerce(constraint, raw)   // one value against one constraint
types := neo4jAdapter.ParamTypesForType(t, "")   // property-name -> constraint map for a type
params, err := neo4jAdapter.CoerceParams(params, types)  // whole parameter map
```

This repairs JSON round-trip artifacts (whole-number floats decoded as `int64`, `Date`/`Timestamp` strings) so values satisfy Neo4j `IS ::` type constraints.

### Label Management

```go
label := adapter.Label(ctx, "inventory", "Product")  // "inventory__Product"

// Detect label collisions (two types producing the same Neo4j label)
result := adapter.DetectLabelCollisions(ctx, s)
```

**Cypher reserved words:** the DSL does not reserve them. A property named
`match` is valid yammm (and fine for JSON/CSV export) but is rejected at
Neo4j constraint/shape generation with `ErrReservedKeyword`, because
property names appear unquoted in generated Cypher; the check is
case-insensitive. Namespaced labels usually absorb reserved type names
(`inventory__MATCH` is fine). Check early with
`ValidateIdentifier(name, context)` or by running `ConstraintsForSchema`
before write time.

### Constraint Diffing

```go
diff := adapter.DiffConstraints(desired, actual, "inventory")
// diff.ToCreate, diff.ToDrop, diff.Unchanged
```

### Schema Inference (from Live Database)

```go
schema, err := adapter.InferSchema(constraints, relationships, "inventory")
// Returns .yammm source code inferred from Neo4j metadata
```

---

## gogen Adapter (Go Code Generation)

```go
import "github.com/simon-lentz/yammm/adapter/gogen"
```

Schema-in, bytes-out: `gogen.Marshal` maps a loaded, resolved schema to formatted, type-checked Go source — one struct per type, named Enum/DataType types, `EDGE_` association structs, a Graph aggregate, and an embedded `SerializedModel` that re-loads hermetically (`schema.WithSourcesOnly`, no filesystem participation). Generated output is stdlib-only (imports at most `time`) and byte-reproducible across checkouts. Unlike the data adapters it has no instance-data path and returns a plain `error` rather than a `diag.Result`.

Schemas with imports are flattened into one self-contained package; cross-schema identifier collisions are resolved by schema-qualification (two schemas' `Region` becomes `GeoRegion` / `CommonRegion`); an unresolvable same-schema clash (a type and a datatype of the same name) is a hard error.

Full API semantics: the gogen section of `docs/API.md`. CLI form: `yammm gen --to go` (see `cli.md`).

---

## jschema Adapter (JSON Schema Generation)

```go
import "github.com/simon-lentz/yammm/adapter/jschema"
```

Schema-in, bytes-out: `jschema.Marshal` maps a loaded, resolved schema to a JSON Schema **draft 2020-12** document describing the instance-data JSON object form `yammm check` accepts — one top-level key per concrete type (entry types bare, directly imported types alias-qualified as `common.Region`), each an array of instances; `EDGE_` defs carrying required `_target_<pk>` foreign-key fields; compositions always arrays (`minItems: 1` when required, `maxItems: 1` for to-one); named DataTypes as `$ref`ed `$defs` entries; schema doc-comments flowing through as `description` for editor hover. Association presence is deliberately NOT `required` per-file (yammm defers it to graph assembly). Output is deterministic and self-checked (valid JSON, every `$ref` resolves) before return. Options: `WithSchemaID` (the `"$id"`, omitted when unset), `WithTitle`, `WithDescription`. Plain `error`, no instance-data path, no source-backing requirement (Builder-built schemas accepted).

Wire the generated document into an editor for completion and validation while authoring data files (e.g. `# yaml-language-server: $schema=./fleet.schema.json`).

Full API semantics: the JSON Schema Generation section of `docs/API.md`. CLI form: `yammm gen --to jsonschema` (see `cli.md`).

---

## markdown Adapter (Markdown + Mermaid Documentation Generation)

```go
import "github.com/simon-lentz/yammm/adapter/markdown"
```

Schema-in, bytes-out: `markdown.Marshal` maps a loaded, resolved schema to one self-contained Markdown reference document covering the whole import closure — a Mermaid class diagram (each type's own members as `name KindLabel` pairs, `<<Abstract>>`/`<<Part>>` stereotypes, DSL-labeled relation edges, `Parent <|-- Child` inheritance edges), per-type sections in declaration order (flattened property tables with `from <Owner>` inherited-row markers, DSL-form constraint rendering like `String[1, 100]`, relation bullets with linked targets and edge-property sub-tables, invariant source fences extracted from the schema source), and Name | Definition | Description data-type tables. Imported schemas get their own `## Schema <Name> (imported as <alias>)` sections with collision-proof `schemaName.TypeName` headings. Output is deterministic and structurally self-checked (fence balance, link→anchor resolution, table column counts) before return. Option: `WithClassDiagram(false)` omits the diagram. Plain `error`, no instance-data path, no source-backing requirement (on Builder-built schemas invariants degrade to message-only).

Full API semantics: the Markdown Documentation Generation section of `docs/API.md`. CLI form: `yammm gen --to md` (see `cli.md`).

---

## Adapter Error Codes

| Code | Adapter | Meaning |
|------|---------|---------|
| `E_ADAPTER_PARSE` | All | Format-specific parsing error |
| `E_CSV_COERCE` | CSV | Cell value could not be coerced to expected type |
| `E_NEO4J_LABEL_COLLISION` | Neo4j | Two types produce the same Neo4j label |
| `E_NEO4J_INVALID_IDENTIFIER` | Neo4j | Name not valid as Neo4j identifier |
| `E_NEO4J_UNSUPPORTED_TYPE` | Neo4j | Constraint kind has no Neo4j type mapping |
