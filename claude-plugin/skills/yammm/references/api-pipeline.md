# Go API Pipeline Reference

The yammm Go library follows a four-stage pipeline: **load schema -> validate instances -> build graph -> persist or write**. Every call that can fail returns `(T, error)`, or an `error` alone when it has no value. `schema.FindModuleRoot` and `location.ResolveSourcePath` return two values before their error, a call where the caller needs no reason returns `false` as its last value, and `snapshot.ScanDir` yields an error only when the scan stops, holding a file's own failure in `ScanEntry.Err`. A call that judges its input, such as a schema load, a validation, `Graph.Add`, `Graph.AddOrMerge` and `Graph.Check`, a snapshot read or `snapshot.Marshal`, a Cypher generation and `neo4j.Diff`, returns the failures it finds as a `*diag.ResultError`, with stable error codes and, where a diagnostic has a source position, its span. An option a call does not take is refused with a plain error, and some calls return a plain error for their own refusals, among them `gogen.Marshal`, `markdown.Marshal`, `instance.BuilderFor`, `graph.ParseKey`, and `neo4j.NodeQueries` and `neo4j.EdgeQueries`, which mark a value they cannot write with `neo4j.ErrUnrepresentable`. A plain error can wrap a `*diag.ResultError`, as `NodeQueries`' does when `cypher.Config.CheckLabels` refuses the schema's labels, so read every error with `errors.AsType`.

---

## Error Handling Pattern

Every load and validation function returns its value beside an error, or an error alone where it has no value, as `snapshot.Verify` and `Graph.Check` do:

```go
s, err := schema.Load(ctx, "path/to/schema.yammm",
    schema.WithWarnings(func(w diag.Issue) { /* a Warning, Info or Hint */ }))
if err != nil {
    if errors.Is(err, diag.E_IMPORT_CYCLE) { /* test one code */ }
    if re, ok := errors.AsType[*diag.ResultError](err); ok {
        // re.Result.Issues() iterates the failures
    }
    return fmt.Errorf("load schema: %w", err)
}
```

- A failure is a `*diag.ResultError`: the Fatal and Error issues the call drew. `errors.AsType` finds it through `%w` tags and `errors.Join`, and `errors.Is(err, code)` matches each code it saw, a dropped one included.
- A Warning, Info or Hint is never in the error. A call that can draw one takes a `WithWarnings(func(diag.Issue))` option: every schema load, `snapshot.Unmarshal` and `snapshot.Verify`, `cypher.Config.Constraints` and `neo4j.Diff`. Without it they are discarded.
- A call refuses an option it does not take with an error, with no diagnostic code, that names the option and the call.

Key `diag.Result` methods, on a failure's `re.Result`:

| Method | Returns |
| ------ | ------- |
| `HasFatal()` | `true` if any Fatal issue |
| `SeverityCounts()` | Counts by severity, over every issue seen |
| `Issues()` | Iterator over the retained issues |
| `Errors()` | Iterator over Fatal + Error issues |
| `HasCode(code)` | `true` if any retained issue carries the given diagnostic code |
| `TruncationNote()` | One-line dropped-issues note when the issue limit was hit (empty otherwise) |

---

## Stage 1: Schema Loading

Load a schema from a file, string, or in-memory sources.

### From File

```go
import "github.com/simon-lentz/yammm/schema"

s, err := schema.Load(ctx, "path/to/schema.yammm")
if err != nil {
    return err
}
```

### From String (No Imports)

```go
s, err := schema.LoadString(ctx, `
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
s, err := schema.LoadSourcesWithEntry(ctx, sources, "main.yammm",
    schema.WithModuleRoot("/module/root"))
