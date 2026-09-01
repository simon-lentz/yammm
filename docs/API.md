# YAMMM Go Library API Reference

This document is the API reference for the YAMMM Go library. It covers schema loading, instance validation, graph construction, snapshot persistence, adapters, and diagnostics.

The YAMMM language itself — grammar, types, expressions, constraints, and diagnostic codes — is specified in [SPEC.md](SPEC.md).

## Error Handling Conventions

Most load and validation functions return a `(T, diag.Result)` pair. The value half is whatever the call produces — `*schema.Schema`, `[]byte`, `[]*instance.ValidInstance`, `[]snapshot.ScanEntry` — and is often not a pointer.

Some validation surfaces have no value to return and produce a bare `diag.Result`: `snapshot.Verify`, `graph.Graph.Add`, `graph.Graph.AddComposed`, `graph.Graph.Check`, and `neo4j.Adapter.DetectLabelCollisions`. For these, `result.OK()` is the whole answer.

**Read the result first, then the value.** The two are independent, and all four combinations occur:

- `value != nil && result.OK()` — success. The result may still carry warnings.
- `value == nil && !result.OK()` — failure.
- `value != nil && !result.OK()` — a partial or unvalidated answer, and a documented contract on several surfaces. `snapshot.Info` returns a populated summary beside an Error-severity `E_SNAPSHOT_INTEGRITY_MISMATCH`; `snapshot.ScanDirSliceWith` returns the entries collected before cancellation beside a Fatal `E_CONTEXT_CANCELLED`; `instance.Validator.Validate` returns one slot per input, nil for the failures, beside a merged non-OK result. Discarding the value here loses real data.
- `value == nil && result.OK()` — nothing to do. `instance.Validator.Validate` and `ValidateForComposition` return `nil, diag.OK()` for nil input. For a non-nil empty input `Validate` returns the empty slice unconditionally; `ValidateForComposition` returns it only once the parent type and the relation resolve, and returns `nil` beside a non-OK result when either does not.

Use `result.Err()` to convert to a standard Go `error` when `!result.OK()`.

Adapter constructors do not fail: `neo4j.New`, `json.New` and `csv.New` each return `*Adapter` alone, and an `Option` cannot report a problem.

Pure transformations split three ways rather than one. `snapshot.Marshal`, `neo4j.Adapter.ConstraintsForSchema`, `IndexesForSchema` and `ShapeForSchema` return `(T, diag.Result)`. `gogen.Marshal`, `jschema.Marshal`, `markdown.Marshal`, `json.Adapter.MarshalObject` and `csv.Adapter.MarshalSnapshot` return `(T, error)`. The introspection query builders return neither: `neo4j.IntrospectConstraintsQuery` returns a `string`, and `IntrospectRelationshipsQuery` returns `(string, map[string]any)`.

## Loading Schemas

Schemas are loaded from files or in-memory sources using the `schema` package.

### Load Functions

```go
// Load from file path
s, result := schema.Load(ctx, "path/to/schema.yammm", opts...)

// Load from string content (sourceCode first, then sourceName)
s, result := schema.LoadString(ctx, content, "source-name", opts...)

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
| `WithImportsAllowed` | Whether import declarations are processed (default `true`); `false` refuses them with `E_IMPORT_NOT_ALLOWED` |
| `WithSourcesOnly` | With `true`, restrict import resolution to pre-registered in-memory sources — a miss errors instead of reading the filesystem (hermetic loads of embedded sources) |
| `WithSyntheticRoot` | Give in-memory sources synthetic identities under a root such as `embedded://app`, so type identities do not move with the working directory (see [Synthetic source identities](#synthetic-source-identities)) |

### Synthetic source identities

`LoadSourcesWithEntry` derives each source's `SourceID` from the module root and
the key, so the identities — and therefore every `TypeID`, and therefore every
type row a `.ys` snapshot records — carry a filesystem path. That path moves with
the working directory, the checkout, and the container mount point.
`WithSyntheticRoot` replaces it:

```go
s, result := schema.LoadSourcesWithEntry(ctx,
    gen.SerializedSources(),  // a generated package's embedded sources
    gen.SerializedEntry,
    "",                       // no module root: the synthetic root stands in
    schema.WithSourcesOnly(true),
    schema.WithSyntheticRoot("embedded://app"),
)
```

A source keyed `a/b/x.yammm` then has the identity `embedded://app/a/b/x.yammm`
on every machine. The root also stands in for the module root, so module-style
imports resolve under it.

The option refuses rather than degrades. Each of these is an error:

| Condition | Why |
| --------- | --- |
| An invalid root (empty, or absolute-looking after the trailing slash is trimmed) | An absolute-looking root collides with file-backed identities |
| A root without `WithSourcesOnly` | An unresolved import would read from disk and mix a file-backed identity into the same closure |
| A root together with a non-empty `moduleRoot` argument | The two name one concept and the load can honor only one |
| The option passed to `Load` or `LoadString` | It could only ever be a silent no-op there |
| An absolute source key, or one resolving to the root itself | An absolute key collides with file-backed identities, and the root is not itself a source. A key that escapes the root is allowed: it keeps its leading `..` and yields a stable, distinct identity |

Two limitations are deliberate. Relative imports (`"./x"`, `"../x"`) are not
supported: they resolve through the importing source's canonical path, which no
synthetic identity has. And `Schema.ModuleRoot()` stays empty, which makes a
schema loaded this way an unsupported input to `gogen.Marshal` — keep generators
on disk loads.

**Adopting a synthetic root re-keys every snapshot already written**, because the
recorded type identities change. Treat the switch, and any later change to the
root value, as a state-clearing change.

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

`LoadString` — and any load with `WithImportsAllowed(false)` — still rejects
import declarations categorically with a single `E_IMPORT_NOT_ALLOWED`, but
the rejection no longer suppresses the source's other findings: the
remaining diagnostics are reported alongside it, with references through
the rejected aliases deferred. Rejected imports are never probed or
resolved.

#### Issue limit

`WithIssueLimit` bounds *collection*: once the limit is reached, every further
issue is counted in `Result.DroppedCount()` and flagged by
`Result.LimitReached()`. **The cap retains the most severe issues seen, not the
first ones seen.** When the store is full and an arriving issue is more severe
than the least severe stored issue, the stored one is evicted and the arriving
one takes its slot; only an issue that is no more severe than the least severe
stored one is itself the drop. Arrival order breaks ties within a single
severity — among equally severe issues the earliest-arrived are retained — but
never decides survival across severities, so an error declared late in a file
still displaces a warning collected early. Raising the limit is not required to
see it. The *display* order of surviving issues is always deterministic.
Dropped issues still count toward `Result.OK()` / `HasErrors()` /
`SeverityCounts()` (the counts reflect every issue *seen*, not only those
stored), so truncation never flips a failing result to OK and the
all-or-nothing contract holds regardless of the limit.
`WithIssueLimit(0)` (or `diag.NoLimit`) means unlimited. When the cap was hit,
the JSON output format carries `limitReached` and `droppedCount`; those two are
the authoritative pair. `limit` reports the producing collector's own cap and is
omitted when it is zero, so a truncated result merged into an unlimited
collector carries `limitReached` and `droppedCount` without it. The CLI's text
output appends a dropped-issues note.

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
        WithPrimaryKey("name", schema.NewStringConstraint()).
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
| `WithName(name)` | Set the schema name. **Required** — `Build()` refuses an empty name with `E_INVALID_NAME` |
| `WithSourceID(id)` | Set the source ID (required if `AddImport` is used) |
| `WithDocumentation(doc)` | Set schema-level documentation |
| `WithRegistry(r)` | Provide a schema registry for cross-schema type resolution |
| `WithIssueLimit(limit)` | Maximum diagnostics to collect (default: 100) |
| `WithImportResolver(resolver)` | Custom resolver for import paths (needed for synthetic source IDs with relative imports) |
| `AddImport(path, alias)` | Add an import declaration |
| `AddType(name)` | Begin building a type definition (returns `*TypeBuilder`) |
| `AddDataType(name, constraint)` | Add a named data type alias |
| `Build()` | Construct the final `*Schema` from builder state |

`Build()` validates declared names against the DSL's own productions, so every
builder-built schema remains expressible in `.yammm` form: type and datatype
names start with an uppercase letter, property names with a lowercase letter,
and relation names with a letter of either case — all continuing with letters,
digits, or underscores. Violations fail the build with `E_INVALID_NAME`.
Schema names and invariant names are quoted strings in the DSL, so they are
not held to those productions — but neither may be empty: `Build()` reports
`E_INVALID_NAME` for a missing schema name and for an empty invariant name.
Import aliases are validated during completion (`E_INVALID_ALIAS`).

A qualified reference (`alias.Type` in `Extends`, a relation or composition
target, or a qualified datatype constraint) must resolve at build time: the
alias must name a declared import backed by a registry (`WithRegistry` +
`AddImport`). No later link step exists that could resolve one, so the build
fails either way — but with different codes. An alias no `AddImport` declares
fails with `E_UNKNOWN_TYPE`, with or without a registry. An alias that *is*
declared but whose path resolves to nothing in the registry fails earlier, at
import resolution, with `E_IMPORT_RESOLVE`.

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

- `Property.AnnotationsSlice()` / `Annotation(name) (*Annotation, bool)` — a property's `@name` annotations.
- `Type.AnnotationsSlice()` / `AllAnnotations()` / `AllAnnotationsSlice()` — a type's `@@` members (own, and own-plus-inherited).
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

`Properties` is a `map[string]any` of native Go values. The checkers accept them directly — `time.Time` for a `Timestamp` or `Date`, any Go integer width for an `Integer` — so marshaling a struct through JSON is one convenient way to build the map, not a requirement. `instance.BuilderFor` is another.

### Validator Creation

```go
validator := instance.NewValidator(schema, opts...)
```

### Validator Options

| Option | Description |
| ------ | ----------- |
| `WithStrictPropertyNames` | Require exact case matching (default: false) |
| `WithAllowUnknownFields` | Silently ignore unknown fields (default: false) |

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

// Validate instances in a composition context (part types allowed). Primary keys
// are enforced exactly as ValidateOne enforces them; what is relaxed lives
// elsewhere: a part type may declare no primary key at load, and a keyless
// child type skips the duplicate-key scan.
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

`RawInstance.Properties` is `map[string]any` of native Go values; marshaling a struct through JSON is one way to build it, not a requirement. Property names may use any casing by default, and the validator normalizes them — under `WithStrictPropertyNames(true)`, which `RecommendedOptions()` sets, a name that differs in case goes unmatched and draws `E_UNKNOWN_FIELD` unless `WithAllowUnknownFields(true)` is also set.

### Schema-Aware Raw Instance Builder

`instance.BuilderFor(schema, typeName)` returns a `*SchemaBuilder` that constructs `RawInstance` values while enforcing schema shape at build time — unknown properties, unknown relations, and cardinality mismatches all surface from `Build()` with file:line locators captured via `runtime.Callers`. This shifts shape failures out of `ValidateOne`'s domain:

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

The builder covers properties and property-less association edges; an
association that declares edge properties, and a composition, each record a
shape error — construct such instances from raw data instead.

The builder does not construct composed children. `EdgeTo` on a composition is refused with a shape error reading *"is a composition; the builder does not support composed children — construct the instance from raw data"*, and there is no other entry point that takes a child builder. A type with a **required** composition therefore builds clean here and fails `ValidateOne` with `E_UNRESOLVED_REQUIRED_COMPOSITION`; construct those instances from raw data.

#### Methods

| Method | Purpose |
| ------ | ------- |
| `BuilderFor(s, typeName)` | Construct a builder bound to a schema type. Errors on nil schema, unknown type, or abstract type. |
| `Property(name, value)` | Set a property value. Unknown names accumulate as errors surfaced at Build. |
| `EdgeTo(name, targetKey...)` | Add an edge target on an association without edge properties. Variadic key supports composite PKs. |
| `Build()` | Produce the `RawInstance`. Returns the first accumulated error, suffixed `(and N more build error(s))` when more exist. |

Build errors include the bound type's name, the offending property/relation, and the caller's file:line.

`SchemaBuilder` is NOT concurrent-safe; construct one per goroutine. The bound `*schema.Schema` remains safe to share across many concurrent builders.

