# Go API Pipeline Reference

The yammm Go library follows a four-stage pipeline: **load schema -> validate instances -> build graph -> persist or write**. An operation that reports diagnostics returns a `diag.Result`, beside the value where the call produces one, with stable error codes and, where a diagnostic has a source position, its span. Some operations return an `error` instead, among them the Neo4j adapter's `BatchNodeQueries` and `BatchEdgeQueries`, `gogen.Marshal` and `markdown.Marshal`, and `instance.BuilderFor`.

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

The third argument names the entry source. An empty entry path selects the lexicographically smallest key.

### Load Options

```go
schema.Load(ctx, path,
    schema.WithModuleRoot("/custom/root"),
    schema.WithImportsAllowed(false),
)
```

Two options apply to `LoadSourcesWithEntry` alone; `Load` and `LoadString` refuse `WithSyntheticRoot` and `WithSourcesOnly(true)`:

```go
schema.LoadSourcesWithEntry(ctx, sources, "main.yammm", "",
    schema.WithSyntheticRoot("embedded://app"), // identities under a root no checkout or mount point moves
)
```

### Schema Type (Read API)

```go
s.Name()                          // schema name string
s.Type("User")                    // (Type, bool) lookup
s.Types()                         // iter.Seq[Type], in declaration order
s.DataType("Email")               // (DataType, bool) lookup
s.DataTypes()                     // iter.Seq[DataType], in declaration order
s.Imports()                       // iter.Seq[Import]
s.ImportByAlias("common")         // (Import, bool)
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
// Validate a batch of one type
raws := []instance.RawInstance{
    {Properties: map[string]any{"id": "abc-123", "name": "Widget"}},
}
valids, result := v.Validate(ctx, "Product", raws)

// Validate a single instance
valid, result := v.ValidateOne(ctx, "Product", instance.RawInstance{
    Properties: map[string]any{"id": "abc-123", "name": "Widget"},
})
```

### RawInstance (Input)

```go
type RawInstance struct {
    Properties map[string]any
    Provenance location.Provenance   // optional source metadata
}
```

A parser that cannot read an input as its member's type reports that issue and writes `instance.Refused{Value: <the input>}` in the value's place. At a property, a foreign-key field or an edge property, the validator reports nothing for a `Refused` and refuses the instance: `ValidateOne` returns a zero `ValidInstance` with no error for it, and `instance.ErrRefusedValue` names the case for a caller that must return an error. Anywhere else a `Refused` is a value of no form and draws the issue any such value draws (`E_TYPE_MISMATCH` as a list element, for one). One break draws one issue.

### Build RawInstances with SchemaBuilder

`instance.BuilderFor(s, typeName)` returns a `*SchemaBuilder` that constructs `RawInstance` values while enforcing schema shape at build time — unknown properties, unknown relations, cardinality mismatches, and edge-shape mismatches surface from `Build()` with the offending call site's file:line. Errors on a zero schema, an unknown type, or an abstract type. The builder covers properties and property-less association edges; instances carrying edge properties or composed children are constructed from raw data instead.

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
valid.Type()                       // schema.Type: one handle of the validator's schema load
valid.TypeName()                   // string
valid.PrimaryKey()                 // immutable.Key
valid.Property("name")             // (immutable.Value, bool)
valid.Properties()                 // immutable.Properties
valid.Edge("WORKS_AT")             // (ValidEdgeData, bool)
valid.Compositions()               // iter.Seq2[string, immutable.Value]
valid.Provenance()                 // location.Provenance
```

---

## Stage 3: Graph Construction

Build an instance graph from validated instances. The graph tracks associations, compositions, and detects duplicates.

### Create and Populate a Graph

```go
import "github.com/simon-lentz/yammm/graph"

g := graph.New(s)

// Add validated instances: Validate's result was checked, so a zero entry
// held a refused value its producer reported
for _, valid := range validInstances {
    if valid.IsZero() {
        continue
    }
    result := g.Add(ctx, valid)
    if result.HasErrors() {
        // handle a duplicate primary key, a keyless or part type, etc.
    }
}
```

`Add` panics on an instance another schema load validated, so build the validator and the graph from one `schema.Schema`. One text loaded twice is two loads. A validator over a member of the import closure shares the entry schema's load, so `Add` adds an instance of any type of the closure that can hold a root: concrete, not a part, and keyed, one the entry schema reaches only through another import included.

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

Returns a `graph.Snapshot` -- a point-in-time view of the graph that no method changes.

### Graph Instance (Read API)

```go
inst.TypeName()                    // string
inst.PrimaryKey()                  // immutable.Key
inst.Property("name")              // (immutable.Value, bool)
inst.Properties()                  // immutable.Properties
inst.Composed("ITEMS")             // iter.Seq[Instance]
inst.ComposedRelations()           // []string
```

### Graph Edge (Read API)

```go
edge.Relation()                    // string
edge.Source()                      // Instance
edge.Target()                      // Instance
edge.Property("weight")            // (immutable.Value, bool)
edge.Properties()                  // immutable.Properties
```

---

## Stage 4: Snapshot Persistence

Serialize graph snapshots to the `.ys` format, a JSON document, for storage, transfer, and verification.

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
    snapshot.WithIntegrityCheck(false),
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
if newMeta == nil {                             // a document that states no metadata
    newMeta = map[string]string{}
}
newMeta["pipeline_completed"] = "true"
updated, result := snapshot.UpdateMetadataOrReMarshal(ctx, data, newMeta, s)
```

