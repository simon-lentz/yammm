# YAMMM Go Library API Reference

This document is the API reference for the YAMMM Go library, and it states the library's versioning policy.

The YAMMM language itself — grammar, types, expressions, constraints, and diagnostic codes — is specified in [SPEC.md](SPEC.md).

## Error Handling Conventions

Most load and validation functions return a `(T, diag.Result)` pair. The value half is whatever the call produces, such as a `schema.Schema`, a `[]byte`, a `[]instance.ValidInstance` or a `[]snapshot.ScanEntry`.

Some validation surfaces have no value to return and produce a bare `diag.Result`: `snapshot.Verify`, `graph.Graph.Add`, `graph.Graph.Check`, and `neo4j.Adapter.DetectLabelCollisions`. For these, `result.OK()` is the whole answer.

**Read the result first, then the value.** The two are independent, and all four combinations occur:

- A value and `result.OK()` — success. Where the value is a slice, success covers each entry that is not zero, and a `snapshot.ScanEntry` holds its own file's failure in its own `Result`. A record is a handle (see [Values](#values)), so `IsZero` reports that it holds none. The result may still carry warnings. A `Validate` slice beside an OK result may still hold a zero entry, an input that held an `instance.Refused` value (see [Instance Validation](#instance-validation)): check each entry.
- No value and `!result.OK()` — failure.
- A value and `!result.OK()` — a partial or unvalidated answer, and a documented contract on several surfaces. `snapshot.Info` returns a populated summary beside an Error-severity `E_SNAPSHOT_INTEGRITY_MISMATCH`; `snapshot.ScanDirSlice` returns the entries collected before cancellation beside a Fatal `E_CONTEXT_CANCELLED`; `instance.Validator.Validate` returns one slot per input, a zero `ValidInstance` for each failure, beside a merged result that is non-OK when any failure drew an issue. Discarding the value here loses real data.
- No value and `result.OK()` — nothing to do, or a refused record. `instance.Validator.Validate` returns `nil, diag.OK()` for nil input and the empty slice for a non-nil empty input, once the type name resolves to a type that is neither abstract nor a part type; an unknown name returns `nil` beside a non-OK result, and so does a nil or empty batch of an abstract or part type. `ValidateOne` returns a zero `ValidInstance` and `diag.OK()` for a raw instance whose one fault is an `instance.Refused` value: its producer's result holds the issue.

Use `result.Err()` to convert to a standard Go `error` when `!result.OK()`.

Adapter constructors do not fail: `neo4j.New` and `json.New` each return `*Adapter` alone. `json.New` takes no option, and an `Option` of `neo4j.New` cannot report a problem.

Pure transformations split three ways rather than one. Some return `(T, diag.Result)`, such as `snapshot.Marshal` and `neo4j.Adapter.ShapeForSchema`. Some return `(T, error)`, such as `gogen.Marshal`, `json.Adapter.MarshalObject` and `neo4j.Adapter.BatchNodeQueries`. The introspection query builders return neither: `neo4j.IntrospectConstraintsQuery` and `IntrospectIndexesQuery` return a `string`, and `IntrospectRelationshipsQuery` and `Adapter.IntrospectRelationshipsQueryFor` return `(string, map[string]any)`.

## Values