The shape portion of `ValidateOne` — property and relation names, and association cardinality — is guaranteed to pass on the output of a successful `Build`. Compositions are not covered: the builder cannot produce one. Value-level validation (constraint checks, PK coercion, foreign-key shape and key-component checks) still runs at `ValidateOne` time. Reference integrity — whether an association's target exists — is checked by `graph.Check`, never by the validator.

### Value Functions

The validator's per-value rules, exported for a caller that admits or renders schema-typed values at a boundary this library does not own — a hand-built export, a direct-Cypher parameter map, a pre-filter in front of a write the validator never sees. Values that reach a graph through `Validator` have already passed both.

```go
// CheckValue reports whether val conforms to c by the rule the validator applies
// to every property value: Go kind, bounds, enum membership, pattern, list length
// and element rule, with an alias resolved to its DataType. A nil value and a nil
// constraint are both valid — presence is the caller's rule. Built-in type
// detection only: a Validator's custom value registry is not consulted.
func CheckValue(val any, c schema.Constraint) error

// CanonicalValue returns val in the single stored representation c defines: a
// Timestamp through its declared format (RFC 3339 with nanoseconds otherwise), a
// UUID in canonical lowercase form, a Date as "2006-01-02" in the value's own
// location, and a List by canonicalizing each element through the element
// constraint. Every other kind, an unresolved alias and a nil value pass through.
// On error the returned value is val unchanged, so a caller that heals what it
// can may ignore the error.
func CanonicalValue(val any, c schema.Constraint) (any, error)
```

`CheckValue`'s error is the checker's message and is meant for reading, not matching, with one exception: a panic inside a check is recovered into an `*InternalError` that matches `errors.Is(err, instance.ErrInternalFailure)`, exactly as it does inside the validator. `CanonicalValue` is the rule `adapter/json` and `adapter/csv` render through, and the approved facade over it for the adapter layer.

## Graph Construction

The `graph` package builds an in-memory graph from validated instances.

### Graph Options

`graph.New` and `graph.NewFromSnapshot` accept `graph.Option` values. `graph.WithLogger(*slog.Logger)` attaches a structured logger to the graph's operation boundaries: `Add`, `AddComposed` and `Check` each open and close a traced operation, and edge resolution, forward references, duplicate primary keys and unresolved required associations are logged as they occur. With no logger every trace call returns immediately. It is symmetric with `schema.WithLogger`.

### Graph Operations

```go
g := graph.New(schema)

// Add validated instances
result := g.Add(ctx, validInstance)
if !result.OK() {
    // Handle error
}

// Add a composed child (part type instance embedded in a parent)
result = g.AddComposed(ctx, carTypeID, graph.FormatKey("vin-123"), "WHEELS", composedChild)

// Check completeness (required associations)
result = g.Check(ctx)

// Get immutable snapshot
snap := g.Snapshot()
for _, typeID := range snap.Types() {
    for _, inst := range snap.InstancesOf(typeID) {
        // Process instances
    }
}
```

**Type resolution.** No lookup in this package takes a rendered type name — a rendering is lossy, so keying a lookup by one merges types that are not the same type. `AddComposed` takes the parent's `schema.TypeID`, the same identity `Snapshot.Types` and `Instance.TypeID` hand you. Two lookups then answer different questions. `Add` resolves a root's type from its identity, restricted to the same set as a matter of *ownership* — a graph bound to a schema holds instances of the types that schema declares or directly imports, and the diagnostic's hint says so. A composed child resolves across the *whole* import closure: its type comes from a relation the schema already resolved, and no ownership question arises because the child arrived inside a parent the graph does own. A schema where an imported type composes a part type from a further import therefore loads, validates and builds. Where two identities render alike, a diagnostic naming them falls back to the full identity rather than reading `"X" does not match "X"`.

**A non-OK `Add` leaves the graph unchanged.** `Add` and `AddComposed` walk an instance once. That walk both checks the whole structure — the names and multiplicities of its edges, and every slot and child of its composition tree at any depth — and assembles the tree to install, touching no graph state; only a walk that raised no error reaches the commit. A record that violates one of those rules is refused entire: no instance, no child, no edge and no duplicate record survives it. The check runs for every instance, whatever `ValidInstance.Validated()` reports, because the graph cannot verify that bit and because a validator hole in one of these rules is exactly what the check exists to catch.

The codes it can raise are `E_GRAPH_UNKNOWN_RELATION` (data filed under a name the type does not declare in that slot), `E_GRAPH_CARDINALITY` (a `(one)` association carrying several targets), `E_GRAPH_INVALID_COMPOSITION` (a composed child that is not an instance of its relation's target type), `E_DUPLICATE_COMPOSED_PK` (a `(one)` composition carrying several children, or two children of one `(many)` slot sharing a primary key), `E_GRAPH_TYPE_NOT_FOUND` (a composed child whose type is not in the schema's import closure at all) and `E_GRAPH_INVALID_PK` (a composed child of a keyed part type whose key is empty, has the wrong arity, or disagrees with its own key property). See the package doc's Error Handling section for the full per-method list.

### Batch Assembly

`graph.BatchAssembler` composes a `Validator` and a `Graph` into a single call surface for the common pipeline pattern: validate → add → check → snapshot. The assembler encodes the ordering invariant (validate before add, check before snapshot) so consumers cannot get the sequence wrong, and is concurrent-safe so multiple goroutines may share one assembler:

```go
ba := graph.NewBatchAssembler(ctx, s)

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

**Error shapes.** Both `Add` / `AddValid` and `Finalize` return errors whose cause is a `*diag.ContextualError` (see [Contextual Diagnostic Wrap](#contextual-diagnostic-wrap)). `Add`'s tag is `"<TypeName> (attempt #N)"`, where N is the assembler-wide 1-indexed attempt ordinal: it counts calls across every goroutine sharing the assembler, so it identifies the call and not the caller's input row. A caller that needs to locate its own record keeps that mapping itself. `AddValid(nil)` has no type name to substitute, so its tag is the single token `"nil-instance (attempt #N)"` and its diagnostic is `E_INTERNAL` — the record is absent, not mis-keyed. `Finalize`'s tag is the fixed string `"batch_finalize"` so downstream log consumers have a stable filter key. Recover with `errors.As` or `diag.AsContextualError`.

**What `Finalize`'s error reports.** `Graph.Check` alone. `Snapshot.Diagnostics()` carries the construction diagnostics from every `Add` and `AddComposed`; `Check` accumulates nothing into it, which is what makes `Check` idempotent. A caller that discards per-record `Add` errors must read `res.Snapshot.Diagnostics()` to see them — a nil error from `Finalize` means the completeness check passed, not that every record was added.

**Calling `Finalize` twice.** The second call returns the first call's stored `FinalizeResult` and error, with no second `Check` pass and no second snapshot. A later context, cancelled or not, cannot change the outcome.

**Post-finalize sentinel.** After `Finalize`, subsequent `Add` / `AddValid` calls return `graph.ErrAssemblerFinalized`, matchable via `errors.Is`. Consumers performing retry / cleanup logic key off the sentinel rather than the error string.

**Validator access.** The assembler serializes `ValidateOne` + `Graph.Add` through an internal `sync.Mutex`, covering validation, the graph add, and the success-counter increment as one atomic step.

**Concurrency contract.** `BatchAssembler` is safe for concurrent use across all methods (`Add`, `AddValid`, `Count`, `Graph`, `Finalize`). The library coordinates Add lifecycle against Finalize via an internal `sync.RWMutex` so every Add that returns nil is guaranteed to be in the finalized snapshot, and any Add that arrives after Finalize takes its lock returns `ErrAssemblerFinalized`. No external mutex required at call sites — the worker-pool pattern (one assembler shared across N scraper goroutines, coordinator goroutine calls Finalize at end-of-batch) is supported directly.

**Resuming from a prior snapshot.** `graph.NewBatchAssemblerFromSnapshot(ctx, s, snap)` constructs an assembler whose graph starts pre-populated from an existing `Snapshot` — the same import semantics as `NewFromSnapshot` (see [Seeding from a Snapshot](#seeding-from-a-snapshot)) — instead of `NewBatchAssembler`'s empty graph. Consumers that persist a batch and continue it on a later run seed a new assembler from the loaded snapshot and `Add` only the outstanding records:

```go
snap, res := snapshot.Load(ctx, data, s) // prior batch's .ys
if res.HasErrors() { /* handle */ }

ba := graph.NewBatchAssemblerFromSnapshot(ctx, s, snap)
for _, rec := range outstanding { // e.g. resume-by-set-difference
    if err := ba.Add("TypeName", rec); err != nil { /* handle */ }
}
finRes, err := ba.Finalize(ctx)
```

New adds interact with the seeded state as if it had been assembled in the same batch: they resolve previously-unresolved edges imported from the seed, forward references resolve against seeded instances, a `(type, primary key)` collision with a seeded instance is rejected as `E_DUPLICATE_PK`, and `Finalize`'s check covers the union — a required association imported from the seed and still unresolved fails the batch with `E_UNRESOLVED_REQUIRED`. `Count()` reflects only records added through the assembler (seeded instances are not counted), and construction diagnostics are not carried over from the seed (`.ys`-loaded snapshots carry none by design; `Duplicates` and `Unresolved` are the persistent structural records, and both import). The seed snapshot must originate from the same schema — taken from a `Graph` bound to it, or loaded via `snapshot.Load` against it, which verifies structural compatibility. Seeding from a snapshot built against a different schema is neither detected nor filtered: the import consults no schema, so every type in the seed is installed. Every other contract — lifecycle, finalize barrier, validator access, `FinalizeResult` — is identical to `NewBatchAssembler`.

**Test fixtures.** `snapshot/snapshottest` is the shared round-trip vocabulary for snapshot tests: `BuildSnapshot(tb, s, instances...)` constructs a snapshot from pre-validated instances (build them with `internal/instancetest.VI`), `AssertRoundTrip(tb, snap, s, opts...)` pins Marshal→Load structural equivalence, `AssertDeterministic(tb, snap, opts...)` pins byte-stable marshaling, and `DiffSnapshots(tb, want, got)` is the underlying go-cmp comparison — recursive over composition trees (duplicates included, with each record's conflict, parent and relation coordinates), provenance-presence-aware, and exact for every numeric property, dynamic type included: schema-aware float emission keeps `KindFloat` values `float64` across a marshal/load round trip, so an `int64`/`float64` mismatch is a real defect, not a wire artifact. One deliberate exception: a `float32` compares equal to the `float64` its 32-bit shortest wire form parses to, because `Load` materializes a finite dynamic numeric as `int64` or `float64` and the wire carries value fidelity, not width.

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

The `SnapshotParts` struct holds the snapshot's type list (`Types []schema.TypeID`, required — `RebuildSnapshot` hands it to the rebuilt snapshot rather than deriving it, so leaving it nil yields a snapshot whose `Types()` is nil and whose `AllInstances()` yields nothing) beside fully-resolved instances, edges, duplicates, and unresolved records using value types (`InstanceParts`, `EdgeParts`, `DuplicateParts`, `UnresolvedParts`). Pointer-based cross-references are resolved internally.

`UnresolvedParts.Properties` carries DSL-declared edge property values from the forward reference and is populated only when `Reason` is `"target_missing"` — `"absent"` and `"empty"` describe a missing/empty reference that never had a target. The wire persists the values through Marshal/Load, symmetric with resolved `Edge.Properties`. See the snapshot package's [Wire Format Versions](#wire-format-versions) subsection for the current accept range.

`DuplicateParts` states the conflict's address (`ConflictType`, `ConflictKey`) beside the rejected instance's own identity, plus `ParentType` / `ParentKey` / `Relation` for a composed-child duplicate: a root conflict resolves through the instance index, a composed conflict through the parent's relation slot, and an empty stated key addresses the slot's sole occupant. `RebuildSnapshot` rejects a zero `schema.TypeID` at any parts position with a Fatal `E_INTERNAL` — identity is total at this boundary. Most positions name both the position and the key; a zero entry in `Types` names its index, and a zero `Instances` map key names the group's size.

`SnapshotParts.Attestation` carries the loaded header's validity claim verbatim; a zero value reads as both false. Rebuilt instances always report `Validated() == false` — the claim rides the snapshot, not its instances.

Most users should construct snapshots via `Graph.Add` + `Graph.Snapshot`. `RebuildSnapshot` exists for the `snapshot.Load` deserialization path and testing.

### Snapshot Methods

The `Snapshot` type provides read-only access to graph state:

| Method | Description |
| ------ | ----------- |
| `Schema()` | The schema used for validation |
| `Types()` | All type identities (`[]schema.TypeID`, sorted by TypeID) |
| `InstancesOf(typeID)` | Instances of a type (sorted by primary key) |
| `AllInstances()` | Iterator over all **root** instances in deterministic order. Composed children are not yielded — walk `Instance.ComposedRelations` and `Instance.Composed` for the subtree |
| `InstanceByKey(typeID, key)` | O(1) lookup by type identity and primary key |
| `Edges()` | All resolved edges (sorted) |
| `EdgesFrom(inst)` | Outgoing edges for a specific instance |
| `Duplicates()` | Duplicate primary key records (sorted) |
| `Unresolved()` | Unresolved edge records (sorted) |
| `Diagnostics()` | Construction diagnostics (call `OK()` / `HasErrors()` on the returned `diag.Result`) |
| `Attestation()` | The validity claim the snapshot carries: `Values` (every root and composed child validator-built, at least one instance) and `Associations` (no `Required` unresolved record). A loaded snapshot carries the header's claim verbatim |

### Thread Safety

- `Graph` is safe for concurrent `Add` and `AddComposed` calls
- `Snapshot` values are immutable and safe for concurrent reads
- All output slices are deterministically sorted

### Ordering Guarantees

- `Snapshot.Types()`: Lexicographic by `TypeID` — schema path, then name
- `Snapshot.InstancesOf()`: Lexicographic by primary key
- `Snapshot.Edges()`: Lexicographic tuple (sourceType, sourceKey, relation, targetType, targetKey), then the edge properties
- `Snapshot.Duplicates()`: Lexicographic by (`TypeID`, primaryKey), then relation, conflict `TypeID`, conflict primary key, parent slot, and finally the rejected instance's properties
- `Snapshot.Unresolved()`: Lexicographic by (sourceType, sourceKey, relation, targetType, targetKey), then reason, then required (optional before required), and finally the edge properties

Types are identified by `schema.TypeID`, never by name. A name is a rendering of an identity — bare for a local type, alias-qualified for a directly imported one — so it cannot name a transitively imported type and cannot separate two same-named types in different schemas. Use `schema.TagForm(snap.Schema(), id)` where a name is wanted for output.

### Key Formatting and Parsing

A primary key is carried as a canonical JSON array string — the form `Snapshot.InstanceByKey` takes and `immutable.Key.String` produces.

| Function | Description |
| -------- | ----------- |
| `FormatKey(values...)` | Render key components as a canonical JSON array string |
| `FormatComposedKey(rootType, rootKeyValues, path)` | Render a composed child's identity as a flat path. `rootType` is the owning root's `schema.TypeID`; `rootKeyValues` is the root's raw key **components** (`[]any`), not a formatted key; `path` is one `ComposedStep` per composition hop |
| `ParseKey(s)` | Decode a `FormatKey` string into `[]any` components |
| `ParseKeyStrings(s)` | Decode a `FormatKey` string whose components are all strings |

```go
key := graph.FormatKey("us", int64(12345)) // `["us",12345]`
inst, ok := snap.InstanceByKey(typeID, key)

