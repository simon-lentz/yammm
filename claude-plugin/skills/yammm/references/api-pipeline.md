# Go API Pipeline Reference

The yammm Go library follows a four-stage pipeline: **load schema -> validate instances -> build graph -> persist/export**. Every public entry point returns `(value, diag.Result)` with stable error codes and precise source locations.

---

## Error Handling Pattern

All load/validation functions return `(T, diag.Result)`:

```go
schema, result := schema.Load(ctx, "path/to/schema.yammm")
if result.HasErrors() {
    // result.Err() returns an error wrapping the diagnostic issues
    // result.Issues() iterates all issues
    // result.Errors() iterates Fatal + Error issues only
    return result.Err()
}
```

Key `diag.Result` methods:

| Method | Returns |
|--------|---------|
| `OK()` | `true` if no Fatal or Error issues |
| `HasFatal()` | `true` if any Fatal issue |
| `HasErrors()` | `true` if any Fatal or Error issue |
| `HasWarnings()` | `true` if any Warning issue |
| `Err()` | `nil` if OK, or `*diag.ResultError` |
| `Issues()` | Iterator over all issues |
| `Errors()` | Iterator over Fatal + Error issues |
| `Messages()` | `[]string` of Fatal + Error messages |

---

## Stage 1: Schema Loading

Load a schema from a file, string, or in-memory sources.

### From File

```go
import "github.com/simon-lentz/yammm/schema"

s, result := schema.Load(ctx, "path/to/schema.yammm")
if result.HasErrors() {
    return result.Err()
}
```

### From String (No Imports)

```go
s, result := schema.LoadString(ctx, `
schema "example"
type Item {
    id UUID primary
    name String[1, 100] required
}
`, "example.yammm")
```

### From In-Memory Sources (With Imports)

```go
sources := map[string][]byte{
    "main.yammm":   mainContent,
    "common.yammm": commonContent,
}
s, result := schema.LoadSources(ctx, sources, "/module/root")
```

Use `LoadSourcesWithEntry` when the entry point is not the lexicographically first key:

```go
s, result := schema.LoadSourcesWithEntry(ctx, sources, "main.yammm", "/module/root")
```

### Load Options

```go
schema.Load(ctx, path,
    schema.WithModuleRoot("/custom/root"),
    schema.WithIssueLimit(100),
    schema.WithLogger(logger),
    schema.WithDisallowImports(),
    schema.WithSourceRegistry(reg),
    schema.WithRegistry(registry),
)
```

### Builder API (Programmatic Schema Construction)

```go
s, result := schema.NewBuilder().
    WithName("example").
    AddDataType("Email", emailConstraint).
    AddType("User").
        WithPrimaryKey("id", uuidConstraint).
        WithProperty("name", stringConstraint).
        WithOptionalProperty("email", emailConstraint).
        WithRelation("WORKS_AT", companyRef, false, false).
        WithComposition("ADDRESSES", addressRef, true, true).
        WithInvariant("name_not_empty", nameExpr, "").
        AsAbstract().  // or AsPart()
        Extends(parentRef).
        Done().
    Build()
```

### Schema Type (Read API)

```go
s.Name()                          // schema name string
s.Type("User")                    // (*Type, bool) lookup
s.Types()                         // iter.Seq2[string, *Type]
s.TypeNames()                     // []string (sorted)
s.TypeCount()                     // int
s.DataType("Email")               // (*DataType, bool) lookup
s.DataTypes()                     // iter.Seq2[string, *DataType]
s.Imports()                       // iter.Seq[*Import]
s.ImportByAlias("common")         // (*Import, bool)
```

---

## Stage 2: Instance Validation

Validate raw data against a loaded schema. Produces immutable `ValidInstance` values.

### Create a Validator

```go
import "github.com/simon-lentz/yammm/instance"

v := instance.NewValidator(s,
    instance.WithLogger(logger),
    instance.WithStrictPropertyNames(true),
    instance.WithAllowUnknownFields(false),
    instance.WithMaxIssuesPerInstance(50),
)
```

### Validate Instances

```go
// Validate a batch (common path for JSON/CSV adapter output)
raws := []instance.RawInstance{
    {Properties: map[string]any{"id": "abc-123", "name": "Widget"}},
}
valids, result := v.Validate(ctx, "Product", raws)

// Validate a single instance
valid, result := v.ValidateOne(ctx, "Product", instance.RawInstance{
    Properties: map[string]any{"id": "abc-123", "name": "Widget"},
})

// Validate composed children (part types)
children, result := v.ValidateForComposition(ctx, "Order", "ITEMS", rawItems)
```

### RawInstance (Input)

```go
type RawInstance struct {
    Properties map[string]any
    Provenance *location.Provenance   // optional source metadata
}
```

### ValidInstance (Output -- Immutable)

```go
valid.TypeName()                   // string
valid.PrimaryKey()                 // immutable.Key
valid.Property("name")             // (immutable.Value, bool)
valid.Properties()                 // immutable.Properties
valid.Edge("WORKS_AT")             // (*ValidEdgeData, bool)
valid.Composed("ITEMS")            // (immutable.Value, bool)
valid.Provenance()                 // *location.Provenance
```