The validator reads a property value by the rules below. The graph, the `.ys` writer, and the write paths of `adapter/json` and `adapter/neo4j` hold and write what a validator stored or a `.ys` document stated. `neo4j.CoerceParams`, which takes a value from a caller that writes its own Cypher, reads absence by the rule below and every other value by its Go type alone. [Value Functions](#value-functions) lists the functions that apply the rules.

**The one form.** A scalar value is nil, a `bool`, a `string`, an `int64`, a `uint64`, a `float32`, a `float64`, a `json.Number` whose text is a JSON number literal, a `time.Time` or a `uuid.UUID`. A value of another signed integer kind is read as an `int64`, and one of another unsigned kind as a `uint64`. A list or a map is read element by element, and each element by these rules.

**Carriers.** A value is read through at most 64 pointers and interfaces. A named type whose base is a string, a bool or a number is read as its base value, so a `type Tier string` value is a `string`; `json.Number` is the one exception, and is read as itself. A list, or a map whose keys are strings, is read only as itself: one behind a pointer is no list and no map. Any other struct or array, a map whose keys are not strings, a non-nil channel, function or `unsafe.Pointer`, a complex number and a longer chain of pointers and interfaces are no value of any form.

**Text.** A string that is not valid UTF-8 is no text. The validator refuses it with `E_TYPE_MISMATCH` at a String, a Timestamp, a Date, a UUID, an Enum or a Pattern, before any rule of the kind reads its bytes.

**Absence.** A value that holds none is absent: nil; a pointer, an interface, a slice, a map, a channel, a function or an `unsafe.Pointer` that is nil, bare or behind carriers; and a nil `immutable` container (`immutable.IsNilContainer`). Absent is not empty: `[]any{}` is an empty list, and a nil slice is no list. At a property and at an edge property, an absent value is the same as a missing key: a required one draws `E_MISSING_REQUIRED`, and an optional one is left out. At an association or a composition, an explicit null is refused with `E_EDGE_SHAPE_MISMATCH`, whatever the relation's multiplicity.

**Time, date and UUID.** A valid instance holds a value at a Timestamp as text in the property's declared layout, or in RFC 3339 with fractional seconds when it declares none. It holds a value at a Date as `2006-01-02` in the value's own location, and a value at a UUID in its canonical lowercase form. The `.ys` document and the JSON writer write that text; the Neo4j adapter writes a Timestamp as a ZONED DATETIME and a Date as a DATE. A `time.Time` is valid at a Timestamp or a Date only when the text it is stored as parses back by the rule that checks a string at that kind.

**Keys.** A component of a primary key or a target key must render back unchanged through `graph.FormatKey` and `graph.ParseKey`. A validator stores no such key, and the `.ys` reader refuses one with `E_SNAPSHOT_MALFORMED`.

**Nested collections.** A List whose element is a List or a Vector is valid at the validator, and each writer writes it or refuses it. The `.ys` document and the JSON writer write it and read it back, by the same rules as any list. The Neo4j adapter refuses a value of one with `ErrUnsupportedListElem`, as its DDL refuses the property with `E_NEO4J_UNSUPPORTED_TYPE`: Neo4j has no nested collection property type.

**Ownership.** Each root, each record that a `schema.Schema` or a `graph.Snapshot` holds, and `instance.ValidInstance` and `instance.ValidEdgeData`, which the validator hands out, is a handle: a struct with one unexported pointer. Other results include plain values, such as a constraint, `immutable.Properties` or `diag.Result`. `Snapshot.Attestation` returns a pointer to a copy of a plain struct, or nil. No exported method of a handle returns a pointer to its stored state or a slice of it. An invariant's compiled tree and a pattern's compiled form have no public read: `Invariant.Span` locates an invariant's source in `Sources.Content`, from its doc comment, if it has one, through its expression, and `regexp.Compile` compiles a loaded pattern from `PatternConstraint.Patterns`. A caller holds no address of a handle's record, so `*t = schema.Type{}` does not compile. Two handles are equal when they name one record, so a handle is a map key. The zero handle names no record, and `IsZero` reports it. On a zero `schema` record an accessor panics at the call, an iterator's included; a zero `schema.Sources`, and a zero record of `graph`, `instance` or `location`, answers as holding nothing, with no panic. A handle has no JSON form, and `encoding/json` writes one as `{}`. A lookup returns `(T, bool)`. A handle is a struct and no value of any form, so a handle at a property, the zero one among them, is not absent: the validator refuses it with `E_TYPE_MISMATCH`, where v0.21.1 read a nil pointer as absent.

**Collections.** An accessor over records, or over the references that name them (`TypeRef`, `ResolvedTypeRef`), returns `iter.Seq` or `iter.Seq2`, and no slice form exists. A caller that needs a slice calls `slices.Collect` on an `iter.Seq`. A slice that an accessor returns is a fresh one, such as `Annotation.Args`, `EnumConstraint.Values`, `PatternConstraint.Patterns`, `Snapshot.Types`, `Sources.SourceIDs` and `Instance.ComposedRelations`.

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
| `WithModuleRoot` | Root directory for module-style imports. It is the first rung of the ladder in [Module root discovery](#module-root-discovery), and under it no `yammm.mod` marker is read at all. `Load` refuses a root that is not a directory it can open with a Fatal `E_LOAD_IO_FAILURE`, whether or not the schema imports anything |
| `CaptureSources(&dst)` | Store the load's sources in `dst` before the load reads anything, so a caller can render excerpts for a load that fails and returns no schema; a load that fails before it reads anything leaves `dst` holding that load's empty sources |
| `WithImportsAllowed` | Whether import declarations are processed (default `true`); `false` refuses them with `E_IMPORT_NOT_ALLOWED` |
| `WithSourcesOnly` | With `true`, restrict import resolution to pre-registered in-memory sources — a miss errors instead of reading the filesystem (hermetic loads of embedded sources). It decides where an import is read from, never a source's identity. `LoadSourcesWithEntry` alone takes it: `Load` and `LoadString` refuse it with `true` |
| `WithSyntheticRoot` | Give in-memory sources synthetic identities under a root such as `embedded://app`, so type identities do not move with the working directory (see [Synthetic source identities](#synthetic-source-identities)) |

### Module root discovery

The module root is the directory that module-style imports (`import "lib/dep"`)
resolve against, and the boundary the import sandbox is opened at. `Load`
resolves it over four tiers:

| Tier | Source of the root | Where it applies |
| ---- | ------------------ | ---------------- |
| 1 | Explicit — `WithModuleRoot`, the `--module-root` flag, or the LSP's `ModuleRoot` config | everywhere |
| 2 | Discovered — the nearest ancestor directory holding a `yammm.mod` marker | everywhere |
| 3 | The editor workspace folder containing the file | the LSP only |
| 4 | The entry schema's own directory | everywhere |

Discovery is additive: with no marker anywhere above the entry file, behaviour
is exactly what it was before — the entry schema's own directory.

```go
// The nearest ancestor holding a yammm.mod, "" and false when none does, and
// an error when a marker exists but is malformed or unreadable.
root, found, err := schema.FindModuleRoot(filepath.Dir(entryPath))

var malformed *schema.MalformedModuleRootError
if errors.As(err, &malformed) {
    fmt.Printf("%s line %d: %s\n", malformed.Path, malformed.Line, malformed.Reason)
}

// The diagnostic Load reports for either error kind. Callers outside the
// loader — the editor, a custom front end — build it from here so one code
// keeps one shape.
issue := schema.ModuleRootIssue(err)
```

`schema.ModuleRootMarker` is the marker's file name (`yammm.mod`).

**The marker's content rule.** In this version the file must be empty or hold
only comment lines, where a comment line's first non-space byte is `#`. Any
other content — including a UTF-8 byte order mark, a directory of that name, a
dangling symlink of that name, or a file over 64 KiB — draws
`E_LOAD_MODULE_ROOT_MALFORMED` at Error severity and **fails the load**. A
marker the author wrote and the loader ignored is the failure this mechanism
exists to remove, so there is no silent fall-through. Refusing content now
reserves the whole format space: no file written today can be reinterpreted by
a later release.

**The nearest marker wins**, so a nested marker deliberately narrows a
sub-module's root.

**A discovered root widens the import sandbox.** Every import read goes through
`os.OpenRoot(moduleRoot)`, so committing a `yammm.mod` at a repository root
grants the loader read access to that whole subtree for imports. That widening
is the mechanism working — it is what makes a repository-relative import
resolve — but it is a consequence worth stating before you commit the file.

**The walk is canonical.** It runs on the symlink-resolved ancestor chain, so a
marker reachable only through a symlinked spelling of the entry path is not
consulted. Pass `WithModuleRoot` for that case.

**An explicit root reads no marker.** Tier 1 wins before discovery runs, so a
malformed `yammm.mod` above a load that passes `WithModuleRoot` is never
reported. That is what "explicit wins" means: a caller can always override a
marker it did not put there.

**Import failures name the root and where it came from.** Every diagnostic in
the import-resolution family — `E_IMPORT_RESOLVE`, `E_PATH_ESCAPE`,
`E_IMPORT_CYCLE` — carries `module_root` and `module_root_origin` details, over
the five origin values `explicit`, `discovered`, `synthetic`, `default` and
`none`, and states the same provenance in its message for the text renderer.

### Synthetic source identities

`LoadSourcesWithEntry` derives each source's `SourceID` from the module root and
the key, so the identities — and therefore every `TypeID`, and the
`schema_source` a `.ys` header records — carry a filesystem path. That path moves with
the working directory, the checkout, and the container mount point.
`WithSyntheticRoot` replaces it:

```go
s, result := schema.LoadSourcesWithEntry(ctx,
    gen.SerializedSources(),  // a generated package's embedded sources
    gen.SerializedEntry,
    "",                       // no module root: the synthetic root stands in
    schema.WithSyntheticRoot("embedded://app"),
)
```

A source keyed `a/b/x.yammm` then has the identity `embedded://app/a/b/x.yammm`
on every machine. The root also stands in for the module root, so module-style
imports resolve under it. The root names every identity the load mints, so the
given sources are every source the load reads: an import they do not hold fails
as not among them, and the load reads no disk, whether or not `WithSourcesOnly`
is passed.

The option refuses rather than degrades. Each of these is an error:

| Condition | Why |
| --------- | --- |
| A root not of the form `scheme://authority`, optionally followed by a path — empty, `embedded://`, `embedded:`, `.`, `/abs` — one whose scheme is one letter or is `string` in any case, one whose authority is `.` or `..`, or one whose path climbs above its authority (`embedded://app/..`) | Without an authority two roots join one identity (`embedded://` and `embedded:` both gave `embedded:/main.yammm`), a root with no scheme reads as a directory, a one-letter scheme reads as a drive, and `string://` is the identity `LoadString` mints. A root is normalized once — the scheme in lower case, the text after it in NFC, the path cleaned — so `EMBEDDED://app/`, `embedded://app//` and `embedded://app/.` are all `embedded://app`. The authority keeps its case |
| A root holding, after its scheme, a character outside its set — a `?`, a `#`, a `%`, a backslash, a space, a control, format or other character that does not print, one of `" < > [ ] ^ { \| }` or a backtick, or a `:` or `@` in its authority — or text that is not valid UTF-8 | After the scheme a root holds ASCII letters and digits, `/`, the marks `- . _ ~ ! $ & ' ( ) * + , ; =`, `:` and `@` in its path alone, and a character past ASCII that prints: a letter, mark, number, punctuation or symbol, as `strconv.IsPrint` reports. It names no server, so it takes no port, no user, no query and no fragment, and no percent escape, which would give one root two spellings. The set is applied to the root in NFC, so U+1FEF GREEK VARIA, which NFC makes a backtick, is refused |
| A root together with a non-empty `moduleRoot` argument | The two name one concept and the load can honor only one |
| The option passed to `Load` or `LoadString` | It could only ever be a silent no-op there |
| An absolute source key — as given, or once cleaned and in NFC (`./C:/x.yammm`, or a key spelled with U+212A KELVIN SIGN, which NFC makes `K`) — or one naming the root itself or a directory above it (`..`) | An absolute key collides with file-backed identities, and neither the root nor a directory above it is a source. A key that spells a directory under the root is cleaned to a file key: `x.yammm/` and `x.yammm/.` are `x.yammm`. A key that escapes the root is allowed: it keeps its leading `..` and yields a stable, distinct identity |
| A source key holding a backslash | Windows reads a backslash as a separator and every other host as part of a file name, so one store would name different sources on each |
| A source key holding a character that does not print — a control character, a bidi or other format character, a space past ASCII, a private-use or unassigned code point | Such a character renders as nothing or reorders the text around it, so two identities could read alike. Every synthetic identity the library judges is held to this rule, a `LoadString` name among them; `location.NewSourceID` judges nothing. `location`'s functions, `schema.SyntheticImportKey` and `gogen.Marshal` return an error wrapping `location.ErrUnprintableSourceID`. A load reports its text, so `errors.Is` cannot match it there. The verdict follows the Unicode tables of the Go toolchain that builds yammm, so a character a later Unicode assigns is refused until that toolchain moves. The ASCII space stays legal in a key |

A relative import (`"./x"`, `"../x"`) resolves against the importing source's
key, as text: `"../dep"` from `a/b/main.yammm` is `a/dep.yammm`, and one that
climbs above the root keeps its leading `..`. `schema.SyntheticImportKey`
returns the key any import resolves to, which is how `gogen.Marshal` keys each
source it embeds through an import; the entry and a source nothing imports key
from their identities. `location.NormalizeSyntheticKey` is the key rule itself:
it returns a key cleaned, in NFC and with a leading `..` kept, or refuses it,
and the loader, `SyntheticImportKey` and `gogen.Marshal` all read a key
through it. A key it returns is a fixed point: the rule returns it unchanged. `Schema.ModuleRoot()` reports the synthetic root — it is the
root the load resolved imports against — so a schema loaded this way is a
supported input to `gogen.Marshal`, whose embedded keys are relative to it.

**Adopting a synthetic root changes every `SourceID` and `TypeID` the library
hands out**, and a later change to the root value changes them again. A `.ys`
snapshot written before the switch still loads: a type row holds the schema's
name and the type's name, and no path.

### Error Handling Pattern

All load functions return `(Schema, diag.Result)`:

- A zero `Schema` and `!result.OK()`: Failure (syntax errors, type resolution failures)
- A `Schema` that is not zero and `result.OK()`: Success (result may contain warnings)

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
- **References through a failed import are deferred, not re-blamed.** A failed
  import reports at its declaration. A later alias of one source draws
  `E_DUPLICATE_IMPORT` when the failure keeps the source's identity, as a compile
  failure or a cycle does. Beside it, a source that failed to compile draws that
  alias's own `E_UPSTREAM_FAIL`, and a cycle draws no second report. A file the
  loader cannot read keeps no identity, so two aliases of one
  missing file each draw `E_IMPORT_RESOLVE`. Every reference reached through such an alias is
  skipped silently. A qualifier that names
  no declared import at all remains a genuine `E_UNKNOWN_TYPE`.
- **An alias binds once (keep-first).** The first declaration of an alias
  wins at every layer; a later declaration of the same alias is reported
  with `E_DUPLICATE_IMPORT` and stays inert — not loaded, not resolved,
  not wired. References through the alias resolve against the kept first
  binding.
- **The all-or-nothing contract is unchanged.** Any error still yields a
  zero `Schema`; completeness changes what `result` carries, not the
  contract.

`LoadString` — and any load with `WithImportsAllowed(false)` — rejects import
declarations with a single `E_IMPORT_NOT_ALLOWED`, save an import whose path
derives a reserved alias, which the parser drops with `E_INVALID_ALIAS`. The
rejection does not suppress the source's other findings: the
remaining diagnostics are reported alongside it, with references through
the rejected aliases deferred. Rejected imports are never probed or
resolved.

#### Issue limit

As a load runs it stores at most 100 issues, and a Fatal that stops the load joins them past that number. The limit bounds *collection*: once it is reached, every further
issue is counted in `Result.DroppedCount()` and flagged by
`Result.LimitReached()`. **The cap retains the most severe issues seen, not the
first ones seen.** When the store is full and an arriving issue is more severe
than the least severe stored issue, the stored one is evicted and the arriving
one takes its slot; only an issue that is no more severe than the least severe
stored one is itself the drop. Arrival order breaks ties within a single
severity — among equally severe issues the earliest-arrived are retained — but
never decides survival across severities, so an error declared late in a file
still displaces a warning collected early. The *display* order of surviving issues is always deterministic.
Dropped issues still count toward `Result.OK()` / `HasErrors()` /
`SeverityCounts()` (the counts reflect every issue *seen*, not only those
stored), so truncation never flips a failing result to OK and the
all-or-nothing contract holds regardless of the limit.
Under `snapshot.WithRevalidation` the revalidator caps each row at the
`snapshot.Load` limit (`snapshot.WithIssueLimit`), so that load has one cap and
an unlimited one stores every finding a row draws. When the cap was hit,
the JSON output format carries `limitReached` and `droppedCount`, and a
truncated result merged into any collector keeps both. A cap is a collector's
setting, so a `Result` reports none. The CLI's text output appends a
dropped-issues note.

### One Object Per Source

A load holds one `Schema` per `SourceID`, so every import of one source binds one object, and a diamond holds one compile of the import it shares. A load holds one schema per name: two sources that declare one name are `E_DUPLICATE_SCHEMA`, reported at the second declaration, with `E_UPSTREAM_FAIL` on an import that reaches it. A schema whose load reported an error takes no name and draws no such report. An import that closes a cycle is `E_IMPORT_CYCLE`, on the declaration that closes it.

Each `Load` call compiles its own schemas and shares nothing with another, so `==` on two `Schema` or `Type` handles from two loads of one source is false. Compare `TypeID`s, or `schema.StructuralHash`, across loads.

## Constraint Values

The `schema` package constructs constraint values directly, for a caller that judges or coerces a value at a boundary a loaded schema does not reach.

`schema.CheckConstraint(c) error` returns why `c` cannot judge a value, or `nil` when it can. It refuses what the DSL refuses in a constraint: an argument no `.yammm` source can state — inverted bounds, a non-finite `Float` bound, a negative length, an enum with an empty or repeated value or fewer than two values, an enum value or `Timestamp` format that is not valid UTF-8, a `Pattern` with no pattern or more than two, or one `regexp.Compile` refuses or that writes a surrogate code point with a `\x{…}` escape, a `Vector` dimension outside 1 to 65536 — a `List` with no element constraint, and a constraint of a Go type the `schema` package does not construct, such as a pointer to one, each at any `List` depth. It refuses a reference under a name no `.yammm` source writes — anything but a datatype name, or an import alias, a dot and a datatype name, so `.T1`, `lib.`, `a.b.c`, a lower-case `t1` and a built-in type name such as `String`. A hand-built constraint meets no completion, so it also refuses a DataType reference that resolves to no constraint or to another reference alone, at any `List` depth. The Neo4j adapter's `CoerceParams`, and `instance.CheckValue` and `CanonicalValue`, call it before they read a value.

`schema.NewPatternConstraint` keeps every pattern it is given, and compiles each again with `regexp.Compile` from its source text, as a `.yammm` source compiles it. A pattern therefore matches by its source's Perl reading however its regexp was built: under `regexp.CompilePOSIX`, `^b$` matches `"a\nb"`, and in a `PatternConstraint` it does not. A source that reading refuses, which POSIX syntax can accept (`a**`), is kept as given, and `schema.CheckConstraint` refuses the constraint, as the DSL refuses `Pattern["a**"]`.

### Annotations

Annotations declared in `.yammm` are carried on the loaded schema:

- `Property.Annotations()` / `Annotation(name) (Annotation, bool)` — a property's `@name` annotations.
- `Type.Annotations()` / `AllAnnotations()` — a type's `@@` members (own, and own-plus-inherited).
- `Annotation` exposes `IsZero()`, `Name()`, `Args() []AnnotationArg`, `Documentation()`, `Span()`; `AnnotationArg` exposes `Text()`, `Kind() AnnotationArgKind`, `Span()`.
- `schema.AnnotationSpecs() []AnnotationSpec` returns the built-in registry for editor tooling — a read-only surface, not a registration API. Each `AnnotationSpec` exposes `Name()`, `Placement() AnnotationPlacement` (`PlacementProperty` or `PlacementType`), `Documentation()`, and `ArgHint()`.
- `schema.VectorSimilarityFunctions() []string` returns the similarity keywords `@vector` accepts, so tooling cannot suggest one the loader rejects.

Annotation structure and eligibility are validated at load; see [SPEC.md](SPEC.md#annotations) for the blessed set and the diagnostics they produce.

**Merged views and property identity.** A property inherited from several direct supertypes, none of which descends from another, carries the union of their merged views' annotations, so the entry in `Type.AllProperties()` may be a *synthesized* copy rather than the `Property` its declaring type holds in `Properties()`. `Property.Origin() Property` returns the declared property — itself when nothing was merged — and is the correct key for any map built from declared properties and read back over a merged view. Everything `Origin()` discards is annotation state: name, documentation, constraint, datatype reference, optionality, primary-key status, span, and declaring scope are preserved on the copy.

## Instance Validation

The `instance` package validates Go data against compiled schemas. Each instance is represented as an `instance.RawInstance` struct:

```go
type RawInstance struct {
    Properties map[string]any         // raw property values keyed by name
    Provenance location.Provenance   // optional source location metadata for error reporting
}
```

`Properties` is a `map[string]any` of native Go values. The checkers accept them directly — `time.Time` for a `Timestamp` or `Date` whose stored text reads back (one outside years 0 to 9999 does not), any Go integer width for an `Integer` — so marshaling a struct through JSON is one convenient way to build the map, not a requirement. `instance.BuilderFor` is another.

**A refused value draws its producer's issue alone.** A parser that cannot read an input as its member's type reports that issue and writes `instance.Refused{Value: <the input>}` in the value's place. The validator judges a `Refused` no further: it reports nothing for it and refuses the instance, which fails as a member error at that position fails it: no pass after the one that met it runs, and in an edge target a refused foreign-key field leaves the edge properties unjudged, as a bad one does. `ValidateOne` returns a zero `ValidInstance` with a result that holds no error for the value, a batch entry is zero, and `instance.ErrRefusedValue` (*"instance: the instance holds a refused value"*) names the case for a caller that must return an error. A `Refused` is read as a property's value and, in an edge object, as a foreign-key field's or an edge property's value. Anywhere else it is a value of no form (see [Values](#values)), refused as that position refuses a value of the wrong form: `E_TYPE_MISMATCH` as a list element or behind a pointer, `E_EDGE_SHAPE_MISMATCH` as a relation's value. Under a key that names no member it is an unknown field, as any value there is.

### Validator Creation

```go
validator := instance.NewValidator(schema, opts...)
```

### Validator Options

| Option | Description |
| ------ | ----------- |
| `WithStrictPropertyNames` | Require exact case matching (default: false) |
| `WithAllowUnknownFields` | Silently ignore unknown fields (default: false) |
| `WithIssueLimit` | Cap the issues one instance stores, and count the rest as dropped (default: 100, and 0 is unlimited). [Validation](#validation) states what the batch result keeps of that count |

The `RecommendedOptions()` function returns a curated set of defaults (`WithStrictPropertyNames(true)`, `WithAllowUnknownFields(false)`) as a starting point for common use cases.

### Validation

```go
// Validate a batch of instances for a given type
valid, result := validator.Validate(ctx, "Person", rawInstances)
if !result.OK() {
    // Use diag.Renderer to format issues
    return fmt.Errorf("validation errors: %v", result)
}
// Process each entry that is not zero: a zero one held a refused value its producer reported

// Validate a single instance
one, result := validator.ValidateOne(ctx, "Person", rawInstance)
```

A batch call returns one entry per input instance when the batch ran — a record for a success, a zero entry for a failure — and a nil slice when it did not: a nil batch, an unknown type, an abstract or part type named by `Validate`'s empty batch, or a cancellation, before the first row or on one. A diagnostic that belongs to a row carries exactly one `instance_index` detail, the index into the slice the caller passed; a batch-wide diagnostic carries none and is anchored on the batch's source at its root. A cancelled batch draws exactly one `E_CONTEXT_CANCELLED`, stamped with the row it stopped on; a cancellation before the first row is batch-wide, anchored on the batch's source at its root with no `instance_index`. The batch result carries the truncation facts (`LimitReached`, `DroppedCount`) of each row that finished under a live context, so such a row is never reported as complete when it was capped. A row the cancellation stopped keeps its `E_INTERNAL` issues alone, and its truncation facts are lost.

**Paths name the input document.** A diagnostic's path, and a composed child's provenance path, address the token in the document the caller supplied: the input key as written (`$.Age` for an input `Age` matched to the property `age`, `$.lines[0].qty` for a child under the input key `lines`), or the parent object for something absent or for a collision between two of its keys. The schema spelling rides in the details (`property`, `relation`, `field`). A child's provenance is the parent's, extended by that path; a parent without provenance yields children without.

**Composed nesting is bounded.** A root is depth 0 and its composed children depth 1; a child deeper than `instance.MaxComposedDepth` (32) draws `E_COMPOSITION_DEPTH_EXCEEDED`. The `.ys` reader refuses a document nested past the same number with `E_SNAPSHOT_DEPTH_EXCEEDED`, so a snapshot can carry every nesting the validator accepts.

**A refused instance's key.** `validator.PrimaryKeyOf(typeName, raw) (schema.TypeID, immutable.Key, bool)` returns the type and primary key `raw` would carry as a valid root instance, whether or not the validator accepts the rest of it: each key property is read, checked and coerced as `Validate` reads it, so the key equals the one `Validate` stores for an instance it accepts. It reports false when `typeName` names no type that can hold a root, or a key component is absent, collided, an `instance.Refused`, or refused by its constraint. The CLI's data commands read it to judge a document as a whole: an association whose target the document states but the validator refused is not reported as a missing target, and a key a refused instance shares with another is a duplicate.

> **Note:** Passing an unknown type name to `Validate` or `ValidateOne` produces a validation failure (via `diag.Result`), not a panic or unexpected state — for `Validate`, whatever the batch, a nil or empty one included.

Type names passed to the validator (`Validate`, `ValidateOne` and `PrimaryKeyOf`) are entry-schema-relative: bare names for entry-schema types, alias-qualified names (`common.Region`) for directly imported ones. Relation *targets*, by contrast, are resolved internally by the absolute identity completion records (`Relation.TargetID`), never by re-reading the declared target name against the entry schema — so associations and compositions declared on imported types, or inherited from cross-schema parents, validate against the true target type even when the entry schema does not know the declaring schema's aliases or shadows the target's bare name with a type of its own.

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

`RawInstance.Properties` is `map[string]any` of native Go values; marshaling a struct through JSON is one way to build it, not a requirement. Every input key — a property, a relation field, an edge property, a `_target_` foreign-key field — is matched by one rule: an exact match first, then, by default, an ASCII case-fold (identifiers are ASCII, so a key carrying a non-ASCII letter matches nothing). The fold is exported as `instance.FoldKey(key string) (string, bool)`, and the whole claim decision as `instance.ClaimKeys(t schema.Type, keys []string) instance.Claims` for a node object and `instance.ClaimEdgeKeys(rel schema.Relation, target schema.Type, keys []string) instance.EdgeClaims` for an edge-target object, so a parser that resolves names before validation decides each claim by the default validator's own code. An exact match is claimed first and never collides; two keys folding to one member none matches exactly is `E_CASE_FOLD_COLLISION`, reported once at the object that holds them. The collision is that member's one issue, as it is a relation's: a collided required property, foreign-key field or required edge property draws no `E_MISSING_REQUIRED`, `E_MISSING_FK_TARGET` or `E_PARTIAL_COMPOSITE_FK` beside it. Under `WithStrictPropertyNames(true)`, which `RecommendedOptions()` sets, no fold is attempted: a name that differs in case goes unmatched. It draws `E_UNKNOWN_FIELD` or `E_UNKNOWN_EDGE_FIELD` unless `WithAllowUnknownFields(true)` is also set, and the member it would match stays absent, which draws the absent member's own issue, such as `E_MISSING_REQUIRED`, `E_MISSING_FK_TARGET`, `E_PARTIAL_COMPOSITE_FK` or `E_UNRESOLVED_REQUIRED_COMPOSITION`. A `_target_`-prefixed key that is not one of the target's foreign-key fields is an unknown edge field.

### Schema-Aware Raw Instance Builder

`instance.BuilderFor(schema, typeName)` returns a `*SchemaBuilder` that constructs `RawInstance` values while enforcing schema shape at build time — unknown properties, unknown relations, edge shapes it cannot build (a composition, an association with edge properties, a target key of the wrong arity), and cardinality mismatches all surface from `Build()` with file:line locators captured via `runtime.Callers`. This shifts shape failures out of `ValidateOne`'s domain:

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
| `BuilderFor(s, typeName)` | Construct a builder bound to a schema type. Errors on a zero schema, unknown type, or abstract type. |
| `Property(name, value)` | Set a property value. Unknown names accumulate as errors surfaced at Build. |
| `EdgeTo(name, targetKey...)` | Add an edge target on an association without edge properties. Variadic key supports composite PKs. |
| `Build()` | Produce the `RawInstance`. Returns the first accumulated error, suffixed `(and N more build error(s))` when more exist. |

Build errors include the bound type's name, the offending property/relation, and the caller's file:line.

`SchemaBuilder` is NOT concurrent-safe; construct one per goroutine. The bound `schema.Schema` remains safe to share across many concurrent builders.

The shape portion of `ValidateOne` — property and relation names, and association cardinality — is guaranteed to pass on the output of a successful `Build`. Compositions are not covered: the builder cannot produce one. Value-level validation (constraint checks, PK coercion, foreign-key shape and key-component checks) still runs at `ValidateOne` time. Reference integrity — whether an association's target exists — is checked by `graph.Check`, never by the validator.

### Value Functions

The validator's per-value rules, exported for a caller that admits or renders schema-typed values at a boundary this library does not own — a hand-built export, a direct-Cypher parameter map, a pre-filter in front of a write the validator never sees. Values that reach a graph through `Validator` have already passed both.

```go
// CheckValue reports whether val conforms to c by the rule the validator applies
// to every property value: Go kind and form, bounds, enum membership, pattern,
// list length and element rule and a Vector's dimension, with an alias resolved
// to its DataType. A nil constraint is valid, and so is a value that holds none
// (IsAbsent) under a constraint schema.CheckConstraint accepts —
// presence is the caller's rule, as it is the validator's. A non-nil constraint is judged first by
// schema.CheckConstraint, and one it refuses is an error whatever the value.
func CheckValue(val any, c schema.Constraint) error

// CanonicalValue returns val in the single stored representation c defines — the
// value the validator stores for the same input: an Integer as int64, a Float as
// float64, a Vector as []float64, a List with each element canonicalized through
// the element constraint, a Timestamp rendered through its declared format
// (RFC 3339 with fractional seconds otherwise), a UUID in canonical lowercase
// form, a Date as "2006-01-02" in the value's own location, and a String, Enum,
// Pattern or Boolean carried by a named Go type as the base value. A nil
// constraint passes val through, and so does a nil val under a constraint
// schema.CheckConstraint accepts, while any other value that holds none renders
// as nil; a constraint it refuses is an error, as it is for CheckValue. On error the returned value is
// val unchanged, so a caller that heals what it can may ignore the error.
func CanonicalValue(val any, c schema.Constraint) (any, error)

// IsAbsent reports whether v holds no value: nil, or a pointer, an interface, a
// map, a slice, a channel, a function or an unsafe pointer that is nil, bare or
// behind at most 64 pointers and interfaces, or a nil immutable container.
// The validator reads such a value at a property as a missing key: schema/gogen
// emits a typed nil pointer for an absent optional scalar, and a nil slice for
// an absent optional list. adapter/neo4j writes such a value as null.
func IsAbsent(v any) bool
```

`CheckValue`'s error is the checker's message and is meant for reading, not matching, with two exceptions: a constraint `schema.CheckConstraint` refuses returns that refusal, and a panic inside a check or a coercion is recovered into an `*InternalError` that matches `errors.Is(err, instance.ErrInternalFailure)`, exactly as it does inside the validator. Both functions judge a constraint as `adapter/neo4j.CoerceParams` does, so a hand-built constraint `schema.CheckConstraint` refuses, or a DataType reference that resolves to no constraint or to another reference alone, is an error with no value checked. `CanonicalValue` is the rule `adapter/json` renders through, and `IsAbsent` the rule `adapter/neo4j` reads a value that holds none by; each is the approved facade over its rule for the adapter layer. `CheckValue`, `CanonicalValue` and the validator read every value by the rules of [Values](#values): a pointer to a `json.Number` is a number, which every kind that reads text refuses, and a value that holds none is absent. A `json.Number` whose text is no JSON number literal (`immutable.IsNumberLiteral`), such as `+5`, `007`, `1_000`, `0x1p4` or `.5`, is no value of any kind: an `Integer` or a `Float` refuses it as one (*`expected integer, got a json.Number that is no JSON number literal: "+5"`*), and no snapshot holds one.

## Graph Construction

The `graph` package builds an in-memory graph from validated instances.

### Graph Operations

```go
g := graph.New(schema)

// Add validated instances
result := g.Add(ctx, validInstance)
if !result.OK() {
    // Handle error
}

// Check completeness (required associations)
result = g.Check(ctx)

// Get a point-in-time snapshot
snap := g.Snapshot()
for _, typeID := range snap.Types() {
    for inst := range snap.InstancesOf(typeID) {
        // Process instances
    }
}
```

**Type resolution.** Save `BatchAssembler.Add`, whose `typeName` goes to `instance.Validator.ValidateOne`, no lookup in this package takes a rendered type name — a rendering is lossy, so keying a lookup by one merges types that are not the same type. `Add` judges an instance by the schema object that validated it: the instance holds its `schema.Type` (`ValidInstance.Type()`), and `Add` compares that handle with the one `Schema.TypeByID` returns for its identity. A mismatch panics, as every other programmer error in this package does: the type is outside the graph's import closure, or another load validated the instance. One text loaded twice is two loads, so an instance crossed between them panics whether or not the texts differ. A load holds one handle per type however many import paths reach it, so a validator over any member of a closure serves a graph over any schema of that load whose own closure holds the type. Past that check, two questions remain. A root's type is restricted as a matter of *ownership* — a graph bound to a schema holds instances of the types that schema can name, which are those it declares or directly imports, and the diagnostic's hint says so. `schema.Addressable` is that rule, and the import, `snapshot.Load` and `snapshot.Verify` apply it, with the rest of the root rule, to every type a snapshot *denotes* — `Snapshot.Types`, whether or not the type holds instances — so every name `adapter/json` keys its output by resolves back to one type that can hold a root. A composed child is exempt and reaches the *whole* import closure: its type comes from a relation the schema already resolved, and no ownership question arises because the child arrived inside a parent the graph does own. A schema where an imported type composes a part type from a further import therefore loads, validates and builds. Where two identities render alike, a diagnostic naming them falls back to the full identity rather than reading `"X" does not match "X"`.

**What `Add` checks.** `Add` checks the instance's load and its root type, then assembles the tree to install and commits it under the graph's lock. It checks no structure below the root type: the validator of the same load already judged the property names, the names, property names, multiplicities and target keys of a root's edges, and every slot and child of its composition tree at any depth. A refused record installs nothing: no instance, no child and no edge, and the graph's diagnostics hold its issue. A key a held instance states is such a refusal, and the graph keeps a duplicate record of it.

`Add` panics on a nil receiver or context, a zero instance, and an instance whose type handle fails the check above: another load's, or one outside the graph's import closure. It raises `E_GRAPH_TYPE_NOT_FOUND` (a root's type in the load's closure that the graph's schema neither declares nor directly imports, as from a member's validator), `E_GRAPH_MISSING_PK` and `E_GRAPH_INVALID_COMPOSITION` (a keyless or a keyed part read out of `ValidInstance.Compositions` and added as a root), `E_DUPLICATE_PK`, and `E_CONTEXT_CANCELLED`. `E_GRAPH_ABSTRACT_TYPE`, `E_GRAPH_INVALID_PK`, `E_GRAPH_UNKNOWN_RELATION`, `E_UNKNOWN_FIELD`, `E_UNKNOWN_EDGE_FIELD` and `E_DUPLICATE_COMPOSED_PK` have no route through `Add`: the validator or the schema load refuses those shapes first. The import raises all six over a snapshot no validator of its schema judged. The `.ys` reader raises `E_GRAPH_UNKNOWN_RELATION` and `E_DUPLICATE_COMPOSED_PK`, and refuses the other shapes as `E_SNAPSHOT_INVALID_ROOT` or `E_SNAPSHOT_MALFORMED`. A validator defect reaches the graph, because `Add` does not judge the record again; `snapshot.WithRevalidation` runs a validator over a document's roots when it loads. See the package doc's Error Handling section for the full per-method list.

### Batch Assembly

`graph.BatchAssembler` composes a `Validator` and a `Graph` into a single call surface for the common pipeline pattern: validate → add → check → snapshot. `Add` validates before it adds, and `Graph` returns the assembled graph for its `Check` and its `Snapshot`. The assembler is concurrent-safe, so multiple goroutines may share one assembler:

```go
ba := graph.NewBatchAssembler(ctx, s)

for i, rec := range records {
    if err := ba.Add("TypeName", buildRawInstance(rec)); err != nil {
        return fmt.Errorf("record %d: %w", i, err)
    }
}

g := ba.Graph()
if result := g.Check(ctx); result.HasErrors() {
    return fmt.Errorf("batch: %w", result.Err())
}
snap := g.Snapshot()
qualityCollector.Merge(snap.Diagnostics())
// snap ready for snapshot.Marshal / snapshot.WriteFile / etc.
```

**Error shapes.** `Add` / `AddValid` / `AddValidOrMerge` return errors whose cause is a `*diag.ContextualError` (see [Contextual Diagnostic Wrap](#contextual-diagnostic-wrap)), save a refused value's. A raw instance whose one fault is an `instance.Refused` value (see [Instance Validation](#instance-validation)) makes `Add` return an error that matches `instance.ErrRefusedValue`, carries the tag below in its text, and adds no diagnostic to `Snapshot.Diagnostics()`: the value's producer reported it. `Add`'s tag is `"<TypeName> (attempt #N)"`, where N is the assembler-wide 1-indexed attempt ordinal: it counts calls across every goroutine sharing the assembler, so it identifies the call and not the caller's input row. A caller that needs to locate its own record keeps that mapping itself. `AddValid` and `AddValidOrMerge` given a zero `ValidInstance` have no type name to substitute, so the single token `zero-instance` takes its place, the tag reads `"zero-instance (attempt #N)"`, and the diagnostic is `E_INTERNAL` — the record is absent, not mis-keyed. Recover with `errors.As` or `diag.AsContextualError`.

`AddValid` and `AddValidOrMerge` panic on an instance of another load, as `Graph.Add` does.

**What `Check` and the snapshot report.** `Check` reports each required association left unresolved and a cancelled context, and nothing else. `Snapshot.Diagnostics()` carries every construction diagnostic: those of each add the graph took, the validator's results for `Add`, and the issue `AddValid` or `AddValidOrMerge` draws for a zero instance. `Check` accumulates nothing into it, which is what makes `Check` idempotent. A caller that discards per-record `Add` errors must read `Snapshot.Diagnostics()` to see them — a clean `Check` means the completeness check passed, not that every record was added. A record `Add` refuses for an `instance.Refused` value adds nothing there: its issue is in the result of the parser that wrote the value, which such a caller keeps.

**Merging a duplicate key.** `ba.AddValidOrMerge(valid)` adds `valid` as `AddValid` does, and returns `(merged bool, err error)`, `merged` false whenever `err` is not nil, so a caller counts its merges without racing other workers. When the graph already holds a root of `valid`'s type at its primary key, it merges instead of refusing: `valid`'s association records install on the held root, and `valid`'s properties and provenance are dropped, so the held root's stand. A merge records no `Duplicate` and raises no error. A target the held root already names under that association adds nothing. A merge that would give a `(one)` association a second target is refused whole, before anything installs, with an `E_GRAPH_CARDINALITY` for each such association. A `valid` carrying composed children is refused as `Add` refuses a duplicate, `E_DUPLICATE_PK`. The merge is decided and installed under the graph's lock, so N workers adding listings of one key to one assembler keep every listing's edges. It is for a source that lists one record under several parents, each listing naming its own association targets; the graph package doc's "Merge on a Duplicate Key" section states every rule. A merge adds no instance, so `Count()` does not count it.

**Validator access.** The assembler serializes `ValidateOne` + `Graph.Add` through an internal `sync.Mutex`, covering validation, the graph add, and the success-counter increment as one atomic step.

**Concurrency contract.** `BatchAssembler` is safe for concurrent use across all methods (`Add`, `AddValid`, `AddValidOrMerge`, `Count`, `Graph`). No external mutex is required at call sites. In the worker-pool pattern, one assembler is shared across N goroutines, and a coordinator waits for them before it calls `Graph().Check` and `Graph().Snapshot`, so every `Add` that returned nil is in the snapshot.

**Resuming from a prior snapshot.** `graph.NewBatchAssemblerFromSnapshot(ctx, s, snap)` constructs an assembler whose graph starts pre-populated from an existing `Snapshot` — the same import semantics as `NewFromSnapshot` (see [Seeding from a Snapshot](#seeding-from-a-snapshot)) — instead of `NewBatchAssembler`'s empty graph. Consumers that persist a batch and continue it on a later run seed a new assembler from the loaded snapshot and `Add` only the outstanding records:

```go
snap, res := snapshot.Load(ctx, data, s) // prior batch's .ys
if res.HasErrors() { /* handle */ }

ba, res := graph.NewBatchAssemblerFromSnapshot(ctx, s, snap)
if res.HasErrors() { /* snap breaks a structural fact under s; ba is nil */ }
for _, rec := range outstanding { // e.g. resume-by-set-difference
    if err := ba.Add("TypeName", rec); err != nil { /* handle */ }
}
result := ba.Graph().Check(ctx)
```

New adds interact with the seeded state as if it had been assembled in the same batch: they resolve previously-unresolved edges imported from the seed, forward references resolve against seeded instances, a `(type, primary key)` collision with a seeded instance is rejected as `E_DUPLICATE_PK` by `Add` and `AddValid`, and merged or refused by `AddValidOrMerge`, and `Graph().Check` covers the union — a required association imported from the seed and still unresolved draws `E_UNRESOLVED_REQUIRED` in its result. `Count()` reflects only records added through the assembler (seeded instances are not counted), and construction diagnostics are not carried over from the seed (`.ys`-loaded snapshots carry none by design; `Duplicates` and `Unresolved` are the persistent structural records, and both import). A seed built against another schema is held to `s` under the graph package doc's "Structural facts" and refused, with a nil assembler, where it breaks any of them, save that the import derives the Records fact's records again and drops an absent record under a relation that is no association of `s`; a seed taken from a `Graph` bound to `s`, or loaded via `snapshot.Load` against it, holds to them and is not walked. Every other contract is identical to `NewBatchAssembler`.

**Test fixtures.** `snapshot/snapshottest` is the shared round-trip vocabulary for snapshot tests: `BuildSnapshot(tb, s, instances...)` constructs a snapshot from instances an `instance.Validator` validated over `s` itself, `AssertRoundTrip(tb, snap, s, opts...)` pins Marshal→Load structural equivalence, `AssertDeterministic(tb, snap, opts...)` pins byte-stable marshaling, and `DiffSnapshots(tb, want, got)` is the underlying go-cmp comparison — recursive over composition trees (duplicates included, with each record's conflict), provenance-presence-aware, and exact on every numeric property's dynamic type: schema-aware float emission keeps `KindFloat` values `float64` across a marshal/load round trip, so an `int64`/`float64` mismatch is a real defect, not a wire artifact.

### Seeding from a Snapshot

A new mutable `Graph` can be seeded from an existing `Snapshot`, enabling incremental graph building on top of persisted or previously-constructed state:

```go
// Create a graph pre-populated with an existing snapshot's data
g, res := graph.NewFromSnapshot(schema, existingSnap)
if res.HasErrors() { /* existingSnap breaks a structural fact under schema; g is nil */ }

// New instances can be added on top of the imported data
result := g.Add(ctx, newInstance)

// Edges referencing imported instances resolve automatically
snap := g.Snapshot()
```

`NewFromSnapshot` imports all instances, edges and duplicates from the source snapshot, and its unresolved records as they stand when the snapshot is bound to the importing schema. It does not import the snapshot's construction diagnostics. Subsequent `Add` calls may resolve previously-unresolved edges if they supply the missing targets. The import derives the records of a snapshot built against another schema again, and holds it to the importing schema under the other facts of the graph package doc's "Structural facts": one that breaks a fact, save the `absent` record this paragraph drops, is refused with a nil `Graph`, and nothing is installed. Keys are rewritten under the importing schema's key constraints, and the unresolved records are derived again as `Graph.Add` derives them from the snapshot's data under it: a `target_missing` record whose target the snapshot holds becomes an edge, an `absent` or `empty` record under an association the schema makes optional, or an `absent` record under a relation that is no association of it, is dropped, and a root holding no edge and no record under an association it makes required gains an `absent` record there. A snapshot records nothing for an empty list under an optional association, so that gained record reads `absent` where `Graph.Add`, given the list, records `empty`. A `target_missing` record under an association the schema retargets is refused with `E_GRAPH_UNKNOWN_RELATION`, as an edge is, where the importing schema resolves the record's target type; where it does not, the identity rule's one `E_GRAPH_TYPE_NOT_FOUND` for that identity stands for the record.

To run the batch-assembly lifecycle on top of a seeded graph, `graph.NewBatchAssemblerFromSnapshot` constructs a `BatchAssembler` whose graph is seeded the same way — see [Batch Assembly](#batch-assembly).

### Snapshot Methods

The `Snapshot` type provides read access to graph state, and no method of it changes it:

| Method | Description |
| ------ | ----------- |
| `Schema()` | The schema used for validation |
| `Types()` | The type identities the snapshot denotes (`[]schema.TypeID`, sorted by TypeID) |
| `InstancesOf(typeID)` | Iterator over the instances of a type (sorted by primary key) |
| `AllInstances()` | Iterator over all **root** instances in deterministic order. Composed children are not yielded — walk `Instance.ComposedRelations` and `Instance.Composed` for the subtree |
| `InstanceByKey(typeID, key)` | O(1) lookup by type identity and primary key. The key is a `FormatKey` string, tried as spelled, then with each Timestamp, Date or UUID component canonicalized under the type's key constraints. One instant at two offsets is two addresses |
| `Edges()` | Iterator over all resolved edges (sorted) |
| `EdgesFrom(inst)` | Iterator over the outgoing edges of a specific instance |
| `Duplicates()` | Iterator over the duplicate primary key records (sorted), read through `Instance()` and `Conflict()` |
| `Unresolved()` | Iterator over the unresolved edge records (sorted), read through `Source()`, `Relation()`, `TargetType()`, `TargetKey()`, `Required()`, `Reason()`, `Property(name)` and `Properties()` |
| `Diagnostics()` | Construction diagnostics (call `OK()` / `HasErrors()` on the returned `diag.Result`) |
| `Attestation()` | The validity claim the snapshot carries: `Values` (every root and composed child validator-built, at least one instance) and `Associations` (no `Required` unresolved record). A loaded snapshot carries the header's claim verbatim |

### Thread Safety

- `Graph` is safe for concurrent `Add` calls
- `Snapshot` values have no method that changes them and are safe for concurrent reads. A record is a handle, so no caller holds its address
- Every collection a `Snapshot` method returns is deterministically sorted, save the ties the graph package doc's Ordering Guarantees name. `Instance.Composed` keeps the order the children were added in

### Ordering Guarantees

- `Schema.Types()` and `Schema.DataTypes()`: Declaration order. v0.21.1 yielded name order, so a consumer that writes generated output from either sorts it first.
- `Snapshot.Types()`: Lexicographic by the `TypeID`'s `String`
- `Snapshot.InstancesOf()`: Lexicographic by primary key
- `Snapshot.Edges()`: Lexicographic tuple (sourceType, sourceKey, relation, targetType, targetKey), then the edge properties
- `Snapshot.Duplicates()`: Lexicographic by (`TypeID`, primary key), then the rejected instance's properties, and finally its provenance
- `Snapshot.Unresolved()`: Lexicographic by (sourceType, sourceKey, relation, targetType, targetKey), then reason, then required (optional before required), and finally the edge properties

Types are identified by `schema.TypeID`, never by name. A name is a rendering of an identity — bare for a local type, alias-qualified for a directly imported one — so a transitively imported type renders bare, and it can render as another type of its name does. Two directly imported same-named types render apart. Use `schema.TagForm(snap.Schema(), id)` where a name is wanted for output.

### Key Formatting and Parsing

A primary key is carried as a canonical JSON array string — the form `immutable.Key.String` produces and `Snapshot.InstanceByKey` takes. The graph tries an address it receives as spelled, then with each Timestamp, Date or UUID component canonicalized under the type's key constraints, so `FormatKey` over a string its constraint accepts addresses the key that string canonicalizes to. One instant at two offsets is two addresses.

| Function | Description |
| -------- | ----------- |
| `FormatKey(values...)` | Render key components as a canonical JSON array string |
| `ParseKey(s)` | Decode a `FormatKey` string into `[]any` components |

```go
key := graph.FormatKey("us", int64(12345)) // `["us",12345]`
inst, ok := snap.InstanceByKey(typeID, key)

values, err := graph.ParseKey(key)         // []any{"us", int64(12345)}
```

`ParseKey` returns the component types a snapshot round trip produces — `string`, `int64`, `float64`, `bool`, `nil` — because it classifies numbers by lexical form, the same rule the `.ys` reader's materialization applies (its revalidation judges the document's own text): a literal carrying `.`, `e`, or `E` is `float64`, an int-shaped literal is `int64`. An int-shaped literal beyond the int64 range comes back as `float64`, and `FormatKey` writes one for any value `encoding/json` renders as such a literal, as a whole `float64` of magnitude 2^63 to below 1e21 or a `uint64` above `math.MaxInt64` is rendered; a literal with no finite Go value, such as `1e999`, is an error naming the component's index. So is a component that is itself an array or object: `FormatKey`'s domain is scalars.

The round trip holds in both directions over those types, a string that is not valid UTF-8 aside (`FormatKey` writes each invalid byte as U+FFFD), with two documented carve-outs — `FormatKey` over a `float64(5)` component renders `[5]` and `ParseKey` reads it back as `int64(5)`, and over a `float64` negative zero renders `[-0]`, read back as `int64(0)` and rendered `[0]`. No stored key holds the invalid string or the negative zero. The `.ys` reader refuses a key literal that does not read back as written, such as `5.0` or `-0`.

Composed children have no key of their own. A part instance is identified
through its parent composition, so this package mints no address for one; an
exporter that must give each part node a separate identity derives it, and
`adapter/neo4j` is the only one that does — see its `_composed_key` property in
the Neo4j section.

## Import Closure & Type Lookup

Two `Schema` accessors expose the import closure — the schema plus every transitively imported schema — without callers hand-rolling the walk:

```go
// The schema itself, followed by every transitively imported schema:
// a breadth-first walk over each schema's imports in declaration order,
// deduplicated by SourceID (a diamond import appears once, at its
// first-reached position). Deterministic.
for sc := range s.Closure() {
    fmt.Println(sc.Name())
}

// Resolve an absolute type identity (schema.TypeID) anywhere in the closure —
// the identities completion records on relations (Relation.TargetID) and
// types (Type.ID). No local names or import aliases are involved.
if target, ok := s.TypeByID(rel.TargetID()); ok {
    fmt.Println(target.Name())
}
```

`TypeByID` returns `ok == false` for a zero or unknown `TypeID`. Both accessors compute the closure lazily on first use and cache it; like all `Schema` accessors they are safe for concurrent use. The generators (`schema/gogen`, `schema/jschema`, `schema/markdown`) drive their emission walks off `Closure()`, and the instance layer resolves relation targets through `TypeByID`.

## Schema Identity

The `schema` package provides a content-based hashing function for structural compatibility checks:

```go
hash := schema.StructuralHash(s) // returns "sha256:<hex>"
```

`StructuralHash` computes a deterministic hash over the rules that decide what instance data is valid, across the schema's whole import closure (`Schema.Closure()`). For every member schema — the entry schema and each schema it imports, directly or transitively — the input covers **the member's schema name**, its types (properties with constraints and modifiers, the primary-key set, associations with targets — each target carrying the name of the closure member that owns it — multiplicities and edge properties, compositions, **inheritance edges**, **invariant expressions**, and the **`abstract` and `part` markers**) and its data types. Two schemas that hash the same agree on every one of those rules everywhere in their closures. The converse does not hold: a hash can also move on a change to an imported schema this schema never references, because closure membership is part of the identity.

Each member is framed under its schema name — renaming any member's `schema "X"` declaration changes the hash even when every declaration is identical — the entry schema first and the remaining members ordered by name, so the order imports are declared in does not enter the digest. An invariant's *name* is not hashed at all; it is the failure message. Invariant expressions hash order-independently per type — the blobs are sorted by their serialized bytes, so a duplicate expression is not folded away — while the operands inside one expression hash in declared order, because operand order is semantic.

**Excluded, deliberately:** annotations, which drive downstream store DDL and never reject data; import aliases and source paths — a member frame carries the declared schema name, never its source, a relation target is the pair (owning schema name, type name), and a supertype hashes by name alone, so `embedded://` and disk loads of one schema text agree; and documentation, source spans, and every derived index or cache.

The hash is used by the `snapshot` package to verify that a persisted snapshot is compatible with the schema provided at load time. `StructuralHashVersion` (currently `4`) identifies the hashing algorithm version; v0.15.0 raised it from `1` when invariants and the `abstract` / `part` markers joined the input, v0.17.0 raised it to `3` when the input widened from the entry schema's own declarations to its whole import closure, and version 4 raised it again when a relation target became the pair (owning schema name, type name).

## Snapshot Persistence

The `snapshot` package serializes and deserializes `graph.Snapshot` values to and from the yammm snapshot persistence format (`.ys`).

### File Format

The `.ys` format is JSON-based and preserves structural fidelity: instances with properties, primary keys, edges, compositions, provenance, duplicates, and unresolved edge records all survive a `Marshal`/`Load` round-trip, with two qualifications. Provenance survives as source name and path only: a loaded instance's `Provenance().Span()` is zero. `Marshal` refuses a source name or raw path that is not valid UTF-8 (`E_SNAPSHOT_MALFORMED`), and writes no bytes. It stops at its first refusal. The wire carries a number's value, not its width, for a value a validator admits. `Load` materializes dynamic numeric values as `int64` and `float64`, with two exceptions: a literal no finite `float64` can hold (`1e400`) returns as an unconverted `json.Number`, and so does a number nested more than 64 levels deep inside a dynamic value, where normalization stops recursing. An integer literal past `int64` returns as the nearest `float64`. A snapshot holds what a validator stored or a document stated, and the wire carries each as itself, so `Marshal` refuses no value a snapshot holds. It returns nil bytes with Error `E_SNAPSHOT_MALFORMED` for an indent that is not JSON whitespace or a metadata key or value that is not valid UTF-8. Fatal `E_INTERNAL` marks only a broken invariant of the snapshot itself.

The format includes:

- A **version** field for format evolution (current value: `4`, the only readable version; see [Wire Format Versions](#wire-format-versions))
- A **schema structural hash** for compatibility verification (see [Schema Identity](#schema-identity))
- An **integrity hash** (SHA-256 over the whole document, header included) for corruption detection
- A **features array**, which this version writes empty and refuses any entry of

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

// Read the header and the types table only, from []byte — skips instance body and
// integrity check. A scan of the whole document still checks its shape, member names and text,
// so cost scales with file size; HeaderOnlyRead is the sibling whose cost is the members up to the types table and that table.
header, result := snapshot.HeaderOnly(ctx, data)

// Streaming sibling: read those two from any io.Reader without
// materializing the full document into memory. Reads at most
// snapshot.MaxHeaderSize (16 MiB) bytes from the reader.
header, result := snapshot.HeaderOnlyRead(ctx, r)

// Write .ys bytes atomically to disk — tmp+fsync+rename. Consumers
// needing crash-safe persistence should use this instead of os.WriteFile.
if err := snapshot.WriteFile(path, data); err != nil { /* ... */ }
```

`Load`, `Verify`, `Info`, `HeaderOnly` and `UpdateMetadata` refuse with `E_SNAPSHOT_MALFORMED` a document holding a string of invalid UTF-8 or a surrogate escape that is half of no pair (`\ud800`), raw or escaped, at any position: `encoding/json` would silently read either as U+FFFD. `HeaderOnlyRead` and `ScanDir` read up to the end of the `types` table and hold what they read to the same rule. The five whole-document surfaces hold a document to this rule, and to its outer shape and member names, before they read its header, so such a document draws `E_SNAPSHOT_MALFORMED` whatever version it states. `HeaderOnlyRead` and `ScanDir` judge the version first.

`Load` refuses a document that breaks a fact of the graph package doc's "Structural facts", whatever the options say; `Verify` runs the same checks, and `Info`, which reads no schema, runs those that need none. It does not re-validate instance data by default. The header's attestation is the writer's claim, not a proof — anyone can recompute the integrity hash, so any process can write a document whose instances never earned it, and a `.ys` can hold values outside their constraints and invariant violations. Pass `WithRevalidation` to run each root the reader walks and the structural checks left whole — no check refused its type, its tree, its edges, its unresolved records, or its required and `(one)` associations, no record whose types-table row the reader refused could be its, and no other root the reader walks stands at its address, the reader walking no root of an instances entry that repeats a types-table row or names none; an edge that names no root does not stop it, unless it stands beside another record under a `(one)` association — with its composed children, edges and `target_missing` records, back through the real validator, or `WithValueConformance` for the narrower canonical-form check. `Load` always performs structural validation of the `.ys` format itself and verifies schema compatibility using `schema.StructuralHash`.

`HeaderOnly` and `HeaderOnlyRead` intentionally skip integrity verification — the returned `HeaderInfo.IntegrityHash` is the stored value, not a verification result. Use `Verify` when the document's hash must be confirmed. Neither function consults a schema; dispatch callers use the `HeaderInfo.SchemaHashMatches(s)` helper (see [SchemaHashMatches — dispatch-site cross-check](#schemahashmatches--dispatch-site-cross-check) below) to compare the stored schema hash against a loaded `schema.Schema`.

### Marshal Options

| Option | Description |
| ------ | ----------- |
| `WithIndent` | Indentation string (`""` for compact, `"\t"` for tabs) |
| `WithCreatedAt` | Set `created_at` timestamp (omitted by default for determinism) |
| `WithMetadata` | User-provided key-value annotations; `Marshal` refuses a key or value that is not valid UTF-8 (`E_SNAPSHOT_MALFORMED`) |

### Load Options

| Option | Description |
| ------ | ----------- |
| `WithIssueLimit` | Maximum issues `Load` and `Verify` store (default 100, the cap `schema.Load` holds; 0 for unlimited). The walk completes unless the context is cancelled, so a completed walk's `DroppedCount` is exact |
| `WithIntegrityCheck` | Whether the integrity hash is verified (default `true`); `false` skips it for debugging hand-edited files |
| `WithValueConformance` | With `true`, report a root's or a composed child's stored `Timestamp`, `Date` or `UUID` value, or list of them, that does not conform to its constraint (`W_SNAPSHOT_VALUE_NONCONFORMING`, Warning); not re-validation |
| `WithRevalidation` | Run each root the reader walks and the structural checks left whole (no check refused its type, its tree, its edges, its unresolved records, or its required and `(one)` associations, no record whose types-table row the reader refused could be its, and no other root the reader walks stands at its address; an edge that names no root does not stop it, unless it stands beside another record under a `(one)` association) — composed children, edges, `target_missing` records and invariants included — back through `instance.Validator`, reporting each finding about the data at the given severity (a cancellation or an `E_INTERNAL` keeps its own); `Error` refuses the load. An unresolved record no check refused, under an association the schema declares required, draws `W_SNAPSHOT_UNRESOLVED_REQUIRED` |

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
    CreatedAt           string            // as the header states it: RFC 3339 as yammm writes it, or empty
    Metadata            map[string]string // user-provided annotations, nil if absent

    // Content summary. Types holds one entry per types-table row;
    // InstanceCounts holds one entry per type an instances group names whose
    // row resolves, the number of its root instances. No count holds a composed child.
    Types           []TypeRef
    InstanceCounts  map[TypeRef]int
    TotalInstances  int
    TotalEdges      int
    DuplicateCount  int
    UnresolvedCount int

    // The header's validity claim, nil for a document that states none, as a pre-v0.15.0 one does.
    Attestation *graph.Attestation

    // File metadata.
    FileSize        int64  // len(data)
    IntegrityStatus string // "ok", "mismatch", or "skipped"
}
```

`snapshot.TypeRef{Schema, Name string}` is the schema-less display surface: `Info`, `HeaderOnly` and `HeaderOnlyRead` run without an import closure to resolve against, so each type is reported as the identity the document states, and two same-named types in different schemas stay distinct — both in `Types` and as `InstanceCounts` keys. `TypeRef` is comparable, and its `String()` and `MarshalText()` render `schema#name`. The `#` separator is deliberate beside `schema.TypeID.String()`'s `path:name` form: TypeID's rendering is byte-order-bearing (the wire's diagnostics records sort by it), so the display form stays visibly distinct rather than moving wire bytes to unify the two.

`TypeRef` renders and does not parse. It carries no `UnmarshalText`, so `SnapshotInfo` and `HeaderInfo` serialize one way, and nothing reads them back. `yammm snapshot info --format json` does not write these structs: it owns its wire shape and emits snake_case keys, carrying each `TypeRef` through as the same `schema#name` string. This is a decision, not a gap — the surfaces are a report projection, the in-process way to scan many documents is `ScanDir` / `ScanDirSlice`, which hands back `HeaderInfo` values directly, and `path#name` is injective over the values yammm produces but not over an arbitrary `TypeRef`. Adding `UnmarshalText` is additive and stays available if a consumer needs it.

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
    CreatedAt           string // as the header states it: RFC 3339 as yammm writes it, or empty
    Metadata            map[string]string

    // Types table (adjacent to the header; read in the same streaming pass).
    Types []TypeRef

    // The header's validity claim, nil for a document that states none, as a pre-v0.15.0 one does.
    Attestation *graph.Attestation

    // File metadata. HeaderOnly reports len(data); ScanDir reports the
    // file's size on disk; a bare HeaderOnlyRead reports zero, because a
    // size is not knowable from an io.Reader.
    FileSize int64
}
```

`HeaderOnlyRead` accepts any `io.Reader` and reads at most `snapshot.MaxHeaderSize = 16 * 1024 * 1024` bytes (16 MiB — headroom for consumers that carry large work-set arrays in header metadata). Larger headers are rejected with an Error-severity `E_SNAPSHOT_MALFORMED` issue whose message begins `header exceeded MaxHeaderSize` — distinguished from generic JSON-parse failures so operators can diagnose the cause. A header cut short (`io.EOF` or `io.ErrUnexpectedEOF` from the reader, or an error wrapping either) surfaces as `E_SNAPSHOT_MALFORMED`, and another reader error that stops the header's decode as a Fatal `E_SNAPSHOT_IO` carrying the first such error as a detail, rather than as a bare error return. A reader error after the types table decodes is not read. Context cancellation is checked once at function entry; individual `Read` calls on the passed reader are not cancellable mid-read.

#### SchemaHashMatches — dispatch-site cross-check

Neither `HeaderOnly` nor `HeaderOnlyRead` consults a schema, so neither verifies that the stored schema hash matches the caller's loaded schema. `HeaderInfo.SchemaHashMatches(s schema.Schema) bool` performs that comparison at the dispatch site:

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

`SchemaHashMatches` returns `false` without panicking when the receiver is nil, the schema is zero, the header's `SchemaHash` is empty, the header's `SchemaHashAlgorithm` does not match `schema.StructuralHashVersion`, or `schema.StructuralHash(s)` returns an empty string. Dispatch callers treat `false` as "unknown or incompatible schema, do not proceed" rather than silently continuing.

### Atomic Writing

`snapshot.WriteFile` persists bytes to a path using the `tmp+fsync+rename` protocol — the standard crash-safe write primitive every yammm consumer needs when turning `Marshal` output into a durable `.ys` file:

```go
func WriteFile(path string, data []byte) error

const TmpSuffix = ".tmp"
```

The payload is first written to a staging file of its own beside the file it replaces, `fsync`'d, closed, and then renamed into place. The staging name is the replaced file's stem, a random token, its extension and `snapshot.TmpSuffix` — `snap.12345.ys.tmp` for `snap.ys`, and through a symbolic link the replaced file is the one the link names — so concurrent writers of one path never share a staging file. The rename is the atomic commit point: either the new file takes over (rename succeeded) or the previous file is left untouched (any earlier step failed). With concurrent writers, each rename commits one writer's bytes whole and the last one wins. On any intermediate failure, `WriteFile` removes its own staging file and returns an error wrapped with the failing step (`create temp`, `write temp`, `sync temp`, `close temp`, or `rename temp to final`). It never removes a staging file it did not create.

**Which file is written.** `WriteFile` evaluates a `..` in `path` on its text first, as the schema loader evaluates it: `path` is cleaned, so a `..` cancels the element before it rather than following a symbolic link, and `dir/link/../snap.ys` writes `dir/snap.ys`; a relative `path` that climbs out of the working directory is made absolute from it as `filepath.Abs` spells it, the directory the loader climbs from. A symbolic link at `path` is then followed, as a chain, and survives: the file it names is written, and a dangling link creates that file. A link's target is read as the kernel reads it — on Unix a `..` in the target takes the parent of the directory reached on disk, on Windows it is evaluated on the text — and the staging file is made beside the file the rename replaces. A cycle of links at `path` is refused with an error wrapping `syscall.ELOOP`, tagged `resolve target`; a cycle in a directory of the path fails when the staging file is created, tagged `create temp`.

`WriteFile` does not validate that `data` is a valid `.ys` document — it is a general-purpose atomic-write primitive, and callers are responsible for the payload (typically the output of `Marshal`).

**Durability semantics.** File mode is `0o666` subject to umask, matching `os.Create`; callers needing stricter permissions should `chmod` after `WriteFile` returns. The file is `fsync`'d before rename but the parent directory is NOT `fsync`'d, so on some filesystems the rename may not be durable across a crash — consumers with stronger durability requirements should fork the helper and add parent-directory fsync.

**Crash recovery.** If the process crashes between `fsync` and `rename`, the complete staging file remains beside the file written, under a name that ends in that file's extension and `snapshot.TmpSuffix` (`.ys.tmp` for a `.ys` target). The `TmpSuffix` constant is exported so downstream primitives and consumer cleanup tools key off a single source of truth rather than hard-coding `.tmp`; the directory-iterator primitive (`ScanDir`, see [Directory Iteration](#directory-iteration) below) skips entries with the suffix automatically.

### Directory Iteration

For dispatch-style workloads that enumerate the `.ys` files of a directory — retention sweeps, link-engine discovery, operator "what's in this dir" inspection — `snapshot.ScanDir` exposes a lazy iterator over one `ScanEntry` per file, with the header parsed on-demand via `HeaderOnlyRead`:

```go
type ScanEntry struct {
    Name     string               // basename, e.g., "CA.ys"
    Path     string               // full path, filepath.Join(dir, Name)
    Header   *snapshot.HeaderInfo // nil when Result has error-severity issues
    Result   diag.Result          // OK on the happy path; carries errors per-file
    FileSize int64                // zero if the file could not be opened or stat'd, else its size on disk
    ModTime  time.Time            // modification time, or the zero Time if unknown
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

yammm writes UTC at second precision, so a time given to `WithCreatedAt` does not round-trip its sub-second part. `UpdateMetadata` keeps a foreign header's `created_at` text, fractional seconds and non-UTC offset included, and writes its JSON escapes anew.

`FileSize` and `ModTime` are populated whenever the file was opened, **including when its header then failed to parse** — a corrupt file still occupies disk and still has a modification time, and a caller accounting for either needs it before it branches on `Header`. When the header did parse, `Header.FileSize` repeats the size; there is no `Header.ModTime`, because a modification time is filesystem metadata about the container rather than a property of the document, and neither `HeaderOnly` nor `HeaderOnlyRead` could answer it. Both come from a stat of the **opened handle**, so for a symlinked `.ys` they describe the target rather than the link. A stat failure leaves both zero and raises no diagnostic. Neither case needs a compensating `os.Stat` per entry.

**Error surface, in two categories:**

- The iterator's second yielded value (the `error`) is non-nil ONLY for operation-level failures that end iteration: a dir-open error (`ENOENT`, `EACCES`, `ENOTDIR`, ...) is yielded as a single `(ScanEntry{}, err)` pair wrapping the underlying `os` error; a context cancelled at entry, or cancellation observed between files, is yielded as `(ScanEntry{}, ctx.Err())`. The zero-value `ScanEntry` signals "no file was reached." Cancellation observed between files takes precedence over any concurrent per-file failure.
- Per-file failures (corrupt header, per-file `os.Open` / `Read` failure) live on `ScanEntry.Result`; the iterator's error is `nil` for those and iteration continues. A failed `os.Open` surfaces as a Fatal `E_SNAPSHOT_IO`, synthesized by the scan. Everything after the handle exists comes from `HeaderOnlyRead`: a corrupt or truncated header is an Error-severity `E_SNAPSHOT_MALFORMED`, and a reader error that stops the header's decode, and that is not and does not wrap `io.EOF` or `io.ErrUnexpectedEOF`, is a Fatal `E_SNAPSHOT_IO` carrying the reader's error as a detail.

**Filtering:**

- Only regular files and symlinks are included, and a symlink only when its target is a regular file or cannot be stat'd (a broken link among them), whatever an entry is named: a directory, FIFO, socket, or device ending in `.ys` is skipped rather than opened. Of those, only basenames ending in `.ys` are scanned.
- Files whose basename ends with `snapshot.TmpSuffix` are skipped — the atomic-write staging files that `WriteFile` may leave behind on crash. Both primitives key off the shared exported constant; the single source of truth keeps them from drifting.
- Symlinks are followed: one is included when its target is a regular file or cannot be stat'd, and skipped otherwise. A broken symlink is included and yields a per-file Fatal `E_SNAPSHOT_IO` entry with the underlying `os.Open` error surfaced as a detail.
- Entries are yielded in the order returned by `os.ReadDir`, which sorts by filename. `dir` is cleaned before it is read, so a `..` cancels the element before it rather than following a symbolic link, and each entry's `Path` names a file of the directory listed. An empty `dir` names no directory: it fails as a missing one does, and never lists the working directory.

**`ScanDirSlice` semantics:**

`ScanDirSlice` materializes the full iteration into a slice plus an outer `diag.Result`. The outer Result surfaces operation-level errors only (dir does not exist → Fatal `E_SNAPSHOT_IO`; context cancellation → Fatal `E_CONTEXT_CANCELLED`); per-file errors remain on each `ScanEntry.Result`. Context cancellation returns *partial* results — the returned slice contains entries processed before cancellation, and callers who want fail-fast-on-cancel check `result.HasFatal()` before consuming the slice.

**CLI integration.** `yammm snapshot info --dir <path>` wraps `ScanDirSlice` to produce a tabular per-file summary (text) or a `[]{name, path, file_size, mod_time, header, diagnostics}` JSON array (`diagnostics` is the entry's result on the diagnostic stream's wire) (`mod_time` is RFC 3339 in UTC, empty when the file could not be stat'd). The flag is mutually exclusive with the positional file argument; single-file mode continues to work unchanged.

### Metadata Updates

For workflows that change header metadata on an existing `.ys` without re-serializing the body — pipeline phase transitions, operator-maintained flags, `force-complete` audit trails — `snapshot.UpdateMetadata` reuses the body bytes verbatim and recomputes only the SHA-256 integrity hash:

```go
func UpdateMetadata(
    ctx context.Context,
    data []byte,
    newMeta map[string]string,
) ([]byte, diag.Result)

func UpdateMetadataOrReMarshal(
    ctx context.Context,
    data []byte,
    newMeta map[string]string,
    s schema.Schema,
) ([]byte, diag.Result)
```

**When to use which.** `UpdateMetadataOrReMarshal` is the default entry point for most consumers: it runs the fast path, and when that reports an Error or a Fatal — a body-offset failure, a malformed header, an unsupported version, feature or hash algorithm — it transparently falls back to `Load + Marshal`, unless the context was cancelled or `newMeta` holds a key or value that is not valid UTF-8, which no path can write; a fallback that succeeds emits a Warning-severity `W_UPDATE_METADATA_FALLBACK` on the returned `diag.Result` so operators can observe the transition. When the fallback's `Load` fails too, the returned result is `Load`'s alone, so no break draws two issues. A cancelled `Load` may not have reached the break, so it keeps the fast path's refusal beside its own result. When its `Marshal` fails, the result is the fast path's refusal and `Marshal`'s issues. The primitive `UpdateMetadata` stays exported for consumers who genuinely need the strict fast path — benchmarks isolating the fast-path cost, or workflows where any Load + Marshal round-trip is operationally unacceptable.

**Speedup.** On a 14.7 MB `.ys` input the fast path is several times faster than the equivalent `Load + Marshal` round trip. **The enforced lower bound is 3×**: `TestUpdateMetadataRatioFloor` in the `snapshot` package measures both paths over a median of five samples and fails below that ratio. This document states the floor rather than an absolute timing, because the measured ratio varies with hardware and falls as `UpdateMetadata` gains header checks before the body splice.

**`created_at`.** `UpdateMetadata` keeps the existing `created_at` text, and writes its JSON escapes anew. To change it, re-marshal with `WithCreatedAt`. `metadata` itself is replaced entirely — there is no merge. Callers retrieve the current metadata via `HeaderOnly`, copy it, apply their changes, and pass the result.

**Failure modes.** The primitive returns structured diagnostics with stable codes:

- `E_SNAPSHOT_MALFORMED` — the document's outer shape, its header or its types table is malformed: a first byte other than `{`, whitespace included; the four top-level keys absent, repeated, out of order, `null`, joined by a fifth key or followed by bytes other than whitespace; a top-level value that is not valid JSON, or a document that ends before its top-level object closes; a header or a `types` table that does not decode, or a header that states a null or absent `features`; a `types` table stating one identity on two rows; any object of the document that repeats a member name, or spells a member the format declares in another case; a string holding invalid UTF-8 or a surrogate escape that is half of no pair; or a `newMeta` key or value that is not valid UTF-8. `HeaderOnly` refuses each such document with that code, save one with whitespace before its `{`, which it reads. `HeaderOnlyRead` stops once the `types` table decodes and holds to these rules what it read: that the document is a JSON object whose first section is the header and that holds a `types` section, the header and the table, the value of each section before `types`, and each string up to the end of the table. It checks neither the rest of the outer shape nor the member names.
- `E_SNAPSHOT_UNSUPPORTED_VERSION` — the header states a version no read path accepts (v1, v2 and v3 included; v4 is the only readable version). The document is refused rather than relabelled — `UpdateMetadata` is a header rewrite, never a migration — and `UpdateMetadataOrReMarshal`'s `Load` fallback refuses it the same way.
- `E_UPDATE_METADATA_BODY_OFFSET` — the header parsed cleanly but the body-boundary tracking resolved to an unexpected byte pattern, indicating the input does not match the shape `Marshal` produces. Byte-identical recovery via the fast path is not possible; `UpdateMetadataOrReMarshal` falls back to `Load + Marshal` automatically, unless `newMeta` holds a key or value that is not valid UTF-8.
- `E_SNAPSHOT_UNSUPPORTED_FEATURE` — the header's `features` array holds an entry this version does not recognize. One issue per unrecognized entry.
- `E_SNAPSHOT_UNSUPPORTED_HASH_ALGORITHM` — the header's `schema_hash_algorithm` is not `schema.StructuralHashVersion`. Error-severity here; the Warning downgrade applies only to header-only reads.
- `E_CONTEXT_CANCELLED` — ctx was cancelled at entry. Propagates as cancellation without re-attempting via the slow path.

**Wire-format contracts.** The primitive depends on two contracts documented in `snapshot/wire.go`'s package Godoc: the field-order contract (top-level keys are `{yammm_snapshot, types, instances, diagnostics}` in that order) and the body-byte-range stability contract (the byte range from the `,` after the header value through the document's closing `}` is reused verbatim). Both are pinned by `TestWireFormat_TopLevelKeyOrder` and `TestWireFormat_BodySuffixContract` in `snapshot/wire_test.go`, so a future Marshal-side shape change that would silently break the primitive fails at the wire-format test level.

**CLI integration.** `yammm snapshot update-metadata --set key=value [--unset key] <file>` wraps the primitive for operator tooling. `--set` and `--unset` are both repeatable; at least one is required. The command uses the strict fast path (not the fallback wrapper) — a body-offset failure surfaces as `ExitValidation` (1) with `E_UPDATE_METADATA_BODY_OFFSET` in the diagnostic output; the recovery path is a fresh `yammm snapshot save`. The write is atomic (tmp+fsync+rename) and preserves the file's mode: the CLI evaluates a `..` in the path on its text, as it does for every path it reads or writes, then follows the path's symlinks, stages every write it makes on a sibling of the file they resolve to, sets the mode before the rename rather than after it, and creates a file that did not exist at `0600` on Unix (Windows honours only a mode's write bit). A FIFO, a device or a descriptor path such as `/dev/stdout` is written through instead, and anything else — a read-only file, a directory, a looping link, a directory that cannot hold the staging file — is refused naming the path. It does not call `snapshot.WriteFile`, whose documented mode policy — `0o666` subject to umask, with callers told to chmod afterwards — is a library contract for consumers rather than the CLI's.

### Wire Format Versions

The `.ys` wire format uses an integer version field in the header for forward evolution, and this package reads one version. yammm v0.12.0 introduced v3 — the `types` section is a table of identity rows, `instances` is a list of `{type, items}` groups keyed by table row, and every other type reference is a nullable row index that never defaults — and retired every earlier version. v2 named types where it needed to denote them, which no name can do for a transitively imported type or for two same-named types in different schemas.

v4 keys each table row as `{schema, name}` — the declaring schema's **name**, not its source path. A name travels between machines and a path does not: under v3, one schema text loaded from two directories produced documents carrying an identical `schema_hash` (`StructuralHash` excludes source paths by design) and mutually unreadable type tables, so a snapshot written on a build machine failed to load on a runner with one `E_SNAPSHOT_UNKNOWN_TYPE` per row and nothing to do but regenerate it. Schema names are unique across an import closure — a load refuses a second schema with an existing name (`E_DUPLICATE_SCHEMA`) — which is what makes them sufficient to denote a type. `snapshot.TypeRef` carries the same pair and renders it as `schema#name`.

`snapshot.MinReadableVersion` (exported constant, value `4`) names the lowest version this package accepts on read paths. The `currentVersion` (unexported, value `4`) is the version emitted on every write, and `MinReadableVersion` is **derived from it** rather than written as a second literal — this package reads only the version it writes, so widening the accept range is a deliberate change rather than a bump that forgets its other half. The accept range on read is the closed interval `[MinReadableVersion, currentVersion]`; documents outside the range surface Error-severity `E_SNAPSHOT_UNSUPPORTED_VERSION` with the observed version and the supported version named in the message, once the surface has read the header. A whole-document surface first holds the document to its outer shape, its member names and the text rule, and refuses a break of any with `E_SNAPSHOT_MALFORMED`. **v1, v2 and v3 documents are no longer read** — v4 is the only readable version, and a v3 document is refused rather than migrated. An older reader rejects a v4 document the same way, so an operator running an older binary sees a structured diagnostic rather than a misread types section.

See the [Versioning Policy](#versioning-policy) for the full pre-1.0 / post-1.0 wire-format policy.

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
result.DroppedCount()     // Issues dropped after limit
result.SeverityCounts()   // Counts by severity level
result.CodeCounts(diag.Warning) // Seen-based per-code counts at one severity — a copy; truthful under truncation where HasCode is not
result.TruncationNote()   // Canonical dropped-issues line; "" when nothing was dropped

// Conversion
result.Err()              // Returns error if !OK(), nil otherwise
result.String()           // "OK" when there is no issue at all; otherwise a summary line (errors, warnings, info, hints, the limit note) and one line per retained issue at every severity
```

### Values and Details in an Issue

A message repeats a data value, or a name the data writes, through `diag.QuoteValue` or `diag.ValueText`. A property value, a key and a number literal are each held to this one rule. `QuoteValue` quotes the text as `strconv.Quote` does. `ValueText` leaves text the message shows unquoted, such as a key's JSON or a type identity. Text longer than 32 bytes is cut to its longest prefix of whole characters within 32 bytes, and its length in bytes follows: `"ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"… (4096 bytes)`. A name the data writes, such as an unknown member name, is cut by the same rule. A name the schema declares, and a type identity the schema resolves, are shown whole. `ValueText` renders text that is not valid UTF-8 as `QuoteValue` does, whatever its length.

`IssueBuilder.WithDetail`, `WithDetails` and `WithExpectedGot` hold every detail value to valid UTF-8. A value that is valid UTF-8 is held as given. Any other value is held as the quoted Go string `strconv.Quote` returns, so every detail value of a built `Issue` is valid UTF-8.

### Rendering Diagnostics

```go
renderer := diag.NewRenderer(
    diag.WithSourceProvider(provider),   // source text for excerpts
    diag.WithExcerpts(true),             // show source excerpts
    diag.WithModuleRoot("/project"),     // text locations relative to this root
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
- `limit_reached` (bool) and `dropped` (int) — emitted together when the result was truncated at its issue limit, so a consumer reading `issues` knows it is not reading all of them. Omitted otherwise.
- `issues` (slice of objects) — one entry per issue, each carrying `severity`, `message`, and optional `code`, `source_name`, `path`, `location:{source,line,column}`, `hint`, `details:{...}`. Always emitted as a slice. Log aggregators iterate the slice directly — there are no positional `issue_0`, `issue_1`… attributes.

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

Every valid type name whose value is an array is an entry of the returned map, an empty array included, unless a fault or a cancellation stops the parse before it is read; so a caller that validates each entry reaches every type name whose array was read, and a name the schema does not declare draws `E_INSTANCE_TYPE_NOT_FOUND` from `instance.Validator` whatever its array holds. The shapes that follow, which a JSON decoder would resolve silently, are Error `E_ADAPTER_PARSE` diagnostics instead, each reported where it stands, because what a decoder keeps of them is one reader's convention and not the document's meaning. A type name repeated as a root key is reported at the repeat, and both arrays' instances are in the entry, each indexing its own array, where decoding keeps the last array and drops the first batch whole. A member name repeated inside one object, at any depth of an instance, is reported at each repeat, and the instance is produced holding the value the decoder kept — the last, which `encoding/json` keeps. Names compare as the decoder reads them: escapes resolved, and each byte of an invalid UTF-8 sequence read as U+FFFD. A `null` at a type name is reported where it stands, where decoding yields a nil slice with no error. A `null` element of an instance array is reported where it stands, where decoding yields a nil map with no error, and no instance is produced for it. Anything but white space and closed comments after the root object is reported where it starts, whether or not it reads as a JSON value; an unterminated block comment is content. And a fault the decoder cannot read past — a syntax error, a truncated document — is reported and parsing stops there, returning every instance read before it and reading nothing after it. The fault is placed at the first byte no continuation of the document can repair, the document read as JSONC: jsonc blanks a comma a closing delimiter follows, so a comma that opens an object or an array can still be repaired, and so can a comment still open. A document that ends too soon, inside a comment included, is placed at its end. The nesting limit is the running `encoding/json`'s: its own implementation counts it within each value it decodes whole, and the v2 implementation (`GOEXPERIMENT=jsonv2`, the default from Go 1.27) counts it from the root.

**Every instance carries provenance.** `source` names the document, and each `RawInstance.Provenance` holds that name, the instance's path in the document (`$.Person[0]`, indexing the array as the document writes it, so a failed element still consumes its index) and a point span at its opening brace. A parse diagnostic carries a span in `source` wherever its offset lies in the data or at its end; the cancellation diagnostic carries none. Columns count runes from the line start of the bytes passed in, not of the buffer JSONC preprocessing returns — that buffer writes each byte of a closed comment as one byte, a space unless it is a tab or a line break (JSONC Support below states an unclosed block comment's), so a multibyte rune inside a comment would move every later column on its line — and a malformed UTF-8 sequence decomposes exactly as it does for schema sources. A leading byte order mark is trimmed before decoding and its length added back to every byte offset, so it occupies column 1 of line 1 and every later position on line 1 sits one column further.

### Serialization

```go
// Serialize a snapshot to JSON bytes
data, err := adapter.MarshalObject(ctx, snap, writeOpts...)

// Write a snapshot to an io.Writer (returns bytes written). The document is
// built in memory first, so peak memory is the whole serialized snapshot.
n, err := adapter.WriteObject(ctx, w, snap, writeOpts...)

```

The object output is keyed by the name the entry schema addresses each root type by — bare for a local type, `alias.Name` for a directly imported one. Two such names never collide, because every snapshot root is a type the entry schema can name; `Graph.Add` refuses any other root, and the import, `snapshot.Load` and `snapshot.Verify` refuse a snapshot or a document that holds one. So there is no collision for the writers to check.

Every constructor of a snapshot holds it to the facts `Graph.Add` keeps (the graph package doc's "Structural facts"), so every relation the writers meet renders in a shape `ParseObject` and the validator accept. A value can still have no JSON form that reads back as itself: a `json.Number` that spells a number no finite `float64` holds, where no `Integer` or `Float` converts it. Only `snapshot.Load` without revalidating at Error admits one, and a graph `graph.NewFromSnapshot` seeds from such a snapshot keeps it in its own `Snapshot`. That value is refused with an error matching `json.ErrUnrepresentable`, naming the instance or edge and the property, so a caller separates it from an I/O failure without matching the message text. The writers' other errors carry no class: `ErrZeroSnapshot`, a cancelled context, a write failure, and the invariant guards no constructor lets a snapshot reach, which are a type the entry schema cannot name, an instance or an edge target whose type does not resolve, and an encoding failure of the assembled document.

**Cancellation.** Every JSON entry point observes its context at each unit of work it has. `ParseObject` checks once per top-level key, the unit it reads, and returns the types read so far beside a Fatal `E_CONTEXT_CANCELLED`, since the parse did not finish. `MarshalObject` checks once per type group and returns an error wrapping the context's; `WriteObject` checks again once the document is built and writes nothing if it is cancelled by then.

The writers emit the shapes `ParseObject` and `instance.Validator` accept (v0.15.0): an association renders as a `_target_<pk>`-keyed object with its edge properties beside the key fields — one object for `(one)`, an array of objects for `(many)` — with key components and edge-property values rendered through their constraints' canonical forms. A composition renders as an array of child objects for every multiplicity. The output round-trips when the snapshot's `Attestation()` reports `Values` — every instance entered through the validator — and `Unresolved()` is empty: `ParseObject` plus the validator accept all of it. A loaded snapshot carries its header's claim, and `graph.Attestation` states what that claim proves. An unresolved edge is not written, so an invariant that reads its relation, even an optional one, can fail on the way back; persist such a snapshot in the `.ys` format when its edges must survive. Any other snapshot is written as held — each value in the form its constraint stores where the constraint renders it, a nil value as `null` at a property or inside a list — save the values the writer refuses, and the validator judges the result. A float is written in the digits `encoding/json` gives it, with `.0` appended when they hold no `.`, `e` or `E`, so a whole `Float` reads back as a float: `1.0`, `2500000.0`, `1e+21`. The decimal point follows the value's Go type, not its constraint, so a float under an `Integer` is written `6.0` too.

### Write Options

| Option | Description |
| ------ | ----------- |
| `WithIndent` | Indentation string for formatted output |

### JSONC Support

The adapter always preprocesses input with `tidwall/jsonc`; there is no option to turn it off.

- Strips `//` and `/* */` comments
- Removes trailing commas
- Preserves byte offsets and line breaks, so a byte position in the preprocessed text still addresses the original. A block comment the input never closes keeps its `/*` and has the rest written as spaces, its tabs and line breaks kept but its last byte a space whatever it held, and gains one byte when nothing follows the `/*`

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
| `WithScalarTypeConstraints` | Emit `PROPERTY_TYPE` constraints — `REQUIRE n.p IS :: T` — for list-shaped and scalar types alike (Enterprise only). Property-type constraints require Neo4j 5.9, and a `LIST` type 5.10; on an older Enterprise server this option is what turns them off |
| `WithRequiredOnlyTypeConstraints` | Emit type constraints only for required properties |
| `WithNodeKeyConstraints` | Emit `NODE KEY` constraints (Enterprise; every Neo4j 5 release holds them) |
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

Generation is **all-or-nothing**: any validation error returns `(nil, result)`, so one unemittable property withholds every constraint of the schema rather than emitting a partial script that looks complete. Beside a name Neo4j refuses as an identifier (Cypher reserved words, below) and two labels that sanitize alike (`E_NEO4J_LABEL_COLLISION`), the one shape a *valid* schema can hit is a list whose element is itself a collection — `List<List<T>>`, `List<Vector[N]>`, or a list of a list-typed alias. Those are legal yammm but Neo4j has no nested collection property type, so `ConstraintsForSchema` and `ConstraintsStructured` report `E_NEO4J_UNSUPPORTED_TYPE` for one, required or optional, under every option, `WithScalarTypeConstraints(false)` and `WithEdition(Community)` included: an option decides which constraints are emitted, and with them which property names are checked as identifiers, never whether a nested collection is refused. `IndexesForSchema` and `ShapeForSchema` do not make that check. The write path does: `BatchNodeQueries` and `CoerceParams` refuse a value of such a property with an error wrapping `ErrUnsupportedListElem`, an empty list included, and read an absent one as nil, which `BatchNodeQueries` writes as null; `yammm export --to cypher` exits 1 over data holding one. Model the inner collection as a `part type` reached by a composition if the model must export to Neo4j. A property of an abstract type that no concrete type and no part type extends draws no report: no label carries it. A bare `Vector` is fine — it maps to `LIST<FLOAT NOT NULL>`.

### Indexes

Indexes are derived from a schema's `@index`, `@@index`, `@vector`, `@fulltext`, and `@@fulltext` annotations:

```go
// Generate Cypher CREATE INDEX / CREATE VECTOR INDEX / CREATE FULLTEXT INDEX statements
statements, result := adapter.IndexesForSchema(ctx, s)

// Generate structured index descriptors
indexes, result := adapter.IndexesStructured(ctx, s)
```

The `Index` struct contains `Name`, `Kind` (`IndexRange`, `IndexVector`, or `IndexFulltext`), `Label`, `Properties` (declared order, significant for composites), `VectorDimensions`, `VectorSimilarity`, and the complete `Statement`. A property-level `@index` yields a single-property range index; a type-level `@@index(a, b)` yields a composite range index; a property-level `@vector(cosine|euclidean)` yields a vector index whose dimension comes from the property's `Vector[N]` constraint; a property-level `@fulltext` and a type-level `@@fulltext(a, b)` yield fulltext indexes rendered with the `CREATE FULLTEXT INDEX ... FOR (n:Label) ON EACH [n.a, n.b]` list form.

Index names are always emitted (`{label}_{props}_idx` for range, `{label}_{prop}_vector_idx` for vector, `{label}_{props}_fulltext_idx` for fulltext). The readable name is not injective — it joins on underscores, which property names may themselves contain — so two indexes whose names would collide each receive a short deterministic digest suffix. Only names that would actually clash are suffixed. A load refuses an index annotation with no eligible target (`E_UNKNOWN_ANNOTATION_TARGET`, `E_INVALID_ANNOTATION_TARGET`), so no schema it returns draws the adapter's own codes for the two cases, `E_NEO4J_UNKNOWN_PROPERTY` and `E_NEO4J_INVALID_INDEX_TARGET`. Indexes are emitted for every edition (range, vector, and fulltext indexes are core query features on both Community and Enterprise); abstract types are skipped, part types are not. The emitted `CREATE VECTOR INDEX ... OPTIONS` statement form requires Neo4j 5.15+.

A declared fulltext index carries no analyzer or `eventually_consistent` configuration — no `OPTIONS` clause is emitted, so the server default applies — and the diff therefore does not compare analyzer configuration: a remote fulltext index with a custom analyzer but matching (label, properties) reads as a Match. That is a documented known limit of the v1 fulltext surface, not an accident.

### Graph Shape

```go
// Compute the graph shape (labels, primary keys, @writeOnce keys) for a schema
shapes, result := adapter.ShapeForSchema(ctx, s)
```

`ShapeForSchema` returns a `*GraphShape` that holds, by `schema.TypeID`, each non-abstract type's Neo4j label, primary keys and `@writeOnce` properties. It exports no field: a caller passes it to the write surface and cannot change it.

The walk covers the whole import closure (`Schema.Closure()`): every member schema's non-abstract, renderable types — imported and transitively imported ones included — get a shape, labelled under the declaring schema's name (v0.15.0). A label collision across closure members is refused with `E_NEO4J_LABEL_COLLISION`.

### Write Queries

`BatchNodeQueries` and `BatchEdgeQueries` operate on a complete `graph.Snapshot` for high-throughput batch writes. Both require a `GraphShape` that `Adapter.ShapeForSchema` built from the snapshot's schema object, and refuse any other, another load of one source included: that load can declare a key another way, so a merge key would reach the driver under one type while the same property is coerced from this schema to another. A shape is keyed by `schema.TypeID`, so two closure types sharing one bare name each get their own label. Two root types render one label only where their sanitized schema and type names compose one label, as two schema names that sanitize alike do, or schema `a` with type `B__C` and schema `a__B` with type `C`; `ShapeForSchema` refuses that closure.

```go
shapes, _ := adapter.ShapeForSchema(ctx, s)
nodeQueries, err := adapter.BatchNodeQueries(ctx, snap, shapes, writeOpts...)
edgeQueries, err := adapter.BatchEdgeQueries(ctx, snap, shapes, writeOpts...)
```

Both return query structs (`BatchNodeQuery`, `BatchEdgeQuery`) with `Statement` and `Params` fields, ready for driver execution. `BatchEdgeQuery` also carries `RelationType`, the Neo4j relationship type.

An error matching `neo4j.ErrUnrepresentable`, naming the type and the property, the edge property or the primary key, as the JSON adapter marks its own, refuses these values: one its constraint's coercion refuses, a nested collection's (matching `ErrUnsupportedListElem` too), a map at a property the schema declares, which Neo4j stores as no property value, a list or a map a document nests more than 256 levels deep, a `json.Number` that is an integer literal past `int64` at any position but a `Float`, or that no finite `float64` holds, or a primary key that is absent or holds no value. An integer at a `Float` position passes them and rounds to its nearest `float64`. No snapshot these writers accept holds a property the schema does not declare: the validator keeps one out of what `Graph.Add` takes, and the import and the `.ys` reader refuse one. Of the values the mark refuses, a validated snapshot holds only a nested collection's, which the validator accepts; any other is held only by a snapshot whose values no validator refused at Error. `CoerceParams` marks its refusals of a value so too. No other error carries the class: a caller's own, such as a constraint `schema.CheckConstraint` refuses, a constraint at a nested map's or a rows param's own key, a `GraphShape` the adapter did not build for the snapshot's schema or a relationship type that is no valid identifier, and a cancelled context. `yammm export --to cypher` exits 1 over a refusal of the class, as it exits over the JSON adapter's refusals.

`BatchNodeQueries` returns a phased, ordered slice; each query carries a `Kind` (`NodeMerge`, `CompositionReplace`, `CompositionCreate`). Every node merge precedes every composition replace, which precedes every composition create (parent-first by depth) — executing the slice in order is correct, and the ordering is a documented guarantee (v0.15.0).

Composed children are written under ownership semantics: a parent write replaces its composed subtree. The replace phase deletes every part reachable from each written root through the schema's composition closure — whether or not the snapshot carries children — and the create phase rebuilds the tree fresh (`SET c = …`, never `+=`). Part nodes carry their identity in the `_composed_key` property, which this adapter mints: a flat JSON array of the owning root's **label**, the root's key values, then one segment per composition hop — `["shop__Order",["o1"],["SECTIONS",["s1"]],["NOTES",0]]`, with a `(one)` hop as the relation name alone, `["INVOICE"]`. It is rooted at the owning root rather than the immediate parent, and carries no source path. For a keyless `(many)` part the segment is positional and not a stable identity across writes, which replace semantics make safe. Part DDL pairs with this: `UNIQUE` + `NOT NULL` on `_composed_key`, and no `UNIQUE`/`NODE KEY` on a part's declared primary key.

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

A type's write-once properties come from its `@writeOnce` annotations, which `ShapeForSchema` records on the shape; `ImmutableKeysFor(t schema.Type) []string` returns them (own and inherited). The derived set per type drives the `ON CREATE` / `ON MATCH` split, selected per type in a batch. Node merges only — relationship merges have no `ON CREATE` / `ON MATCH` split.

### Relationship match counting

Every generated relationship statement ends with `RETURN count(*) AS matched_rows`, reflecting that call's (or chunk's) MERGE match count only — 0 when the MERGE is a structural no-op (silent-failure condition). Consumers issuing multiple chunks sum `matched_rows` across results to detect silent no-ops. Node statements stay `RETURN`-free: constraint violations on nodes surface as driver errors.

### Property Coercion

The write surface (`Adapter.BatchNodeQueries` / `Adapter.BatchEdgeQueries`) coerces schema-typed property values to the driver-native types Neo4j TYPE constraints require — repairing the JSON round-trip where a whole-number `Float` decodes as `int64`, and `Date` / `Timestamp` values travel as strings. The write surface first refuses a value that holds one at a list whose element is a collection, by the property's type, with an error wrapping `ErrUnsupportedListElem`. It refuses a list or a map nested more than 256 levels deep; it hands a JSON number literal on as the number the validator reads at its constraint (an `int64` for an integer literal, a `float64` at a `Float` and for a float literal), refusing one no such number holds; and then it coerces the value. The coercion step is exported so consumers writing **direct Cypher** (parameterized `MERGE` / `SET` built by hand, bypassing the `Adapter` write path) can apply its rules; it reads through no carrier save to ask whether a value holds none (`instance.IsAbsent`), so it judges a pointer or a named type that holds a value as the value it is, not as the scalar it holds:

```go
// ParamTypes maps a Cypher parameter name to the schema constraint its value
// must satisfy. Nested params use "outer.inner" dot-notation (e.g.
// "rows.unit_price"). The value is the full Constraint, not just its
// Kind, so List properties can be element-coerced (the element type a bare
// Kind would discard).
type ParamTypes map[string]schema.Constraint

// CoerceParams coerces every value in a parameter map against its declared
// constraint, walking one level of nested map[string]any and of rows (a
// []map[string]any, or a []any of map[string]any values and untyped nils, at
// least one a map), which it
// returns as a []any so a nil row reaches the driver as null.
// Scalars take the coercion rules below; []any lists are element-coerced against the
// List element type, and refused whole, an empty list included, under a List
// whose element is a List or a Vector. A value that holds none, a param, a field or a row, is
// returned as nil, declared or not. A field or a row's field is typed at
// "outer.inner", and a non-nil constraint at a nested map's or a rows param's
// own key is an error. A non-nil constraint in types that schema.CheckConstraint refuses
// is an error before any value is coerced, and of several the error names the
// first key in sorted order. Returns the first error, naming the offending key.
func CoerceParams(params map[string]any, types ParamTypes) (map[string]any, error)

// ParamTypesForType derives a ParamTypes from a schema type's properties, own
// and inherited. Pass prefix "" for top-level params, or an outer name (e.g.
// "rows") for a nested param map — keys are dot-joined to match CoerceParams.
func ParamTypesForType(t schema.Type, prefix string) ParamTypes

```

Coercion rules: `Float` ← an `int`, `int8`…`int64`, `uint`, `uint8`…`uint64` (not a `uintptr` or a named type) or `float32` → `float64` (a `float64` passes through); `Timestamp` ← a string parsed against the constraint's custom Go layout when it declares one (`Timestamp["…"]`) or RFC3339 / RFC3339Nano otherwise → `time.Time` (a `time.Time` passes through); `Date` ← `"2006-01-02"` string, `time.Time` or `dbtype.Date` → `dbtype.Date` at midnight UTC; `Integer` ← an `int`, `int8`…`int64`, `uint`, `uint8`…`uint64` (not a `uintptr` or a named type) → `int64` (a float, whole or not, and an unsigned value outside the `int64` range, is an error, as the validator's Integer rule refuses them); every other scalar kind passes through unchanged, save a map, which is refused under every kind (below). `List<T>` values are coerced element-wise into the concrete typed slice (`List<Float>` of `int64`s → `[]float64`, `List<Date>` of strings → `[]dbtype.Date`, and so on); a `List<Timestamp["…"]>` honors the element's custom layout too, and a value at a `List` whose element is a `List` or a `Vector` is refused whole (Values, Nested collections).

**Zone handling, for `Timestamp`.** The driver sends an instant either as an offset or as a time-zone identifier the server must resolve, so the coercion expresses every instant in a location the driver can send. Two rules, in order. First, `time.Local` is always re-expressed in its offset: a value built by `time.Now` carries the host's location, whose name is `"Local"` on a host with no `TZ` set and which no server resolves. Second, any other location is kept in three cases: it is `time.UTC`, it already carries the driver's `Offset` zone name, or the host's tz database resolves its name (an IANA name such as `Europe/Berlin`, or a legacy name such as `EST`) to the offset the value carries. Every other location is re-expressed in its offset. A value parsed from text lands wherever `time.Parse` put it: a `Z` suffix yields `time.UTC`, which is kept as-is; an offset matching the host's local offset at that instant yields `time.Local`, which the first rule re-expresses; any other offset yields an unnamed zone, which the second rule re-expresses in its offset: the driver selects the offset encoding by zone name, so an unnamed zone is not yet that form. A layout with no zone abbreviation gives one coerced value on every host. `time.Parse` gives an abbreviation the offset the host's local zone has for it, and a zero offset when that zone has none, save a `GMT` form with hours such as `GMT+3`, which it reads as those hours. So under a custom layout that holds an abbreviation, the instant depends on the host: `2024-01-01 12:00 PST` reads as -08:00 under `America/Los_Angeles` and as +00:00 under UTC. Re-expression never moves an instant. A consumer binary that must keep IANA names on a host without a tz database imports `time/tzdata`.

The four transforming kinds (`Float`, `Integer`, `Timestamp`, `Date`) are **strict** — scalar and list element alike: a value they can neither pass through as already-driver-native nor repair (a non-numeric under `Float`; a float or non-integer under `Integer`; a non-temporal or unparseable value under `Timestamp` / `Date`) returns an error rather than reaching the driver wrong-typed. The other scalar kinds are lenient as a scalar: a correct value of those is already driver-native, so there is nothing to repair or reject, and instance validation is the type authority. A map is refused under every kind, whatever its key and element types: Neo4j stores no map as a property value. It is judged by its Go type alone, so a pointer to one or a struct is a value of the lenient passthrough. A value that holds none (`instance.IsAbsent`), such as a nil pointer, slice or map, is returned as nil at each level the coercion reads — a param, a field of a nested param map, a row, and, on the write path, a relation property — and the driver writes it as null. A constraint `schema.CheckConstraint` refuses is an error whatever the value, a nil included. `CoerceParams` coerces a `[]any` it reads under a declared `List` or `Vector` whose elements are scalars element by element, and refuses a nil element there. Every other value below those levels is kept as given, a nil included, save under a declared `List` whose element is a collection, which is refused whole (Values, Nested collections); the driver packs a nil slice kept so as an empty list. An unhandled kind also returns an error (a new `schema.ConstraintKind` is caught at build time by an exhaustiveness lint).

The rendering counterpart — the string form the library stores for a `Timestamp`, `Date` or `UUID` — is `instance.CanonicalValue`, under [Value Functions](#value-functions).

**When to call:** at any direct-Cypher parameter boundary that writes schema-typed `Timestamp` / `Date` / `Float` properties — scalar or `List<…>` — e.g. an enrichment `MERGE` or relationship-maintenance query built outside the `Adapter` write path. Writes that go through `Adapter.BatchNodeQueries` / `BatchEdgeQueries`, or that already pass native Go types, need no extra coercion — those coerce their own merge keys and endpoint keys as well as their properties. Because `ParamTypes` carries the full constraint, `ParamTypesForType` is the easiest way to build one; it derives the element types lists need. When a hand-built merge key does not sit where the properties sit, coerce it explicitly — a `Date` primary key reaching the driver as a string matches no node whose property is a `DATE`, and every re-run inserts a duplicate, or fails where a uniqueness constraint holds the key.

### Schema Inference

```go
// Generate a .yammm scaffold from introspected Neo4j constraints and relationships
yammmSource := adapter.InferSchema(constraints, relationships, schemaFilter)
```

`InferSchema` takes `[]RemoteConstraint` and `[]RemoteRelationship` values (obtained from introspection queries) and produces a `.yammm` source string. Each label is parsed as `Adapter.Label` writes it under the adapter's prefix and separator, so a label another configuration wrote is not read as this schema's type; `schemaFilter` is compared as a label writes it, and a filter that matches none of the constraints read leaves a TODO line saying so. Helper functions `IntrospectConstraintsQuery`, `IntrospectRelationshipsQuery`, `ParseRemoteConstraints`, and `ParseRemoteRelationships` assist with gathering introspection data from a live database.

### Constraint Diffing

```go
// Ownership is exact set membership, computed once from the schema (see Index Diffing below)
owned := adapter.OwnedLabels(ctx, s)

// Compute the semantic diff between desired schema constraints and actual database constraints
diff := adapter.DiffConstraints(desired, actual, owned)
```

`DiffConstraints` returns a `*ConstraintDiffResult` with **five** sets: `Match` (identical), `Drift` (four producers: a TYPE constraint whose enforced type differs; a constraint holding a desired constraint's name under a different definition; a desired constraint whose name is already taken; and a desired UNIQUE or NODE KEY constraint whose label and properties a RANGE index already serves), `Create` (missing from database), `Drop` (in database but not in schema), and `Unverified` — constraints present on both sides whose definition could not be compared because the records carried no `propertyType`. That happens when a TYPE record reaches `ParseRemoteConstraints` with no `propertyType`, or with a null one, as a record does that came from a caller's own query that does not yield the column rather than from `IntrospectConstraintsQuery`. A desired TYPE constraint with no record at all lands in `Create`, or in `Drift` where another constraint, or an index the caller passes as `alsoBlocking`, holds its name. Folding `Unverified` into `Match` reports an unchecked constraint as verified. A drift gate must count it as an incomplete check, exactly as on the index side.

Both diff results also carry `Excluded int` — the number of remote objects the comparison neither paired nor dropped: the schema owns no label they carry, or they are of a kind this configuration cannot declare. On the constraint side that is a relationship constraint, a node constraint kind the DSL cannot express, and under `WithEdition(Community)` every kind but UNIQUE; on the index side it is every row `RemoteIndex.Declarable` refuses, as the next section lists. A counted object can be in a set as well: one that holds the name of a desired object nothing realises blocks its `CREATE`, and is the `Actual` of the `Drift` that reports the block. The count is not drift; in a database shared with other applications a non-zero count is the normal state. It is there so that "0 to drop" cannot be read as "the database is accounted for": ownership is derived from the schema, so objects left behind by a type deleted or renamed since the last apply sit on a label no current type declares and nothing in the schema can name them. A caller reporting "in sync" should say how many objects were left out of that claim.

`DiffIndexes` and `DiffConstraints` each take a variadic list of the other's remote objects (`alsoBlocking`). Index and constraint names share ONE Neo4j namespace, and a `CREATE ... IF NOT EXISTS` under a name the database already holds creates nothing. Over a name of its own kind it is a silent no-op, and a constraint's backing index counts as an index. Over the other kind's name the server refuses it. A caller that introspected both should pass both — otherwise a blocked declaration reports as a create on every run, which the server refuses. A constraint backed by an index appears in `SHOW INDEXES` under the constraint's name and is seen automatically; NOT NULL and TYPE constraints have no backing index and reach the index diff only this way.

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

`DiffIndexes` returns an `*IndexDiffResult` with **five** sets: `Match`, `Drift` (four producers: a vector index whose dimension or similarity differs; a definition change under a name the database already holds; an index in a state that serves no queries and will not recover (`FAILED`); and a desired index that no remote index realises, whose name another object holds, a constraint's backing index included, or whose definition an index realising a different declaration holds), `Create`, `Drop`, and `Unverified`. Composite property order is significant, a deliberate divergence from `DiffConstraints`: a same-set/different-order remote index is a distinct index — create, and drop if `RemoteIndex.Declarable` accepts it, when its name differs too, and drift when it holds the desired index's name (a `CREATE INDEX ... IF NOT EXISTS` under a name an index already holds is a silent no-op). A schema-owned remote index that `RemoteIndex.Declarable` accepts and no declaration takes is reported as a drop; drops are reported, never applied.

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

Every schema-owned remote index the caller passes in is accounted for: it is the `Actual` a desired index was compared with, in `Match`, `Drift` or `Unverified`; it is reported as a drop; or — for a shape the schema cannot declare (a kind other than RANGE, VECTOR and FULLTEXT, such as TEXT, POINT, BTREE or an unreported one; a multi-label FULLTEXT; a relationship index; a constraint's backing index that serves no declaration) — it is counted in `Excluded`. A constraint's backing index that serves a declaration is that declaration's `Actual` whatever its state, a `FAILED` one in `Drift` and a `POPULATING` one in `Unverified`, and is not counted. A block puts an index in two places: one that holds a desired index's name is the `Actual` of that drift as well, whether it is counted in `Excluded` or is another declaration's `Actual`. Two remote indexes sharing one semantic identity (an operator-created index alongside the schema's own) are told apart by name — the schema's own index carries the name the adapter emits — so the redundant one is reported rather than absorbed, and which is which does not depend on the order the server returned rows in. `DiffConstraints` gives the same guarantee.

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

The DROP builders take a name the database already holds — from introspection or a diff result — rather than one this package generated, so they quote instead of reject. Every non-empty name, a valid identifier and a reserved word included, is backtick-quoted with embedded backticks doubled, so no name can alter the statement's shape. Rejecting a reserved word would make the object undroppable, which is the opposite of the point. An empty or all-space name is an error wrapping `ErrEmptyIdentifier`, and `Constraint.DropStatement` returns it under `WithNamedConstraints(false)`, which leaves every `Name` empty.

The caller owns the verb. Index and constraint names share one namespace, so the object blocking a desired constraint may be an index: `ConstraintDrift.Actual` with an empty `Type` and a non-empty `Name` is that case, and it needs `DropIndexStatement`. These functions build statements; they never execute one.

The DSL reserves no Cypher keyword as one; of the six spellings its grammar
refuses as a property name, four are Cypher keywords too (`as`, `in`, `true` and
`false`). A property named `match` or a type named `MATCH` is valid yammm and exports cleanly through the JSON
adapter, but identifiers that appear unquoted in generated Cypher (property
names, primary keys, assembled labels) are validated during constraint and index
generation and rejected with `E_NEO4J_INVALID_IDENTIFIER`, whose detail begins
with `ErrReservedKeyword`'s text — the check is case-insensitive. `ShapeForSchema` validates the assembled **label** only; it is
not a property-name gate. Namespaced labels usually absorb reserved type names
(`app__MATCH` is not a keyword); a reserved property name fails wherever an
emitter validates it, once per declared property — and `WithScalarTypeConstraints(false)`,
`WithRequiredOnlyTypeConstraints(true)` and `WithEdition(Community)` each narrow
that set. For
export-compatibility feedback before write time, run `ConstraintsForSchema`
**and** `IndexesForSchema`, or call `ValidateIdentifier` on names directly — the
index pass is what covers a property named only by an `@index`, `@@index`,
`@vector`, `@fulltext` or `@@fulltext` annotation.

## Go Source Generation

The `schema/gogen` package generates Go source from a schema: one struct per type, named Enum/DataType types, generated temporal types, `EDGE_` association structs, a `Graph` aggregate, and the embedded schema source. Unlike the data adapters above, gogen has no instance-data path — it is schema-in, bytes-out. Generated output is stdlib-only (it imports at most `time` and `encoding/json`).

### Generation

```go
func Marshal(s schema.Schema, opts ...Option) ([]byte, error)
```

```go
data, err := gogen.Marshal(s, gogen.WithPackageName("model"))
```

`Marshal` takes a schema a load returned, including one loaded under `schema.WithSyntheticRoot`: `Schema.ModuleRoot()` reports the synthetic root, so the emitted keys are relative to it exactly as a disk load's keys are relative to its module root (see [Imports](#imports) for how each key is derived).

### Options

| Option | Description |
| ------ | ----------- |
| `WithPackageName` | Override the generated package name (default: derived from the schema name; a derived keyword, predeclared identifier such as `nil`, `string` or `len`, `main` or `init` takes a `_` suffix). The name must be a Go identifier that is not a keyword and not `_`; `Marshal` refuses any other, the empty name included, with `ErrInvalidPackageName`, and `CheckPackageName` applies the same rule before a schema is loaded |
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
| `Enum` (inline) | `string` underlying | **yes** — `type <Type><Field> string` + value consts, at any List depth (`List<Enum[…]>` → `[]<Type><Field>`); on an edge property the owner is the `EDGE_` struct (`EDGE_Person_works_at_CompanyRole`); the inline enum element of a List DataType is `<DataType>Element` (`type Tags = List<Enum[…]>` → `type TagsElement string` + consts, `type Tags []TagsElement`) |
| DataType (`type X = …`) | the underlying's Go type | **yes** — `type X <underlying>` (an Enum DataType also emits value consts); a DataType over `Date` or `Timestamp` is `type X struct{ time.Time }` with the codec its layout needs |

The library stores a `Date` as `"2006-01-02"` and a custom-layout `Timestamp` through its declared layout, and the JSON adapter writes those strings; a bare `time.Time` cannot decode a `Date` or any layout whose text is not RFC 3339. The generated `Date` and per-layout types embed `time.Time`, so every `time.Time` method but `MarshalJSON` and `UnmarshalJSON` is promoted (an existing `.Format(…)` caller compiles unchanged), a value is built as `Date{Time: t}`, and their own `MarshalJSON`/`UnmarshalJSON` exchange the stored form. A per-layout type's bare spelling is `"Timestamp"` plus the layout's letters and digits — `Timestamp20060102150405` for `"2006-01-02 15:04:05"`. When another entity claims that spelling, the layout takes its exact spelling, `"Timestamp_"` plus its layout with every other rune written as its hexadecimal code point between underscores — `Timestamp_2006_2D_01_2D_02` for `"2006-01-02"`, `Timestamp_20060102` for `"20060102"` when another layout shares its base (see [Names](#names)). A layout named only by a temporal DataType is no entity, since that DataType is its own type. A default-layout `Timestamp` stays `time.Time`, whose own codec already exchanges RFC 3339 with nanoseconds, the stored form.

A named DataType is rendered faithfully in every position — scalar field, list element at any depth (`List<FipsCode>` → `[]FipsCode`, `List<List<FipsCode>>` → `[][]FipsCode`), the element of a List DataType (`type Codes = List<FipsCode>` → `type Codes []FipsCode`), edge property, and `_target_` primary-key field — never degraded to its primitive. The generated temporal types are rendered in the same positions: `List<Date>` → `[]Date`, a `Date` primary key → `Date` in its `_target_` field. A List of a temporal DataType names that DataType, as any List of a DataType does: with `type Day = Date`, `List<Day>` → `[]Day`.

### Field and Relation Rules

- **Optional scalar** → pointer + `,omitempty` (`*string`, `*int64`, `*time.Time`, `*Date`); driven by `Property.IsOptional`.
- **Optional `List`/`Vector`** → the slice stays nilable (no extra pointer) + `,omitzero`: a nil slice is left out and a present empty list is written as `[]`, because the library keeps an empty list apart from an absent one and `,omitempty` would drop it. `encoding/json` reads `,omitzero` from Go 1.24; a consumer built below it writes a nil slice as `null`, which instance validation reads as an absent optional.
- **Association** → the owner-qualified edge struct: `*EDGE_<Owner>_<field>_<Target>` (single) or `[]*EDGE_…` (many), never the target type directly.
- **Composition** → `[]*<Child>` for **every** multiplicity, `(one)` included: `adapter/json` exchanges an array for a `(one)` composition too, and a required-one composition cycle rendered as a value type would be an illegal recursive Go type.
- **Every relation field** carries `,omitempty`, required ones included: the validator refuses a `null` relation, and a required relation left unset is refused where presence is checked — graph assembly for an association (`E_UNRESOLVED_REQUIRED`), instance validation for a composition (`E_UNRESOLVED_REQUIRED_COMPOSITION`).
- **JSON tags** preserve the yammm property or relation field name verbatim; Go field names are the mapped CamelCase identifiers. Two fields of one struct whose identifiers meet each take `Field_` and their wire key (`Field_foo_1` and `Field_foo1` for `foo_1` and `foo1`), an `EDGE_` struct's `_target_` key field included.
- **Inheritance is flattened** — each struct carries its own and inherited properties and relations as direct fields (own-first ordering); no Go embedding.

### Structural Output

- **Struct per type** (including abstract and part types). A type's doc comment carries through as its struct's Go doc comment, and a property's, an edge property's included, as its field's; a data type's, a relation's and the schema's own are not written. The text is the loaded schema's, whose continuation lines lost the indentation they all share, byte for byte, when the parser read the comment (see [Comments](SPEC.md#comments)), so layout that indents every continuation line alike makes no code block; lines indented by different bytes, such as a tab on one and spaces on another, share none and keep theirs, which the Go doc-comment formatter reads as a code block.
- **`EDGE_<Owner>_<edge>_<Target>` structs** for associations — that spelling is the struct's bare one, from its owner's and its target's names (see [Names](#names)) — carrying the target type's primary keys as `_target_<pk>` fields with the association's own properties beside them, the shape `adapter/json` exchanges. (An association whose target type has no primary key is rejected: the edge would have no `_target_` fields to identify a target node.)
- **`Graph` aggregate** — one slice field per concrete type the entry schema can name, tagged `,omitempty` and keyed by its `schema.AddressableTag`, the name the JSON adapter keys its object by: the bare type name for the entry schema's own types, `alias.Name` for a directly imported one, so `adapter/json` output decodes into it. A type the entry schema reaches only through another import has no such name and takes no field; its struct is still emitted, as every type in the closure is. The keys are the envelope keys `schema/jschema` emits.
- **`SerializedSources` / `SerializedEntry`** — the embedded-source surface, emitted identically by every generated package whatever its source count: `func SerializedSources() map[string][]byte` returns every source the schema's load held, by key, and `const SerializedEntry` names the entry key. They read an unexported package-level store, so the identifiers a consumer sees do not vary with how many files a schema spans. A `SchemaHash` const carries the structural hash. The embedded source is **guaranteed re-loadable through the recipe the file prints**: `Marshal` re-loads the store exactly as emitted, under a synthetic root, which reads no disk, and returns the bytes only when the `StructuralHash` matches, so a non-re-loadable model is a generation error, never a silent claim.

### Names

Every top-level declaration shares one Go package-block namespace: each type, data type, per-layout type, `EDGE_` struct, inline enum, List element enum and enum value constant, and the reserved names gogen emits or once emitted (`Graph`, `SchemaHash`, `SerializedSources`, `SerializedEntry`, `SerializedModel`, `SerializedModelEntry`, `Date`). Each entity has a bare spelling, built from the names as the schema writes them: a type's or data type's name as an exported identifier under the initialisms, `"Timestamp"` and a layout's letters and digits, `EDGE_<Owner>_<edge>_<Target>`, `<Owner><Field>` for an inline enum (its owner being a type or an `EDGE_` struct), `<DataType>Element`, and an enum's bare spelling followed by the value as an exported identifier (`"a-b"` gives `AB`). An entity takes its bare spelling when no other entity and no reserved name has that spelling. Otherwise every claimant takes its exact spelling — a word naming its family, `_`, and its identity's parts joined by `__`, each rune other than a letter or digit written `_<hex>_`:

| Family | Exact spelling |
| ------ | -------------- |
| type | `Type_<schema>__<Name>` |
| data type | `DataType_<schema>__<Name>` |
| per-layout type | `Timestamp_<layout>` |
| `EDGE_` struct | `Association_<schema>__<Owner>__<RELATION>` |
| inline enum on a type | `Enum_<schema>__<Owner>__<field>` |
| inline enum on an edge property | `AssociationEnum_<schema>__<Owner>__<RELATION>__<field>` |
| List element enum | `Element_<schema>__<DataType>` |
| enum value constant | `Const_<its enum's exact spelling>__<value>` |

Two schemas' `Region` are `Type_geo__Region` and `Type_common__Region`; a type and a data type `Region` of one schema are `Type_geo__Region` and `DataType_geo__Region`; `Url` and `URL`, which the initialisms map to one name, are `Type_geo__Url` and `Type_geo__URL`; a type named `Graph` is `Type_geo__Graph`. A bare spelling either starts `EDGE_` or holds `_` only after a digit or an `X` and before a digit, and an exact spelling holds `_` directly after its word, so no bare spelling is an exact one; and an exact spelling reads back to one family and one identity. A name therefore depends on the set of claims, never on declaration or import order, and adding a declaration never hands a name to another entity: it can only move a claimant from its bare spelling to its exact one.

### Imports

gogen handles schemas with `import`s too: the full import closure is flattened into one self-contained package, named by the rule [Names](#names) states. Embedded source keys are the names the re-load registers each source under, never absolute generation-machine paths, so generated output is byte-reproducible across checkouts and CI runners. The entry keys by its path under the load's recorded module root (`Schema.ModuleRoot()` — for a plain `Load` the `WithModuleRoot` value when given, then the directory of the nearest ancestor holding a `yammm.mod` and failing that the entry file's own directory, the canonicalized root argument for `LoadSourcesWithEntry`, and the synthetic root under `WithSyntheticRoot`), compared as an identity, so a root spelled with a decomposed character keys as its sources do, and an entry outside the root keys with leading `..` segments. A file entry loaded with no root keys against its own directory. A `LoadString` source has no root and keys by the base name it was loaded under, split at either separator and written in NFC; a name with no base, such as `.`, names no file and is refused. Every imported source keys by the text that imports it, resolved against its importer's key by `schema.SyntheticImportKey`, the loader's own rule — so an import through a symlinked directory (`lib -> real`, `import "lib/dep"`) keys as `lib/dep.yammm`, not by where the link resolves. The module root is therefore an input to the generated bytes alongside the schema, the options and the yammm version: committing a `yammm.mod` above a schema moves the keys for an otherwise unchanged generation, and a generator that passes the root explicitly sees no change. `Marshal` refuses two layouts a load accepts, naming the paths, because no store re-loads them: one source imported by two paths under two keys, and two sources taking one key (a relative import through a symlink reads the link target's neighbour on load and the key's text on re-load). It also refuses a key the re-load's rule refuses, a source with no path relative to the root, such as one on another drive, because a key is never a generation-machine path, and a `LoadString` name with no base, which names no file. Before returning, it verifies the store re-loads hermetically through the recipe the generated file prints, under the synthetic root named there.

Re-load under a synthetic root:

```go
s, result := schema.LoadSourcesWithEntry(ctx,
    gen.SerializedSources(), gen.SerializedEntry, "",
    schema.WithSyntheticRoot("embedded://app"))
```

The synthetic root is the recipe: it keeps the re-loaded type identities free of any working directory, checkout or mount point (see [Synthetic source identities](#synthetic-source-identities)). Any root `WithSyntheticRoot` accepts serves; `Marshal` verifies the one the generated file prints, and the package's tests re-load under another the store of every fixture that has a golden.

**Removed in v0.13.0.** The shape-dependent `SerializedModel` and `SerializedModelEntry` declarations, and their `LoadString` / `LoadSourcesWithEntry`-with-`"."` recipes, are gone. Regenerate; every hand-written call site should already read the pair above, which v0.12.3 added.

### Validation

Generated source is run through `go/format` until a pass writes it back unchanged, at most eight passes, and then type-checked with `go/types` before return, so duplicate declarations, unused imports, and undefined references surface as generation errors rather than broken Go. The type-check uses a hermetic stub importer for `time` and `encoding/json`, declaring exactly what generated code calls, so `Marshal` needs no Go toolchain, GOROOT, or build cache at runtime.

### CLI

```text
yammm gen --to go <schema.yammm> [--package <name>] [--output <path>] [--initialisms GUID,JWT] [--module-root <dir>]
```

The `gen` command loads the schema (resolving imports), generates Go, and writes it to stdout or the `--output` path. A `--package` value `WithPackageName` refuses is a usage error (exit 2), judged by `CheckPackageName` before the schema is loaded.

## JSON Schema Generation

The `schema/jschema` package generates a JSON Schema **draft 2020-12** document from a schema, for editor-assisted authoring of instance-data JSON files, which `yammm check` then judges. Like gogen it is schema-in, bytes-out — no instance-data path, plain `error` — and it imports only public yammm packages and the standard library.

### Generation

```go
func Marshal(s schema.Schema, opts ...Option) ([]byte, error)
```

```go
data, err := jschema.Marshal(s, jschema.WithSchemaID("https://example.com/fleet.schema.json"))
```

`Marshal` takes a schema a `schema.Load*` function returned.

### Options

| Option | Description |
| ------ | ----------- |
| `WithSchemaID` | Set the document `"$id"` (omitted entirely when unset) |

The document title is the schema name.

### The Emitted Document

The document describes the object form `adapter/json`'s `ParseObject` + `instance.Validator` read, in the terms below. It does not reproduce that validation: the fidelity caveats name each class of file the two judge differently.

- **Envelope** — one object keyed by type name (`{"Person": [...], "Car": [...]}`), each key an array of instances. Entry-schema **concrete, non-`part`** types under their bare name (a `part` type is `$defs`-only and an `abstract` type appears in neither position — neither can be instantiated at top level); **directly imported** types under their alias-qualified name (`common.Region` — the only form the validator resolves for them); transitively imported types are `$defs`-only.
- **Instance objects** — properties by name, relations by their field name (the UPPER_SNAKE relation name in lower case); `additionalProperties: false` (the instance layer rejects unknown fields); required properties in `required`.
- **Associations** — an edge object (to-one) or array of edge objects (to-many), each `$ref`ing an `EDGE_<Owner>_<field>_<Target>` def carrying the required `_target_<pk_name>` foreign-key fields (validated against the target key's own constraint — a DataType-typed key keeps its `$ref`) plus edge properties. Association **presence is deliberately not `required`**: the instance layer defers it to graph assembly, so a per-file requirement would flag files yammm validates cleanly; the generated `description` states the multiplicity, and for a **required** association the enforcement point as well.
- **Compositions** — always arrays of child objects; required compositions are `required` + `minItems: 1`; to-one compositions get `maxItems: 1` (instance validation refuses a second child, `E_DUPLICATE_COMPOSED_PK`).
- **Constraints** — the full mapping (bounds → `minLength`/`minimum`/…, `Enum` → inline `enum`, multi-`Pattern` → all-must-match `allOf`, `Vector[N]` → fixed-size number array, named DataTypes → `$defs` entries, `$ref`ed from every position — scalar property, list element at any depth, edge property, `_target_*` key field, and inside another DataType's constraint; each `Pattern` rewritten into syntax RE2 and ECMA-262 read alike). Schema doc-comments flow through as `description`, the JSON Schema keyword for human-readable documentation.

Cross-schema imports are closure-flattened into one self-contained document, whose `$defs` keys follow the rule gogen's names follow ([Names](#names)), over names as written and with only types, data types and associations as claimants: a type's or data type's bare key is its name, and an association's is `EDGE_<Owner>_<field>_<Target>`. An entity keeps its bare key when no other entity claims it; otherwise every claimant takes its exact key — `<schemaName>.<Name>` for a type, `<schemaName>.<Name>.datatype` for a data type, `<schemaName>.<Owner>.<RELATION>.edge` for an association. So a type and a data type `Region` of one schema are `geo.Region` and `geo.Region.datatype`, and `A`'s `B_X` and `A_b`'s `X`, whose parts join to one bare key, are `p.A.B_X.edge` and `p.A_b.X.edge`. A bare key holds no `.`, and an exact key read from the right names one kind and one identity, since a closure holds one schema per name. gogen compares Go spellings after the identifier transform, and jschema compares names as written, so `Url` and `URL` keep their bare keys here. A `$ref` percent-encodes every key character a URI fragment cannot hold, so a schema name holding `%`, a space or a non-ASCII letter still resolves. Abstract types get no `$defs` entry (nothing can legally `$ref` one); their members appear flattened into each subtype.

Fidelity caveats. The emitted schema does not reproduce yammm's validation, and each divergence changes only what the editor flags. Every data command — `yammm check`, `load`, `export` and `snapshot save` — runs the three stages the cases below name on a data file it reads: the parse, instance validation and graph assembly. `export` given a snapshot document instead, one whose first member is `yammm_snapshot` whatever the file is named, reads it through `snapshot.Load` and runs neither of the last two.

- **Checks the schema cannot state** — the editor accepts each file: two instances sharing a primary key (graph assembly, `E_DUPLICATE_PK`); two children of one `(many)` composition sharing one (instance validation, `E_DUPLICATE_COMPOSED_PK`); a required association that is absent, an empty array, or whose `_target_<pk_name>` fields name no instance (graph assembly, `E_UNRESOLVED_REQUIRED`); an instance that fails an invariant, since invariants are not emitted (instance validation, `E_INVARIANT_FAIL`); composed children nested deeper than `instance.MaxComposedDepth` (instance validation, `E_COMPOSITION_DEPTH_EXCEEDED`).
- **Numbers** — yammm's `Integer` is an integer literal — no `.`, `e` or `E` — that `int64` holds, and its `Float` is any literal read to its nearest finite `float64`; JSON Schema's `integer` is a number with no fractional part, whatever its spelling. The editor accepts on an Integer a whole decimal or exponent literal (`5.0`, `1e2`) and an integer outside `int64` such as `9223372036854775808` or `-9223372036854775809`, and on a Float a literal no `float64` holds (`1e400`); yammm refuses each (`E_TYPE_MISMATCH`). A validator that reads a number exactly judges a Float's bounds on the literal's own value, where yammm judges its nearest `float64`, so it flags `1.00000000000000001` under `Float[_, 1.0]`, which yammm accepts. Validators differ among themselves: vscode-json-languageservice reads `integer` by the absence of `.`, so it flags `5.0` and accepts `5e-1`, whose value is fractional.
- **Formats** — the emitted `format` keywords (`uuid`, `date`, `date-time`) are annotations by default under 2020-12, so an editor that does not assert formats accepts a malformed UUID, Date or Timestamp that instance validation refuses (`E_CONSTRAINT_FAIL`); a validator that asserts formats reads each by its own grammar, which differs from yammm's parsers in both directions: it flags a UUID without hyphens, which yammm accepts, and accepts a date-time with a lowercase `t` or `z`, or with a leap second (`23:59:60`), which yammm refuses.
- **Names and nulls** — the schema targets canonical spellings of property names, relation field names and `_target_<pk_name>` fields, which yammm matches case-insensitively by default; a `null` for an optional property, which yammm reads as absent, is flagged, since each property takes its value's type alone; an object — the envelope, an instance or an edge object — that names one member twice passes a JSON Schema validator and `adapter/json` refuses it (`E_ADAPTER_PARSE`). `adapter/json` also reads comments and trailing commas, which no JSON document holds, so a validator over strict JSON does not read such a file, and one that reads JSONC, as VS Code does for a `jsonc` document, does.
- **Patterns and layouts** — yammm compiles a pattern as RE2, JSON Schema reads it as ECMA-262, and a Go-based validator reads it as RE2, so each pattern is rewritten into syntax both read alike under ECMA-262's `u` flag: case folding becomes explicit classes, `.` becomes `[^\n]` (or `[\s\S]` under the `s` flag), `\s` becomes its five ASCII characters, POSIX and Unicode classes become ranges, `\d` and `\w` become their ASCII ranges, and a string of Unicode scalar values matches the rewrite exactly when it matches the source. A pattern holding a line anchor under the `m` flag has no such form; it is not asserted, and the fragment's `description` carries its source form. The rewrite needs the `u` flag, which ajv applies by default: a validator without it reads classes by UTF-16 code unit, refuses a pattern whose class holds a range above U+FFFF (a Unicode class such as `\p{L}` rewrites to one), and matches half of a character above U+FFFF with a class. A JSON string holding a lone surrogate (`"\ud800"`) is decoded to U+FFFD by yammm and kept by an ECMA-262 validator, so the two can disagree on that value. A custom `Timestamp["layout"]` emits a plain string with the source form in its `description`.

**Editor wiring.** For a JSON data file, a `json.schemas` mapping in the editor's settings. A JSON data file cannot carry a `"$schema"` member: the envelope admits no member but a type name, and `adapter/json` refuses the key as a type tag (`E_INVALID_TYPE_TAG`). A `# yaml-language-server: $schema=<path>` comment wires a YAML file, which yammm's data commands do not read, so it gets the editor's checks alone.

### Validation

Output is deterministic (byte-identical across runs and checkouts, so generated documents can be committed and drift-checked by regenerate-and-diff). Before returning, `Marshal` self-checks: the bytes must parse as JSON and every `$ref` must resolve, as a validator resolves it, to an entry of the `$defs` object those bytes hold — a failure is a generation error, never emitted output. The package's contract-alignment tests additionally prove, per corpus case, that sample data validates identically under yammm and under the emitted schema compiled by a real 2020-12 validator.

### CLI

```text
yammm gen --to jsonschema <schema.yammm> [--schema-id <uri>] [--output <path>] [--module-root <dir>]
```

Per-target flags are enforced: `--package`/`--initialisms` are rejected for the jsonschema target, `--schema-id` for the go target.

## Markdown Documentation Generation

The `schema/markdown` package generates a Markdown reference document — a Mermaid class diagram plus per-type sections — from a schema, giving every schema a canonical, regenerable documentation artifact. Like its gen-family siblings it is schema-in, bytes-out — no instance-data path, plain `error` — and its third-party modules are `github.com/yuin/goldmark`, the CommonMark parser it reads its own output with, and `golang.org/x/net`, whose HTML5 parser reads the HTML that output renders to.

### Generation

```go
func Marshal(s schema.Schema, opts ...Option) ([]byte, error)
```

```go
doc, err := markdown.Marshal(s)                                  // full document
doc, err := markdown.Marshal(s, markdown.WithClassDiagram(false)) // no class diagram
doc, err := markdown.Marshal(s, markdown.WithClassMembers(false)) // diagram without member lines
```

`Marshal` takes a schema a `schema.Load*` function returned.

### The Emitted Document

One document per invocation, covering the entry schema plus its whole import closure:

- **Title + schema doc** — `# Schema <Name>`, then the schema's doc-comment as its author wrote it.
- **Class diagram** — a `## Class Diagram` heading, then one Mermaid `classDiagram` fence over the entire closure. Classes carry each type's **own** members as `name KindLabel` pairs (named DataTypes show their name; constraint detail stays out of the diagram) — `WithClassMembers(false)` drops the member lines and keeps the classes, stereotypes and edges; an abstract type carries `<<Abstract>>` and a part type that is not abstract `<<Part>>`; edges are `Parent <|-- Child` for inheritance and DSL-labeled `Owner --> Target : NAME (mult)` / `Owner *-- Child : NAME (mult)` for each type's own associations and compositions — inherited structure is conveyed by the inheritance edges, not redrawn. A class id is the display sanitized to ASCII letters, digits and underscores, with every `direction` in it written `direc_tion` — Mermaid reads a line ending in `direction` before a line starting with a direction keyword as one direction statement — and two displays that sanitize to one id stay two classes, the later suffixed `_2`, `_3`, …. A class whose id differs from its display emits the form `class common_Region["common.Region"]`; Mermaid namespaces are deliberately not used (some Markdown renderers do not support them in class diagrams). **The labelled form needs Mermaid 10.1.0 or later**, the oldest release the `mermaid` test below runs; Mermaid 9 fails it with a lexical error. An imported type is always labelled, and an entry-schema type only when its id differs from its name, so an import-free schema in which no id differs renders on Mermaid 9. When the emitter has written a labelled class it says so in the document: one sentence under the `## Class Diagram` heading, before the fence — *This diagram uses Mermaid's labelled class form and needs Mermaid 10.1.0 or later.* A document with no labelled class carries no such sentence.
- **Type sections** — a `## Types` heading, then one `### <TypeName>` per type in declaration order: a badge line (`*Abstract type*` / `*Part type*` / `Extends: [Parent](#parent)`), the doc-comment, then a **flattened property table** (Property | Type | Modifiers | Description) over the full inheritance chain — the Type column renders each constraint's DSL form (`String[1, 100]`, `List<FipsCode>`), and inherited rows carry `from <Owner>` in Modifiers. Associations and compositions follow as DSL-notation bullets with linked targets, an inherited relation carrying the same provenance as a ` — from <Owner>` marker; a relation's doc-comment nests as an indented block under its bullet, and its edge properties as a sub-table. A bullet is `-` and three spaces, so what nests under it starts at column 4, a tab stop, and a doc comment there reads as it does at column 0, a tab in it included.
- **Invariants** — the failure message as a bullet, written as a quoted Go string literal and escaped for Markdown (an inherited invariant carrying the ` — from <Owner>` marker), the doc-comment beneath, then the declaration source in a `yammm` fence extracted via the invariant's span, laid out as written: each continuation line loses as much of the white space up to the declaration's column as it starts with, and keeps the rest, a CR LF or a lone CR is a line end, and the comments the span starts with, the doc comment first, are stripped whatever they hold, since the doc renders separately.
- **Data Types** — a Name | Definition | Description table for each schema that declares a data type.
- **Imported schemas** — one `## Schema <Name> (imported as <alias>)` section per import in closure order (transitive imports, which have no entry alias, head as plain `## Schema <Name>`), the schema's doc-comment beneath the heading when it has one, then its type sections and data types.

**A type is named the way the entry schema names it** — in its heading, every link to it, the `from <Owner>` markers and the diagram: the bare name for a type the entry declares, the entry's own tag `<alias>.<TypeName>` for one it imports directly (the tag `schema.AddressableTag` returns, which is how a data file names it), and `<TypeName> (<schemaName>)` for one it reaches only through another import, which has no tag. **Every heading takes the anchor GitHub gives it**: its slug — the text lowercased, every character but letters and other alphabetic characters, marks, decimal digits, connector punctuation and hyphens removed, spaces made hyphens — suffixed `-1`, `-2`, … when an earlier heading took it. The generator reads the emitted document with a CommonMark parser, renders it to HTML, applies GitHub's tag filter and builds the tree an HTML5 parser builds, and allocates over every heading element of that tree, as GitHub anchors every `h1` to `h6` element it renders — the Markdown headings, doc-comment headings included, and each heading a doc comment writes as raw HTML, anchored by the slug of its text content — so each link lands on the heading it names: two headings that slug alike each keep a working link, and a type named `Types` links to its own heading rather than the `## Types` section.

**Doc-comment text is written as the Markdown its author wrote**: a link, table or heading inside it is the author's own, in a description cell too. The text is the schema's documentation, whose continuation lines lost the indentation they all share, byte for byte, when the parser read the comment (see [Comments](SPEC.md#comments)), so layout that indents every continuation line alike makes no indented code block; lines indented by different bytes, such as a tab on one and spaces on another, share none and keep theirs. Its line ends are written as LF, since CommonMark ends a line at a CR LF or a lone CR. A block a doc comment leaves open — a fenced code block, or an HTML block a blank line does not end — is closed at the end of that comment's block with the line a CommonMark parser reads as its end, a raw-text element such as `<textarea>` with `</pre>`, which ends its block and which GitHub's tag filter leaves alone. Raw HTML a doc comment leaves open for an HTML parser — a comment, a CDATA section, a tag, a quoted attribute value, a `noscript` element's text — is closed after it by one line that reads as nothing from a closed state, which a description cell takes on the cell's line after a space (`<!-- -->`, `<![CDATA[ ]]>`, `<wbr x='"'>` or `</noscript>`), kept only when an HTML5 parser then reads an element written after the comment; a Markdown heading inside the comment that its own open HTML takes is no heading, as on GitHub. An element a doc comment leaves open, such as a `<div>`, is the author's. The generator's own fences are sized past any backtick run in their body. **Every other text the schema supplies renders literally under GitHub-flavored Markdown**; GitHub's emoji filter runs after it, and outside code it can still read a `:shortcode:`, as it did in a heading when measured. A code cell — a property's name and Type, a data type's name and Definition — shows its text byte for byte: a code span with each pipe escaped, or, for text holding a backtick, a backslash before a pipe or a line break, a `<code>` element whose Markdown-significant characters are entities. A description cell holds the doc comment's Markdown on one line: a line break inside a code span folds to the space Markdown reads it as, one inside raw HTML or an HTML block to a space an HTML parser reads as white space, except in a `<pre>` or `<listing>` element's text, where it is a `<br>` (the break right after the start tag, which an HTML parser drops, folds to nothing), and any other to `<br>` in place of the backslash that makes it a hard break, and a pipe after an even run of backslashes gains one, so the row keeps its cells and the pipe renders as in the doc comment. A schema name, an invariant message, a type's import tag and a type reference are escaped for Markdown, and a control character in a schema name is written as its Go escape (`\n`). GitHub reads a `www.` host, a URL and an email address as a link even through a backslash escape or an entity code, so an empty HTML comment (`<!---->`), which a browser does not show, splits each colon, each `@` and each dot after `www` in text escaped this way and in a `<code>` cell. A Mermaid class label writes every character but an ASCII letter or digit, a dot, and an underscore between two ASCII letters or digits as an entity code (`#32;` for a space), which Mermaid decodes when it renders — Mermaid 11 reads a label as Markdown, and Mermaid's render rewrites the sequences it uses as entity placeholders. An edge label and a member line hold schema identifiers and the generator's words: each writes a double quote as `#quot;`, and the number sign, colon, semicolon, percent sign, `<`, `>` and `&` as `#35;`, `#58;`, `#59;`, `#37;`, `#60;`, `#62;` and `#38;`, so the multiplicity `one:many` labels an edge `one#58;many`.

### Validation

Output is deterministic (byte-identical across runs and checkouts), so generated documents can be committed and drift-checked by regenerate-and-diff; the document is a versioned surface, whose tiers the [Versioning Policy](#versioning-policy) states. Before returning, `Marshal` reads the document with a CommonMark parser (`github.com/yuin/goldmark`, with GitHub's extensions) and checks the structure it wrote: no block is left open at the end, every heading it wrote is a top-level heading of its level that reads as the text it meant and holds the anchor the document's heading elements allocate it, outside doc comments the links read are exactly the internal links it wrote, each to such a heading, with no other link, autolink or image, every table it wrote reads with its columns and rows, and the class diagram reads as a fenced code block each of whose lines is a form the emitter writes, a subset of Mermaid's class-diagram grammar and stricter than it, read with each entity code replaced as Mermaid's render replaces it — a failure is a generator bug returned as an error, never emitted output. No schema is refused for its names or its doc-comment text: the escapes above keep every schema-supplied text in a form the check accepts, and doc-comment text is not checked. A test behind the `mermaid` build tag hands every golden's diagram to Mermaid 10.1.0, 11.17.2 and 12.0.0 and requires each to parse the classes, labels and relations the schema states. It reads what Mermaid's parser holds, not the SVG Mermaid 11 renders a label's Markdown into; the class label's escape covers that by writing no character Markdown reads.

### CLI

```text
yammm gen --to md <schema.yammm> [--no-class-diagram] [--no-class-members] [--output <path>] [--module-root <dir>]
```

`markdown` is accepted as an alias for `md`. Per-target flag enforcement extends to the md target: `--no-class-diagram` and `--no-class-members` are rejected for other targets, and go/jsonschema-only flags are rejected for md.

## Formatting

The `format` package provides canonical formatting for `.yammm` schema files:

```go
func TokenStream(text string) (string, error)

// Wrapped by TokenStream's error when its output would not preserve the source.
var ErrNotPreserved error

// TokenStream's error when the source does not parse. Issue is the parser's
// first diagnostic for text the grammar does not match or an escape the
// language does not have, naming no source. Its
// line and column are positions in the text TokenStream was given; its byte
// offset counts each CRLF and each lone CR of that text as one byte.
type SyntaxError struct {
    Issue diag.Issue
}

// Two helpers. Nothing outside this package calls them, apart from the gate that
// pins these signatures.
func NormalizeIndentation(line string) string
func DisplayWidth(line string) int

const LineWidthThreshold = 100 // display cells: a tab counts as 4, an East Asian wide rune as 2
```

```go
formatted, err := format.TokenStream(input)
```

`TokenStream` returns an error, and no output, in two cases.

- **The source fails to parse** — the grammar does not match its tokens, or a string holds an escape the language does not have (malformed digits, or a surrogate code point), save in an invariant expression or an annotation argument, which format as written. The error is a `*format.SyntaxError` carrying the first such issue, and its `Error()` reads `parse failed: ` and the message. A source that parses but that a load refuses formats successfully: inverted bounds, a character the source rules refuse inside a comment or a literal, or an escape whose value is not UTF-8.
- **The output would not preserve the source.** Whitespace, a trailing comma before `]` or `{`, and a leading byte order mark are the only things the formatter may change. After phase 5, when the output differs from the input with its line ends made LF, the two token sequences are compared with those set aside, and every comment's text line by line. On a mismatch the error wraps `format.ErrNotPreserved`: the defect is the formatter's, never the input's. An input already formatted is returned unchanged and costs no comparison.

The CLI reports a parse failure as the positioned `E_SYNTAX` diagnostic and exits with `ExitValidation`. It maps a refusal to `ExitRuntime`, leaving the file unchanged. The LSP returns no edits for either, and logs a refusal as a warning.

Before phase 1, line endings normalize to LF: CRLF and a lone CR both become LF, so a CRLF file always reports as unformatted under `yammm fmt --check`. A leading UTF-8 byte order mark is no token, so the output never carries a leading one and a marked file reports as unformatted the same way; a mark inside a comment or a literal is written back where it stands.

The formatter then applies a five-phase pipeline:

1. **Token-stream rewriting:** canonical spacing between tokens and indentation normalization — **except inside invariant expressions**, whose regions keep the author's own spacing between tokens (a continuation line's leading indentation is still normalized)
2. **Blank line collapsing:** removes excess blank lines while preserving section breaks, and *inserts* one after the schema header and after the last import when the following line is not already blank
3. **Line wrapping:** wraps long lines (enums, extends clauses, invariants) at `LineWidthThreshold`, and collapses an existing multiline enum, extends clause or datatype-alias enum back onto one line when the joined form fits. A construct is not collapsed when any of its lines carries a comment, because the comment would then stand in front of everything after it. A property whose suffix carries an annotation is not wrapped at all: the annotation would land on the closing `]` line, where it attaches to nothing and the schema no longer loads. A modifier on that line is legal. A multiline invariant is never collapsed
4. **Column alignment:** pads the member-name column, measured in display cells, so the column after it lines up — the type for a property, the multiplicity for a relationship, the `=` for a datatype alias — and aligns trailing inline comments. It runs over contiguous runs of one member kind — properties, relationships, and file-scope datatype aliases — broken by a kind change or by any line that is not alignable — a blank line, a comment-only line, a type head or closing brace, an `extends` clause, the first line of a multiline construct. A type-block boundary breaks a run because its brace lines are not alignable, not because blocks are tracked
5. **Text finalization:** trims trailing whitespace from each line, removes trailing blank lines, and ensures the file ends with a newline

Phase 1 records the lexer's view of each line as it emits that line. The record holds three facts:

- the class of the line — blank, comment, or content
- the offset where a trailing comment starts, when the line has one
- the extent of every string and regex literal

A blank line inside a block comment is comment text, not a section break. Phases 2 and 3 read this record, which phase 1 builds from the token stream it already holds, at no second lex. Phase 3 builds lines from pieces of others and has no record for them. So whenever phase 3 changed the text, phase 4 reads a record lexed from phase 3's text; when it changed nothing, phase 4 reads phase 1's. No phase derives these facts from the text by hand.

Three consequences follow. A comma inside a string literal is not a value separator. A bracket inside a comment does not open or close a construct. A comment line is never wrapped, aligned, or read as an enum value or a type name, whatever its text looks like. A comment inside a multiline enum or `extends` list keeps its place, and the formatter re-indents the construct instead of collapsing it.

Output is deterministic and idempotent. The formatter is used by the LSP server for `textDocument/formatting` and by the CLI for the `yammm fmt` command.

## Versioning Policy

This section states yammm's versioning commitments, so consumers and contributors have one reference for what a release bump may change. From the release after `v0.21.1`, a release states its moves in a GitHub release note written once at the tag. `docs/VERSIONING.md` at `v0.21.1` holds the notes of the releases from `v0.3.0` on, `v0.3.1` aside.

### Scope

yammm's versioning covers four surfaces that evolve on related but distinct cadences:

1. **Go API surface** — exported types, functions, methods, constants, variables, struct fields, options, and their documented behavior under the `github.com/simon-lentz/yammm` module.
2. **`.ys` wire format** — the on-disk shape of snapshots produced by `snapshot.Marshal` and the metadata updates (`snapshot.UpdateMetadata`, `snapshot.UpdateMetadataOrReMarshal`) and consumed by every read path of the `snapshot` package (`snapshot.Load`, `snapshot.Verify`, `snapshot.Info`, `snapshot.HeaderOnly`, `snapshot.HeaderOnlyRead`, the `ScanDir` family and the metadata updates). The wire format carries its own `version` field in the header (currently `4`, first tagged by v0.19.0; `3` from v0.12.0 through v0.18.0).
3. **Generated Go output (`schema/gogen`)** — the shape of the Go source `gogen.Marshal` emits for a given schema and options. A committed surface since v0.6.0: a downstream consumer byte-pins committed generated packages against it. Its tier rules are in "Generated-output surface" below.
4. **Emitted Markdown (`schema/markdown`)** — the shape of the reference document `markdown.Marshal` emits for a given schema and options. A versioned surface from the release after `v0.21.1`: a downstream consumer commits a generated document and drift-checks it in CI. Its tier rules are in "Emitted-Markdown surface" below.

The surfaces share a semver tag but have independent version-bump triggers, documented below.

#### Excluded from the v1.0 Go-API compatibility promise

Two packages are **outside** surface 1 for the purpose of the v1.0 promise, and stay changeable at a minor version after the 1.0 tag:

- **`lsp/`** — the language-server implementation and its `Config`. It is consumed through the `yammm-lsp` binary and the VS Code extension, both of which ship in lockstep with the library tag, so its Go API has no independent consumer to stabilize for.
- **`format/`** — the formatter entry points, consumed through the `yammm` CLI and the LSP on the same footing. Both ship in lockstep with the library tag, which is the whole ground for the exclusion: it does not depend on whether some other Go consumer exists.

A formatter output change is still a behavior change, and an LSP protocol change is still governed by LSP's own versioning. The exclusion drops surface 1's promise for these packages' Go symbols and their documented behavior, and this policy sets no tier for either change.

### Pre-1.0 policy (current)

yammm is currently pre-1.0. Under Go module semver conventions, `0.X.Y` releases are understood to be unstable — backward-incompatible changes are permitted at minor-version bumps — but yammm's own policy is narrower than that. The following rules apply:

#### Minor releases (`v0.X.0 → v0.X+1.0`)

- **Additive API changes** — new exported symbols, new option types, new methods on existing types, new struct fields added at the end of a struct (subject to the type's own versioning contract, if any). Such a change can still break a consumer that builds the struct positionally, compares a struct the new field makes non-comparable, or implements an interface that gains the method, and a new `String` or `MarshalJSON` method changes how the type prints or encodes.
- **Subtractive and breaking API changes** — removal of exported symbols, and breaking changes to an exported symbol's signature, name, or type (e.g. changing a method's return type, renaming an exported function, or collapsing two carrier types into one), provided (a) a grep across the known consumer set returns **either** zero external Go references **or** external references confined to a consumer that pins an older yammm release and whose migration is tracked as a downstream backlog item — in which case the semver pin carries the coordination, so tagging the breaking release cannot break that consumer's build and the migration lands on the consumer's own schedule; (b) the change is explicitly documented in the release notes; and (c) the release-notes entry enumerates every removed, renamed, or signature-changed symbol for downstream auditing.
- **Behavior tightenings** — changes where the only observable difference is that a previously-erroring call becomes a no-op for input that was already semantically equivalent to the stored state, or similar correctness-preserving narrowings (e.g., `Registry.Register` becoming idempotent for exact-`SourceID` + exact-`StructuralHash` re-registration in v0.3.0). The release note states the contract change.
- **Wire-format additions** — new optional header or instance fields added under the `omitempty` convention, provided the new field's absence in an older reader does not cause silent data loss. A v0.X reader consuming a v0.X+1-produced document skips the unknown field; a v0.X+1 reader consuming a v0.X-produced document reads missing fields as their zero value. A new `features` entry and a new top-level key are not additions: every older read path refuses the first with `E_SNAPSHOT_UNSUPPORTED_FEATURE`, and each that reads the whole document refuses the second with `E_SNAPSHOT_MALFORMED`.

#### Wire-format-incompatible changes

Any wire-format change whose absence in an older reader would cause **silent data loss** is required to:

1. **Increment the header's `version` field** (e.g., `1` → `2`).
2. **Rely on yammm's existing unknown-version-rejection semantics** — older readers refuse the new version rather than partial-parsing a document they do not fully understand, with Error-severity `E_SNAPSHOT_UNSUPPORTED_VERSION` when the document keeps the old shape.
3. **Document the bump and the asymmetric-reader semantics** in the release notes: which older versions accept the new file (if any — typically none), which newer versions accept the older file (none since v0.12.0: this package reads only the version it writes).

The canonical example is v0.3.0's `UnresolvedEdge.Properties` addition: the new wire field could not be `omitempty`-safe alone because a pre-v0.3.0 reader would silently drop edge properties on cross-batch unresolved edges. The v1 → v2 bump forced pre-v0.3.0 readers to reject cleanly; v0.3.0's readers accepted both v1 and v2 files (v1 files had no Properties to drop, so the read was lossless).

Wire-format version bumps are permitted at minor-version bumps during pre-1.0 (they are the yammm-native signal for "this format has diverged"); they are not deferred to major bumps under pre-1.0.

#### Patch releases (`v0.X.Y → v0.X.Y+1`)

- Bug fixes that preserve the public API and wire format.
- Documentation-only changes.
- Internal refactors, test improvements, and dependency updates.
- **Additive API changes** — new exported symbols whose addition leaves every existing signature and documented behavior unchanged. A new struct field added at the end of an existing struct also qualifies, provided the field keeps a comparable struct comparable and no consumer is licensed to construct the type positionally — the type is documented as constructed by this library and returned to the caller, or the measurement finds no positional composite literal of it in this module or in the known consumer set. The minor clause's broader form still governs a struct a consumer is expected to build.

Patch releases never introduce breaking changes: no removed or renamed exported symbols, no signature changes, no tightenings of documented behavior, and no wire-format changes. Anything in those categories requires at least a minor release.

### Generated-output surface (`schema/gogen`)

For an unchanged schema under an unchanged load name, unchanged `Marshal` options, an unchanged module root, an unchanged yammm version and an unchanged Go release, generated output is byte-identical (all walks and name assignments are deterministic).

The module root is an input because a file load's entry key in `SerializedEntry` and `SerializedSources` moves with it, and so does every key a chain of relative imports resolves from it. The entry keys by its path relative to the root. An imported source keys by the text that imports it: a relative path (`./deep/d`, `../common/c`) resolved against its importer's key, so it moves with that key, and a module-style path (`common/c`) resolved against the root, which does not move. A `LoadString` source, which has no root, keys by its base name in NFC. The root is the one the caller supplied, the directory of the nearest ancestor holding a `yammm.mod` marker, the synthetic root under `WithSyntheticRoot`, or the entry file's own directory — so committing a marker above a schema changes those keys for an otherwise unchanged generation. A generator that passes the root explicitly sees no change. Across versions, changes to the output classify as:

#### Breaking (major post-1.0; pre-1.0 minor under the standard subtractive rules)

- Renaming or removing the package clause or any emitted exported identifier derived from an unchanged schema, such as struct names, field names, named DataType/enum types and their value constants, `EDGE_` struct names, `Graph` fields, or the `SerializedSources` / `SerializedEntry` / `SchemaHash` declarations.
- Changing the type-mapping table (e.g. `Integer` → anything other than `int64`), the pointer-optionality rules, the json-tag shape, or the flat `_target_` fields of `EDGE_` structs.
- Changing the `SerializedSources` / `SerializedEntry` re-load semantics (the documented recipe: `LoadSourcesWithEntry` under `WithSyntheticRoot`) or the meaning of `SchemaHash` (`schema.StructuralHash`).

#### Additive / behavioral (minor or patch, release-noted)

- New emitted declarations, whatever their reason. A new declaration reserves its name, so one that a schema entity already holds moves that entity's identifier, and the Breaking tier's first bullet governs that move.
- Identifier derivation for new inputs — existing identifiers for existing inputs must not change, so an addition to the default initialisms set, which renames identifiers that hold the word, is Breaking.
- Byte-level changes that rename no identifier and change no semantics (comment text inside the generated file, formatting). Consumers that byte-pin via drift gates will see these and regenerate; the release notes must call them out.

#### Explicitly uncommitted

- Doc-comment prose inside generated files.
- Declaration order beyond "deterministic for a given yammm version".

The golden corpus under `schema/gogen/testdata/` is this contract's executable form: a golden may change only in a release whose notes account for the change under the tiers above.

### Emitted-Markdown surface (`schema/markdown`)

For an unchanged schema, unchanged `Marshal` options and an unchanged yammm version, the emitted document is byte-identical. No path, module root or source identity reaches the document. Three inputs of the build reach it besides the schema and the options: the Unicode tables of the Go release that builds the generator decide how a character a later Unicode version added is escaped and slugged, and the goldmark and `golang.org/x/net/html` releases the build selects — `go.mod` names each minimum, and a consumer's module graph can select later ones — read doc comments and headings for anchors and for the blocks a doc comment leaves open. Across versions, changes to the output classify as:

#### Breaking (major post-1.0; pre-1.0 minor under the standard subtractive rules)

- Changing the text of a heading an unchanged schema produces, or the anchor that heading takes: the title, a schema heading, the fixed section headings, and a type's display name (its bare name, the entry schema's `alias.Name` tag, or `Name (schema)`). A link into the document targets an anchor, so an anchor that moves breaks the link.
- Removing a section, a table or a table column an unchanged schema produces, or changing a table's columns or their order.
- Changing the rule a Type or Definition cell renders a constraint by: the constraint's DSL form, byte for byte.

#### Additive / behavioral (minor or patch, release-noted)

- New sections, rows, bullets or diagram elements for a schema feature the document did not render before, and new options whose default leaves the output unchanged.
- Corrections: a byte change that makes the document state the schema as written where it stated something else — an escape added so text renders literally, a reference resolved to the type it names — and that moves no heading's text or anchor. A correction that moves one is Breaking under the first rule above. Consumers that byte-pin will see these and regenerate; the release notes must call them out.
- Byte-level changes that move no heading, anchor, column or cell rule (layout inside the diagram, whitespace between blocks). Release-noted on the same terms.

#### Explicitly uncommitted

- Doc-comment text, which the document carries as Markdown the author wrote. A link, heading or table inside it is the author's, and a heading inside it takes an anchor as any heading does.
- The sentences the generator writes (badge wording, the Mermaid renderer-floor sentence).
- Mermaid class ids, and the order of the diagram's lines beyond "deterministic for a given yammm version".

The golden corpus under `schema/markdown/testdata/` is this contract's executable form: a golden may change only in a release whose notes account for the change under the tiers above.

### Post-1.0 policy (future)

After the `v1.0.0` release, yammm commits to the stricter semver contract that consumers expect from a stable library:

#### Major releases (`vN.X.Y → vN+1.0.0`)

- Removes an exported symbol.
- Changes an exported symbol's signature (parameter types, return types, method set).
- Tightens an exported contract in any way that changes observable behavior (errors become silent, nil returns become non-nil, success paths change their side-effect shape).
- Bumps the `.ys` wire format version.
- Introduces a new required field, new required option, or any other change that forces consumers to update call sites.

#### Minor releases (`vN.X.Y → vN.X+1.0`)

- New exported symbols (types, functions, methods, constants, variables).
- New options that default to pre-existing behavior when omitted.
- New wire-format fields with `omitempty` semantics that do not trigger silent data loss on older readers.
- Documentation, test, and internal-refactor improvements that preserve the public API surface.

#### Patch releases follow pre-1.0 patch-release rules verbatim