values, err := graph.ParseKey(key)         // []any{"us", int64(12345)}
parts, err := graph.ParseKeyStrings(`["us","ca"]`) // []string{"us", "ca"}
```

`ParseKey` returns the component types a snapshot round trip produces — `string`, `int64`, `float64`, `bool`, `nil` — because it classifies numbers by lexical form, the same rule the `.ys` reader applies: a literal carrying `.`, `e`, or `E` is `float64`, an int-shaped literal is `int64`. An int-shaped literal beyond the int64 range comes back as `float64`; a literal with no finite Go value, such as `1e999`, is an error naming the component's index. So is a component that is itself an array or object: `FormatKey`'s domain is scalars.

The round trip holds in both directions over those types, with one documented carve-out — `FormatKey(float64(5))` renders `5` and `ParseKey` reads it back as `int64(5)`. The wire has the same asymmetry, by the same rule. `FormatComposedKey` has no inverse: composed identities are rendered by the writers and read back by nobody.

A composed identity is a **flat path** — the owning root's type identity, the root's key components, then one two-element segment per composition hop:

```go
ck, err := graph.FormatComposedKey(vehicleTypeID, []any{"ABC123"},
    []graph.ComposedStep{
        {Relation: "WHEELS", KeyOrIndex: []any{"front-left"}},
        {Relation: "NOTES", KeyOrIndex: 0},
    })
// ["file:///m/car.yammm:Vehicle",["ABC123"],["WHEELS",["front-left"]],["NOTES",0]]
```

`KeyOrIndex` is `nil` for a `(one)` composition, the child's key components for a keyed part, and its 0-based position for a keyless one. The address is flat, so its length grows linearly with composition depth. It leads with the root's type identity because a key value is not an identity: two root types sharing a primary-key value and a relation name would otherwise mint one address for children on a single part label.

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

`StructuralHash` computes a deterministic hash over the rules that decide what instance data is valid, across the schema's whole import closure (`Schema.Closure()`). For every member schema — the entry schema and each schema it imports, directly or transitively — the input covers **the member's schema name**, its types (properties with constraints and modifiers, the primary-key set, associations with targets, multiplicities and edge properties, compositions, **inheritance edges**, **invariant expressions**, and the **`abstract` and `part` markers**) and its data types. Two schemas that hash the same agree on every one of those rules everywhere in their closures. The converse does not hold: a hash can also move on a change to an imported schema this schema never references, because closure membership is part of the identity.

Each member is framed under its schema name — renaming any member's `schema "X"` declaration changes the hash even when every declaration is identical — the entry schema first and the remaining members ordered by name, so the order imports are declared in does not enter the digest. An invariant's *name* is not hashed at all; it is the failure message. Invariant expressions hash as an order-independent set per type, while the operands inside one expression hash in declared order, because operand order is semantic.

**Excluded, deliberately:** annotations, which drive downstream store DDL and never reject data; import aliases and source paths — relation and supertype targets hash by name, never by schema path, and a member frame carries the declared schema name, never its source, so `embedded://` and disk loads of one schema text agree; and documentation, source spans, and every derived index or cache.

The hash is used by the `snapshot` package to verify that a persisted snapshot is compatible with the schema provided at load time. `StructuralHashVersion` (currently `3`) identifies the hashing algorithm version; v0.15.0 raised it from `1` when invariants and the `abstract` / `part` markers joined the input, and v0.17.0 raised it to `3` when the input widened from the entry schema's own declarations to its whole import closure.

## Snapshot Persistence

The `snapshot` package serializes and deserializes `graph.Snapshot` values to and from the yammm snapshot persistence format (`.ys`).

### File Format

The `.ys` format is JSON-based and preserves structural fidelity: instances with properties, primary keys, edges, compositions, provenance, duplicates, and unresolved edge records all survive a `Marshal`/`Load` round-trip, with three qualifications. Provenance survives as source name and path only: a loaded instance's `Provenance().Span()` is zero. A duplicate's `Diagnostic` is not persisted and reads as a zero `diag.Issue` after `Load`; code that runs on both constructed and loaded snapshots guards on `IsZero`. And the wire carries numeric value fidelity, not width — `Load` materializes dynamic numeric values as `int64` and `float64`, with two exceptions — a literal no finite `float64` can hold (`1e400`) returns as an unconverted `json.Number`, and so does a number nested more than 64 levels deep inside a dynamic value, where normalization stops recursing — so a `float32` property returns as the `float64` its 32-bit shortest wire form parses to (converting it back to `float32` returns the original). A caller-assembled snapshot the writer cannot serialize returns nil bytes with a Fatal `E_INTERNAL`; snapshots built through `graph.Graph` or `Load` never trigger that path.

The format includes:

