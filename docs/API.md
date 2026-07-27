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
| `WithLogger` | Structured logger for load diagnostics |
| `WithDisallowImports` | Prevent import declarations from being processed |
| `WithSourcesOnly` | Restrict import resolution to pre-registered in-memory sources — a miss errors instead of reading the filesystem (hermetic loads of embedded sources) |

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

### Diagnostic Completeness

One load pass reports every *independent* error in the schema and its import
closure — each exactly once:

- **Import failures accumulate.** Every unresolvable or failed import is
  reported at its own declaration; one broken import neither hides its
  siblings nor suppresses the source's own semantic diagnostics. A shared
  broken import in a diamond-shaped graph is compiled once — its own
  diagnostics appear once, and each importer adds one `E_UPSTREAM_FAIL` at
  its own declaration.
- **References through a failed import are deferred, not re-blamed.** An
  alias whose import failed produces one diagnostic at the import
  declaration; `extends` clauses, relation targets, and primary-key types
  reached through that alias are skipped silently. A qualifier that names
  no declared import at all remains a genuine `E_UNKNOWN_TYPE`.
- **An alias binds once (keep-first).** The first declaration of an alias
  wins at every layer; a later declaration of the same alias is reported
  with `E_DUPLICATE_IMPORT` and stays inert — not loaded, not resolved,
  not wired. References through the alias resolve against the kept first
  binding.
- **The all-or-nothing contract is unchanged.** Any error still yields a
  nil `*Schema`; completeness changes what `result` carries, not the
  contract.

`LoadString` — and any load with `WithDisallowImports` — still rejects
import declarations categorically with a single `E_IMPORT_NOT_ALLOWED`, but
the rejection no longer suppresses the source's other findings: the
remaining diagnostics are reported alongside it, with references through
the rejected aliases deferred. Rejected imports are never probed or
resolved.

#### Issue limit

`WithIssueLimit` bounds *collection*: once the limit is reached, further
issues are dropped — counted in `Result.DroppedCount()` and flagged by
`Result.LimitReached()`. Which issues survive the cap is collection-order
dependent (declaration order, so earlier declarations win cap survival);
the *display* order of surviving issues is always deterministic.
Dropped issues still count toward `Result.OK()` / `HasErrors()` /
`SeverityCounts()` (the counts reflect every issue *seen*, not only those
stored), so truncation never flips a failing result to OK and the
all-or-nothing contract holds regardless of the limit.
`WithIssueLimit(0)` (or `diag.NoLimit`) means unlimited. The JSON output
format carries `limit`, `limitReached`, and `droppedCount` whenever the cap
was hit (including a truncated result with no errors); the CLI's text output
appends a dropped-issues note.

### Shared Registry Semantics

Passing the same `*Registry` to multiple `Load` calls in one process is safe and efficient (since v0.3.0). The behavior has two coordinated parts:

1. **`Registry.Register` is idempotent for exact-match.** Registering the same `SourceID` twice with an identical `StructuralHash` is a no-op — the first pointer remains stored and type-index entries are not re-indexed. Divergent content under the same `SourceID` still returns `*RegistryError{Kind: DuplicateSourceID}`; the error message carries both structural hashes so the mismatch is diagnosable.
2. **`loadImport` short-circuits cross-`Load` via the shared `Registry`.** When an import's `SourceID` is already present in the shared registry, the loader reuses the existing `*Schema` pointer and skips the read, parse, compile, and re-register pipeline entirely. This is where cross-pipeline schema caching pays off.

```go
reg := schema.NewRegistry()

// First Load registers book_catalog as a transitive import of catalog_geo.
sA, _ := schema.Load(ctx, "catalog_geo.yammm",
    schema.WithRegistry(reg), schema.WithModuleRoot(moduleRoot))

// Second Load re-uses book_catalog from the registry — no re-parse.
sB, _ := schema.Load(ctx, "store_promotions.yammm",
    schema.WithRegistry(reg), schema.WithModuleRoot(moduleRoot))

// reg.Len() == 3: [book_catalog (shared), catalog_geo, store_promotions]
```

**Top-level asymmetry.** The cross-`Load` short-circuit fires only for *imports*. The top-level schema returned by each `Load` call is always a fresh compile. Calling `Load(A, WithRegistry(reg))` twice produces two distinct `*Schema` pointers; `reg.LookupBySourceID(AID)` continues to return the first call's pointer (the idempotent `Register` does not overwrite).

**SourceID discipline.** Cross-`Load` sharing only fires when imports resolve to the same `SourceID`. `SourceID`s for file-backed schemas derive from the canonical absolute path, so `WithModuleRoot` values that resolve to different canonical paths for the same file yield different `SourceID`s and do not share. For `LoadString`, the synthetic `string://<sourceName>` scheme means two `LoadString` calls sharing a Registry must use distinct `sourceName` values unless they are intentionally re-registering byte-identical content.

**Default behavior unchanged.** When `WithRegistry` is absent, each `Load` constructs its own `Registry` — safe for any usage pattern. The defensive `WithRegistry(schema.NewRegistry())` pattern some consumers carry is still valid post-v0.3.0; it is simply no longer necessary.

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

`Build()` validates declared names against the DSL's own productions, so every
builder-built schema remains expressible in `.yammm` form: type and datatype
names start with an uppercase letter, property names with a lowercase letter,
and relation names with a letter of either case — all continuing with letters,
digits, or underscores. Violations fail the build with `E_INVALID_NAME`.
Schema names and invariant names are quoted strings in the DSL and stay
free-form; import aliases are validated during completion (`E_INVALID_ALIAS`).

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
| `WithTypeAnnotation(name, args...)` | Add a type-level `@@name(args)` annotation |
| `WithPropertyAnnotation(propertyName, name, args...)` | Add a `@name(args)` annotation to a property |
| `Done()` | Complete the type definition and return to the parent `Builder` |

### Annotations

Annotations declared in `.yammm` (or via the Builder methods above) are carried on the loaded schema:

- `Property.Annotations()` / `AnnotationsSlice()` / `Annotation(name) (*Annotation, bool)` — a property's `@name` annotations.
- `Type.Annotations()` / `AllAnnotations()` (with `*Slice` variants) — a type's `@@` members (own, and own-plus-inherited).
- `Annotation` exposes `Name()`, `Args() []AnnotationArg`, `Documentation()`, `Span()`; `AnnotationArg` exposes `Text()`, `Kind() AnnotationArgKind`, `Span()`.
- `schema.AnnotationSpecs() []AnnotationSpec` returns the built-in registry for editor tooling — a read-only surface, not a registration API. Each `AnnotationSpec` exposes `Name()`, `Placement() AnnotationPlacement` (`PlacementProperty` or `PlacementType`), `Documentation()`, and `ArgHint()`.
- `schema.VectorSimilarityFunctions() []string` returns the similarity keywords `@vector` accepts, so tooling cannot suggest one the loader rejects.

