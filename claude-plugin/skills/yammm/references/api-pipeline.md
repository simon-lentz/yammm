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
| ------ | ------- |
| `OK()` | `true` if no Fatal or Error issues |
| `HasFatal()` | `true` if any Fatal issue |
| `HasErrors()` | `true` if any Fatal or Error issue |
| `HasWarnings()` | `true` if any Warning issue |
| `Err()` | `nil` if OK, or `*diag.ResultError` |
| `Issues()` | Iterator over all issues |
| `Errors()` | Iterator over Fatal + Error issues |
| `HasCode(code)` | `true` if any issue carries the given diagnostic code |
| `TruncationNote()` | One-line dropped-issues note when the issue limit was hit (empty otherwise) |

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
s, result := schema.LoadSourcesWithEntry(ctx, sources, "main.yammm", "/module/root")
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
    schema.WithRegistry(registry),
    schema.WithSourcesOnly(), // hermetic: imports resolve only against in-memory sources — pair with LoadSourcesWithEntry, which seeds them
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

`Build()` validates declared names against the DSL's own productions (`E_INVALID_NAME`): type and datatype names start with an uppercase letter, property names with a lowercase letter, relation names with a letter of either case — all continuing with letters, digits, or underscores. Schema names and invariant names are free-form strings.

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
    instance.WithStrictPropertyNames(true),
    instance.WithAllowUnknownFields(false),
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

### Build RawInstances with SchemaBuilder

`instance.BuilderFor(s, typeName)` returns a `*SchemaBuilder` that constructs `RawInstance` values while enforcing schema shape at build time — unknown properties, unknown relations, and cardinality mismatches surface from `Build()` with the offending call site's file:line. Errors on nil schema, unknown type, or abstract type. The builder covers properties and property-less association edges; instances carrying edge properties or composed children are constructed from raw data instead.

```go
b, err := instance.BuilderFor(s, "Person")
if err != nil { /* handle */ }
raw, err := b.
    Property("id", "p1").
    Property("name", "Alice").
    EdgeTo("works_at", "c1"). // association; variadic key supports composite PKs
    Build()
```

### ValidInstance (Output -- Immutable)

```go
valid.TypeName()                   // string
valid.PrimaryKey()                 // immutable.Key
valid.Property("name")             // (immutable.Value, bool)
valid.Properties()                 // immutable.Properties
valid.Edge("WORKS_AT")             // (*ValidEdgeData, bool)
valid.Compositions()               // iter.Seq2[string, immutable.Value]
valid.Provenance()                 // *location.Provenance
```

---

## Stage 3: Graph Construction

Build an instance graph from validated instances. The graph tracks associations, compositions, and detects duplicates.

### Create and Populate a Graph

```go
import "github.com/simon-lentz/yammm/graph"

g := graph.New(s)

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

### Update Metadata In Place

```go
header, res := snapshot.HeaderOnly(ctx, data)   // read existing metadata
newMeta := maps.Clone(header.Metadata)          // replace-not-merge contract
newMeta["pipeline_completed"] = "true"
updated, result := snapshot.UpdateMetadataOrReMarshal(ctx, data, newMeta, s)
```

`UpdateMetadataOrReMarshal(ctx, data, newMeta, s, opts...)` is the default entry point: it rewrites the header's metadata map (a full replace — clone the old map and mutate) while reusing the body bytes verbatim (fast path), and transparently falls back to `Load + Marshal` on any recoverable failure, emitting `W_UPDATE_METADATA_FALLBACK` so the transition is observable. The strict-fast-path primitive `UpdateMetadata(ctx, data, newMeta, opts...)` stays exported for consumers where a round-trip is unacceptable. CLI form: `yammm snapshot update-metadata --set k=v`.

---

## Thread Safety and Immutability

- **Loaded schemas** (`*schema.Schema`) are immutable and safe for concurrent use
- **Validated instances** (`*instance.ValidInstance`) are immutable
- **Graph snapshots** (`*graph.Snapshot`) are immutable
- **The `Graph` type** (`*graph.Graph`) is concurrent-safe for `Add` and `AddComposed` calls — multiple goroutines may add instances in parallel; the graph handles forward references and duplicate detection atomically. `Snapshot()` acquires a read lock, briefly blocking concurrent Adds, and returns an immutable snapshot
- **`graph.BatchAssembler`** is the recommended high-level entry point for the validate→add→check→snapshot pipeline pattern: composes Validator + Graph, encodes the ordering invariant, concurrent-safe. Construct with `NewBatchAssembler` (empty graph) or `NewBatchAssemblerFromSnapshot` (graph seeded from a prior snapshot — the resume path: new adds resolve against, and may complete, the seeded state). See the Batch Assembly section of `docs/API.md`
- **Validators** (`*instance.Validator`) are safe for concurrent use (stateless after construction)

---

## Test Helpers

Two packages provide shared vocabulary for consumer test suites:

- **`instance/instancetest`** — `VI(typeName, opts...)` builds a `*ValidInstance` fixture directly (no validation pass), defaulting every field a scenario does not name; options include `PK(parts...)`, `Props(m)`, `Edges(m)`, `Composed(m)`, `Provenance(p)`, `TypeID(id)`.
- **`snapshot/snapshottest`** — `BuildSnapshot(tb, s, instances...)` constructs a snapshot from pre-validated instances; `AssertRoundTrip(tb, snap, s, opts...)` pins Marshal→Load structural equivalence; `AssertDeterministic(tb, snap, opts...)` pins byte-stable marshaling; `DiffSnapshots(tb, want, got)` is the underlying go-cmp comparison.

---

## Complete Pipeline Example

```go
// 1. Load schema
s, result := schema.Load(ctx, "inventory.yammm")
if result.HasErrors() {
    return result.Err()
}

// 2. Parse data (using JSON adapter; nil registry — location tracking off)
adapter, _ := jsonAdapter.New(nil)
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
