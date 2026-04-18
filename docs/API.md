# YAMMM Go Library API Reference

This document is the API reference for the YAMMM Go library. It covers schema loading, instance validation, graph construction, snapshot persistence, adapters, and diagnostics.

The YAMMM language itself — grammar, types, expressions, constraints, and diagnostic codes — is specified in [SPEC.md](SPEC.md).

## Error Handling Conventions

All load and validation functions return `(*T, diag.Result)` pairs:

- `value != nil && result.OK()`: success (result may contain warnings)
- `value == nil && !result.OK()`: failure

Use `result.Err()` to convert to a standard Go `error` when `!result.OK()`. Adapter constructors and pure transformations (serialization, query generation) return `(T, error)` instead.

## Loading Schemas

Schemas are loaded from files or in-memory sources using the `schema` package.

### Load Functions

```go
// Load from file path
s, result := schema.Load(ctx, "path/to/schema.yammm", opts...)

// Load from string content (sourceCode first, then sourceName)
s, result := schema.LoadString(ctx, content, "source-name", opts...)

// Load from in-memory sources with import resolution (moduleRoot is required)
s, result := schema.LoadSources(ctx, sources, moduleRoot, opts...)

// Load from in-memory sources with an explicit entry point
// Useful in LSP scenarios where multiple documents are open but only one is being analyzed
s, result := schema.LoadSourcesWithEntry(ctx, sources, entryPath, moduleRoot, opts...)
```

### Load Options

| Option | Description |
| ------ | ----------- |
| `WithRegistry` | Schema registry for cross-schema type resolution |
| `WithModuleRoot` | Root directory for module-style imports |
| `WithIssueLimit` | Maximum diagnostic issues to collect (default: 100) |
| `WithSourceRegistry` | Source registry for position tracking |
| `WithLogger` | Structured logger for load diagnostics |
| `WithDisallowImports` | Prevent import declarations from being processed |

### Error Handling Pattern

All load functions return `(*Schema, diag.Result)`:

- `schema == nil && !result.OK()`: Failure (syntax errors, type resolution failures)
- `schema != nil && result.OK()`: Success (result may contain warnings)

```go
s, result := schema.Load(ctx, "schema.yammm")
if !result.OK() {
    // Use diag.Renderer to format issues
    return fmt.Errorf("schema errors: %v", result)
}
// Use s
```

## Building Schemas Programmatically

The `schema` package provides a fluent builder API for constructing schemas without parsing `.yammm` files.

```go
s, result := schema.NewBuilder().
    WithName("MySchema").
    WithSourceID(location.MustNewSourceID("test://my-schema.yammm")).
    AddType("Person").
        WithProperty("name", schema.NewStringConstraint()).
        WithOptionalProperty("age", schema.IntegerBetween(0, 150)).
        Done().
    AddType("Car").
        WithPrimaryKey("vin", schema.NewStringConstraint()).
        WithRelation("OWNER", schema.NewTypeRef("", "Person", location.Span{}), false, false).
        Done().
    Build()
```

### Builder Methods

| Method | Description |
| ------ | ----------- |
| `NewBuilder()` | Create a new schema builder |
| `WithName(name)` | Set the schema name |
| `WithSourceID(id)` | Set the source ID (required if `AddImport` is used) |
| `WithDocumentation(doc)` | Set schema-level documentation |
| `WithRegistry(r)` | Provide a schema registry for cross-schema type resolution |
| `WithIssueLimit(limit)` | Maximum diagnostics to collect (default: 100) |
| `WithImportResolver(resolver)` | Custom resolver for import paths (needed for synthetic source IDs with relative imports) |
| `AddImport(path, alias)` | Add an import declaration |
| `AddType(name)` | Begin building a type definition (returns `TypeBuilder`) |
| `AddDataType(name, constraint)` | Add a named data type alias |
| `Build()` | Construct the final `*Schema` from builder state |

### TypeBuilder Methods

| Method | Description |
| ------ | ----------- |
| `WithProperty(name, constraint)` | Add a required property |
| `WithOptionalProperty(name, constraint)` | Add an optional property |
| `WithPrimaryKey(name, constraint)` | Add a primary key property |
| `WithRelation(name, target, optional, many)` | Add an association |
| `WithComposition(name, target, optional, many)` | Add a composition |
| `Extends(ref)` | Add a parent type for inheritance |
| `AsPart()` | Mark the type as a part type |
| `AsAbstract()` | Mark the type as abstract |
| `WithTypeDocumentation(doc)` | Set documentation for the type |
| `WithInvariant(name, expr, doc)` | Add an invariant constraint |
| `Done()` | Complete the type definition and return to the parent `Builder` |

## Instance Validation

The `instance` package validates Go data against compiled schemas. Each instance is represented as an `instance.RawInstance` struct:

```go
type RawInstance struct {
    Properties map[string]any         // raw property values keyed by name
    Provenance *location.Provenance   // optional source location metadata for error reporting
}
```

Go structs with typed fields must be marshaled to JSON and unmarshaled into `map[string]any` before validation.

### Validator Creation

```go
validator := instance.NewValidator(schema, opts...)
```

### Validator Options

| Option | Description |
| ------ | ----------- |
| `WithLogger` | Structured logger for debug output |
| `WithStrictPropertyNames` | Require exact case matching (default: false) |
| `WithAllowUnknownFields` | Silently ignore unknown fields (default: false) |
| `WithMaxIssuesPerInstance` | Maximum issues per instance (default: 100) |
| `WithValueRegistry` | Custom value registry for type classification |

The `RecommendedOptions()` function returns a curated set of defaults (`WithStrictPropertyNames(true)`, `WithAllowUnknownFields(false)`) as a starting point for common use cases.