Annotation structure and eligibility are validated at load; see [SPEC.md](SPEC.md#annotations) for the blessed set and the diagnostics they produce.

**Merged views and property identity.** A property inherited from several ancestors carries the union of their annotations, so the entry in `Type.AllProperties()` / `AllPropertiesSlice()` may be a *synthesized* copy rather than the `*Property` its declaring type holds in `PropertiesSlice()`. `Property.Origin() *Property` returns the declared property — itself when nothing was merged — and is the correct key for any map built from declared properties and read back over a merged view. Everything `Origin()` discards is annotation state: name, constraint, datatype reference, optionality, primary-key status, span, and declaring scope are preserved on the copy.

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

Type names passed to the validator (`Validate`, `ValidateOne`, and `ValidateForComposition`'s parent tag) are entry-schema-relative: bare names for entry-schema types, alias-qualified names (`common.Region`) for directly imported ones. Relation *targets*, by contrast, are resolved internally by the absolute identity completion records (`Relation.TargetID`), never by re-reading the declared target name against the entry schema — so associations and compositions declared on imported types, or inherited from cross-schema parents, validate against the true target type even when the entry schema does not know the declaring schema's aliases or shadows the target's bare name with a type of its own.

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

### Schema-Aware Raw Instance Builder

`instance.BuilderFor(schema, typeName)` returns a `*SchemaBuilder` that constructs `RawInstance` values while enforcing schema shape at build time — unknown properties, unknown relations, cardinality mismatches, and the `EdgeTo`-vs-`EdgeToWith` split all surface from `Build()` with file:line locators captured via `runtime.Callers`. This shifts shape failures out of `ValidateOne`'s domain and eliminates the "Schema declares X (one), so yammm expects a single edge object" class of hand-maintained comments in consumer code:

```go
b, err := instance.BuilderFor(s, "Person")
if err != nil {
    return instance.RawInstance{}, err
}
raw, err := b.
    Property("id", "p1").
    Property("name", "Alice").
    EdgeTo("works_at", "c1").                     // "one" association, single PK
    EdgeTo("knows", "p2").                        // "many" association
    EdgeTo("part_of", "publisher-1", "book-99").  // composite PK (variadic)
    Build()
```

For associations that declare edge properties, use `EdgeToWith`:

```go
raw, err := b.
    Property("id", "e1").
    EdgeToWith("works_at", []any{"c1"}, map[string]any{
        "role":  "Engineer",
        "since": "2020-01-01",
    }).
    Build()
```

For compositions, pass child builders — the parent enforces target-type matching at build time:

```go
addr, _ := instance.BuilderFor(s, "Address")
raw, err := b.
    Property("id", "p1").
    Composed("addresses", addr.Property("id", "a1").Property("street", "Main St")).
    Build()
```

The variadic `Composed(name, children...)` accepts single, multiple-positional, and slice-unpack call shapes interchangeably; all three produce identical output.

#### Methods

| Method | Purpose |
| ------ | ------- |
| `BuilderFor(s, typeName)` | Construct a builder bound to a schema type. Errors on nil schema, unknown type, or abstract type. |
| `Property(name, value)` | Set a property value. Unknown names accumulate as errors surfaced at Build. |
| `EdgeTo(name, targetKey...)` | Add an edge target on an association without edge properties. Variadic key supports composite PKs. |
| `EdgeToWith(name, targetKey, props)` | Add an edge target on an association with edge properties. `targetKey` is an explicit `[]any` to avoid absorbing `props` into variadic slots. |
| `Composed(name, children...)` | Add composed children. Variadic supports single, multiple, or slice-unpack shapes. |
| `WithProvenance(sourceName, pathStr)` | Attach source location metadata. |
| `Build()` | Produce the `RawInstance`. Returns the first accumulated error (with a "(and N more)" suffix when more exist). |
| `MustBuild()` | Panic on error. Test-only; production code should use `Build` and propagate. |

Build errors include the bound type's name, the offending property/relation, and the caller's file:line. `errors.Unwrap` walks into composition-child chains so `errors.As` reaches the primary cause when nesting.

`SchemaBuilder` is NOT concurrent-safe; construct one per goroutine. The bound `*schema.Schema` remains safe to share across many concurrent builders.

The shape portion of `ValidateOne` — property/relation names, cardinality, composition target-type matching — is guaranteed to pass on the output of a successful `Build`. Value-level validation (constraint checks, PK coercion, reference integrity) still runs at `ValidateOne` time.

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

### Batch Assembly

`graph.BatchAssembler` composes a `Validator` and a `Graph` into a single call surface for the common pipeline pattern: validate → add → check → snapshot. The assembler encodes the ordering invariant (validate before add, check before snapshot) so consumers cannot get the sequence wrong, and is concurrent-safe so multiple goroutines may share one assembler:

```go
ba := graph.NewBatchAssembler(ctx, s,
    graph.WithValidatorOptions(instance.RecommendedOptions()...))

for i, rec := range records {
    if err := ba.Add("TypeName", buildRawInstance(rec)); err != nil {
        return fmt.Errorf("record %d: %w", i, err)
    }
}

res, err := ba.Finalize(ctx)
if err != nil {
    // res.Snapshot is still non-nil — the partial snapshot at the
    // point Check tripped is inspectable for diagnostics.
    return fmt.Errorf("batch: %w", err)
}
qualityCollector.Merge(res.Snapshot.Diagnostics())
// res.Snapshot ready for snapshot.Marshal / snapshot.WriteFile / etc.
```

**`FinalizeResult.Snapshot` is always non-nil**, on both success and error paths. The struct exists to encode this contract at the type level; callers read `res.Snapshot` directly without gating on `err == nil`. On the error path `res.Snapshot` is the partial snapshot at the point Graph.Check tripped — operators logging a failed batch see which instances were assembled before the check failure.

**Error shapes.** Both `Add` / `AddValid` and `Finalize` return errors whose cause is a `*diag.ContextualError` (see [Contextual Diagnostic Wrap](#contextual-diagnostic-wrap)). `Add`'s tag is `"<TypeName> (record #N)"` so the offending record is locatable from the error alone; `Finalize`'s tag is the fixed string `"batch_finalize"` so downstream log consumers have a stable filter key. Recover with `errors.As` or `diag.AsContextualError`.

**Post-finalize sentinel.** After `Finalize`, subsequent `Add` / `AddValid` calls return `graph.ErrAssemblerFinalized`, matchable via `errors.Is`. Consumers performing retry / cleanup logic key off the sentinel rather than the error string.

**Validator-access modes.** Default mode serializes `ValidateOne` + `Graph.Add` through an internal `sync.Mutex` — appropriate for I/O-bound consumers (streaming ETL pipelines are the canonical example) where validation is a tiny fraction of per-record wall-clock. CPU-bound consumers profile-flag validation as the hot path opt into pool mode via `graph.WithValidatorPool(n)`, which distributes N pre-constructed validators through an internal buffered channel matching the goroutine-per-CPU-core shape:

```go
ba := graph.NewBatchAssembler(ctx, s,
    graph.WithValidatorPool(runtime.NumCPU()))
// ... concurrent Adds proceed in parallel up to N validators ...
```

Pool mode preserves every external contract (error shapes, Finalize one-shot semantics, success-count monotonicity) and is a one-option swap with no caller-side ripple. `n <= 0` panics at construction.

**Concurrency contract.** `BatchAssembler` is safe for concurrent use across all methods (`Add`, `AddValid`, `Count`, `Graph`, `Finalize`). The library coordinates Add lifecycle against Finalize via an internal `sync.RWMutex` so every Add that returns nil is guaranteed to be in the finalized snapshot, and any Add that arrives after Finalize takes its lock returns `ErrAssemblerFinalized`. No external mutex required at call sites — the worker-pool pattern (one assembler shared across N scraper goroutines, coordinator goroutine calls Finalize at end-of-batch) is supported directly.

**Resuming from a prior snapshot.** `graph.NewBatchAssemblerFromSnapshot(ctx, s, snap, opts...)` constructs an assembler whose graph starts pre-populated from an existing `Snapshot` — the same import semantics as `NewFromSnapshot` (see [Seeding from a Snapshot](#seeding-from-a-snapshot)) — instead of `NewBatchAssembler`'s empty graph. Consumers that persist a batch and continue it on a later run seed a new assembler from the loaded snapshot and `Add` only the outstanding records:

```go
snap, res := snapshot.Load(ctx, data, s) // prior batch's .ys
if res.HasErrors() { /* handle */ }

ba := graph.NewBatchAssemblerFromSnapshot(ctx, s, snap,
    graph.WithValidatorOptions(instance.RecommendedOptions()...))
for _, rec := range outstanding { // e.g. resume-by-set-difference
    if err := ba.Add("TypeName", rec); err != nil { /* handle */ }
}
finRes, err := ba.Finalize(ctx)
```

New adds interact with the seeded state as if it had been assembled in the same batch: they resolve previously-unresolved edges imported from the seed, forward references resolve against seeded instances, a `(type, primary key)` collision with a seeded instance is rejected as `E_DUPLICATE_PK`, and `Finalize`'s check covers the union — a required association imported from the seed and still unresolved fails the batch with `E_UNRESOLVED_REQUIRED`. `Count()` reflects only records added through the assembler (seeded instances are not counted), and construction diagnostics are not carried over from the seed (`.ys`-loaded snapshots carry none by design; `Duplicates` and `Unresolved` are the persistent structural records, and both import). The seed snapshot must originate from the same schema — taken from a `Graph` bound to it, or loaded via `snapshot.Load` against it, which verifies structural compatibility. Every other contract — lifecycle, finalize barrier, validator-access modes, `FinalizeResult` — is identical to `NewBatchAssembler`.

**Test fixtures.** `snapshot/snapshottest` is the shared round-trip vocabulary for snapshot tests: `BuildSnapshot(tb, s, instances...)` constructs a snapshot from pre-validated instances (build them with `instance/instancetest.VI`), `AssertRoundTrip(tb, snap, s, opts...)` pins Marshal→Load structural equivalence, `AssertDeterministic(tb, snap, opts...)` pins byte-stable marshaling, and `DiffSnapshots(tb, want, got)` is the underlying go-cmp comparison — recursive over composition trees, provenance-presence-aware, and exact for same-typed numeric properties (only mixed `int64`/`float64` pairs coerce, matching the wire's whole-float narrowing).

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

To run the batch-assembly lifecycle on top of a seeded graph, `graph.NewBatchAssemblerFromSnapshot` constructs a `BatchAssembler` whose graph is seeded the same way — see [Batch Assembly](#batch-assembly).

### Rebuilding from Parts

The `RebuildSnapshot` function constructs a `Snapshot` directly from pre-resolved data without running the graph construction pipeline:

```go
snap, result := graph.RebuildSnapshot(schema, parts)
```

The `SnapshotParts` struct holds fully-resolved instances, edges, duplicates, and unresolved records using value types (`InstanceParts`, `EdgeParts`, `DuplicateParts`, `UnresolvedParts`). Pointer-based cross-references are resolved internally.

`UnresolvedParts.Properties` carries DSL-declared edge property values from the forward reference and is populated only when `Reason` is `"target_missing"` — `"absent"` and `"empty"` describe a missing/empty reference that never had a target. For documents in `.ys` wire-format version 1 (produced before yammm v0.3.0) the field is always empty; v2 documents persist the values through Marshal/Load symmetric with resolved `Edge.Properties`. See the snapshot package's [Wire Format Versions](#wire-format-versions) subsection for the v1 → v2 bump rationale.

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

## Import Closure & Type Lookup

Two `Schema` accessors expose the import closure — the schema plus every transitively imported schema — without callers hand-rolling the walk:

```go
// The schema itself, followed by every transitively imported schema:
// a breadth-first walk over each schema's imports in declaration order,
// deduplicated by SourceID (a diamond import appears once, at its
// first-reached position). Deterministic; the returned slice is a copy.
for _, sc := range s.Closure() {
    fmt.Println(sc.Name())
}

// Resolve an absolute type identity (schema.TypeID) anywhere in the closure —
// the identities completion records on relations (Relation.TargetID) and
// types (Type.ID). No local names or import aliases are involved.
if target, ok := s.TypeByID(rel.TargetID()); ok {
    fmt.Println(target.Name())
}
```

`TypeByID` returns `ok == false` for a zero or unknown `TypeID`. Both accessors compute the closure lazily on first use and cache it; like all `Schema` accessors they are safe for concurrent use. The generators (`adapter/gogen`, `adapter/jschema`, `adapter/markdown`) drive their emission walks off `Closure()`, and the instance layer resolves relation targets through `TypeByID`.

## Schema Identity

The `schema` package provides a content-based hashing function for structural compatibility checks:

```go
hash := schema.StructuralHash(s) // returns "sha256:<hex>"
```

`StructuralHash` computes a deterministic hash of a schema's structural shape. Two schemas produce the same hash if and only if they define the same types, properties, relations, compositions, data types, and constraints (by name, kind, and parameters).

Invariants and annotations are deliberately excluded from the hash — they constrain runtime validation or downstream store DDL but do not affect structural shape (what instance data is valid).

The hash is used by the `snapshot` package to verify that a persisted snapshot is compatible with the schema provided at load time. `StructuralHashVersion` (currently `1`) identifies the hashing algorithm version.

## Snapshot Persistence

The `snapshot` package serializes and deserializes `graph.Snapshot` values to and from the yammm snapshot persistence format (`.ys`).

### File Format

The `.ys` format is JSON-based and preserves full structural fidelity: instances with properties, primary keys, edges, compositions, provenance, duplicates, and unresolved edge records all survive a `Marshal`/`Load` round-trip.

The format includes:

- A **version** field for format evolution (current value: `2`, unchanged since yammm v0.3.0; see [Wire Format Versions](#wire-format-versions))
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

### Wire Format Versions

The `.ys` wire format uses an integer version field in the header for forward evolution. yammm v0.3.0 introduced the v1 → v2 bump paired with [`UnresolvedEdge.Properties`](#graph) — the new persisted `"properties"` field on unresolved-edge wire entries cannot be `omitempty`-safe alone (a pre-v0.3.0 reader would silently drop the field), so the version bump pairs with the existing unknown-version-rejection path to force older readers to error cleanly instead.

`snapshot.MinReadableVersion` (exported constant, value `1`) names the lowest version this package accepts on read paths. The `currentVersion` (unexported, value `2`, unchanged since yammm v0.3.0) is the version emitted on every write. The accept range on read is the closed interval `[MinReadableVersion, currentVersion]`; documents outside the range surface Fatal `E_SNAPSHOT_UNSUPPORTED_VERSION` with the observed version and the supported range named in the message.

**Asymmetric-reader semantics.** A v2 reader (yammm v0.3.0+) accepts both v1 and v2 documents. v1 documents simply lack the new `"properties"` field on unresolved-edge wires; the load path populates the in-memory `UnresolvedEdge.Properties` as empty, which is lossless since v1 never carried the data. A v1 reader (yammm v0.2.x and earlier) rejects v2 documents via the unknown-version path — operators running an older binary against a v0.3.0-written `.ys` see a structured diagnostic rather than a silently-incomplete document.

See [`docs/VERSIONING.md`](VERSIONING.md) for the full pre-1.0 / post-1.0 wire-format policy.

## Diagnostics

The `diag` package implements YAMMM's five-level severity model. See [Severity Levels](SPEC.md#severity-levels) and [Diagnostic Codes](SPEC.md#diagnostic-codes) in the language specification for the semantic definitions.

### Result Methods

A `Result` is produced by a `Collector` or, for the terminal one-shot case, by `diag.Collect(issues...)`. The issue iterators return `iter.Seq[Issue]`; collect a `[]Issue` with `slices.Collect(result.Errors())` when you need one.

```go
// Status checks
result.OK()             // No fatal or error issues
result.HasErrors()      // Has fatal or error issues
result.HasFatal()       // Has fatal issues
result.HasWarnings()    // Has warning issues
result.HasInfo()        // Has info issues
result.HasHints()       // Has hint issues
result.HasCode(code)    // Has an issue with the given code, at any severity
result.LimitReached()   // Issue collection limit was reached

// Issue access (returns iter.Seq[Issue]; use slices.Collect for a []Issue)
result.Issues()                          // All collected issues
result.Errors()                          // Fatal and error issues
result.Warnings()                        // Warning issues
result.BySeverity(diag.Warning)          // Issues at a specific severity
result.IssuesAtLeastAsSevereAs(diag.Warning) // Issues at or above a threshold

// Metadata
result.Len()              // Total issue count
result.Limit()            // Configured collection limit
result.DroppedCount()     // Issues dropped after limit
result.SeverityCounts()   // Counts by severity level

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

### Contextual Diagnostic Wrap

At error boundaries — the places where a diagnostic crosses from the code that produced it to the code that surfaces it — callers attach a human-readable context label to a `diag.Result` via `Result.WithContext(tag)`, which returns an `error` directly: `nil` when the result is OK, a `*diag.ContextualError` otherwise.

```go
if err := result.WithContext("schema_load"); err != nil {
    logger.Error("pipeline startup failed", slog.Any("diagnostic", err))
    return fmt.Errorf("startup: %w", err)
}
```

A single type carries the tag through error chains and structured logging:

```go
// Returned (as a non-nil error) by Result.WithContext(tag) when the result is
// not OK. Implements error and slog.LogValuer; its Unwrap returns
// *diag.ResultError so existing errors.As consumers keep working unchanged.
type ContextualError struct {
    Result diag.Result
    Tag    string
}
```

**Slog shape.** `(*ContextualError).LogValue()` returns a group with these attributes:

- `context` (string) — the tag. Always emitted.
- `code` (string) — the first error-severity issue's stable code. Omitted when the result has no error-severity issue with a non-zero code.
- `counts` (group) — `{errors: int, warnings: int}`. `errors` sums Fatal + Error; `warnings` is the Warning count. Always emitted.
- `issues` (slice of objects) — one entry per issue, each carrying `severity`, `message`, and optional `code`, `path`, `location:{source,line,column}`, `hint`, `details:{...}`. Always emitted as a slice. Log aggregators iterate the slice directly — there are no positional `issue_0`, `issue_1`… attributes.

`Issue.LogValue()` emits the same per-issue shape and is independently useful when a consumer wants to log a single issue: `logger.Error("problem", slog.Any("issue", issue))`.

**Error-chain recovery.** `diag.AsContextualError(err, fallbackTag)` recovers a `*ContextualError` from an arbitrarily-wrapped error. If the chain carries a `*ContextualError`, its original tag survives; if it carries only a bare `*ResultError` (from `Result.Err()` without a tag), the supplied fallbackTag is synthesized so unified error handlers see a uniform shape across both patterns:

```go
if ce, ok := diag.AsContextualError(err, "validation"); ok {
    for issue := range ce.Result.Issues() {
        // triage
    }
    logger.Error("failed", slog.Any("diagnostic", ce))
}
```

The helper walks through `fmt.Errorf("...: %w", err)` and other `Unwrap` chains transparently.

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

Generation is **all-or-nothing**: any validation error returns `(nil, result)`, so one unemittable property withholds the whole schema's DDL rather than emitting a partial script that looks complete. The one shape a *valid* schema can hit is a list whose element is itself a collection — `List<List<T>>`, `List<Vector[N]>`, or a list of a list-typed alias. Those are legal yammm but Neo4j has no nested collection property type, so they report `E_NEO4J_UNSUPPORTED_TYPE`; model the inner collection as a `part type` reached by a composition if the model must export to Neo4j. A bare `Vector` is fine — it maps to `LIST<FLOAT NOT NULL>`.

### Indexes

Indexes are derived from a schema's `@index`, `@@index`, and `@vector` annotations:

```go
// Generate Cypher CREATE INDEX / CREATE VECTOR INDEX statements
statements, result := adapter.IndexesForSchema(ctx, s)

// Generate structured index descriptors
indexes, result := adapter.IndexesStructured(ctx, s)
```

The `Index` struct contains `Name`, `Kind` (`IndexRange` or `IndexVector`), `Label`, `Properties` (declared order, significant for composites), `VectorDimensions`, `VectorSimilarity`, and the complete `Statement`. A property-level `@index` yields a single-property range index; a type-level `@@index(a, b)` yields a composite range index; a property-level `@vector(cosine|euclidean)` yields a vector index whose dimension comes from the property's `Vector[N]` constraint.

Index names are always emitted (`{label}_{props}_idx` for range, `{label}_{prop}_vector_idx` for vector). The readable name is not injective — it joins on underscores, which property names may themselves contain — so two indexes whose names would collide each receive a short deterministic digest suffix. Only names that would actually clash are suffixed. Load-time validation defers eligibility whenever a target's type cannot be resolved, so the adapter re-checks: an annotation naming a property the type does not have reports `E_NEO4J_UNKNOWN_PROPERTY`, and one naming a property whose type cannot carry the index reports `E_NEO4J_INVALID_INDEX_TARGET`. Indexes are emitted for every edition (range and vector indexes are core query features on both Community and Enterprise); abstract types are skipped, part types are not. The emitted `CREATE VECTOR INDEX ... OPTIONS` statement form requires Neo4j 5.15+.

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
| `WithImmutableKeys` | Properties only set on creation, not updated. Unions with schema-derived `@writeOnce` keys. Node merges only. |
| `WithNodeChunkSize` | `UNWIND` batch size for node queries (default: 5000) |
| `WithEdgeChunkSize` | `UNWIND` batch size for edge queries (default: 5000) |

Explicitly-passed immutable keys union with the immutable keys derived from a type's `@writeOnce` annotations, which `ShapeForSchema` records on each `NodeShape` as `ImmutableKeys`. Because they travel on the shape, `NodeQueryFor` honors `@writeOnce` even when called with a nil `*schema.Type` — the documented streaming call shape. `ImmutableKeysFor(t *schema.Type) []string` returns a type's `@writeOnce` properties (own and inherited); the effective immutable set per written type is the union of explicit and derived keys, and `BatchNodeQueries` selects the `ON CREATE` / `ON MATCH` split per type (a non-empty explicit list still splits every type, preserving the prior contract).

Only the explicitly-passed keys are validated against the schema at query-generation time: every one must name a declared property (own or inherited) of a node type being written. `NodeQueryFor` rejects a key that is not a property of its schema type (and skips both derivation and the check when `schemaType` is nil); `BatchNodeQueries` rejects a key that is a property of no node type in the snapshot, while accepting a key real for at least one written type (it may legitimately apply to a subset of a multi-type snapshot). A mistyped key would otherwise be honored silently and the real property rewritten on every re-MERGE, defeating the write-once guarantee. Derived `@writeOnce` keys are schema-true by construction and are not re-validated.

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

### Property Coercion

The write surface (`Adapter.BatchNodeQueries` / `Adapter.BatchEdgeQueries` and the single-item `NodeQueryFor` / `EdgeQueriesFor`) coerces schema-typed property values to the driver-native types Neo4j TYPE constraints require — repairing the JSON round-trip where a whole-number `Float` decodes as `int64`, and `Date` / `Timestamp` values travel as strings. The coercion chokepoint is exported so consumers writing **direct Cypher** (parameterized `MERGE` / `SET` built by hand, bypassing the `Adapter` write path) can apply the same rules:

```go
// Coerce a single SCALAR value to the driver-native type the constraint requires
// (e.g. a Timestamp["layout"] is parsed against its custom layout). Takes the
// full Constraint, not just its Kind, so the custom layout is available; the
// alias chain is resolved internally. Collection values are handled by
// CoerceParams, which element-coerces against the constraint.
func Coerce(constraint schema.Constraint, raw any) (any, error)

// ParamTypes maps a Cypher parameter name to the schema constraint its value
// must satisfy. Nested params use "outer.inner" dot-notation (e.g.
// "rows.unit_price"). The value is the full Constraint, not just its
// Kind, so List properties can be element-coerced (the element type a bare
// Kind would discard).
type ParamTypes map[string]schema.Constraint

// CoerceParams coerces every value in a parameter map against its declared
// constraint, walking one level of nested map[string]any and []map[string]any.
// Scalars route through Coerce; []any lists are element-coerced against the
// List element type. Returns the first coercion error, naming the offending key.
func CoerceParams(params map[string]any, types ParamTypes) (map[string]any, error)

// ParamTypesForType derives a ParamTypes from a schema type's properties, own
// and inherited. Pass prefix "" for top-level params, or an outer name (e.g.
// "rows") for a nested param map — keys are dot-joined to match CoerceParams.
func ParamTypesForType(t *schema.Type, prefix string) ParamTypes

// ParamTypesForMergeKeys derives a ParamTypes for the MERGE-key parameters the
// node builders read: one entry per primary key under the `key_` namespace.
// Prefix "" gives $key_<pk> (BuildNodeMergeQuery); "rows" gives row.key_<pk>
// (BuildBatchNodeMergeQuery).
func ParamTypesForMergeKeys(t *schema.Type, prefix string) ParamTypes
```

Coercion rules: `Float` ← any Go integer width (`int`, `int8`…`int64`, `uint`…`uint64`) or `float32` → `float64` (a `float64` passes through); `Timestamp` ← a string parsed against the constraint's custom Go layout when it declares one (`Timestamp["…"]`) or RFC3339 / RFC3339Nano otherwise → `time.Time` (a `time.Time` passes through); `Date` ← `"2006-01-02"` string or `time.Time` → `dbtype.Date` (a `dbtype.Date` passes through); every other scalar kind passes through unchanged. `List<T>` values are coerced element-wise into the concrete typed slice (`List<Float>` of `int64`s → `[]float64`, `List<Date>` of strings → `[]dbtype.Date`, and so on); a `List<Timestamp["…"]>` honors the element's custom layout too.

The three transforming kinds (`Float`, `Timestamp`, `Date`) are **strict** — scalar and list element alike: a value they can neither pass through as already-driver-native nor repair (a non-numeric under `Float`; a non-temporal or unparseable value under `Timestamp` / `Date`) returns an error rather than reaching the driver wrong-typed. The other scalar kinds are lenient: a correct value of those is already driver-native, so there is nothing to repair or reject, and instance validation is the type authority. A nil value always passes through; an unhandled kind also returns an error (a new `schema.ConstraintKind` is caught at build time by an exhaustiveness lint).

**When to call:** at any direct-Cypher parameter boundary that writes schema-typed `Timestamp` / `Date` / `Float` properties — scalar or `List<…>` — e.g. an enrichment `MERGE` or relationship-maintenance query built outside the `Adapter` write path. Writes that go through `Adapter.NodeQueryFor` / `BatchNodeQueries` / `EdgeQueriesFor` / `BatchEdgeQueries`, or that already pass native Go types, need no extra coercion — those coerce their own merge keys and endpoint keys as well as their properties. Because `ParamTypes` carries the full constraint, `ParamTypesForType` is the easiest way to build one; it derives the element types lists need.

**Cover the merge key too.** `ParamTypesForType` describes ONE shape: a map keyed by property name. A hand-built parameter map for `BuildNodeMergeQuery` or `BuildBatchNodeMergeQuery` also carries merge keys, which do not sit where the properties sit — and the merge key is the one value the pattern matches on, so leaving it uncoerced is the failure with no error attached: a `Date` primary key reaching the driver as a string matches no node whose property is a `DATE`, and every re-run inserts a duplicate. Take those from `ParamTypesForMergeKeys` and merge the two:

```go
// $key_<pk> at the top level, properties nested under $props.
pt := neo4j.ParamTypesForType(t, "props")
maps.Copy(pt, neo4j.ParamTypesForMergeKeys(t, ""))
params, err := neo4j.CoerceParams(params, pt)
```

Two functions rather than one because a type may declare a property literally named `key_<pk>`: in a flat row that spelling is the property, and in `BuildBatchNodeMergeQuery`'s row shape it is the merge key (the properties live nested under `row.props`). One map cannot answer both without silently choosing. `CoerceParams` walks one level of nesting, so a two-level map needs one call per nested map. The relationship builders key on two types and have no single-type equivalent; their spellings are `$from_key_<pk>` / `$to_key_<pk>` and `row.from_<pk>` / `row.to_<pk>`.

### Schema Inference

```go
// Generate a .yammm scaffold from introspected Neo4j constraints and relationships
yammmSource, err := adapter.InferSchema(constraints, relationships, schemaFilter)
```

`InferSchema` takes `[]RemoteConstraint` and `[]RemoteRelationship` values (obtained from introspection queries) and produces a `.yammm` source string. Helper functions `IntrospectConstraintsQuery`, `IntrospectRelationshipsQuery`, `ParseRemoteConstraints`, and `ParseRemoteRelationships` assist with gathering introspection data from a live database.

### Constraint Diffing

```go
// Ownership is exact set membership, computed once from the schema (see Index Diffing below)
owned := adapter.OwnedLabels(ctx, s)

// Compute the semantic diff between desired schema constraints and actual database constraints
diff := adapter.DiffConstraints(desired, actual, owned)
```

`DiffConstraints` returns a `*ConstraintDiffResult` with **five** sets: `Match` (identical), `Drift` (same identity, different definition), `Create` (missing from database), `Drop` (in database but not in schema), and `Unverified` — constraints present on both sides whose definition could not be compared because the database did not report what was needed. A TYPE constraint's enforced type is not reported at all before Neo4j 5.9, so folding `Unverified` into `Match` reports an unchecked constraint as verified. A drift gate must count it as an incomplete check, exactly as on the index side.

Both diff results also carry `Excluded int` — the number of remote objects that entered **no** set, because the comparison had nothing to say about them: the schema owns no label they carry, or they are of a kind this configuration cannot declare (a relationship constraint, a node constraint kind the DSL cannot express, and under `WithEdition(Community)` a NOT NULL or TYPE constraint). It is not drift; in a database shared with other applications a non-zero count is the normal state. It is there so that "0 to drop" cannot be read as "the database is accounted for": ownership is derived from the schema, so objects left behind by a type deleted or renamed since the last apply sit on a label no current type declares and nothing in the schema can name them. A caller reporting "in sync" should say how many objects were left out of that claim.

`DiffIndexes` and `DiffConstraints` each take a variadic list of the other's remote objects (`alsoBlocking`). Index and constraint names share ONE Neo4j namespace, and a `CREATE ... IF NOT EXISTS` under a name the database already holds is a silent no-op, so a caller that introspected both should pass both — otherwise a blocked declaration reports as a create the server ignores on every run. A constraint backed by an index appears in `SHOW INDEXES` under the constraint's name and is seen automatically; NOT NULL and TYPE constraints have no backing index and reach the index diff only this way.

Desired objects pair with remote ones in three phases, strongest evidence first: name **and** identity together, then identity alone, then name alone. Identity outranks a bare name match deliberately — a remote object whose name matches but whose definition does not is weaker evidence than one that realises the declaration exactly, so claiming by name first would let the misnamed object consume the declaration and leave the exact one reported as an orphan to drop. Within a phase, pairing is positional over the remaining candidates, which is reachable only when several are indistinguishable under that phase's evidence. The classification therefore does not depend on the order `SHOW CONSTRAINTS` or `SHOW INDEXES` returned rows in. `DiffIndexes` pairs identically.

Ownership is decided before any of this, by set membership against `OwnedLabels` — never by a rule applied to a remote object's label string. A schema named `app` does not claim the objects of a sibling named `app__legacy` because `app__legacy`'s labels are simply not in `app`'s set, not because a prefix or segment test excluded them.

### Index Diffing

```go
// Ownership is exact set membership, computed once from the schema
owned := adapter.OwnedLabels(ctx, s)

// Compute the semantic diff between desired schema indexes and actual database indexes
diff := adapter.DiffIndexes(desired, actual, owned)
```

`OwnedLabels` is the set of labels this adapter emits for the schema. The diff entry points take it rather than a schema name because ownership cannot be recovered from a label string: `Label` composes a label from a caller-configurable prefix and separator around two sanitized free-form names, so for any rule that tries to read a schema back out of a label there is a configuration, or a sibling schema name, that satisfies the rule without belonging to the schema.

`DiffIndexes` returns an `*IndexDiffResult` with **five** sets: `Match`, `Drift` (a vector index whose dimension or similarity differs, a definition change under a name the database already holds, or an index in a state that serves no queries), `Create`, `Drop`, and `Unverified`. Composite property order is significant, a deliberate divergence from `DiffConstraints`: a same-set/different-order remote index is a distinct index — create + drop when its name differs too, and drift when it holds the desired index's name (a `CREATE ... IF NOT EXISTS` under a name the database already holds is a silent no-op). A schema-owned remote index with no declaration is reported as a drop; drops are reported, never applied.

`Unverified` holds indexes that exist on both sides but whose definition could **not** be compared — the database reported no readable vector configuration (the reason names which setting was unread), or the index is still `POPULATING`. A setting the database did disclose and that disagrees outranks a second setting being unreadable, so a demonstrably wrong dimension is reported as drift rather than downgraded to unverified. They are neither confirmed in sync nor confirmed drifted. A drift gate must therefore treat a non-empty `Unverified` as an incomplete check, not a pass:

```go
diff := adapter.DiffIndexes(desired, actual, owned)
inSync := len(diff.Drift) == 0 && len(diff.Create) == 0 && len(diff.Drop) == 0 &&
    len(diff.Unverified) == 0 // omitting this reports an unchecked index as verified
```

Every schema-owned remote index the caller passes in is accounted for exactly once: it either matches a desired index or is reported as a drop. Two remote indexes sharing one semantic identity (an operator-created index alongside the schema's own) are told apart by name — the schema's own index carries the name the adapter emits — so the redundant one is reported rather than absorbed, and which is which does not depend on the order the server returned rows in. `DiffConstraints` gives the same guarantee.

### Introspection Queries

```go
// Get a Cypher query for introspecting relationship topology
query, params := adapter.IntrospectRelationshipsQueryFor(schemaFilter)
```

This returns a parameterized Cypher query string and parameters — consumers execute it against their own driver. Package-level helpers assist with gathering introspection data:

| Function | Description |
| -------- | ----------- |
| `IntrospectConstraintsQuery()` | Static Cypher for `SHOW CONSTRAINTS YIELD *` |
| `IntrospectIndexesQuery()` | Static Cypher projecting `name, type, entityType, labelsOrTypes, properties, options, state, owningConstraint`, filtered to `type <> 'LOOKUP'` |
| `IntrospectRelationshipsQuery(labelPrefix)` | Parameterized Cypher for relationship topology discovery |
| `ParseRemoteConstraints(records)` | Parse driver output into `[]RemoteConstraint` |
| `ParseRemoteIndexes(records)` | Parse driver output into `[]RemoteIndex` (including the options map) |
| `ParseRemoteRelationships(records)` | Parse driver output into `[]RemoteRelationship` |

The introspection types are:

- `RemoteConstraint` — constraint metadata (name, type, entity type, labels/types, properties, property type). `Type` is verbatim what the server reported, and the node-uniqueness spelling **depends on the server generation**: Neo4j 5.x reports `UNIQUENESS`, 2026.x reports `NODE_PROPERTY_UNIQUENESS`. The other kinds are stable. `DiffConstraints` and `InferSchema` fold both spellings internally; a consumer switching on the field itself must accept both, or it silently stops recognising every UNIQUE constraint when the database is upgraded.
- `RemoteRelationship` — relationship topology (relation type, source/target labels)
- `RemoteIndex` — index metadata (name, type, entity type, labels/types, properties, options, state, owning constraint); `VectorDimensions()` and `VectorSimilarity()` read a vector index's configuration from the options map for drift detection, and `IsOnline()` reports whether the index is in a state that serves queries (an unreported state counts as online)

`IntrospectIndexesQuery()` returns **constraint-backing indexes** as well as standalone ones — `RemoteIndex.OwningConstraint` identifies them. The diff needs them because a backing index holds its constraint's name against every `CREATE INDEX` and already serves the definition it covers, which are both conditions under which the server silently no-ops a declared index. A consumer filtering rows itself must test that field rather than assume the query excluded them.

### Utility Functions

| Function | Description |
| -------- | ----------- |
| `SanitizeIdentifier(s)` | Escape a string for use as a Neo4j label or property name |
| `ValidateIdentifier(name, context)` | Validate that a name is a legal Neo4j identifier |
| `CypherReservedKeywords()` | Return the set of Cypher reserved keywords |

Cypher reserved words are not reserved by the DSL: a property named `match` or a
type named `MATCH` is valid yammm and exports cleanly through the JSON and CSV
adapters, but identifiers that appear unquoted in generated Cypher (property
names, primary keys, assembled labels) are validated during constraint and shape
generation and rejected with `ErrReservedKeyword` — the check is
case-insensitive. Namespaced labels usually absorb reserved type names
(`app__MATCH` is not a keyword); a reserved property name always fails. For
export-compatibility feedback before write time, run `ConstraintsForSchema` or
call `ValidateIdentifier` on names directly.

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

## Go Source Generation

The `adapter/gogen` package generates Go source from a schema: one struct per type, named Enum/DataType types, `EDGE_` association structs, a `Graph` aggregate, and an embedded `SerializedModel`. Unlike the data adapters above, gogen has no instance-data path — it is schema-in, bytes-out. Generated output is stdlib-only (it imports at most `time`).

### Generation

```go
func Marshal(s *schema.Schema, opts ...Option) ([]byte, error)
```

```go
data, err := gogen.Marshal(s, gogen.WithPackageName("model"))
```

`Marshal` requires a **completed, source-backed** schema — one loaded via `schema.Load`, `schema.LoadString`, or `schema.LoadSources`/`LoadSourcesWithEntry`. A schema built programmatically with `schema.Builder` retains no source (`Sources()` is nil) and is rejected, because the embedded `SerializedModel` and its round-trip self-check require the source.

### Options

| Option | Description |
| ------ | ----------- |
| `WithPackageName` | Override the generated package name (default: derived from the schema name) |
| `WithInitialisms` | Extra acronyms (e.g. `"GUID"`, `"JWT"`) upper-cased wholesale in exported Go identifiers; merged with the default golint set, matched case-insensitively |

`WithInitialisms` is how a downstream generator injects its own domain vocabulary, so domain acronyms live at the call site and never in yammm. gogen's default set is the canonical golint `commonInitialisms` list (`id`→`ID`, `url`→`URL`, `json`→`JSON`, …).

### Type Mapping

| Constraint kind | Go type | Named type emitted? |
| --------------- | ------- | ------------------- |
| `String` / `UUID` / `Pattern` | `string` | — |
| `Integer` | `int64` | — |
| `Float` | `float64` | — |
| `Boolean` | `bool` | — |
| `Timestamp` / `Date` | `time.Time` | — |
| `Vector` | `[]float64` | — |
| `List<T>` | `[]<elem>` | element via the same mapper |
| `Enum` (inline) | `string` underlying | **yes** — `type <Type><Field> string` + value consts |
| DataType (`type X = …`) | the underlying's Go type | **yes** — `type X <underlying>` (an Enum DataType also emits value consts) |

A named DataType is rendered faithfully in every position — scalar field, list element (`List<FipsCode>` → `[]FipsCode`), edge property, and edge `Where`-block primary key — never degraded to its primitive.

### Field and Relation Rules

- **Optional scalar** → pointer + `,omitempty` (`*string`, `*int64`, `*time.Time`); driven by `Property.IsOptional`.
- **Optional `List`/`Vector`** → the slice stays nilable (no extra pointer) + `,omitempty`.
- **Relation** → reference type always: `*<T>` (single) or `[]*<T>` (many). `,omitempty` is driven by `Relation.IsOptional`, so required relations (`(one)`, `(one:many)`) emit no `omitempty`.
- **JSON tags** preserve the original yammm name in snake_case; Go field names are the mapped CamelCase identifiers.
- **Inheritance is flattened** — each struct carries its own and inherited properties and relations as direct fields (own-first ordering); no Go embedding.

### Structural Output

- **Struct per type** (including abstract and part types). Schema doc-comments carry through verbatim.
- **`EDGE_<Owner>_<edge>_<Target>` structs** for associations — owner-qualified, so they are unique by construction — carrying the association's own properties plus a `Where` block of the target type's primary keys. (An association whose target type has no primary key is rejected: its `Where` block would be empty, leaving the edge unable to identify a target node.)
- **`Graph` aggregate** — one slice field per concrete type, keyed by the singular snake_case form of the type name.
- **`SerializedModel`** — the verbatim `.yammm` source(s): a string `var` for a single file, or a `map[string]string` keyed by module-root-relative path (plus a `SerializedModelEntry` const) for a schema with imports. A `SchemaHash` const carries the structural hash. The embedded model is **guaranteed re-loadable**: `Marshal` re-loads it at generation time and confirms the `StructuralHash` matches, so a non-re-loadable model is a generation error, never a silent claim.

### Imports

gogen handles the full range of yammm schemas, including schemas with `import`s: the full import closure is flattened into one self-contained package. Cross-schema identifier collisions are resolved by schema-qualification (two schemas' `Region` → `GeoRegion` / `CommonRegion`); an unresolvable same-schema clash (a type and a datatype of the same name) is a hard error. Embedded `SerializedModel` keys are relative to the load's recorded module root (`Schema.ModuleRoot()` — the `WithModuleRoot` value when given, else the entry's directory), so generated output is byte-reproducible across checkouts and CI runners and the keys match the sources' module-style import statements on re-load. `Marshal` verifies the embedded model re-loads hermetically (`schema.WithSourcesOnly` — no filesystem participation) before returning; re-load the multi-source form the same way: `schema.LoadSourcesWithEntry(ctx, toBytes(SerializedModel), SerializedModelEntry, ".", schema.WithSourcesOnly())`.

### Validation

Generated source is run through `go/format` and then type-checked with `go/types` before return, so duplicate declarations, unused imports, and undefined references surface as generation errors rather than broken Go. The type-check uses a hermetic stub importer for `time`, so `Marshal` needs no Go toolchain, GOROOT, or build cache at runtime.

### CLI

```text
yammm gen --to go <schema.yammm> [--package <name>] [--output <path>] [--initialisms GUID,JWT] [--module-root <dir>]
```

The `gen` command loads the schema (resolving imports), generates Go, and writes it to stdout or the `--output` path.

## JSON Schema Generation

The `adapter/jschema` package generates a JSON Schema **draft 2020-12** document from a schema, for editor-assisted authoring of the instance-data JSON that `yammm check` accepts. Like gogen it is schema-in, bytes-out — no instance-data path, plain `error` — and the adapter itself is stdlib-only.

### Generation

```go
func Marshal(s *schema.Schema, opts ...Option) ([]byte, error)
```

```go
data, err := jschema.Marshal(s, jschema.WithSchemaID("https://example.com/fleet.schema.json"))
```

`Marshal` requires a completed schema (always true for one returned by `schema.Load*` or `schema.Builder.Build`); unlike gogen it does **not** require a source-backed schema — nothing is embedded — so Builder-built schemas are accepted.

### Options

| Option | Description |
| ------ | ----------- |
| `WithSchemaID` | Set the document `"$id"` (omitted entirely when unset) |
| `WithTitle` | Override the document title (default: the schema name) |
| `WithDescription` | Override the generated top-level description |

### The Emitted Document

The document describes exactly the object form `adapter/json`'s `ParseObject` + `instance.Validator` accept:

- **Envelope** — one object keyed by type name (`{"Person": [...], "Car": [...]}`), each key an array of instances. Entry-schema types under their bare name; **directly imported** types under their alias-qualified name (`common.Region` — the only form the validator resolves for them); transitively imported types are `$defs`-only.
- **Instance objects** — properties by name, relations by their lower_snake field name; `additionalProperties: false` (the instance layer rejects unknown fields); required properties in `required`.
- **Associations** — an edge object (to-one) or array of edge objects (to-many), each `$ref`ing an `EDGE_<Owner>_<field>_<Target>` def carrying the required `_target_<pk_name>` foreign-key fields (validated against the target key's own constraint — a DataType-typed key keeps its `$ref`) plus edge properties. Association **presence is deliberately not `required`**: the instance layer defers it to graph assembly, so a per-file requirement would flag files yammm validates cleanly; the generated `description` states the multiplicity and enforcement point instead.
- **Compositions** — always arrays of child objects; required compositions are `required` + `minItems: 1`; to-one compositions get `maxItems: 1` (mirroring graph-assembly enforcement).
- **Constraints** — the full mapping (bounds → `minLength`/`minimum`/…, `Enum` → inline `enum`, multi-`Pattern` → all-must-match `allOf`, `Vector[N]` → fixed-size number array, named DataTypes → `$defs` entries `$ref`ed from every position). Schema doc-comments flow through as `description` — this is what editors show on hover.

Cross-schema imports are closure-flattened into one self-contained document with gogen-parity collision handling (bare names where unique, `<schemaName>.<Name>` on collision, hard error when qualification cannot separate). Abstract types get no `$defs` entry (nothing can legally `$ref` one); their members appear flattened into each subtype.

Fidelity caveats (always degrading toward editor under-/over-flagging, never yammm-side): the schema targets canonical property-name spellings (yammm matches case-insensitively by default); patterns pass through verbatim (yammm compiles RE2, JSON Schema validators assume ECMA-262); a custom `Timestamp["layout"]` emits a plain string with the source form in its `description`. Emitted `format` keywords are annotations by default under 2020-12.

### Validation

Output is deterministic (byte-identical across runs and checkouts, so generated documents can be committed and drift-checked by regenerate-and-diff). Before returning, `Marshal` self-checks: the bytes must parse as JSON and every `$ref` must resolve to an emitted `$defs` entry — a failure is a generation error, never emitted output. The package's contract-alignment tests additionally prove, per corpus case, that sample data validates identically under yammm and under the emitted schema compiled by a real 2020-12 validator.

### CLI

```text
yammm gen --to jsonschema <schema.yammm> [--schema-id <uri>] [--output <path>] [--module-root <dir>]
```

Per-target flags are enforced: `--package`/`--initialisms` are rejected for the jsonschema target, `--schema-id` for the go target.

## Markdown Documentation Generation

The `adapter/markdown` package generates a Markdown reference document — a Mermaid class diagram plus per-type sections — from a schema, giving every schema a canonical, regenerable documentation artifact. Like its gen-family siblings it is schema-in, bytes-out — no instance-data path, plain `error` — and the adapter is stdlib-only.

### Generation

```go
func Marshal(s *schema.Schema, opts ...Option) ([]byte, error)
```

```go
doc, err := markdown.Marshal(s)                                  // full document
doc, err := markdown.Marshal(s, markdown.WithClassDiagram(false)) // tables only
```

`Marshal` requires a completed schema (always true for one returned by `schema.Load*` or `schema.Builder.Build`). Source backing is optional: on a Builder-built schema, invariant sections degrade to their message line instead of a source fence — nothing else needs source content.

### The Emitted Document

One document per invocation, covering the entry schema plus its whole import closure:

- **Title + schema doc** — `# Schema <Name>`, then the schema's doc-comment verbatim.
- **Class diagram** — one Mermaid `classDiagram` fence over the entire closure. Classes carry each type's **own** members as `name KindLabel` pairs (named DataTypes show their name; constraint detail stays out of the diagram); abstract and part types carry `<<Abstract>>` / `<<Part>>` stereotypes; edges are `Parent <|-- Child` for inheritance and DSL-labeled `Owner --> Target : NAME (mult)` / `Owner *-- Child : NAME (mult)` for each type's own associations and compositions — inherited structure is conveyed by the inheritance edges, not redrawn. Qualified names (invalid as Mermaid class ids) emit the sanitized-id form `class common_Region["common.Region"]`; Mermaid namespaces are deliberately not used (GitHub's renderer does not support them in class diagrams).
- **Type sections** — one `### <TypeName>` per type in declaration order: a badge line (`*Abstract type*` / `*Part type*` / `Extends: [Parent](#parent)`), the doc-comment, then a **flattened property table** (Property | Type | Modifiers | Description) over the full inheritance chain — the Type column renders each constraint's DSL form (`String[1, 100]`, `List<FipsCode>`), and inherited rows carry `from <Owner>` in Modifiers. Associations and compositions follow as DSL-notation bullets with linked targets, an inherited relation carrying the same provenance as a ` — from <Owner>` marker; edge properties nest as a sub-table under their relation's bullet.
- **Invariants** — the failure message as a bullet (an inherited invariant carrying the ` — from <Owner>` marker), the doc-comment beneath, then the declaration source (`! "message" expression`, exactly as written) in a `yammm` fence extracted via the invariant's span.
- **Data Types** — a Name | Definition | Description table per schema.
- **Imported schemas** — one `## Schema <Name> (imported as <alias>)` section per import in closure order (transitive imports, which have no entry alias, head as plain `## Schema <Name>`), with collision-proof `### <schemaName>.<TypeName>` headings.

Arbitrary doc/enum text cannot break structure: table cells escape backslashes and pipes and fold newlines to `<br>`; a value containing a backtick renders through an entity-escaped `<code>` element; fences are sized past any backtick run in their body.

### Validation

Output is deterministic (byte-identical across runs and checkouts), so generated documents can be committed and drift-checked by regenerate-and-diff. Before returning, `Marshal` structurally self-checks: every fence closes, every internal link resolves to an emitted heading, and every table separator matches its header's column count — a failure is a generation error, never emitted output. A schema whose two type headings slug to the same anchor is also rejected (a rename-able input collision, since an internal link to one would otherwise resolve to the other's section).

### CLI

```text
yammm gen --to md <schema.yammm> [--no-class-diagram] [--output <path>] [--module-root <dir>]
```

`markdown` is accepted as an alias for `md`. Per-target flag enforcement extends to the md target: `--no-class-diagram` is rejected for other targets, and go/jsonschema-only flags are rejected for md.

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