`UpdateMetadataOrReMarshal(ctx, data, newMeta, s)` is the default entry point: it rewrites the header's metadata map (a full replace — clone the old map and mutate) while reusing the body bytes verbatim (fast path), and transparently falls back to `Load + Marshal` on any Error or Fatal but a cancellation and metadata that is not valid UTF-8, emitting `W_UPDATE_METADATA_FALLBACK` when the fallback succeeds so the transition is observable. `UpdateMetadata(ctx, data, newMeta)` is the strict fast path alone, for a consumer that cannot accept a round-trip. CLI form: `yammm snapshot update-metadata --set k=v`.

---

## Thread Safety and Immutability

- **Loaded schemas** (`schema.Schema`) are immutable and safe for concurrent use
- **Validated instances** (`instance.ValidInstance`) are immutable
- **Graph snapshots** (`graph.Snapshot`) have no method that changes them, and a record is a handle, so no caller holds its address
- **The `Graph` type** (`*graph.Graph`) is concurrent-safe for `Add` and `AddOrMerge` calls — multiple goroutines may add instances in parallel; the graph handles forward references, duplicate detection and merges atomically. `Snapshot()` acquires a read lock, briefly blocking concurrent adds, and returns a snapshot later Adds do not change
- **The validate→add pipeline** shares one `Validator` and one `Graph` across its workers: each worker validates a record with `ValidateOne`, checks `IsZero`, and adds it with `Add`, or with `AddOrMerge` for a source that lists one record under several parents. The caller waits for its workers, then checks the graph and takes its snapshot. Each call returns its own result, and a snapshot holds no diagnostics. To resume, seed the graph with `graph.NewFromSnapshot` (new adds resolve against, and may complete, the seeded state; it returns a `diag.Result` and takes a snapshot bound to the schema as it stands and refuses one built against another schema that breaks any of the schema's structural facts, save that it derives the Records fact's records again, refusing an `empty` record under a (one) association, and drops an absent record under a relation that is no association of the schema). See the Batch Assembly section of `docs/API.md`
- **Validators** (`*instance.Validator`) are safe for concurrent use (stateless after construction)

---

## Test Helpers

One package provides shared vocabulary for consumer test suites:

- **`snapshot/snapshottest`** — `BuildSnapshot(tb, s, instances...)` constructs a snapshot from pre-validated instances; `AssertRoundTrip(tb, snap, s, opts...)` pins Marshal→Load structural equivalence; `AssertDeterministic(tb, snap, opts...)` pins byte-stable marshaling; `DiffSnapshots(tb, want, got)` is the underlying go-cmp comparison.

Build its instances through `instance.Validator`. `internal/instancetest` is this module's own test support: Go refuses an import of an `internal/` package from another module.

---

## Complete Pipeline Example

```go
// 1. Load schema
s, result := schema.Load(ctx, "inventory.yammm")
if result.HasErrors() {
    return result.Err()
}

// 2. Decode the data: one array of objects per type name. UseNumber keeps an
// integer apart from a float, as the validator's Integer rule needs.
var doc map[string][]map[string]any
dec := json.NewDecoder(bytes.NewReader(jsonData))
dec.UseNumber()
if err := dec.Decode(&doc); err != nil {
    return err
}

// 3. Validate instances
v := instance.NewValidator(s)
g := graph.New(s)
for typeName, objects := range doc {
    raws := make([]instance.RawInstance, len(objects))
    for i, props := range objects {
        raws[i] = instance.RawInstance{Properties: props}
    }
    valids, result := v.Validate(ctx, typeName, raws)
    if result.HasErrors() {
        return result.Err()
    }
    for _, valid := range valids {
        if valid.IsZero() {
            continue // its producer refused a value and reported it
        }
        if res := g.Add(ctx, valid); res.HasErrors() {
            return res.Err()
        }
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