```

The third argument names the entry source. An empty entry path selects the lexicographically smallest key. A relative key resolves under the `WithModuleRoot` root, or under the working directory without one; under `WithSyntheticRoot` it joins the synthetic root and names no host path.

### Load Options

```go
schema.Load(ctx, path,
    schema.WithModuleRoot("/custom/root"),
    schema.WithImportsAllowed(false),
)
```

`LoadString` refuses `WithModuleRoot` and `WithImportsAllowed`. Two options apply to `LoadSourcesWithEntry` alone, and `Load` and `LoadString` refuse them: `WithSyntheticRoot`, which `LoadSourcesWithEntry` refuses beside `WithModuleRoot`, and `WithSourcesOnly`:

```go
schema.LoadSourcesWithEntry(ctx, sources, "main.yammm",
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

v := instance.NewValidator(s)
```

Every input key names its member exactly: a property by its declared name, a relation by its field name, its declared name in lower case (`works_at` for `WORKS_AT`), an edge property by its name and a foreign-key field by its `_target_` name. A key in any other case is an unknown field (`E_UNKNOWN_FIELD`, or `E_UNKNOWN_EDGE_FIELD` in an edge object), and the member it was meant for stays absent. `instance.WithIssueLimit(n)` caps the issues one instance stores (100 by default, 0 for no limit).

### Validate Instances

```go
// Validate a batch of one type
raws := []instance.RawInstance{
    {Properties: map[string]any{"id": "abc-123", "name": "Widget"}},
}
// One slot per input, a zero instance for each one err refuses, when the
// batch runs; nil when it does not (an unknown type, a cancellation)
valids, err := v.Validate(ctx, "Product", raws)

// Validate a single instance
valid, err := v.ValidateOne(ctx, "Product", instance.RawInstance{
    Properties: map[string]any{"id": "abc-123", "name": "Widget"},
})
```

### RawInstance (Input)

```go
type RawInstance struct {
    Properties map[string]any
    Provenance instance.Provenance   // optional source metadata
}
```

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
valid.Composed("ITEMS")            // iter.Seq[ValidInstance]: the children under one composition, in input order
valid.Provenance()                 // instance.Provenance
```

---

## Stage 3: Graph Construction

Build an instance graph from validated instances. The graph tracks associations, compositions, and detects duplicates.

### Create and Populate a Graph

```go
import "github.com/simon-lentz/yammm/graph"

g := graph.New(s)

// Add validated instances: Validate's error was checked, so no entry is zero
for _, valid := range validInstances {
    if err := g.Add(ctx, valid); err != nil {
        // handle a duplicate primary key, a keyless or part type, etc.
    }
}
```

`Add` panics on an instance another schema load validated, so build the validator and the graph from one `schema.Schema`. One text loaded twice is two loads. A validator over a member of the import closure shares the entry schema's load, so `Add` adds an instance of any type of the closure that can hold a root: concrete, not a part, and keyed, one the entry schema reaches only through another import included.

### Check Graph Completeness

```go
if err := g.Check(ctx); errors.Is(err, diag.E_UNRESOLVED_REQUIRED) {
    // required associations not satisfied
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
data, err := snapshot.Marshal(ctx, snap,
    snapshot.WithIndent("  "),
    snapshot.WithCreatedAt(time.Now()),
    snapshot.WithMetadata(map[string]string{"source": "pipeline"}),
)
```

### Unmarshal (Deserialize)

```go
snap, err := snapshot.Unmarshal(ctx, data, s,
    snapshot.WithIntegrityCheck(false),
)
```

### Verify (Lightweight Validation)

```go
if err := snapshot.Verify(ctx, data, s); err != nil {
    // schema mismatch, integrity failure, dangling references, etc.
}
```

### Stat (Inspect Without Loading)

```go
info, err := snapshot.Stat(ctx, data)
// info.SchemaName, info.TotalInstances, info.IntegrityStatus, etc.
// info is non-nil beside err when the document decodes and only its body
// breaks a structural check
```

`Info` embeds `Header` (`Version`, `SchemaName`, `SchemaHash`, `SchemaHashAlgorithm`, `IntegrityHash`, `CreatedAt` — a `time.Time`, zero when the header states none — `Metadata`, `Types`, `Attestation`) and adds `InstanceCounts`, `TotalInstances`, `TotalEdges`, `DuplicateCount`, `UnresolvedCount` and `IntegrityStatus` (`snapshot.IntegrityOK` or `snapshot.IntegrityMismatch`). `snapshot.ReadHeader(ctx, r)` reads the `Header` alone from an `io.Reader`.

### Update Metadata In Place

```go
header, err := snapshot.ReadHeader(ctx, bytes.NewReader(data)) // read existing metadata
newMeta := maps.Clone(header.Metadata)          // replace-not-merge contract
if newMeta == nil {                             // a document that states no metadata
    newMeta = map[string]string{}
}
newMeta["pipeline_completed"] = "true"
updated, err := snapshot.UpdateMetadata(ctx, data, newMeta)
```

`UpdateMetadata(ctx, data, newMeta, opts...)` replaces the header's metadata map (a full replace — clone the old map and mutate), writes a new header and keeps every other byte, for every document `Unmarshal` accepts under its default options. It verifies the integrity hash first and refuses a mismatch, or a header that holds no hash, with `E_SNAPSHOT_INTEGRITY_MISMATCH`, unless `WithIntegrityCheck(false)`, the one option it takes, skips that check; it decodes no instance. It keeps `created_at` as written; to change it, re-marshal with `WithCreatedAt`. For a header past `MaxHeaderSize`, read the metadata with `Stat`, since `ReadHeader` refuses it. CLI form: `yammm snapshot update-metadata --set k=v`.

---

## Thread Safety and Immutability

- **Loaded schemas** (`schema.Schema`) are immutable and safe for concurrent use
- **Validated instances** (`instance.ValidInstance`) are immutable
- **Graph snapshots** (`graph.Snapshot`) have no method that changes them, and a record is a handle, so no caller holds its address
- **The `Graph` type** (`*graph.Graph`) is concurrent-safe for `Add` and `AddOrMerge` calls — multiple goroutines may add instances in parallel; the graph handles forward references, duplicate detection and merges atomically. `Snapshot()` acquires a read lock, briefly blocking concurrent adds, and returns a snapshot later Adds do not change
- **The validate→add pipeline** shares one `Validator` and one `Graph` across its workers: each worker validates a record with `ValidateOne`, returns on its error, and adds it with `Add`, or with `AddOrMerge` for a source that lists one record under several parents. The caller waits for its workers, then checks the graph and takes its snapshot. Each call returns its own error, and a snapshot holds no diagnostics. To resume, seed the graph with `graph.NewFromSnapshot(snap)` (new adds resolve against, and may complete, the seeded state; it binds the graph to the snapshot's own schema, installs its records as they stand, and panics on a zero snapshot). See the Batch Assembly section of `docs/API.md`
- **Validators** (`*instance.Validator`) are safe for concurrent use (stateless after construction)

---

## Test Helpers

One package provides shared vocabulary for consumer test suites:

- **`snapshot/snapshottest`** — `BuildSnapshot(tb, s, instances...)` constructs a snapshot from pre-validated instances; `AssertRoundTrip(tb, snap, opts...)` pins Marshal→Unmarshal structural equivalence against the snapshot's own schema; `AssertDeterministic(tb, snap, opts...)` pins byte-stable marshaling; `DiffSnapshots(tb, want, got)` is the underlying go-cmp comparison.

Build its instances through `instance.Validator`. `internal/instancetest` is this module's own test support: Go refuses an import of an `internal/` package from another module.

---

## Complete Pipeline Example

```go
// 1. Load schema
s, err := schema.Load(ctx, "inventory.yammm")
if err != nil {
    return err
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
    valids, err := v.Validate(ctx, typeName, raws)
    if err != nil {
        return fmt.Errorf("validate %s: %w", typeName, err)
    }
    for _, valid := range valids {
        if err := g.Add(ctx, valid); err != nil {
            return err
        }
    }
}

// 4. Check graph completeness
if err := g.Check(ctx); err != nil {
    return err
}

// 5. Persist snapshot
data, err := snapshot.Marshal(ctx, g.Snapshot())
if err != nil {
    return err
}
os.WriteFile("inventory.ys", data, 0644)
```