- A **version** field for format evolution (current value: `3` since yammm v0.12.0, the only readable version; see [Wire Format Versions](#wire-format-versions))
- A **schema structural hash** for compatibility verification (see [Schema Identity](#schema-identity))
- An **integrity hash** (SHA-256 over the whole document, header included) for corruption detection
- A **features array** for forward compatibility

### Functions

```go
// Serialize a snapshot to .ys bytes (deterministic by default)
data, result := snapshot.Marshal(ctx, snap, opts...)

// Deserialize .ys bytes back to a snapshot (verifies schema compatibility)
snap, result := snapshot.Load(ctx, data, schema, loadOpts...)

// Validate a .ys file without materializing a snapshot
// Peak memory scales with document size: the instances section is decoded first
result := snapshot.Verify(ctx, data, schema, loadOpts...)

// Read summary metadata and statistics without full deserialization
// Cost and peak memory scale with file size: the instance sections are decoded
info, result := snapshot.Info(ctx, data)

// Read header metadata only, from []byte — skips instance body and
// integrity check. It still scans the whole document to check its shape,
// so cost scales with file size; HeaderOnlyRead is the O(header) sibling.
header, result := snapshot.HeaderOnly(ctx, data)

// Streaming sibling: read header metadata from any io.Reader without
// materializing the full document into memory. Reads at most
// snapshot.MaxHeaderSize (16 MiB) bytes from the reader.
header, result := snapshot.HeaderOnlyRead(ctx, r)

// Write .ys bytes atomically to disk — tmp+fsync+rename. Consumers
// needing crash-safe persistence should use this instead of os.WriteFile.
if err := snapshot.WriteFile(path, data); err != nil { /* ... */ }
```

`Load` does not re-validate instance data by default: it returns what was written. The header's attestation is the writer's claim, not a proof — `graph.RebuildSnapshot` is exported, so any process can sign a document whose instances never earned it, and a `.ys` can hold values outside their constraints, invariant violations, and edges that would fail the graph layer's Add-time guards. Pass `WithRevalidation` to run every instance back through the real validator, or `WithValueConformance` for the narrower canonical-form check. `Load` always performs structural validation of the `.ys` format itself and verifies schema compatibility using `schema.StructuralHash`.

`HeaderOnly` and `HeaderOnlyRead` intentionally skip integrity verification — the returned `HeaderInfo.IntegrityHash` is the stored value, not a verification result. Use `Verify` when the document's hash must be confirmed. Neither function consults a schema; dispatch callers use the `HeaderInfo.SchemaHashMatches(s)` helper (see [SchemaHashMatches — dispatch-site cross-check](#schemahashmatches--dispatch-site-cross-check) below) to compare the stored schema hash against a loaded `*schema.Schema`.

### Marshal Options

| Option | Description |
| ------ | ----------- |
| `WithIndent` | Indentation string (`""` for compact, `"\t"` for tabs) |
| `WithCreatedAt` | Set `created_at` timestamp (omitted by default for determinism) |
| `WithMetadata` | User-provided key-value annotations |

### Load Options

| Option | Description |
| ------ | ----------- |
| `WithIssueLimit` | Maximum issues `Load` and `Verify` store (default 100, matching `schema.Load`; 0 for unlimited). The walk always completes, so `DroppedCount` is exact |
| `WithIntegrityCheck` | Whether the integrity hash is verified (default `true`); `false` skips it for debugging hand-edited files |
| `WithValueConformance` | With `true`, report a stored `Timestamp`/`Date`/`UUID` value that does not conform to its constraint (`W_SNAPSHOT_VALUE_NONCONFORMING`, Warning); not re-validation |
| `WithRevalidation` | Run every instance — composed children, edges, invariants included — back through `instance.Validator`, reporting each finding at the given severity; `Error` refuses the load. A `Required` unresolved record draws `W_SNAPSHOT_UNRESOLVED_REQUIRED` |

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

    // Content summary. Types is the whole denotation set; InstanceCounts
    // holds one entry per type the snapshot itself holds.
    Types           []TypeRef
    InstanceCounts  map[TypeRef]int
    TotalInstances  int
    TotalEdges      int
    DuplicateCount  int
    UnresolvedCount int

    // The header's validity claim, nil for a pre-v0.15.0 document.
    Attestation *graph.Attestation

    // File metadata.
    FileSize        int64  // len(data)
    IntegrityStatus string // "ok", "mismatch", or "skipped"
}
```

`snapshot.TypeRef{SchemaPath, Name string}` is the schema-less display surface: `Info`, `HeaderOnly` and `HeaderOnlyRead` run without an import closure to resolve against, so each type is reported as the identity the document states, and two same-named types in different schemas stay distinct — both in `Types` and as `InstanceCounts` keys. `TypeRef` is comparable, and its `String()` and `MarshalText()` render `path#name`. The `#` separator is deliberate beside `schema.TypeID.String()`'s `path:name` form: TypeID's rendering is byte-order-bearing (the wire's types-table sort rides it), so the display form stays visibly distinct rather than moving wire bytes to unify the two.

`TypeRef` renders and does not parse. It carries no `UnmarshalText`, so `SnapshotInfo` and `HeaderInfo` serialize one way: `yammm snapshot info --format json` writes them, and nothing reads them back. This is a decision, not a gap — the surfaces are a report projection, the in-process way to scan many documents is `ScanDir` / `ScanDirSlice`, which hands back `HeaderInfo` values directly, and `path#name` is injective over the values yammm produces but not over an arbitrary `TypeRef`. Adding `UnmarshalText` is additive and stays available if a consumer needs it.

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
    CreatedAt           string // RFC 3339 or empty
    Metadata            map[string]string

    // Types table (adjacent to the header; read in the same streaming pass).
    Types []TypeRef

    // The header's validity claim, nil for a pre-v0.15.0 document.
    Attestation *graph.Attestation

    // File metadata. HeaderOnly reports len(data); ScanDir reports the
    // file's size on disk; a bare HeaderOnlyRead reports zero, because a
    // size is not knowable from an io.Reader.
    FileSize int64
}
```

`HeaderOnlyRead` accepts any `io.Reader` and reads at most `snapshot.MaxHeaderSize = 16 * 1024 * 1024` bytes (16 MiB — headroom for consumers that carry large work-set arrays in header metadata). Larger headers are rejected with an Error-severity `E_SNAPSHOT_MALFORMED` issue whose message begins `header exceeded MaxHeaderSize` — distinguished from generic JSON-parse failures so operators can diagnose the cause. Reader errors (`io.EOF`, `io.ErrUnexpectedEOF`, or arbitrary I/O errors) during the header read surface uniformly as `E_SNAPSHOT_MALFORMED` rather than as a bare error return. Context cancellation is checked once at function entry; individual `Read` calls on the passed reader are not cancellable mid-read.

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

#### UnknownTypes — the other half of the same cross-check

`HeaderInfo.UnknownTypes(s *schema.Schema) []TypeRef` returns the header's types-table rows that `s`'s import closure does not declare. An empty result means every row binds at `Load`.

```go
if !header.SchemaHashMatches(s) {
    return stateStaleSchema  // the schema shape changed under one source path
}
if unknown := header.UnknownTypes(s); len(unknown) > 0 {
    return stateStaleSchema  // the same shape, recorded under different paths
}
```

The two are complements, and a dispatch caller wants both. `schema.StructuralHash` hashes names and never source paths, so a snapshot written under one schema layout and read under another **passes** the hash check, classifies as resumable, and then fails at `snapshot.Load` with one `E_SNAPSHOT_UNKNOWN_TYPE` per row. Both checks run on a header-only read, before any body decode.

The returned rows and the closure's own `SourceID().String()` values side by side are the whole diagnosis; log them.

Nil-safety: a nil receiver returns nil, and a nil schema returns every row. `SnapshotInfo` carries the same `Types` rows and deliberately has no such method — a caller holding one has already paid the full decode, which is the cost this method exists to avoid.

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
    Name     string               // basename, e.g., "CA.ys"
    Path     string               // full path, filepath.Join(dir, Name)
    Header   *snapshot.HeaderInfo // nil when Result has error-severity issues
    Result   diag.Result          // OK on the happy path; carries errors per-file
    FileSize int64                // size on disk, or zero if the file could not be opened
    ModTime  time.Time            // modification time, or the zero Time if unknown
}

// Lazy iterator — headers are parsed on demand; callers can break early
// without paying the parse cost for remaining files.
func ScanDir(ctx context.Context, dir string) iter.Seq2[ScanEntry, error]

// Materializing convenience wrapper.
func ScanDirSlice(ctx context.Context, dir string) ([]ScanEntry, diag.Result)

// The same two, under options. Given none they are the two above exactly.
func ScanDirWith(ctx context.Context, dir string, opts ...ScanOption) iter.Seq2[ScanEntry, error]
func ScanDirSliceWith(ctx context.Context, dir string, opts ...ScanOption) ([]ScanEntry, diag.Result)

// Reject a file before it is opened.
func WithScanFilter(keep func(ScanCandidate) bool) ScanOption

type ScanCandidate struct {
    Name string // basename, e.g., "CA.ys"
    Path string // full path, filepath.Join(dir, Name)
}

// Info describes the file the open would read; the stat follows a symlink.
func (c ScanCandidate) Info() (fs.FileInfo, error)
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

Both `HeaderInfo` and `SnapshotInfo` carry `CreatedAtTime() (time.Time, bool)`, which parses `CreatedAt` under RFC 3339. It reports `false` for an empty value **and** for a malformed one: a caller that only wants the timestamp cannot act on the difference, and one that guesses picks the zero time, which sorts first and silently corrupts any chronological ordering. A caller that must tell absent from corrupt tests `CreatedAt != ""` first.

yammm writes UTC at second precision, so a time given to `WithCreatedAt` does not round-trip its sub-second part. The parser accepts more than yammm writes, deliberately — `UpdateMetadata` preserves a foreign header byte-for-byte, and fractional seconds and non-UTC offsets both parse under this layout and keep their offset.

`FileSize` and `ModTime` are populated whenever the file was opened, **including when its header then failed to parse** — a corrupt file still occupies disk and still has a modification time, and a caller accounting for either needs it before it branches on `Header`. When the header did parse, `Header.FileSize` repeats the size; there is no `Header.ModTime`, because a modification time is filesystem metadata about the container rather than a property of the document, and neither `HeaderOnly` nor `HeaderOnlyRead` could answer it. Both come from a stat of the **opened handle**, so for a symlinked `.ys` they describe the target rather than the link. A stat failure leaves both zero and raises no diagnostic. Neither case needs a compensating `os.Stat` per entry.

**Error surface, in two categories:**

- The iterator's second yielded value (the `error`) is non-nil ONLY for operation-level failures that end iteration: a dir-open error (`ENOENT`, `EACCES`, `ENOTDIR`, ...) is yielded as a single `(ScanEntry{}, err)` pair wrapping the underlying `os` error; context cancellation observed between files is yielded as `(ScanEntry{}, ctx.Err())`. The zero-value `ScanEntry` signals "no file was reached." Cancellation observed between files takes precedence over any concurrent per-file failure.
- Per-file failures (corrupt header, per-file `os.Open` / `Read` failure) live on `ScanEntry.Result`; the iterator's error is `nil` for those and iteration continues. A failed `os.Open` surfaces as a Fatal `E_SNAPSHOT_IO`, synthesized by the scan. Everything after the handle exists — a corrupt header, and any read error partway through it — comes from `HeaderOnlyRead` as an Error-severity `E_SNAPSHOT_MALFORMED`; that function never emits `E_SNAPSHOT_IO`.

**Filtering:**

- Only regular files are included, whatever an entry is named: a directory, FIFO, socket, or device ending in `.ys` is skipped rather than opened. Of those, only basenames ending in `.ys` are scanned.
- Files whose basename ends with `snapshot.TmpSuffix` are skipped — the atomic-write staging files that `WriteFile` may leave behind on crash. Both primitives key off the shared exported constant; the single source of truth keeps them from drifting.
- Symlinks are followed: one is included when its target is a regular file and skipped when it is not. A broken symlink is included and yields a per-file Fatal `E_SNAPSHOT_IO` entry with the underlying `os.Open` error surfaced as a detail.
- Entries are yielded in the order returned by `os.ReadDir`, which sorts by filename.
- A `WithScanFilter` predicate, when one is configured, runs **last** — after every rule above — and rejects the file before it is opened.

**Pre-open filtering.**

`ScanDirWith` and `ScanDirSliceWith` accept `WithScanFilter`, which admits only the files its predicate returns true for and decides **before the file is opened**: a rejected file yields no `ScanEntry`, costs no open, and parses no header. A rejection is not an error — nothing is reported for it.

The predicate is consulted for exactly the entries the unfiltered scan would yield, and for no others. Running last is what makes that true, and it is also what keeps the non-regular-file guard ahead of caller code: `os.Open` on a FIFO blocks until a writer appears, so a name-only predicate must never be able to admit one.

`ScanCandidate.Info()` describes the file the open would read. The stat **follows a symlink**, so it answers for the target — the same file `FileSize` and `ModTime` will describe — where the directory read's own `DirEntry.Info()` would answer for the link. One candidate costs at most one stat however often `Info` is called, and none at all when no predicate asks. A broken symlink still reaches the predicate, and `Info` returns the stat error rather than a zero `FileInfo`; a predicate that ignores that error and dereferences the result will panic.

Two properties are worth stating plainly. The predicate cannot end iteration and is not handed a context — it runs between the scan's own cancellation checks, so a predicate that blocks makes the scan unresponsive until it returns, and one that panics unwinds the range. And `Info` observes the file *before* the open while `ScanEntry.ModTime` and `FileSize` observe it *after*: a file rewritten in between reports values the predicate did not see.

Cost per file, against an unfiltered scan: a rejected file saves the open, the fstat and the header read; an admitted one pays one extra stat, or none when the entry is a symlink, whose stat the regular-file check has already paid for.

```go
// Skip .ys files older than the retention window without opening them.
cutoff := time.Now().Add(-7 * 24 * time.Hour)
recent := snapshot.WithScanFilter(func(c snapshot.ScanCandidate) bool {
    info, err := c.Info()
    return err == nil && !info.ModTime().Before(cutoff)
})
for entry, err := range snapshot.ScanDirWith(ctx, dir, recent) {
    // ...
}
```

**`ScanDirSlice` semantics:**

`ScanDirSlice` materializes the full iteration into a slice plus an outer `diag.Result`. The outer Result surfaces operation-level errors only (dir does not exist → Fatal `E_SNAPSHOT_IO`; context cancellation → Fatal `E_CONTEXT_CANCELLED`); per-file errors remain on each `ScanEntry.Result`. Context cancellation returns *partial* results — the returned slice contains entries processed before cancellation, and callers who want fail-fast-on-cancel check `result.HasFatal()` before consuming the slice. `ScanDirSliceWith` is the same wrapper under the same `ScanOption` values the iterator takes — a filter's rejections are simply absent from the slice and contribute nothing to the outer Result.

**CLI integration.** `yammm snapshot info --dir <path>` wraps `ScanDirSlice` to produce a tabular per-file summary (text) or a `[]{name, path, file_size, mod_time, header, issues}` JSON array (`mod_time` is RFC 3339 in UTC, empty when the file could not be stat'd). The flag is mutually exclusive with the positional file argument; single-file mode continues to work unchanged.

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

**When to use which.** `UpdateMetadataOrReMarshal` is the default entry point for most consumers: it runs the fast path and transparently falls back to `Load + Marshal` whenever the fast path reports an Error or a Fatal — a body-offset failure, a malformed header, an unsupported version, feature or hash algorithm; anything but cancellation — emitting a Warning-severity `W_UPDATE_METADATA_FALLBACK` on the returned `diag.Result` so operators can observe the transition. When the fallback then fails too, the returned result is the union of both passes, so an issue the two share appears twice. The primitive `UpdateMetadata` stays exported for consumers who genuinely need the strict fast path — benchmarks isolating the fast-path cost, or workflows where any Load + Marshal round-trip is operationally unacceptable.

**Speedup.** On a 20 MB `.ys` input the fast path is several times faster than the equivalent `Load + Marshal` round trip. **The enforced lower bound is 3×**: `TestUpdateMetadataRatioFloor` in the `snapshot` package measures both paths over a median of five samples and fails below that ratio. This document states the floor rather than an absolute timing, because the measured ratio varies with hardware and falls as `UpdateMetadata` gains header checks before the body splice.

**Preserve-vs-override.** By default `UpdateMetadata` preserves the existing `created_at` byte-for-byte from the input header. Pass `WithUpdateCreatedAt(t)` to override; there is no other way to change `created_at` via the metadata-rewrite path. A **zero** `time.Time` preserves rather than overriding, matching what `Marshal` does with a zero `WithCreatedAt` — so a caller that parsed a timestamp and lost it cannot stamp `0001-01-01T00:00:00Z` over a good value. Do not read `created_at` and hand it back to preserve it: preservation is the default, and re-formatting rewrites a foreign header's fractional seconds and offset for no gain. Read the value with `CreatedAtTime()` and leave the option alone. `metadata` itself is replaced entirely — there is no merge. Callers retrieve the current metadata via `HeaderOnly`, copy it, apply their changes, and pass the result.

**Failure modes.** The primitive returns structured diagnostics with stable codes:

- `E_SNAPSHOT_MALFORMED` — the document's outer shape or header is malformed: the four top-level keys absent, repeated, out of order, `null`, joined by a fifth key or followed by trailing bytes; a header that does not parse (truncated JSON, missing required fields); or a `types` table stating one identity on two rows. Same code `HeaderOnly` / `HeaderOnlyRead` emit for equivalent conditions.
- `E_SNAPSHOT_UNSUPPORTED_VERSION` — the header states a version no read path accepts (v1 and v2 included; v3 is the only readable version). The document is refused rather than relabelled — `UpdateMetadata` is a header rewrite, never a migration — and `UpdateMetadataOrReMarshal`'s `Load` fallback refuses it the same way.
- `E_UPDATE_METADATA_BODY_OFFSET` — the header parsed cleanly but the body-boundary tracking resolved to an unexpected byte pattern, indicating the input does not match the shape `Marshal` produces. Byte-identical recovery via the fast path is not possible; `UpdateMetadataOrReMarshal` falls back to `Load + Marshal` automatically.
- `E_SNAPSHOT_UNSUPPORTED_FEATURE` — the header's `features` array holds an entry this version does not recognize. One issue per unrecognized entry.
- `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` — the header's `schema_hash_algorithm` is not `schema.StructuralHashVersion`. Error-severity here; the Warning downgrade applies only to header-only reads.
- `E_CONTEXT_CANCELLED` — ctx was cancelled at entry. Propagates as cancellation without re-attempting via the slow path.

**Wire-format contracts.** The primitive depends on two contracts documented in `snapshot/wire.go`'s package Godoc: the field-order contract (top-level keys are `{yammm_snapshot, types, instances, diagnostics}` in that order) and the body-byte-range stability contract (the byte range from the `,` after the header value through the document's closing `}` is reused verbatim). Both are pinned by `TestWireFormat_TopLevelKeyOrder` and `TestWireFormat_BodySuffixContract` in `snapshot/wire_test.go`, so a future Marshal-side shape change that would silently break the primitive fails at the wire-format test level.

**CLI integration.** `yammm snapshot update-metadata --set key=value [--unset key] <file>` wraps the primitive for operator tooling. `--set` and `--unset` are both repeatable; at least one is required. The command uses the strict fast path (not the fallback wrapper) — a body-offset failure surfaces as `ExitValidation` (1) with `E_UPDATE_METADATA_BODY_OFFSET` in the diagnostic output; the recovery path is a fresh `yammm snapshot save`. The write is atomic via `snapshot.WriteFile`.

### Wire Format Versions

The `.ys` wire format uses an integer version field in the header for forward evolution, and this package reads one version. yammm v0.12.0 introduced v3 — the `types` section is a table of `{schema_path, name}` identity rows, `instances` is a list of `{type, items}` groups keyed by table row, and every other type reference is a nullable row index that never defaults — and retired every earlier version. v2 named types where it needed to denote them, which no name can do for a transitively imported type or for two same-named types in different schemas.

`snapshot.MinReadableVersion` (exported constant, value `3` since yammm v0.12.0) names the lowest version this package accepts on read paths. The `currentVersion` (unexported, value `3`) is the version emitted on every write. The accept range on read is the closed interval `[MinReadableVersion, currentVersion]`; documents outside the range surface Error-severity `E_SNAPSHOT_UNSUPPORTED_VERSION` with the observed version and the supported version named in the message. **v1 and v2 documents are no longer read** — v3 is the only readable version. An older reader (yammm v0.11.0 and earlier) rejects a v3 document the same way, so an operator running an older binary against a v0.12.0-written `.ys` sees a structured diagnostic rather than a misread types section.

See [`docs/VERSIONING.md`](VERSIONING.md) for the full pre-1.0 / post-1.0 wire-format policy.

## Diagnostics

The `diag` package implements YAMMM's five-level severity model. See [Severity Levels](SPEC.md#severity-levels) and [Diagnostic Codes](SPEC.md#diagnostic-codes) in the language specification for the semantic definitions.

### Result Methods

Every non-empty `Result` comes from a `Collector`; `diag.OK()` is the constructor for an empty success result and needs none. For the terminal one-shot case, collect into an unlimited collector and take its `Result()` immediately (`c := diag.NewCollector(0); c.Collect(issue); return nil, c.Result()`). The issue iterators return `iter.Seq[Issue]`; collect a `[]Issue` with `slices.Collect(result.Errors())` when you need one.

```go
// Status checks
result.OK()             // No fatal or error issues
result.HasErrors()      // Has fatal or error issues
result.HasFatal()       // Has fatal issues
result.HasWarnings()    // Has warning issues
result.HasCode(code)    // A retained issue with the given code, at any severity. For gating that must stay truthful under truncation use CodeCounts/HasErrors/SeverityCounts
result.LimitReached()   // Issue collection limit was reached

// Issue access (returns iter.Seq[Issue]; use slices.Collect for a []Issue)
result.Issues()                          // The retained issues; incomplete when DroppedCount() > 0
result.Errors()                          // Fatal and error issues
result.BySeverity(diag.Warning)          // Issues at a specific severity

// Metadata
result.Len()              // Retained issue count; the total seen is Len() + DroppedCount()
result.Limit()            // Configured collection limit
result.DroppedCount()     // Issues dropped after limit
result.SeverityCounts()   // Counts by severity level
result.CodeCounts(diag.Warning) // Seen-based per-code counts at one severity — a copy; truthful under truncation where HasCode is not
result.TruncationNote()   // Canonical dropped-issues line; "" when nothing was dropped

// Conversion
result.Err()              // Returns error if !OK(), nil otherwise
result.String()           // "OK" when there is no issue at all; otherwise a summary line (errors, warnings, info, hints, the limit note) and one line per retained issue at every severity
```

### Rendering Diagnostics

```go
renderer := diag.NewRenderer(
    diag.WithSourceProvider(provider),   // source text for excerpts
    diag.WithExcerpts(true),             // show source excerpts
    diag.WithModuleRoot("/project"),     // strip prefix from paths
    diag.WithColors(true),              // ANSI color output
    diag.WithDistinguishFatal(true),    // distinguish fatal from error
)
output := renderer.FormatResult(result)

// Structured output — the only JSON renderer
raw := renderer.FormatResultJSON(result)
```

`FormatResult` and `FormatResultJSON` are the whole rendering surface. The per-issue renderers (`FormatIssue`, `FormatIssues`, `FormatIssueJSON`) were removed in v0.12.0.

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
    Result Result
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

The `adapter/json` package parses JSON/JSONC into raw instances.

### Adapter Creation

```go
adapter := json.New()
```

Input is preprocessed as JSONC: comments and trailing commas are tolerated.

### Parsing

`ParseObject` is the whole parse surface. It accepts `[]byte` data:

```go
// Parse a top-level object keyed by type name: {"Person": [...], "Car": [...]}
byType, result := adapter.ParseObject(ctx, source, data)
```

`ParseArray`, `ParseTypedArray` and `ParseOne` were removed in v0.12.0.

### Serialization

```go
// Serialize a snapshot to JSON bytes
data, err := adapter.MarshalObject(ctx, snap, writeOpts...)

// Write a snapshot to an io.Writer (returns bytes written). The document is
// built in memory first, so peak memory is the whole serialized snapshot.
n, err := adapter.WriteObject(ctx, w, snap, writeOpts...)

```

The object output is keyed by rendered type name. When two types in the snapshot render the same name — a transitively imported type beside a same-named local one — `MarshalObject` and `WriteObject` return an error naming both identities instead of merging the pair under one key.

The writers emit exactly what `ParseObject` and `instance.Validator` accept (v0.15.0): an association renders as a `_target_<pk>`-keyed object with its edge properties beside the key fields — one object for `(one)`, an array of objects for `(many)` — with key components and edge-property values rendered through their constraints' canonical forms. A composition renders as an array of child objects for every multiplicity. Unresolved edges are not written; the round-trip identity is scoped to fully resolved graphs.

### Write Options

| Option | Description |
| ------ | ----------- |
| `WithIndent` | Indentation string for formatted output |

### JSONC Support

The adapter always preprocesses input with `tidwall/jsonc`; there is no option to turn it off.

- Strips `//` and `/* */` comments
- Removes trailing commas
- Preserves byte offsets and line breaks, so a byte position in the preprocessed text still addresses the original

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
| `WithLabelSeparator` | Separator between the schema name and the type name inside one label (default: `__`) |
| `WithLabelPrefix` | Prefix for generated labels. Applied only when a schema name is supplied to `Label`; an unscoped label is the sanitized type name alone |
| `WithScalarTypeConstraints` | Emit `PROPERTY_TYPE` constraints — `REQUIRE n.p IS :: T` — for list-shaped and scalar types alike (Enterprise only). Property-type constraints require Neo4j 5.9; on a 5.0–5.8 server this option is what turns them off |
| `WithRequiredOnlyTypeConstraints` | Emit type constraints only for required properties |
| `WithNodeKeyConstraints` | Emit `NODE KEY` constraints (requires Neo4j 5.7+) |
| `WithNamedConstraints` | Emit a name on each constraint. `IF NOT EXISTS` is emitted either way; names are what `DROP CONSTRAINT` and diff tooling need |

### Edition Gating

Enterprise edition supports all constraint types (UNIQUE, NOT NULL, NODE KEY, PROPERTY_TYPE). Community edition supports UNIQUE constraints only. NOT NULL and PROPERTY_TYPE are omitted there — they have no Community equivalent to fall back to — and `W_NEO4J_EDITION_CONSTRAINT_OMITTED` (Warning) reports the omission once per call with a count per kind, so the guarantees the schema declares and the database will not hold are named in the result rather than silently absent from the script (v0.14.0).

NODE KEY is the exception: it *encodes* UNIQUE + NOT NULL rather than expressing a guarantee of its own, so under Community it degrades to its UNIQUE half instead of being dropped, and `W_NEO4J_NODE_KEY_UNSUPPORTED` (Warning) reports the substitution. Community output is therefore identical whether or not `WithNodeKeyConstraints(true)` is set. Dropping it whole would take the primary key's NOT NULL with it — that NOT NULL is skipped whenever a NODE KEY is meant to cover it — leaving the primary key wholly unenforced, which is what this behavior fixes (v0.9.1).

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

Indexes are derived from a schema's `@index`, `@@index`, `@vector`, `@fulltext`, and `@@fulltext` annotations:

```go
// Generate Cypher CREATE INDEX / CREATE VECTOR INDEX / CREATE FULLTEXT INDEX statements
statements, result := adapter.IndexesForSchema(ctx, s)

// Generate structured index descriptors
indexes, result := adapter.IndexesStructured(ctx, s)
```

The `Index` struct contains `Name`, `Kind` (`IndexRange`, `IndexVector`, or `IndexFulltext`), `Label`, `Properties` (declared order, significant for composites), `VectorDimensions`, `VectorSimilarity`, and the complete `Statement`. A property-level `@index` yields a single-property range index; a type-level `@@index(a, b)` yields a composite range index; a property-level `@vector(cosine|euclidean)` yields a vector index whose dimension comes from the property's `Vector[N]` constraint; a property-level `@fulltext` and a type-level `@@fulltext(a, b)` yield fulltext indexes rendered with the `CREATE FULLTEXT INDEX ... FOR (n:Label) ON EACH [n.a, n.b]` list form.

Index names are always emitted (`{label}_{props}_idx` for range, `{label}_{prop}_vector_idx` for vector, `{label}_{props}_fulltext_idx` for fulltext). The readable name is not injective — it joins on underscores, which property names may themselves contain — so two indexes whose names would collide each receive a short deterministic digest suffix. Only names that would actually clash are suffixed. Load-time validation defers eligibility whenever a target's type cannot be resolved, so the adapter re-checks: an annotation naming a property the type does not have reports `E_NEO4J_UNKNOWN_PROPERTY`, and one naming a property whose type cannot carry the index reports `E_NEO4J_INVALID_INDEX_TARGET`. Indexes are emitted for every edition (range, vector, and fulltext indexes are core query features on both Community and Enterprise); abstract types are skipped, part types are not. The emitted `CREATE VECTOR INDEX ... OPTIONS` statement form requires Neo4j 5.15+.

A declared fulltext index carries no analyzer or `eventually_consistent` configuration — no `OPTIONS` clause is emitted, so the server default applies — and the diff therefore does not compare analyzer configuration: a remote fulltext index with a custom analyzer but matching (label, properties) reads as a Match. That is a documented known limit of the v1 fulltext surface, not an accident.

### Graph Shape

```go
// Compute the graph shape (labels, primary keys, required fields) for a schema
shapes, result := adapter.ShapeForSchema(ctx, s)
```

`ShapeForSchema` returns a `*GraphShape` whose `Types` map is keyed by `schema.TypeID` and holds `NodeShape` values. Each `NodeShape` describes the `Type` (original yammm type name), `Label` (fully qualified Neo4j label), `PrimaryKeys`, `RequiredFields`, and `ImmutableKeys` (the type's `@writeOnce` properties, always populated and non-nil even when empty) for a type.

The walk covers the whole import closure (`Schema.Closure()`): every member schema's non-abstract, renderable types — imported and transitively imported ones included — get a shape, labelled under the declaring schema's name (v0.15.0). A label collision across closure members is refused with `E_NEO4J_LABEL_COLLISION`.

### Write Queries

`BatchNodeQueries` and `BatchEdgeQueries` operate on a complete `graph.Snapshot` for high-throughput batch writes. Both refuse a snapshot in which two types render the same type name — they would share one node shape and one label — returning an error that names both identities and the rendered name.

```go
shapes, _ := adapter.ShapeForSchema(ctx, s)
nodeQueries, err := adapter.BatchNodeQueries(ctx, snap, shapes, writeOpts...)
edgeQueries, err := adapter.BatchEdgeQueries(ctx, snap, shapes, writeOpts...)
```

Both return query structs (`BatchNodeQuery`, `BatchEdgeQuery`) with `Statement` and `Params` fields, ready for driver execution. `BatchEdgeQuery` also carries `RelationType`, the Neo4j relationship type.

`BatchNodeQueries` returns a phased, ordered slice; each query carries a `Kind` (`NodeMerge`, `CompositionReplace`, `CompositionCreate`). Every node merge precedes every composition replace, which precedes every composition create (parent-first by depth) — executing the slice in order is correct, and the ordering is a documented guarantee (v0.15.0).

Composed children are written under ownership semantics: a parent write replaces its composed subtree. The replace phase deletes every part reachable from each written root through the schema's composition closure — whether or not the snapshot carries children — and the create phase rebuilds the tree fresh (`SET c = …`, never `+=`). Part nodes carry their identity in the `_composed_key` property (`graph.FormatComposedKey`); for a keyless `(many)` part the key is positional and not a stable identity across writes, which replace semantics make safe. Part DDL pairs with this: `UNIQUE` + `NOT NULL` on `_composed_key`, and no `UNIQUE`/`NODE KEY` on a part's declared primary key.

`BuildBatchRelationshipMergeQuery` returns the UNWIND-batched relationship MERGE template those edge queries use internally. It is exported for a consumer resolving edges the adapter write path cannot see — one whose edges cross datasets, for instance — that wants the template without the surrounding param-and-chunk plumbing. It is a pure function: no execution, no driver dependency, no side effects.

```go
stmt := neo4j.BuildBatchRelationshipMergeQuery(
    "app__Author", []string{"author_id"},
    "WROTE",
    "app__Book", []string{"book_id"},
    true, // append SET r += row.rel_props
)
```

The `$rows` parameter carries each endpoint's key properties under two exported prefixes, which are the row shape's contract:

| Constant | Value | Meaning |
| -------- | ----- | ------- |
| `RelFromRowPrefix` | `from_` | Prefix for the source endpoint's key properties in a row |
| `RelToRowPrefix` | `to_` | Prefix for the target endpoint's key properties in a row |

```go
rows := []map[string]any{{
    neo4j.RelFromRowPrefix + "author_id": "a-1",
    neo4j.RelToRowPrefix + "book_id":     "b-1",
    "rel_props": map[string]any{"year": int64(1954)},
}}
```

Assemble rows through the constants rather than through literals. A prefix the consumer spells itself is a prefix nothing keeps in step, and a disagreement is silent: the MATCH binds null, merges nothing, and reports no error. The template always ends with `RETURN count(*) AS matched_rows`; see [Relationship match counting](#relationship-match-counting) for what that column does and does not tell you.

### Write Options

| Option | Description |
| ------ | ----------- |
| `WithNodeChunkSize` | `UNWIND` batch size for node queries (default: 5000) |
| `WithEdgeChunkSize` | `UNWIND` batch size for edge queries (default: 5000) |

A type's write-once properties come from its `@writeOnce` annotations, which `ShapeForSchema` records on each `NodeShape` as `ImmutableKeys`; `ImmutableKeysFor(t *schema.Type) []string` returns them (own and inherited). The derived set per type drives the `ON CREATE` / `ON MATCH` split, selected per type in a batch. Node merges only — relationship merges have no `ON CREATE` / `ON MATCH` split.

### Relationship match counting

Every generated relationship statement ends with `RETURN count(*) AS matched_rows`, reflecting that call's (or chunk's) MERGE match count only — 0 when the MERGE is a structural no-op (silent-failure condition). Consumers issuing multiple chunks sum `matched_rows` across results to detect silent no-ops. Node statements stay `RETURN`-free: constraint violations on nodes surface as driver errors.

### Property Coercion

The write surface (`Adapter.BatchNodeQueries` / `Adapter.BatchEdgeQueries`) coerces schema-typed property values to the driver-native types Neo4j TYPE constraints require — repairing the JSON round-trip where a whole-number `Float` decodes as `int64`, and `Date` / `Timestamp` values travel as strings. The coercion chokepoint is exported so consumers writing **direct Cypher** (parameterized `MERGE` / `SET` built by hand, bypassing the `Adapter` write path) can apply the same rules:

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

// CoerceRelProps returns a copy of an edge's property map with each property the
// relation declares coerced to its driver-native type, through the same rule the
// adapter's own write path applies. The input is never mutated; a nil relation
// returns an unchanged copy. Edge properties are scalar by language rule. Keys are
// processed in sorted order, so the error names the first failing property on
// every run. For the rel_props map inside a $rows row of a hand-built relationship
// MERGE, which sits one level below what CoerceParams walks.
func CoerceRelProps(props map[string]any, rel *schema.Relation) (map[string]any, error)

```

Coercion rules: `Float` ← any Go integer width (`int`, `int8`…`int64`, `uint`…`uint64`) or `float32` → `float64` (a `float64` passes through); `Timestamp` ← a string parsed against the constraint's custom Go layout when it declares one (`Timestamp["…"]`) or RFC3339 / RFC3339Nano otherwise → `time.Time` (a `time.Time` passes through); `Date` ← `"2006-01-02"` string or `time.Time` → `dbtype.Date` (a `dbtype.Date` passes through); `Integer` ← any signed or unsigned Go integer width, or a whole `float32`/`float64` → `int64` (a fractional float, or one outside the `int64` range, is an error); every other scalar kind passes through unchanged. `List<T>` values are coerced element-wise into the concrete typed slice (`List<Float>` of `int64`s → `[]float64`, `List<Date>` of strings → `[]dbtype.Date`, and so on); a `List<Timestamp["…"]>` honors the element's custom layout too.

**Zone handling, for `Timestamp`.** The driver sends an instant either as an offset or as a time-zone identifier the server must resolve, so `Coerce` expresses every instant in a location the driver can send. Two rules, in order: `time.Local` is always re-expressed in its offset — a value built by `time.Now` carries the host's location, whose name is `"Local"` on a host with no `TZ` set and which no server resolves; and any other location is kept only when the host's tz database resolves its name (an IANA name such as `Europe/Berlin`, or a legacy name such as `EST`), otherwise it too is re-expressed in its offset. A value parsed from text lands wherever `time.Parse` put it: a `Z` suffix yields `time.UTC`, which is kept as-is; an offset matching the host's local offset at that instant yields `time.Local`, which the first rule re-expresses; any other offset yields an unnamed zone, which the second rule re-expresses in its offset: the driver selects the offset encoding by zone name, so an unnamed zone is not yet that form. The coerced value depends on the text alone and not on where the process runs. The instant never moves. A consumer binary that must keep IANA names on a host without a tz database imports `time/tzdata`.

The four transforming kinds (`Float`, `Integer`, `Timestamp`, `Date`) are **strict** — scalar and list element alike: a value they can neither pass through as already-driver-native nor repair (a non-numeric under `Float`; a non-temporal or unparseable value under `Timestamp` / `Date`) returns an error rather than reaching the driver wrong-typed. The other scalar kinds are lenient: a correct value of those is already driver-native, so there is nothing to repair or reject, and instance validation is the type authority. A nil value always passes through; an unhandled kind also returns an error (a new `schema.ConstraintKind` is caught at build time by an exhaustiveness lint).

The rendering counterpart — the string form the library stores for a `Timestamp`, `Date` or `UUID` — is `instance.CanonicalValue`, under [Value Functions](#value-functions).

**When to call:** at any direct-Cypher parameter boundary that writes schema-typed `Timestamp` / `Date` / `Float` properties — scalar or `List<…>` — e.g. an enrichment `MERGE` or relationship-maintenance query built outside the `Adapter` write path. Writes that go through `Adapter.BatchNodeQueries` / `BatchEdgeQueries`, or that already pass native Go types, need no extra coercion — those coerce their own merge keys and endpoint keys as well as their properties. Because `ParamTypes` carries the full constraint, `ParamTypesForType` is the easiest way to build one; it derives the element types lists need. When a hand-built merge key does not sit where the properties sit, coerce it explicitly — a `Date` primary key reaching the driver as a string matches no node whose property is a `DATE`, and every re-run inserts a duplicate.

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

`DiffConstraints` returns a `*ConstraintDiffResult` with **five** sets: `Match` (identical), `Drift` (three producers: a TYPE constraint whose enforced type differs; a constraint holding a desired constraint's name under a different definition; and a desired constraint whose name is already taken), `Create` (missing from database), `Drop` (in database but not in schema), and `Unverified` — constraints present on both sides whose definition could not be compared because the records carried no `propertyType`. That happens when a caller feeds `ParseRemoteConstraints` from its own introspection rather than from `IntrospectConstraintsQuery`; a server too old to report the column is also too old to hold the constraint, so the desired constraint simply lands in `Create`. Folding `Unverified` into `Match` reports an unchecked constraint as verified. A drift gate must count it as an incomplete check, exactly as on the index side.

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

`OwnedLabels` is a superset of the labels this adapter emits for the schema: it lists every member type's label, while the emitters additionally skip a type whose assembled label fails `ValidateIdentifier` — a deliberate divergence the method's own godoc states. The diff entry points take it rather than a schema name because ownership cannot be recovered from a label string: `Label` composes a label from a caller-configurable prefix and separator around two sanitized free-form names, so for any rule that tries to read a schema back out of a label there is a configuration, or a sibling schema name, that satisfies the rule without belonging to the schema.

`DiffIndexes` returns an `*IndexDiffResult` with **five** sets: `Match`, `Drift` (four producers: a vector index whose dimension or similarity differs; a definition change under a name the database already holds; an index in a state that serves no queries and will not recover (`FAILED`); and a desired index whose name or definition is already held by another object — including one this schema owns, such as an index realising a different declaration, or a constraint's backing index), `Create`, `Drop`, and `Unverified`. Composite property order is significant, a deliberate divergence from `DiffConstraints`: a same-set/different-order remote index is a distinct index — create + drop when its name differs too, and drift when it holds the desired index's name (a `CREATE ... IF NOT EXISTS` under a name the database already holds is a silent no-op). A schema-owned remote index with no declaration is reported as a drop; drops are reported, never applied.

Fulltext rows participate in that classification exactly as range and vector rows do — **with one exclusion**: a multi-label fulltext index (`FOR (n:A|B)`) is a shape no per-type annotation can declare, so it is never matched, never dropped, and never treated as serving a single-label declaration's definition (the server creates the declaration beside it); it is counted in `Excluded`, while its **name** still blocks every `CREATE`.

Two predicates behind the index diff are exported, for a consumer that classifies remote rows itself:

| Function | Description |
| -------- | ----------- |
| `DeclarableIndexType(remoteType)` | Whether a remote index type string names a kind the DSL can declare (RANGE, VECTOR, FULLTEXT, case-insensitively) |
| `RemoteIndex.Declarable()` | The whole rule: a declarable kind, at most one label, no owning constraint, and entity type NODE or unreported |

`DiffIndexes` applies the method. The kind-only function is the half a consumer wants when it enforces its own ownership and entity-type rules — adopting the full rule instead is a behavior change, not a subset, because the label-count guard and the ownership guard answer different questions. The constraint-side counterpart is a method on `Adapter` rather than on the remote value, because edition gating is part of its answer; the two are not parallel by design.

`Unverified` holds indexes that exist on both sides but whose definition could **not** be compared — the database reported no readable vector configuration (the reason names which setting was unread), or the index is still `POPULATING`. A setting the database did disclose and that disagrees outranks a second setting being unreadable, so a demonstrably wrong dimension is reported as drift rather than downgraded to unverified. They are neither confirmed in sync nor confirmed drifted. A drift gate must therefore treat a non-empty `Unverified` as an incomplete check, not a pass:

```go
diff := adapter.DiffIndexes(desired, actual, owned)
inSync := len(diff.Drift) == 0 && len(diff.Create) == 0 && len(diff.Drop) == 0 &&
    len(diff.Unverified) == 0 // omitting this reports an unchecked index as verified
```

Every schema-owned remote index the caller passes in is accounted for exactly once: it matches a desired index, is reported as a drop, or — for a shape the schema cannot declare (TEXT, POINT, a multi-label FULLTEXT, a relationship index, a constraint's backing index) — is counted in `Excluded`. Two remote indexes sharing one semantic identity (an operator-created index alongside the schema's own) are told apart by name — the schema's own index carries the name the adapter emits — so the redundant one is reported rather than absorbed, and which is which does not depend on the order the server returned rows in. `DiffConstraints` gives the same guarantee.

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

- `RemoteConstraint` — constraint metadata (name, type, entity type, labels/types, properties, property type, and the `CreateStatement` Cypher that recreates it). `Type` is verbatim what the server reported, and the node-uniqueness spelling **depends on the server generation**: Neo4j 5.x reports `UNIQUENESS`, 2026.x reports `NODE_PROPERTY_UNIQUENESS`. The other kinds are stable. `DiffConstraints` and `InferSchema` fold both spellings internally; a consumer switching on the field itself must accept both, or it silently stops recognising every UNIQUE constraint when the database is upgraded.
- `RemoteRelationship` — relationship topology (relation type, source/target labels)
- `RemoteIndex` — index metadata (name, type, entity type, labels/types, properties, options, state, owning constraint); `VectorDimensions()` and `VectorSimilarity()` read a vector index's configuration from the options map for drift detection, and `IsOnline()` reports whether the index is in a state that serves queries (an unreported state counts as online)

`IntrospectIndexesQuery()` returns **constraint-backing indexes** as well as standalone ones — `RemoteIndex.OwningConstraint` identifies them. The diff needs them because a backing index holds its constraint's name against every `CREATE INDEX` and already serves the definition it covers, which are both conditions under which the server silently no-ops a declared index. A consumer filtering rows itself must test that field rather than assume the query excluded them.

### Utility Functions

| Function | Description |
| -------- | ----------- |
| `SanitizeIdentifier(s)` | Escape a string for use as a Neo4j label or property name |
| `ValidateIdentifier(name, context)` | Validate that a name is a legal Neo4j identifier |
| `DropConstraintStatement(name)` | Render `DROP CONSTRAINT <name> IF EXISTS` |
| `DropIndexStatement(name)` | Render `DROP INDEX <name> IF EXISTS` |
| `Constraint.DropStatement()` | The same, from a generated constraint's `Name` |
| `Index.DropStatement()` | The same, from a generated index's `Name` |

The DROP builders take a name the database already holds — from introspection or a diff result — rather than one this package generated, so they quote instead of reject. A name that would pass `ValidateIdentifier` interpolates bare; any other non-empty name, a reserved word included, is backtick-quoted with embedded backticks doubled. Rejecting a reserved word would make the object undroppable, which is the opposite of the point. An empty or all-space name is an error wrapping `ErrEmptyIdentifier`, and `Constraint.DropStatement` returns it under `WithNamedConstraints(false)`, which leaves every `Name` empty.

The caller owns the verb. Index and constraint names share one namespace, so the object blocking a desired constraint may be an index: `ConstraintDrift.Actual` with an empty `Type` and a non-empty `Name` is that case, and it needs `DropIndexStatement`. These functions build statements; they never execute one.

Cypher reserved words are not reserved by the DSL: a property named `match` or a
type named `MATCH` is valid yammm and exports cleanly through the JSON and CSV
adapters, but identifiers that appear unquoted in generated Cypher (property
names, primary keys, assembled labels) are validated during constraint and index
generation and rejected with `ErrReservedKeyword` — the check is
case-insensitive. `ShapeForSchema` validates the assembled **label** only; it is
not a property-name gate. Namespaced labels usually absorb reserved type names
(`app__MATCH` is not a keyword); a reserved property name fails wherever an
emitter validates it — and `WithScalarTypeConstraints(false)` and
`WithRequiredOnlyTypeConstraints(true)` each narrow that set. For
export-compatibility feedback before write time, run `ConstraintsForSchema`
**and** `IndexesForSchema`, or call `ValidateIdentifier` on names directly — the
index pass is what covers a property named only by an `@index`, `@@index`,
`@vector`, `@fulltext` or `@@fulltext` annotation.

## CSV Adapter

The `adapter/csv` package parses delimited data into `instance.RawInstance` values and serializes validated instances to CSV.

### Adapter Creation

```go
adapter := csv.New(opts...)
```

### Parse Options

| Option | Description |
| ------ | ----------- |
| `WithTypeColumn` | Column name for type tagging (multi-type CSV) |
| `WithListSeparator` | Separator for list elements, vector elements, and `(many)` relation groups (default `|`); read by both the parse and write sides |
| `WithSchema` | The schema, so foreign-key cells coerce through the **target** type's primary-key constraints on parse |

The delimiter is `,`, the first row is the header, and list values join on the list separator. A separator or backslash inside an element is backslash-escaped on write and unescaped on parse, so a `|`-bearing element survives the round trip.

### Parsing

Parse methods require a `*schema.Type` for type coercion. All accept an `io.Reader`:

```go
// Parse rows where all rows share a known type
raws, result := adapter.ParseTyped(ctx, source, typeName, reader, schemaType)

// Parse rows with a type-discriminator column (requires WithTypeColumn)
byType, result := adapter.ParseWithTypeColumn(ctx, source, reader, typeResolver)
```

`ParseTyped` and `ParseWithTypeColumn` are the whole parse surface; `ParseOne` was removed in v0.12.0.

The `typeResolver` parameter is a `func(string) *schema.Type` that maps type column values to schema types.

### Serialization

```go
// Serialize a full snapshot (returns one []byte per type)
byType, err := adapter.MarshalSnapshot(ctx, snap)

// Stream a snapshot (writerFor provides a writer per type)
err := adapter.WriteSnapshot(ctx, writerFor, snap)
```

Both snapshot writers key their output by rendered type name. When two types in the snapshot render the same name — a transitively imported type beside a same-named local one — they return an error naming both identities instead of merging the pair, and `WriteSnapshot` requests no writer before that check passes.

### Type Coercion

CSV values are strings. The adapter uses schema constraint metadata to coerce values:

- **Integer**: `strconv.ParseInt`
- **Float**: `strconv.ParseFloat`
- **Boolean**: `strconv.ParseBool` — every spelling it takes (`1`, `t`, `T`, `true`, `TRUE`, `True`, and the false equivalents)
- **Date**: validated as `"2006-01-02"` format, kept as string
- **Timestamp**: validated against the constraint's declared layout when it has one — that layout alone, RFC 3339 refused — and against RFC 3339 / RFC3339Nano for the default layout; kept as string
- **List**: split by list separator, elements coerced recursively
- **Vector**: split by the list separator, each element through `strconv.ParseFloat`

On the write side the adapter renders `Timestamp`, `Date` and `UUID` through their constraint, so a cell carries the same text the validator stores — including foreign-key columns, whose components render through the **target** type's primary-key constraints, and list elements, which render through the element constraint. A value the constraint cannot render is written as it arrived: an export has no diagnostic channel, and one malformed cell must not fail the file.

### Relation Columns

An association renders as dotted columns (v0.15.0): one `<field>._target_<pk>` column per target key component and one `<field>.<prop>` column per declared edge property — the same `_target_` shape the JSON adapter and `instance.Validator` exchange. A `(many)` association zips its group across the list separator: segment `i` of every column in the group describes target `i`, and the segment counts must agree (`E_CSV_COERCE` names the relation on a mismatch). An all-empty group means the association is absent; an empty segment means that optional edge property is absent on that target. Edge properties are scalars by language rule — a `List`-typed relation property draws `E_LIST_ON_EDGE` and a `Vector`-typed one draws `E_INVALID_CONSTRAINT` — which is what makes zipping well-founded.

### Limitations

CSV is a flat format. Compositions are not supported in parsing and are silently omitted during serialization; the JSON adapter carries them.

## Go Source Generation

The `adapter/gogen` package generates Go source from a schema: one struct per type, named Enum/DataType types, generated temporal types, `EDGE_` association structs, a `Graph` aggregate, and the embedded schema source. Unlike the data adapters above, gogen has no instance-data path — it is schema-in, bytes-out. Generated output is stdlib-only (it imports at most `time` and `encoding/json`).

### Generation

```go
func Marshal(s *schema.Schema, opts ...Option) ([]byte, error)
```

```go
data, err := gogen.Marshal(s, gogen.WithPackageName("model"))
```

`Marshal` requires a **completed, source-backed** schema — one loaded via `schema.Load`, `schema.LoadString`, or `schema.LoadSourcesWithEntry`. A schema loaded under `schema.WithSyntheticRoot` is **not** a supported input: `Schema.ModuleRoot()` is empty there, so the emitted keys silently stop matching the disk-loaded ones. Keep generators on disk loads. A schema built programmatically with `schema.Builder` retains no source (`Sources()` is nil) and is rejected, because the embedded source and its round-trip self-check require it.

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
| `Timestamp` | `time.Time` | — |
| `Timestamp["<layout>"]` | `Timestamp<layout>` | **yes** — one `struct{ time.Time }` per distinct layout, with a JSON codec in that layout |
| `Date` | `Date` | **yes** — `type Date struct{ time.Time }`, with a JSON codec in `"2006-01-02"` |
| `Vector` | `[]float64` | — |
| `List<T>` | `[]<elem>` | element via the same mapper |
| `Enum` (inline) | `string` underlying | **yes** — `type <Type><Field> string` + value consts |
| DataType (`type X = …`) | the underlying's Go type | **yes** — `type X <underlying>` (an Enum DataType also emits value consts); a DataType over `Date` or `Timestamp` is `type X struct{ time.Time }` with the codec its layout needs |

The library stores a `Date` as `"2006-01-02"` and a custom-layout `Timestamp` through its declared layout, and the JSON adapter writes those strings; a bare `time.Time` cannot decode either. The generated `Date` and per-layout types embed `time.Time`, so every `time.Time` method is promoted (an existing `.Format(…)` caller compiles unchanged), a value is built as `Date{Time: t}`, and their `MarshalJSON`/`UnmarshalJSON` exchange the stored form. A per-layout type is named from its layout alone — `"Timestamp"` plus the layout's letters and digits, `Timestamp20060102150405` for `"2006-01-02 15:04:05"` — so an unrelated schema edit cannot move it; a schema type of that name keeps it and the generated type takes a numbered suffix. A default-layout `Timestamp` stays `time.Time`, whose own codec already exchanges RFC 3339 with nanoseconds, the stored form.

A named DataType is rendered faithfully in every position — scalar field, list element (`List<FipsCode>` → `[]FipsCode`), edge property, and `_target_` primary-key field — never degraded to its primitive. The generated temporal types are rendered in the same positions: `List<Date>` → `[]Date`, a `Date` primary key → `Date` in its `_target_` field.

### Field and Relation Rules

- **Optional scalar** → pointer + `,omitempty` (`*string`, `*int64`, `*time.Time`, `*Date`); driven by `Property.IsOptional`.
- **Optional `List`/`Vector`** → the slice stays nilable (no extra pointer) + `,omitempty`.
- **Association** → the owner-qualified edge struct: `*EDGE_<Owner>_<field>_<Target>` (single) or `[]*EDGE_…` (many), never the target type directly.
- **Composition** → `[]*<Child>` for **every** multiplicity, `(one)` included: `adapter/json` exchanges an array for a `(one)` composition too, and a required-one composition cycle rendered as a value type would be an illegal recursive Go type.
- `,omitempty` is driven by `Relation.IsOptional`, so required relations (`(one)`, `(one:many)`) emit no `omitempty`.
- **JSON tags** preserve the yammm property or relation field name verbatim; Go field names are the mapped CamelCase identifiers.
- **Inheritance is flattened** — each struct carries its own and inherited properties and relations as direct fields (own-first ordering); no Go embedding.

### Structural Output

- **Struct per type** (including abstract and part types). Schema doc-comments carry through verbatim.
- **`EDGE_<Owner>_<edge>_<Target>` structs** for associations — owner-qualified, so they are unique by construction — carrying the target type's primary keys as `_target_<pk>` fields with the association's own properties beside them, the shape `adapter/json` exchanges. (An association whose target type has no primary key is rejected: the edge would have no `_target_` fields to identify a target node.)
- **`Graph` aggregate** — one slice field per concrete type, tagged `,omitempty` and keyed by the `schema.TagForm` name the JSON adapter keys its object by: the bare type name for the entry schema's own types, `alias.Name` for a directly imported one, so `adapter/json` output decodes into it. A transitively imported type renders bare too; two of them sharing a name fall back to their unique Go type names as keys.
- **`SerializedSources` / `SerializedEntry`** — the embedded-source surface, emitted identically by every generated package whatever its source count: `func SerializedSources() map[string][]byte` returns the whole closure keyed by module-root-relative path, and `const SerializedEntry` names the entry key. They read an unexported package-level store, so the identifiers a consumer sees do not vary with how many files a schema spans. A `SchemaHash` const carries the structural hash. The embedded source is **guaranteed re-loadable**: `Marshal` re-loads it at generation time and confirms the `StructuralHash` matches, so a non-re-loadable model is a generation error, never a silent claim.

### Imports

gogen handles the full range of yammm schemas, including schemas with `import`s: the full import closure is flattened into one self-contained package. Cross-schema identifier collisions are resolved by schema-qualification (two schemas' `Region` → `GeoRegion` / `CommonRegion`); an unresolvable same-schema clash (a type and a datatype of the same name) is a hard error. Embedded source keys are relative to the load's recorded module root (`Schema.ModuleRoot()` — the `WithModuleRoot` value when given, the entry file's directory for a plain `Load`, and the canonicalized root argument for `LoadSourcesWithEntry`; it is `""` for a `LoadString` or Builder-built schema, where gogen falls back to the entry's directory), so generated output is byte-reproducible across checkouts and CI runners and the keys match the sources' module-style import statements on re-load. `Marshal` verifies the embedded source re-loads hermetically (`schema.WithSourcesOnly` — no filesystem participation) before returning.

Re-load under a synthetic root:

```go
s, result := schema.LoadSourcesWithEntry(ctx,
    gen.SerializedSources(), gen.SerializedEntry, "",
    schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
```

Module root `"."` also re-loads, but note what it costs: it canonicalizes against the process working directory, and that directory then lands inside every `TypeID` the re-loaded schema carries. See [Synthetic source identities](#synthetic-source-identities).

**Removed in v0.13.0.** The shape-dependent `SerializedModel` and `SerializedModelEntry` declarations, and their `LoadString` / `LoadSourcesWithEntry`-with-`"."` recipes, are gone. Regenerate; every hand-written call site should already read the pair above, which v0.12.3 added.

### Validation

Generated source is run through `go/format` and then type-checked with `go/types` before return, so duplicate declarations, unused imports, and undefined references surface as generation errors rather than broken Go. The type-check uses a hermetic stub importer for `time` and `encoding/json`, declaring exactly what generated code calls, so `Marshal` needs no Go toolchain, GOROOT, or build cache at runtime.

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

The document title is the schema name.

### The Emitted Document

The document describes exactly the object form `adapter/json`'s `ParseObject` + `instance.Validator` accept:

- **Envelope** — one object keyed by type name (`{"Person": [...], "Car": [...]}`), each key an array of instances. Entry-schema **concrete, non-`part`** types under their bare name (a `part` type is `$defs`-only and an `abstract` type appears in neither position — neither can be instantiated at top level); **directly imported** types under their alias-qualified name (`common.Region` — the only form the validator resolves for them); transitively imported types are `$defs`-only.
- **Instance objects** — properties by name, relations by their lower_snake field name; `additionalProperties: false` (the instance layer rejects unknown fields); required properties in `required`.
- **Associations** — an edge object (to-one) or array of edge objects (to-many), each `$ref`ing an `EDGE_<Owner>_<field>_<Target>` def carrying the required `_target_<pk_name>` foreign-key fields (validated against the target key's own constraint — a DataType-typed key keeps its `$ref`) plus edge properties. Association **presence is deliberately not `required`**: the instance layer defers it to graph assembly, so a per-file requirement would flag files yammm validates cleanly; the generated `description` states the multiplicity, and for a **required** association the enforcement point as well.
- **Compositions** — always arrays of child objects; required compositions are `required` + `minItems: 1`; to-one compositions get `maxItems: 1` (mirroring graph-assembly enforcement).
- **Constraints** — the full mapping (bounds → `minLength`/`minimum`/…, `Enum` → inline `enum`, multi-`Pattern` → all-must-match `allOf`, `Vector[N]` → fixed-size number array, named DataTypes → `$defs` entries, `$ref`ed from the four property positions — scalar property, list element, edge property and `_target_*` key field; a DataType referenced from inside another DataType's constraint is flattened to structure, which validates identically). Schema doc-comments flow through as `description`, the JSON Schema keyword for human-readable documentation.

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
doc, err := markdown.Marshal(s, markdown.WithClassDiagram(false)) // no class diagram
doc, err := markdown.Marshal(s, markdown.WithClassMembers(false)) // diagram without member lines
```

`Marshal` requires a completed schema (always true for one returned by `schema.Load*` or `schema.Builder.Build`). Source backing is optional: on a Builder-built schema, invariant sections degrade to their message line instead of a source fence — nothing else needs source content.

### The Emitted Document

One document per invocation, covering the entry schema plus its whole import closure:

- **Title + schema doc** — `# Schema <Name>`, then the schema's doc-comment verbatim.
- **Class diagram** — a `## Class Diagram` heading, then one Mermaid `classDiagram` fence over the entire closure. Classes carry each type's **own** members as `name KindLabel` pairs (named DataTypes show their name; constraint detail stays out of the diagram) — `WithClassMembers(false)` drops the member lines and keeps the classes, stereotypes and edges; abstract and part types carry `<<Abstract>>` / `<<Part>>` stereotypes; edges are `Parent <|-- Child` for inheritance and DSL-labeled `Owner --> Target : NAME (mult)` / `Owner *-- Child : NAME (mult)` for each type's own associations and compositions — inherited structure is conveyed by the inheritance edges, not redrawn. Qualified names (invalid as Mermaid class ids) emit the sanitized-id form `class common_Region["common.Region"]`; Mermaid namespaces are deliberately not used (some Markdown renderers do not support them in class diagrams). **The labelled form needs Mermaid 10.1.0 or later** — the first release whose class-diagram grammar has the `classLabel` production; Mermaid 9 fails it with a lexical error. Only an imported type is labelled (an entry-schema type name is always a valid id), so a schema with imports is in scope and an import-free one renders on Mermaid 9. When the emitter has written a labelled class it says so in the document: one sentence under the `## Class Diagram` heading, before the fence — *This diagram uses Mermaid's labelled class form and needs Mermaid 10.1.0 or later.* An import-free document carries no such sentence and its bytes do not move.
- **Type sections** — a `## Types` heading, then one `### <TypeName>` per type in declaration order: a badge line (`*Abstract type*` / `*Part type*` / `Extends: [Parent](#parent)`), the doc-comment, then a **flattened property table** (Property | Type | Modifiers | Description) over the full inheritance chain — the Type column renders each constraint's DSL form (`String[1, 100]`, `List<FipsCode>`), and inherited rows carry `from <Owner>` in Modifiers. Associations and compositions follow as DSL-notation bullets with linked targets, an inherited relation carrying the same provenance as a ` — from <Owner>` marker; a relation's doc-comment nests as an indented line under its bullet, and its edge properties as a sub-table.
- **Invariants** — the failure message as a bullet (an inherited invariant carrying the ` — from <Owner>` marker), the doc-comment beneath, then the declaration source in a `yammm` fence extracted via the invariant's span, with its doc comment stripped (it renders separately) and continuation lines dedented.
- **Data Types** — a Name | Definition | Description table per schema.
- **Imported schemas** — one `## Schema <Name> (imported as <alias>)` section per import in closure order (transitive imports, which have no entry alias, head as plain `## Schema <Name>`), the schema's doc-comment beneath the heading when it has one, with collision-proof `### <schemaName>.<TypeName>` headings.

Arbitrary doc/enum text cannot break structure: table cells escape backslashes and pipes and fold newlines to `<br>`; a value containing a backtick renders through an entity-escaped `<code>` element; fences are sized past any backtick run in their body.

### Validation

Output is deterministic (byte-identical across runs and checkouts), so generated documents can be committed and drift-checked by regenerate-and-diff. Before returning, `Marshal` structurally self-checks: every fence closes, no column-zero heading is trapped inside an open fence, every internal link resolves to an emitted heading and is terminated, and every table separator matches its header's column count — a failure is a generation error, never emitted output. A schema whose two type headings slug to the same anchor is also rejected (a rename-able input collision, since an internal link to one would otherwise resolve to the other's section).

### CLI

```text
yammm gen --to md <schema.yammm> [--no-class-diagram] [--no-class-members] [--output <path>] [--module-root <dir>]
```

`markdown` is accepted as an alias for `md`. Per-target flag enforcement extends to the md target: `--no-class-diagram` and `--no-class-members` are rejected for other targets, and go/jsonschema-only flags are rejected for md.

## Formatting

The `format` package provides canonical formatting for `.yammm` schema files:

```go
func TokenStream(text string) (string, error)

// Two pipeline phases (3 and 4) and two phase-1 helpers, exported and consumer-less today
func WrapLongLines(text string) string
func AlignColumns(text string) string
func NormalizeIndentation(line string) string
func DisplayWidth(line string) int

const LineWidthThreshold = 100 // columns; a tab counts as 4
```

```go
formatted, err := format.TokenStream(input)
```

`TokenStream` returns an error **only** when the source fails to parse — that is, when an issue's `Code().Category()` is `diag.CategorySyntax`. A source that parses but is semantically invalid (inverted bounds, say) formats successfully. The CLI maps the error to `ExitValidation`; the LSP swallows it and returns no edits.

Before phase 1, line endings normalize to LF: CRLF and a lone CR both become LF, so a CRLF file always reports as unformatted under `yammm fmt --check`.

The formatter then applies a five-phase pipeline:

1. **Token-stream rewriting:** canonical spacing between tokens and indentation normalization — **except inside invariant expressions**, whose regions keep the author's own spacing between tokens (a continuation line's leading indentation is still normalized)
2. **Blank line collapsing:** removes excess blank lines while preserving section breaks, and *inserts* one after the schema header and after the last import when the following line is not already blank
3. **Line wrapping:** wraps long lines (enums, extends clauses, invariants) at `LineWidthThreshold`, and collapses an existing multiline enum, extends clause or datatype-alias enum back onto one line when the joined form fits. A multiline invariant is never collapsed
4. **Column alignment:** pads the member-name column so the column after it lines up — the type for a property, the multiplicity for a relationship, the `=` for a datatype alias — and aligns trailing inline comments. It runs over contiguous runs of one member kind — properties, relationships, and file-scope datatype aliases — broken by a kind change or by any line that is not alignable — a blank line, a comment-only line, a type head or closing brace, an `extends` clause, the first line of a multiline construct. A type-block boundary breaks a run because its brace lines are not alignable, not because blocks are tracked
5. **Text finalization:** trims trailing whitespace from each line, removes trailing blank lines, and ensures the file ends with a newline

Phases 2–4 read one line classification — blank, comment, or content — computed once between phases 1 and 2. No phase decides on its own whether a line is a comment, so a comment line is never wrapped, aligned, or read as an enum value or a type name, whatever its text looks like; a comment inside a multiline enum or `extends` list keeps its place and the construct is re-indented rather than collapsed.

Output is deterministic and idempotent. The formatter is used by the LSP server for `textDocument/formatting` and by the CLI for the `yammm fmt` command.