### Validation

```go
// Validate a batch of instances for a given type
valid, result := validator.Validate(ctx, "Person", rawInstances)
if !result.OK() {
    // Use diag.Renderer to format issues
    return fmt.Errorf("validation errors: %v", result)
}
// Process valid instances

// Validate a single instance
one, result := validator.ValidateOne(ctx, "Person", rawInstance)

// Validate instances in a composition context (part types allowed,
// primary key enforcement relaxed for composed children)
composed, result := validator.ValidateForComposition(ctx, "Car", "WHEELS", rawWheels)
```

> **Note:** Passing an unknown type name to `Validate` or `ValidateOne` produces a validation failure (via `diag.Result`), not a panic or unexpected state.

### Expected Instance Shape

Instance data is a top-level object keyed by type names whose values are arrays of instances:

```json
{
  "Person": [
    { "id": "550e8400-e29b-41d4-a716-446655440000", "name": "Alice", "age": 30 }
  ],
  "Car": [
    {
      "id": "660e8400-e29b-41d4-a716-446655440001",
      "vin": "ABC123",
      "owner": { "_target_id": "550e8400-e29b-41d4-a716-446655440000" }
    }
  ]
}
```

### Input Format

`RawInstance.Properties` is `map[string]any`. Go structs with typed fields must be marshaled to JSON and unmarshaled into `map[string]any` before validation. Property names may use any casing; the validator normalizes them.

## Graph Construction

The `graph` package builds an in-memory graph from validated instances.

### Graph Options

| Option | Description |
| ------ | ----------- |
| `WithLogger` | Structured logger for graph operations (additions, edge resolution, duplicates) |

### Graph Operations

```go
g := graph.New(schema, graph.WithLogger(logger))

// Add validated instances
result := g.Add(ctx, validInstance)
if !result.OK() {
    // Handle error
}

// Add a composed child (part type instance embedded in a parent)
result = g.AddComposed(ctx, "Car", "vin-123", "WHEELS", composedChild)

// Check completeness (required associations)
result = g.Check(ctx)

// Get immutable snapshot
snap := g.Snapshot()
for _, typeName := range snap.Types() {
    for _, inst := range snap.InstancesOf(typeName) {
        // Process instances
    }
}
```

### Seeding from a Snapshot

A new mutable `Graph` can be seeded from an existing `Snapshot`, enabling incremental graph building on top of persisted or previously-constructed state:

```go
// Create a graph pre-populated with an existing snapshot's data
g := graph.NewFromSnapshot(schema, existingSnap, opts...)

// New instances can be added on top of the imported data
result := g.Add(ctx, newInstance)

// Edges referencing imported instances resolve automatically
snap := g.Snapshot()
```

`NewFromSnapshot` imports all instances, edges, duplicates, and unresolved records from the source snapshot. Subsequent `Add` calls may resolve previously-unresolved edges if they supply the missing targets.

### Rebuilding from Parts

The `RebuildSnapshot` function constructs a `Snapshot` directly from pre-resolved data without running the graph construction pipeline:

```go
snap, result := graph.RebuildSnapshot(schema, parts)
```

The `SnapshotParts` struct holds fully-resolved instances, edges, duplicates, and unresolved records using value types (`InstanceParts`, `EdgeParts`, `DuplicateParts`, `UnresolvedParts`). Pointer-based cross-references are resolved internally.

Most users should construct snapshots via `Graph.Add` + `Graph.Snapshot`. `RebuildSnapshot` exists for the `snapshot.Load` deserialization path and testing.

### Snapshot Methods

The `Snapshot` type provides read-only access to graph state:

| Method | Description |
| ------ | ----------- |
| `Schema()` | The schema used for validation |
| `Types()` | All type names (sorted) |
| `InstancesOf(typeName)` | Instances of a type (sorted by primary key) |
| `Instances()` | Map of type name to instance slices (non-deterministic iteration order) |
| `AllInstances()` | Iterator over all instances in deterministic order |
| `InstanceByKey(typeName, key)` | O(1) lookup by type and primary key |
| `Edges()` | All resolved edges (sorted) |
| `EdgesFrom(inst)` | Outgoing edges for a specific instance |
| `Duplicates()` | Duplicate primary key records (sorted) |
| `Unresolved()` | Unresolved edge records (sorted) |
| `Diagnostics()` | Construction diagnostics |
| `OK()` | No fatal or error diagnostics |
| `HasErrors()` | Has error-level diagnostics |

### Thread Safety

- `Graph` is safe for concurrent `Add` and `AddComposed` calls
- `Snapshot` values are immutable and safe for concurrent reads
- All output slices are deterministically sorted

### Ordering Guarantees

- `Snapshot.Types()`: Lexicographic by type name
- `Snapshot.InstancesOf()`: Lexicographic by primary key
- `Snapshot.Edges()`: Lexicographic tuple (sourceType, sourceKey, relation, targetType, targetKey)
- `Snapshot.Duplicates()`: Lexicographic by (typeName, primaryKey)
- `Snapshot.Unresolved()`: Lexicographic by (sourceType, sourceKey, relation, targetType, targetKey)

The `Instances()` map has non-deterministic iteration order per Go semantics. For deterministic iteration, use `AllInstances()` (iterator) or `Types()` + `InstancesOf()` (slice-based).

### Graph Traversal

The `graph/walk` package provides a visitor-pattern traversal over a `Snapshot`:

```go
err := walk.Walk(ctx, snap, visitor, walk.WithLogger(logger))
```

Traversal is deterministic: types are visited lexicographically, instances by primary key, properties alphabetically, edges in sorted order, and compositions by relation name. The walker returns on the first error from a visitor method or on context cancellation.

