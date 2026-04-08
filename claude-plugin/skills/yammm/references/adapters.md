# Adapter Reference

Adapters parse raw data into `RawInstance` values for validation and serialize validated data for export. Three adapters are provided: JSON, CSV, and Neo4j.

All adapters live in `adapter/{json,csv,neo4j}` packages. The library never imports adapters -- adapters import the library.

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

The `registry` is a `location.PositionRegistry` for tracking source positions. Pass `source.NewRegistry()` for a default registry.

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

### Graph Shape Introspection

```go
shape, result := adapter.ShapeForSchema(ctx, s)
// shape.Types maps type names to NodeShape (Label, PrimaryKeys, RequiredFields)
```

### Query Generation

```go
// Batch node queries for an entire snapshot
nodeQueries, err := adapter.BatchNodeQueries(ctx, snapshot, shape,
    neo4jAdapter.WithImmutableKeys("id"),        // properties set only on creation
    neo4jAdapter.WithNodeChunkSize(5000),         // max nodes per UNWIND batch
)

// Batch edge queries
edgeQueries, err := adapter.BatchEdgeQueries(ctx, snapshot, shape,
    neo4jAdapter.WithEdgeChunkSize(5000),
)
```

Each `BatchNodeQuery` / `BatchEdgeQuery` contains a Cypher statement and parameters map ready for driver execution.

### Label Management

```go
label := adapter.Label(ctx, "inventory", "Product")  // "inventory__Product"

// Detect label collisions (two types producing the same Neo4j label)
result := adapter.DetectLabelCollisions(ctx, s)
```

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

## Adapter Error Codes

| Code | Adapter | Meaning |
|------|---------|---------|
| `E_ADAPTER_PARSE` | All | Format-specific parsing error |
| `E_CSV_COERCE` | CSV | Cell value could not be coerced to expected type |
| `E_NEO4J_LABEL_COLLISION` | Neo4j | Two types produce the same Neo4j label |
| `E_NEO4J_INVALID_IDENTIFIER` | Neo4j | Name not valid as Neo4j identifier |
| `E_NEO4J_UNSUPPORTED_TYPE` | Neo4j | Constraint kind has no Neo4j type mapping |