---

## Stage 3: Graph Construction

Build an instance graph from validated instances. The graph tracks associations, compositions, and detects duplicates.

### Create and Populate a Graph

```go
import "github.com/simon-lentz/yammm/graph"

g := graph.New(s, graph.WithLogger(logger))

// Add validated instances
for _, inst := range validInstances {
    result := g.Add(ctx, inst)
    if result.HasErrors() {
        // handle duplicate PK, type not found, etc.
    }
}

// Add composed children to an existing parent
result := g.AddComposed(ctx, "Order", "order-123", "ITEMS", childInstance)
```

### Check Graph Completeness

```go
result := g.Check(ctx)
if result.HasErrors() {
    // E_UNRESOLVED_REQUIRED: required associations not satisfied
}
```

`Check()` verifies that all required associations (`(one)`, `(one:many)`) have resolved targets. Call it after all instances are added.

### Create a Snapshot

```go
snap := g.Snapshot()
```

Returns an immutable `*graph.Snapshot` -- a point-in-time view of the graph.

### Graph Instance (Read API)

```go
inst.TypeName()                    // string
inst.PrimaryKey()                  // immutable.Key
inst.Property("name")              // (immutable.Value, bool)
inst.Properties()                  // immutable.Properties
inst.Composed("ITEMS")             // []*Instance
inst.ComposedRelations()           // []string
```

### Graph Edge (Read API)

```go
edge.Relation()                    // string
edge.Source()                      // *Instance
edge.Target()                      // *Instance
edge.Property("weight")            // (immutable.Value, bool)
edge.Properties()                  // immutable.Properties
```

---

## Stage 4: Snapshot Persistence

Serialize graph snapshots to the `.ys` binary format for storage, transfer, and verification.

```go
import "github.com/simon-lentz/yammm/snapshot"
```

### Marshal (Serialize)

```go
data, result := snapshot.Marshal(ctx, snap,
    snapshot.WithIndent("  "),
    snapshot.WithCreatedAt(time.Now()),
    snapshot.WithMetadata(map[string]string{"source": "pipeline"}),
)
```

### Load (Deserialize)

```go
snap, result := snapshot.Load(ctx, data, s,
    snapshot.WithSkipIntegrityCheck(),
)
```

### Verify (Lightweight Validation)

```go
result := snapshot.Verify(ctx, data, s)
if result.HasErrors() {
    // schema mismatch, integrity failure, dangling references, etc.
}
```

### Info (Inspect Without Loading)

```go
info, result := snapshot.Info(ctx, data)
// info.SchemaName, info.TotalInstances, info.IntegrityStatus, etc.
```

`SnapshotInfo` fields: `Version`, `Features`, `SchemaName`, `SchemaHash`, `IntegrityHash`, `CreatedAt`, `Metadata`, `Types`, `InstanceCounts`, `TotalInstances`, `TotalEdges`, `IntegrityStatus`.

---

## Thread Safety and Immutability

- **Loaded schemas** (`*schema.Schema`) are immutable and safe for concurrent use
- **Validated instances** (`*instance.ValidInstance`) are immutable
- **Graph snapshots** (`*graph.Snapshot`) are immutable
- **The `Graph` type** (`*graph.Graph`) is concurrent-safe for `Add` and `AddComposed` calls — multiple goroutines may add instances in parallel; the graph handles forward references and duplicate detection atomically. `Snapshot()` acquires a read lock, briefly blocking concurrent Adds, and returns an immutable snapshot
- **`graph.BatchAssembler`** is the recommended high-level entry point for the validate→add→check→snapshot pipeline pattern: composes Validator + Graph, encodes the ordering invariant, concurrent-safe by default with opt-in `WithValidatorPool(n)` for CPU-bound consumers. See `docs/API.md` § Batch Assembly
- **Validators** (`*instance.Validator`) are safe for concurrent use (stateless after construction)

---

## Complete Pipeline Example

```go
// 1. Load schema
s, result := schema.Load(ctx, "inventory.yammm")
if result.HasErrors() {
    return result.Err()
}

// 2. Parse data (using JSON adapter)
adapter, _ := jsonAdapter.New(source.NewRegistry())
parsed, result := adapter.ParseObject(ctx, loc, jsonData)
if result.HasErrors() {
    return result.Err()
}

// 3. Validate instances
v := instance.NewValidator(s)
g := graph.New(s)
for typeName, raws := range parsed {
    valids, result := v.Validate(ctx, typeName, raws)
    if result.HasErrors() {
        return result.Err()
    }
    for _, inst := range valids {
        g.Add(ctx, inst)
    }
}

// 4. Check graph completeness
if result := g.Check(ctx); result.HasErrors() {
    return result.Err()
}

// 5. Persist snapshot
data, result := snapshot.Marshal(ctx, g.Snapshot())
if result.HasErrors() {
    return result.Err()
}
os.WriteFile("inventory.ys", data, 0644)
```