Implement the `walk.Visitor` interface (or embed `walk.BaseVisitor` for no-op defaults):

```go
type Visitor interface {
    EnterInstance(inst *graph.Instance) error
    ExitInstance(inst *graph.Instance) error
    VisitProperty(inst *graph.Instance, name string, value immutable.Value) error
    VisitEdge(edge *graph.Edge) error
    EnterComposition(inst *graph.Instance, relationName string) error
    ExitComposition(inst *graph.Instance, relationName string) error
}
```

## Schema Identity

The `schema` package provides a content-based hashing function for structural compatibility checks:

```go
hash := schema.StructuralHash(s) // returns "sha256:<hex>"
```

`StructuralHash` computes a deterministic hash of a schema's structural shape. Two schemas produce the same hash if and only if they define the same types, properties, relations, compositions, data types, and constraints (by name, kind, and parameters).

Invariants are deliberately excluded from the hash — they constrain runtime validation but do not affect structural shape.

The hash is used by the `snapshot` package to verify that a persisted snapshot is compatible with the schema provided at load time. `StructuralHashVersion` (currently `1`) identifies the hashing algorithm version.

## Snapshot Persistence

The `snapshot` package serializes and deserializes `graph.Snapshot` values to and from the yammm snapshot persistence format (`.ys`).

### File Format

The `.ys` format is JSON-based and preserves full structural fidelity: instances with properties, primary keys, edges, compositions, provenance, duplicates, and unresolved edge records all survive a `Marshal`/`Load` round-trip.

The format includes:

- A **version** field for format evolution
- A **schema structural hash** for compatibility verification (see [Schema Identity](#schema-identity))
- An **integrity hash** (SHA-256 over the document body) for corruption detection
- A **features array** for forward compatibility

### Functions

```go
// Serialize a snapshot to .ys bytes (deterministic by default)
data, result := snapshot.Marshal(ctx, snap, opts...)

// Deserialize .ys bytes back to a snapshot (verifies schema compatibility)
snap, result := snapshot.Load(ctx, data, schema, loadOpts...)

// Validate a .ys file without materializing a snapshot
// Memory usage is O(keys + edge references)
result := snapshot.Verify(ctx, data, schema, loadOpts...)

// Read summary metadata and statistics without full deserialization
// Memory usage is constant regardless of snapshot size
info, result := snapshot.Info(ctx, data)

// Read header metadata only, from []byte — skips instance body and
// integrity check. Cost is proportional to the header size (< 1 KiB
// typical), not the total file size.
header, result := snapshot.HeaderOnly(ctx, data)

// Streaming sibling: read header metadata from any io.Reader without
// materializing the full document into memory. Reads at most
// snapshot.MaxHeaderSize (64 KiB) bytes from the reader.
header, result := snapshot.HeaderOnlyRead(ctx, r)

// Write .ys bytes atomically to disk — tmp+fsync+rename. Consumers
// needing crash-safe persistence should use this instead of os.WriteFile.
if err := snapshot.WriteFile(path, data); err != nil { /* ... */ }
```

`Load` does not re-validate instance data — the persisted snapshot is assumed to contain valid data. However, `Load` performs structural validation of the `.ys` format itself and verifies schema compatibility using `schema.StructuralHash`.

`HeaderOnly` and `HeaderOnlyRead` intentionally skip integrity verification — the returned `HeaderInfo.IntegrityHash` is the stored value, not a verification result. Use `Verify` when the document's hash must be confirmed. Neither function consults a schema; dispatch callers use the `HeaderInfo.SchemaHashMatches(s)` helper (see [Snapshot Info](#snapshot-info) below) to compare the stored schema hash against a loaded `*schema.Schema`.

### Marshal Options

| Option | Description |
| ------ | ----------- |
| `WithIndent` | Indentation string (`""` for compact, `"\t"` for tabs) |
| `WithCreatedAt` | Set `created_at` timestamp (omitted by default for determinism) |
| `WithMetadata` | User-provided key-value annotations |

### Load Options

| Option | Description |
| ------ | ----------- |
| `WithSkipIntegrityCheck` | Disable integrity hash verification (for debugging hand-edited files) |

### Snapshot Info

`Info` returns a `SnapshotInfo` struct:

```go
type SnapshotInfo struct {
    // Header fields.
    Version             int
    Features            []string
    SchemaName          string
    SchemaSource        string
    SchemaHash          string
    SchemaHashAlgorithm int
    IntegrityHash       string
    CreatedAt           string            // RFC 3339 or empty
    Metadata            map[string]string // user-provided annotations, nil if absent

    // Content summary.
    Types           []string
    InstanceCounts  map[string]int // type name -> count
    TotalInstances  int
    TotalEdges      int
    DuplicateCount  int
    UnresolvedCount int

    // File metadata.
    FileSize        int64  // len(data)
    IntegrityStatus string // "ok", "mismatch", or "skipped"
}
```

### Header-Only Reads

For dispatch-style workloads that classify many `.ys` files by header metadata alone — lifecycle state, schema-hash comparison, `CreatedAt` inspection — `HeaderOnly` (byte-slice variant) and `HeaderOnlyRead` (streaming io.Reader variant) return a compact `HeaderInfo`:

```go
type HeaderInfo struct {
    // Header fields (identical to the SnapshotInfo header block).
    Version             int
    Features            []string
    SchemaName          string
    SchemaSource        string
    SchemaHash          string
    SchemaHashAlgorithm int
    IntegrityHash       string
    CreatedAt           string
    Metadata            map[string]string

    // Types array (adjacent to the header; read in the same streaming pass).
    Types []string

    // File metadata. Populated by HeaderOnly (len(data)); zero value
    // from HeaderOnlyRead (not available from an io.Reader).
    FileSize int64
}
```

`HeaderOnlyRead` accepts any `io.Reader` and reads at most `snapshot.MaxHeaderSize = 64 * 1024` bytes. Larger headers are rejected with a Fatal-severity `E_SNAPSHOT_MALFORMED` issue whose message begins `header exceeded MaxHeaderSize` — distinguished from generic JSON-parse failures so operators can diagnose the cause. Reader errors (`io.EOF`, `io.ErrUnexpectedEOF`, or arbitrary I/O errors) during the header read surface uniformly as `E_SNAPSHOT_MALFORMED` rather than as a bare error return. Context cancellation is checked once at function entry; individual `Read` calls on the passed reader are not cancellable mid-read.

#### SchemaHashMatches — dispatch-site cross-check

Neither `HeaderOnly` nor `HeaderOnlyRead` consults a schema, so neither verifies that the stored schema hash matches the caller's loaded schema. `HeaderInfo.SchemaHashMatches(s *schema.Schema) bool` performs that comparison at the dispatch site:

```go
header, result := snapshot.HeaderOnlyRead(ctx, r)
if result.HasErrors() {
    return result.Err()
}
if !header.SchemaHashMatches(s) {
    // stale-schema path: the .ys was produced under a different
    // schema version.
    return errStaleSchema
}
```

`SchemaHashMatches` is nil-safe and returns `false` without panicking when the receiver is nil, the schema is nil, the header's `SchemaHash` is empty, the header's `SchemaHashAlgorithm` does not match `schema.StructuralHashVersion`, or `schema.StructuralHash(s)` returns an empty string. Dispatch callers treat `false` as "unknown or incompatible schema, do not proceed" rather than silently continuing.

### Atomic Writing

`snapshot.WriteFile` persists bytes to a path using the `tmp+fsync+rename` protocol — the standard crash-safe write primitive every yammm consumer needs when turning `Marshal` output into a durable `.ys` file:

```go
func WriteFile(path string, data []byte) error

const TmpSuffix = ".tmp"
```

The payload is first written to `path + snapshot.TmpSuffix`, `fsync`'d, closed, and then renamed into place. The rename is the atomic commit point: either the new file takes over (rename succeeded) or the previous file is left untouched (any earlier step failed). On any intermediate failure, `WriteFile` removes the staging file and returns an error wrapped with the failing step (`create temp`, `write temp`, `sync temp`, `close temp`, or `rename temp to final`).

`WriteFile` does not validate that `data` is a valid `.ys` document — it is a general-purpose atomic-write primitive, and callers are responsible for the payload (typically the output of `Marshal`).

**Durability semantics.** File mode is `0o666` subject to umask, matching `os.Create`; callers needing stricter permissions should `chmod` after `WriteFile` returns. The file is `fsync`'d before rename but the parent directory is NOT `fsync`'d, so on some filesystems the rename may not be durable across a crash — consumers with stronger durability requirements should fork the helper and add parent-directory fsync.

**Crash recovery.** If the process crashes between `fsync` and `rename`, a partial write may remain at `path + snapshot.TmpSuffix`. The `TmpSuffix` constant is exported so downstream primitives and consumer cleanup tools key off a single source of truth rather than hard-coding `.tmp`; the directory-iterator primitive (`ScanDir`, see [Directory Iteration](#directory-iteration) below) skips entries with the suffix automatically.

### Directory Iteration

For dispatch-style workloads that enumerate every `.ys` file in a directory — retention sweeps, link-engine discovery, operator "what's in this dir" inspection — `snapshot.ScanDir` exposes a lazy iterator over one `ScanEntry` per file, with the header parsed on-demand via `HeaderOnlyRead`:

```go
type ScanEntry struct {
    Name   string               // basename, e.g., "CA.ys"
    Path   string               // full path, filepath.Join(dir, Name)
    Header *snapshot.HeaderInfo // nil when Result has error-severity issues
    Result diag.Result          // OK on the happy path; carries errors per-file
}

// Lazy iterator — headers are parsed on demand; callers can break early
// without paying the parse cost for remaining files.
func ScanDir(ctx context.Context, dir string) iter.Seq2[ScanEntry, error]

// Materializing convenience wrapper.
func ScanDirSlice(ctx context.Context, dir string) ([]ScanEntry, diag.Result)
```

Typical call pattern:

```go
for entry, err := range snapshot.ScanDir(ctx, dir) {
    if err != nil {
        return fmt.Errorf("scan: %w", err)
    }
    if entry.Result.HasErrors() {
        log.Warn("skipping", "name", entry.Name, "err", entry.Result.Err())
        continue
    }
    use(entry.Header)
}
```

**Error surface, in two categories:**

- The iterator's second yielded value (the `error`) is non-nil ONLY for operation-level failures that end iteration: a dir-open error (`ENOENT`, `EACCES`, `ENOTDIR`, ...) is yielded as a single `(ScanEntry{}, err)` pair wrapping the underlying `os` error; context cancellation observed between files is yielded as `(ScanEntry{}, ctx.Err())`. The zero-value `ScanEntry` signals "no file was reached." Cancellation observed between files takes precedence over any concurrent per-file failure.
- Per-file failures (corrupt header, per-file `os.Open` / `Read` failure) live on `ScanEntry.Result`; the iterator's error is `nil` for those and iteration continues. Per-file I/O failures surface as a Fatal `E_SNAPSHOT_IO`; corrupt headers surface as an Error-severity `E_SNAPSHOT_MALFORMED` (both inherit from `HeaderOnlyRead`'s diagnostic surface).

**Filtering:**

- Only regular files (not directories) whose basename ends with `.ys` are included. Subdirectories are skipped even when their name ends with `.ys`.
- Files whose basename ends with `snapshot.TmpSuffix` are skipped — the atomic-write staging files that `WriteFile` may leave behind on crash. Both primitives key off the shared exported constant; the single source of truth keeps them from drifting.
- Symlinks are followed; broken symlinks yield a per-file Fatal `E_SNAPSHOT_IO` entry with the underlying `os.Open` error surfaced as a detail.
- Entries are yielded in the order returned by `os.ReadDir`, which sorts by filename.

**`ScanDirSlice` semantics:**

`ScanDirSlice` materializes the full iteration into a slice plus an outer `diag.Result`. The outer Result surfaces operation-level errors only (dir does not exist → Fatal `E_SNAPSHOT_IO`; context cancellation → Fatal `E_CONTEXT_CANCELLED`); per-file errors remain on each `ScanEntry.Result`. Context cancellation returns *partial* results — the returned slice contains entries processed before cancellation, and callers who want fail-fast-on-cancel check `result.HasFatal()` before consuming the slice.

**CLI integration.** `yammm snapshot info --dir <path>` wraps `ScanDirSlice` to produce a tabular per-file summary (text) or a `[]{name, path, header, issues}` JSON array. The flag is mutually exclusive with the positional file argument; single-file mode continues to work unchanged.

### Metadata Updates

For workflows that change header metadata on an existing `.ys` without re-serializing the body — pipeline phase transitions, operator-maintained flags, `force-complete` audit trails — `snapshot.UpdateMetadata` reuses the body bytes verbatim and recomputes only the SHA-256 integrity hash:

```go
func UpdateMetadata(
    ctx context.Context,
    data []byte,
    newMeta map[string]string,
    opts ...UpdateOption,
) ([]byte, diag.Result)

func UpdateMetadataOrReMarshal(
    ctx context.Context,
    data []byte,
    newMeta map[string]string,
    s *schema.Schema,
    opts ...UpdateOption,
) ([]byte, diag.Result)

func WithUpdateCreatedAt(t time.Time) UpdateOption
```

**When to use which.** `UpdateMetadataOrReMarshal` is the default entry point for most consumers: it runs the fast path and transparently falls back to `Load + Marshal` on any recoverable Fatal (body-offset failure, malformed header, other non-cancellation Fatal), emitting a Warning-severity `W_UPDATE_METADATA_FALLBACK` on the returned `diag.Result` so operators can observe the transition. The primitive `UpdateMetadata` stays exported for consumers who genuinely need the strict fast path — benchmarks isolating the fast-path cost, or workflows where any Load + Marshal round-trip is operationally unacceptable.

**Speedup.** On a 20 MB `.ys` input, the fast path runs ~50× faster than the equivalent `Load + Marshal` round trip on M2-class hardware (~10 ms vs ~570 ms). The lower-bound CI gate is 3×; absolute numbers will vary across hardware but the ratio is the stable invariant. The companion benchmark `BenchmarkUpdateMetadataRatio` reports the measured ratio via `b.ReportMetric("x-speedup")` on every `go test -bench` invocation.

**Preserve-vs-override.** By default `UpdateMetadata` preserves the existing `created_at` byte-for-byte from the input header. Pass `WithUpdateCreatedAt(t)` to override; there is no other way to change `created_at` via the metadata-rewrite path. `metadata` itself is replaced entirely — there is no merge. Callers retrieve the current metadata via `HeaderOnly`, copy it, apply their changes, and pass the result.

**Failure modes.** The primitive returns structured diagnostics with stable codes:

- `E_SNAPSHOT_MALFORMED` — the input header does not parse (truncated JSON, missing required fields, wrong first key). Same code `HeaderOnly` / `HeaderOnlyRead` emit for equivalent conditions.
- `E_UPDATE_METADATA_BODY_OFFSET` — the header parsed cleanly but the body-boundary tracking resolved to an unexpected byte pattern, indicating the input does not match the shape `Marshal` produces. Byte-identical recovery via the fast path is not possible; `UpdateMetadataOrReMarshal` falls back to `Load + Marshal` automatically.
- `E_CONTEXT_CANCELLED` — ctx was cancelled at entry. Propagates as cancellation without re-attempting via the slow path.

**Wire-format contracts.** The primitive depends on two contracts documented in `snapshot/wire.go`'s package Godoc: the field-order contract (top-level keys are `{yammm_snapshot, types, instances, diagnostics}` in that order) and the body-byte-range stability contract (the byte range from the `,` after the header value through the document's closing `}` is reused verbatim). Both are pinned by `TestWireFormat_TopLevelKeyOrder` and `TestWireFormat_BodySuffixContract` in `snapshot/wire_test.go`, so a future Marshal-side shape change that would silently break the primitive fails at the wire-format test level.

**CLI integration.** `yammm snapshot update-metadata --set key=value [--unset key] <file>` wraps the primitive for operator tooling. `--set` and `--unset` are both repeatable; at least one is required. The command uses the strict fast path (not the fallback wrapper) — a body-offset failure surfaces as `ExitValidation` (3) with `E_UPDATE_METADATA_BODY_OFFSET` in the diagnostic output; the recovery path is a fresh `yammm snapshot save`. The write is atomic via `snapshot.WriteFile`.

**Test helper.** `snapshot/snapshottest.ExpectMetadataPreserved(tb, before, after, preservedKeys...)` asserts that a named set of metadata keys survived a rewrite unchanged. Intended for tests exercising metadata-rewrite primitives or any workflow that must preserve an invariant key set across a transition.

## Diagnostics

The `diag` package implements YAMMM's five-level severity model. See [Severity Levels](SPEC.md#severity-levels) and [Diagnostic Codes](SPEC.md#diagnostic-codes) in the language specification for the semantic definitions.

### Result Methods

```go
// Status checks
result.OK()             // No fatal or error issues
result.HasErrors()      // Has fatal or error issues
result.HasFatal()       // Has fatal issues
result.HasWarnings()    // Has warning issues
result.HasInfo()        // Has info issues
result.HasHints()       // Has hint issues
result.LimitReached()   // Issue collection limit was reached

// Issue access (returns iter.Seq[Issue])
result.Issues()                          // All collected issues
result.Errors()                          // Fatal and error issues
result.Warnings()                        // Warning issues
result.BySeverity(diag.Warning)          // Issues at a specific severity
result.IssuesAtLeastAsSevereAs(diag.Warning) // Issues at or above a threshold

// Slice variants (returns []Issue)
result.IssuesSlice()
result.ErrorsSlice()
result.WarningsSlice()
result.BySeveritySlice(diag.Warning)          // Issues at exactly the given severity
result.IssuesAtLeastAsSevereAsSlice(diag.Warning) // Issues at or above a threshold

// Metadata
result.Len()              // Total issue count
result.Limit()            // Configured collection limit
result.DroppedCount()     // Issues dropped after limit
result.SeverityCounts()   // Counts by severity level
result.Messages()         // Fatal and error issue messages as strings
result.MessagesAtOrAbove(diag.Warning)        // Messages at or above a severity threshold

// Conversion
result.Err()              // Returns error if !OK(), nil otherwise
result.String()           // "OK" when OK(), formatted issues otherwise
```

### Rendering Diagnostics

```go
renderer := diag.NewRenderer(
    diag.WithSourceProvider(provider),   // source text for excerpts
    diag.WithExcerpts(true),             // show source excerpts
    diag.WithMaxLineColumns(120),        // max columns for excerpts
    diag.WithModuleRoot("/project"),     // strip prefix from paths
    diag.WithColors(true),              // ANSI color output
    diag.WithDistinguishFatal(true),    // distinguish fatal from error
    diag.WithTruncationIndicator("..."),  // truncation marker
)
output := renderer.FormatResult(result)

// Format a single issue
output := renderer.FormatIssue(issue)

// Format a slice of issues
output := renderer.FormatIssues(issues)
```

All renderer options are optional. The zero-config `diag.NewRenderer()` produces plain-text output without excerpts or colors.

## JSON Adapter

The `adapter/json` package parses JSON/JSONC into raw instances with optional location tracking.

### Adapter Creation

```go
adapter, err := json.New(registry, opts...)
```

The `registry` parameter is a `location.PositionRegistry` used for byte-offset-to-position conversion when location tracking is enabled. It may be `nil` when `WithTrackLocations` is not set.

### Parse Options

| Option | Description |
| ------ | ----------- |
| `WithStrictJSON` | Use stdlib JSON only (no comments/trailing commas) |
| `WithTrackLocations` | Enable source position tracking (requires non-nil registry) |
| `WithTypeField` | Field name for type tagging (default: `$type`) |

### Parsing

All parse methods accept `[]byte` data:

```go
// Parse a top-level object keyed by type name: {"Person": [...], "Car": [...]}
byType, result := adapter.ParseObject(ctx, source, data)

// Parse an array with $type fields
byType, result := adapter.ParseArray(ctx, source, data)

// Parse an array where all elements share a known type
raws, result := adapter.ParseTypedArray(ctx, source, typeName, data)

// Parse a single JSON object as a known type
raw, result := adapter.ParseOne(ctx, source, typeName, data)
```

### Serialization

```go
// Serialize a snapshot to JSON bytes
data, err := adapter.MarshalObject(ctx, snap, writeOpts...)

// Stream a snapshot to a writer (returns bytes written)
n, err := adapter.WriteObject(ctx, w, snap, writeOpts...)

// Serialize a single validated instance
data, err := adapter.MarshalInstance(ctx, inst, schemaType, writeOpts...)
```

### Write Options

| Option | Description |
| ------ | ----------- |
| `WithIndent` | Indentation string for formatted output |
| `WithDiagnostics` | Include diagnostics in output |

### JSONC Support

By default, the adapter uses `tidwall/jsonc` to preprocess input:

- Strips `//` and `/* */` comments
- Removes trailing commas
- Preserves byte offsets for accurate diagnostics

## Neo4j Adapter

The `adapter/neo4j` package generates Neo4j 5 constraint statements, label mappings, graph shape metadata, and parameterized write queries from yammm schemas. It does not import connection, session, or transaction packages — consumers supply their own driver.

### Adapter Creation

```go
adapter := neo4j.New(opts...)
```

### Options

| Option | Description |
| ------ | ----------- |
| `WithEdition` | Neo4j edition (`Enterprise` or `Community`); controls constraint types |
| `WithLabelSeparator` | Separator for multi-label nodes (default: `:`) |
| `WithLabelPrefix` | Prefix for all generated labels |
| `WithScalarTypeConstraints` | Emit `PROPERTY_TYPE` constraints (Enterprise only) |
| `WithRequiredOnlyTypeConstraints` | Emit type constraints only for required properties |
| `WithNodeKeyConstraints` | Emit `NODE KEY` constraints (requires Neo4j 5.7+) |
| `WithNamedConstraints` | Use named constraints for idempotent `IF NOT EXISTS` |

### Edition Gating

Enterprise edition supports all constraint types (UNIQUE, NOT NULL, NODE KEY, PROPERTY_TYPE). Community edition supports UNIQUE constraints only; all other types are silently omitted.

### Labels

```go
// Generate a Neo4j label for a schema type
label := adapter.Label(ctx, schemaName, typeName)

// Detect label collisions across all types in a schema
result := adapter.DetectLabelCollisions(ctx, s)
```

### Constraints

Constraints can be generated as Cypher strings or as structured values:

```go
// Generate Cypher CREATE CONSTRAINT statements
statements, result := adapter.ConstraintsForSchema(ctx, s)

// Generate structured constraint descriptors
constraints, result := adapter.ConstraintsStructured(ctx, s)
```

The `Constraint` struct contains the constraint `Name`, `Kind` (`ConstraintUnique`, `ConstraintNotNull`, `ConstraintType`, `ConstraintNodeKey`), `Label`, `Properties`, `TypeExpr`, and the complete `Statement`.

### Graph Shape

```go
// Compute the graph shape (labels, primary keys, required fields) for a schema
shapes, result := adapter.ShapeForSchema(ctx, s)
```

`ShapeForSchema` returns a `*GraphShape` containing a `Types` map of `NodeShape` values. Each `NodeShape` describes the `Type` (original yammm type name), `Label` (fully qualified Neo4j label), `PrimaryKeys`, and `RequiredFields` for a type.

### Write Modes

Write query generation supports two operational modes:

- **Graph mode:** `BatchNodeQueries` and `BatchEdgeQueries` operate on a complete `graph.Snapshot` for high-throughput batch writes
- **Instance mode:** `NodeQueryFor` accepts any `NodeSource` and `EdgeQueryFor`/`EdgeQueriesFor` generate edge queries from validated instance or edge data

`NodeQueryFor` accepts a `NodeSource` interface rather than a concrete type:

```go
type NodeSource interface {
    TypeName() string
    Properties() immutable.Properties
}
```

Both `*instance.ValidInstance` and `*graph.Instance` satisfy this interface.

```go
// Graph mode — batch queries from a snapshot
shapes, _ := adapter.ShapeForSchema(ctx, s)
nodeQueries, err := adapter.BatchNodeQueries(ctx, snap, shapes, writeOpts...)
edgeQueries, err := adapter.BatchEdgeQueries(ctx, snap, shapes, writeOpts...)

// Instance mode — single-instance queries
nodeQuery, err := adapter.NodeQueryFor(ctx, &shape, inst, schemaType, writeOpts...)
edgeQuery, err := adapter.EdgeQueryFor(ctx, edge, shapes, writeOpts...)
edgeQueries, err := adapter.EdgeQueriesFor(ctx, validInst, schemaType, shapes, writeOpts...)
```

All write methods return query structs (`NodeQuery`, `BatchNodeQuery`, `EdgeQuery`, `BatchEdgeQuery`) with `Statement` and `Params` fields, ready for driver execution.

### Write Options

| Option | Description |
| ------ | ----------- |
| `WithImmutableKeys` | Properties only set on creation, not updated |
| `WithNodeChunkSize` | `UNWIND` batch size for node queries (default: 5000) |
| `WithEdgeChunkSize` | `UNWIND` batch size for edge queries (default: 5000) |

### Cypher Builders

The four exported builders produce the Cypher templates the `Adapter` write surface uses internally. They are pure functions — no execution, no driver dependency — and are exposed for advanced consumers (e.g. link engines, custom migration tooling) that want the template without the surrounding parameter-and-chunk plumbing that `BatchNodeQueries` / `BatchEdgeQueries` provide.

```go
// Node merge templates
func BuildNodeMergeQuery(label string, keyNames []string, keys KeyMutability) string
func BuildBatchNodeMergeQuery(label string, keyNames []string, keys KeyMutability) string

// Relationship merge templates (always end with RETURN count(*) AS matched_rows)
func BuildRelationshipMergeQuery(
    fromLabel string, fromKeyNames []string,
    relType string,
    toLabel string, toKeyNames []string,
    hasProps bool,
) string
func BuildBatchRelationshipMergeQuery(
    fromLabel string, fromKeyNames []string,
    relType string,
    toLabel string, toKeyNames []string,
    hasProps bool,
) string
```

The node builders' trailing `KeyMutability` parameter (`MutableKeys` or `ImmutableKeys`) selects the SET-clause shape. `MutableKeys` emits a single `SET n += $props`; `ImmutableKeys` emits the `ON CREATE SET n += $props` / `ON MATCH SET n += $update_props` split, and requires the caller to supply `$update_props` in the parameter map. The enum is complementary to `WithImmutableKeys`: the enum selects the template shape (per-call), while `WithImmutableKeys` at the `Adapter` layer carries the property-name filter that feeds `$update_props` at write time.

Both relationship builders always end with `RETURN count(*) AS matched_rows`. The returned column reflects this call's (or this chunk's) MERGE match count only — 0 when the MERGE is a structural no-op (silent-failure condition), 1 (or the row count) when the relationship exists after the call. Consumers issuing multiple calls or chunks are responsible for summing `matched_rows` across results to detect silent no-ops. Node builders stay `RETURN`-free — constraint violations on nodes surface as driver errors, not silent zero-matches, so there is no analogous guard to emit.

| Type / Constant | Description |
| --------------- | ----------- |
| `KeyMutability` | Enum selecting the node-builder SET-clause shape. Complementary to `WithImmutableKeys`. |
| `MutableKeys` | Single `SET` clause; primary-key and property values are rewritten on MATCH. |
| `ImmutableKeys` | `ON CREATE SET` / `ON MATCH SET` split; caller must supply `$update_props`. |

For routine use, prefer `Adapter.BatchNodeQueries` / `Adapter.BatchEdgeQueries` — they call the same builders internally and handle parameter construction, chunking, and schema-aware property coercion.

### Schema Inference

```go
// Generate a .yammm scaffold from introspected Neo4j constraints and relationships
yammmSource, err := adapter.InferSchema(constraints, relationships, schemaFilter)
```

`InferSchema` takes `[]RemoteConstraint` and `[]RemoteRelationship` values (obtained from introspection queries) and produces a `.yammm` source string. Helper functions `IntrospectConstraintsQuery`, `IntrospectRelationshipsQuery`, `ParseRemoteConstraints`, and `ParseRemoteRelationships` assist with gathering introspection data from a live database.

### Constraint Diffing

```go
// Compute the semantic diff between desired schema constraints and actual database constraints
diff := adapter.DiffConstraints(desired, actual, schemaName)
```

`DiffConstraints` returns a `*ConstraintDiffResult` with `Match` (identical), `Drift` (same key, different definition), `Create` (missing from database), and `Drop` (in database but not in schema) sets.

### Introspection Queries

```go
// Get a Cypher query for introspecting relationship topology
query, params := adapter.IntrospectRelationshipsQueryFor(schemaFilter)
```

This returns a parameterized Cypher query string and parameters — consumers execute it against their own driver. Package-level helpers assist with gathering introspection data:

| Function | Description |
| -------- | ----------- |
| `IntrospectConstraintsQuery()` | Static Cypher for `SHOW CONSTRAINTS YIELD *` |
| `IntrospectIndexesQuery()` | Static Cypher for `SHOW INDEXES YIELD *` |
| `IntrospectRelationshipsQuery(labelPrefix)` | Parameterized Cypher for relationship topology discovery |
| `ParseRemoteConstraints(records)` | Parse driver output into `[]RemoteConstraint` |
| `ParseRemoteRelationships(records)` | Parse driver output into `[]RemoteRelationship` |

The introspection types are:

- `RemoteConstraint` — constraint metadata (name, type, entity type, labels/types, properties, property type)
- `RemoteRelationship` — relationship topology (relation type, source/target labels)
- `RemoteIndex` — index metadata (name, type, entity type, labels/types, properties)

### Utility Functions

| Function | Description |
| -------- | ----------- |
| `SanitizeIdentifier(s)` | Escape a string for use as a Neo4j label or property name |
| `ValidateIdentifier(name, context)` | Validate that a name is a legal Neo4j identifier |
| `CypherReservedKeywords()` | Return the set of Cypher reserved keywords |

## CSV Adapter

The `adapter/csv` package parses delimited data into `instance.RawInstance` values and serializes validated instances to CSV.

### Adapter Creation

```go
adapter := csv.New(opts...)
```

### Parse Options

| Option | Description |
| ------ | ----------- |
| `WithDelimiter` | Field delimiter rune (default: `,`) |
| `WithHeader` | Whether input has a header row (default: true) |
| `WithTypeColumn` | Column name for type tagging |
| `WithNullValue` | String treated as nil (default: `""`) |
| `WithListSeparator` | Separator for list values (default: `\|`) |

### Parsing

Parse methods require a `*schema.Type` for type coercion. All accept an `io.Reader`:

```go
// Parse rows where all rows share a known type
raws, result := adapter.ParseTyped(ctx, source, typeName, reader, schemaType)

// Parse rows with a type-discriminator column (requires WithTypeColumn)
byType, result := adapter.ParseWithTypeColumn(ctx, source, reader, typeResolver)

// Parse a single pre-split row
raw, result := adapter.ParseOne(ctx, source, typeName, columns, row, schemaType)
```

The `typeResolver` parameter is a `func(string) *schema.Type` that maps type column values to schema types.

### Serialization

```go
// Serialize instances of a single type
data, err := adapter.MarshalTyped(ctx, instances, schemaType, writeOpts...)
n, err := adapter.WriteTyped(ctx, w, instances, schemaType, writeOpts...)

// Serialize a full snapshot (returns one []byte per type)
byType, err := adapter.MarshalSnapshot(ctx, snap, writeOpts...)

// Stream a snapshot (writerFor provides a writer per type)
err := adapter.WriteSnapshot(ctx, writerFor, snap, writeOpts...)
```

### Write Options

| Option | Description |
| ------ | ----------- |
| `WithWriteHeader` | Include header row in output |
| `WithWriteNullString` | String to emit for nil values |

### Type Coercion

CSV values are strings. The adapter uses schema constraint metadata to coerce values:

- **Integer**: `strconv.ParseInt`
- **Float**: `strconv.ParseFloat`
- **Boolean**: `strconv.ParseBool` (`"true"`, `"false"`, `"1"`, `"0"`)
- **Date**: validated as `"2006-01-02"` format, kept as string
- **Timestamp**: validated as RFC 3339, kept as string
- **List**: split by list separator, elements coerced recursively

### Limitations

CSV is a flat format. Compositions are not supported in parsing and are silently omitted during serialization. Foreign key columns are included in headers but require `instance.ValidInstance` values (not `graph.Instance`) to populate edge data.

## Formatting

The `format` package provides canonical formatting for `.yammm` schema files:

```go
func TokenStream(text string) (string, error)
```

```go
formatted, err := format.TokenStream(input)
```

The formatter applies a five-phase pipeline:

1. **Token-stream rewriting:** canonical spacing between tokens and indentation normalization
2. **Blank line collapsing:** removes excess blank lines while preserving section breaks
3. **Line wrapping:** wraps long lines (enums, extends clauses, invariants) at the threshold
4. **Column alignment:** aligns property types and modifiers within type blocks
5. **Text finalization:** trims trailing whitespace from each line, removes trailing blank lines, and ensures the file ends with a newline

Output is deterministic and idempotent. The formatter is used by the LSP server for `textDocument/formatting` and by the CLI for the `yammm fmt` command.
