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
adapter := jsonAdapter.New()
```

Input is preprocessed as JSONC: comments and trailing commas are tolerated.

### Parsing

```go
// Parse a JSON object keyed by type name: {"User": [...], "Product": [...]}
parsed, result := adapter.ParseObject(ctx, sourceID, data)
```

`ParseObject` is the whole parse surface; `ParseArray`, `ParseTypedArray` and `ParseOne` were removed in v0.12.0. It returns `RawInstance` values ready for `instance.Validator.Validate()`.

### Writing

```go
// Marshal snapshot to JSON bytes
data, err := adapter.MarshalObject(ctx, snapshot,
    jsonAdapter.WithIndent("  "),
)

// Write snapshot to io.Writer
n, err := adapter.WriteObject(ctx, writer, snapshot)
```

---

## CSV Adapter

```go
import csvAdapter "github.com/simon-lentz/yammm/adapter/csv"
```

### Constructor

```go
adapter := csvAdapter.New(
    csvAdapter.WithTypeColumn("$type"), // column for type discrimination (multi-type CSV)
)
```

The delimiter is `,` unless `csvAdapter.WithDelimiter` sets another (`'\t'` for TSV), the first row is always the header, and list elements, vector elements and edge-column segments join on `|` unless `csvAdapter.WithListSeparator` sets another separator, which the parse side splits on too.

### Parsing

```go
// Parse CSV where all rows are the same type
raws, result := adapter.ParseTyped(ctx, sourceID, "User", reader, schemaType)

// Parse CSV with a type column (multi-type)
parsed, result := adapter.ParseWithTypeColumn(ctx, sourceID, reader, typeResolver)

```

Type coercion: CSV values are strings. The adapter coerces them to the schema's expected types (integers, floats, booleans, lists and vectors); a number cell must be a JSON number literal, and an Integer cell an integer one. Dates and timestamps are checked against their layout and kept as text, as the JSON adapter keeps them. A cell that does not coerce, or in a relation group each target's segment that does not, draws `E_CSV_COERCE` and is an `instance.Refused` holding that text, which the validator reports nothing more for: one bad value is one issue. A fault in the file's structure — a header the parser cannot use, a record the reader refuses, an edge group whose columns disagree, a plain column and a dotted group that write one key — produces `E_ADAPTER_PARSE`, the code the JSON adapter reports a malformed document under; a setting the adapter cannot use produces `E_CSV_CONFIG`, before any byte is read; and a failure the reader returns produces a Fatal `E_ADAPTER_IO`, on which the CLI exits 3. The failure is reported also when the parse ends for another reason at the read that returned it, as at a header the parser refuses.

Column names resolve as the validator resolves JSON keys: exact first, then case-insensitively (ASCII), with each key kept as the header spells it; pass `csvAdapter.WithStrictPropertyNames(true)` when the validator is strict (`instance.RecommendedOptions()` is). A name the schema does not declare reaches the validator in each row whose cell is not empty, which reports it unless `instance.WithAllowUnknownFields` is set. A header that repeats a name or leaves a column unnamed is refused. A malformed record is reported and skipped; a failure the reader returns stops the parse with a Fatal diagnostic, and the instances read before it are returned.

BOM stripping: UTF-8 BOM bytes at the start of input are automatically stripped.

### Writing

```go
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
- `Community`: UNIQUE constraints only; the NOT NULL and PROPERTY_TYPE constraints it cannot hold are reported once per call as `W_NEO4J_EDITION_CONSTRAINT_OMITTED` with a count per kind

### Constraint Generation

```go
// Generate constraint Cypher statements
statements, result := adapter.ConstraintsForSchema(ctx, s)
// statements is []string of CREATE CONSTRAINT IF NOT EXISTS ...

// Structured constraint objects
constraints, result := adapter.ConstraintsStructured(ctx, s)
// Each Constraint has: Name, Kind, Label, Properties, TypeExpr, Statement
```

Generation is **all-or-nothing**: any validation error returns `(nil, result)`, so one unemittable property withholds every constraint of the schema rather than emitting a partial script that looks complete. Beside a name Neo4j refuses as an identifier (Cypher reserved words, below) and two labels that sanitize alike, the one shape a *valid* schema can hit is a list whose element is itself a collection (`List<List<T>>`, `List<Vector[N]>`, or a list of a list-typed alias) — legal yammm, but Neo4j has no nested collection property type, so `ConstraintsForSchema` and `ConstraintsStructured` report `E_NEO4J_UNSUPPORTED_TYPE` under every option, `WithScalarTypeConstraints(false)` included. `IndexesForSchema` and `ShapeForSchema` do not make that check. The write path does: `BatchNodeQueries`, `Coerce` and `CoerceParams` refuse a value of such a property with an error wrapping `ErrUnsupportedListElem`, an empty list included, and read a value that holds none as nil, which `BatchNodeQueries` writes as null. `CoerceParams` refuses a nested param map (a `map[string]any`) or a rows value under such a key as it does under any constraint: a constraint at a nested map's or a rows param's own key is the caller's error, which wraps no class. `yammm export --to cypher` exits 1 over data holding one. The property is judged on each concrete type and part type with a valid label that declares or inherits it, so one declared on an abstract type that no such type extends is not reported. Model the inner collection as a `part type` reached by a composition. A bare `Vector` is fine; it maps to `LIST<FLOAT NOT NULL>`.

### Index Generation

```go
// Generate index Cypher from @index / @@index / @vector / @fulltext / @@fulltext annotations
statements, result := adapter.IndexesForSchema(ctx, s)
// statements is []string of CREATE [VECTOR|FULLTEXT] INDEX ... IF NOT EXISTS ...

// Structured index objects
indexes, result := adapter.IndexesStructured(ctx, s)
// Each Index has: Name, Kind (IndexRange|IndexVector|IndexFulltext), Label, Properties,
// VectorDimensions, VectorSimilarity, Statement
```

Property-level `@index` emits a single-property range index; type-level `@@index(a, b)` a composite range index (declared order significant); property-level `@vector(cosine|euclidean)` a vector index (dimension from the `Vector[N]` constraint); property-level `@fulltext` and type-level `@@fulltext(a, b)` fulltext indexes (`CREATE FULLTEXT INDEX ... ON EACH [...]`). Index names are always emitted; two indexes whose readable names would collide are disambiguated with a short deterministic digest. An index annotation naming a property the type does not have reports `E_NEO4J_UNKNOWN_PROPERTY`, and one naming a property whose type cannot carry the index reports `E_NEO4J_INVALID_INDEX_TARGET`. Indexes emit for every edition (unlike constraints); the `CREATE VECTOR INDEX ... OPTIONS` form requires Neo4j 5.15+.

`DiffIndexes` returns an `*IndexDiffResult` with **five** sets: Match/Drift/Create/Drop plus **Unverified** — indexes present on both sides whose definition could not be compared (no readable vector configuration, or still `POPULATING`). `DiffConstraints` has the same five sets, its `Unverified` covering a TYPE constraint whose remote record carries no `propertyType`, so its enforced type was not compared. A drift gate must count `Unverified` as an incomplete check, not a pass; summing only Drift+Create+Drop reports an unchecked index as in sync. Composite property order is significant, and a schema-owned remote index with no declaration reports as a drop when the schema could have declared it, and is counted in `Excluded` otherwise. See [Diffing Against a Live Database](#diffing-against-a-live-database) for the call shape.

### Graph Shape Introspection

```go
shape, result := adapter.ShapeForSchema(ctx, s)
// shape.Types maps type names to NodeShape (Label, PrimaryKeys, RequiredFields)
```

### Query Generation

```go
// Batch node queries for an entire snapshot
nodeQueries, err := adapter.BatchNodeQueries(ctx, snapshot, shape,
    neo4jAdapter.WithNodeChunkSize(5000), // max nodes per UNWIND batch
)

// Batch edge queries
edgeQueries, err := adapter.BatchEdgeQueries(ctx, snapshot, shape,
    neo4jAdapter.WithEdgeChunkSize(5000),
)
```

Each `BatchNodeQuery` / `BatchEdgeQuery` contains a Cypher statement and parameters map ready for driver execution.

A type's write-once properties come from its `@writeOnce` annotations (`ImmutableKeysFor(t)` returns them, own and inherited); the derived set per type drives the ON CREATE / ON MATCH split, selected per type in a batch. Node merges only — relationship merges have no ON CREATE / ON MATCH split.

### Parameter Coercion (Direct-Cypher Path)

Values passing through `BatchNodeQueries` are read in their one form (`instance.ScalarOf`) and then coerced to driver-native types automatically, save a value that holds one at a list whose element is a collection, which is refused by its type before its form is read. Consumers hand-building Cypher parameters use the coercion step directly; it does not read the form, so it judges a pointer or a named type as the value it is, not as the scalar it holds. A value that holds none, such as a nil pointer, slice or map, it returns as nil, which the driver writes as null:

```go
v, err := neo4jAdapter.Coerce(constraint, raw)   // one value against one constraint
types := neo4jAdapter.ParamTypesForType(t, "")   // property-name -> constraint map for a type
params, err := neo4jAdapter.CoerceParams(params, types)  // whole parameter map
relProps, err := neo4jAdapter.CoerceRelProps(props, rel)  // one edge's property map, returned as a copy
```

This repairs JSON round-trip artifacts (whole-number floats decoded as `int64`, `Date`/`Timestamp` strings) so values satisfy Neo4j `IS ::` type constraints; a float at an Integer is an error, whole or not. `CoerceRelProps` reaches the `rel_props` map inside a `$rows` row of a hand-built relationship MERGE, one nesting level below what `CoerceParams` walks. `CoerceParams` returns the rows of a `[]map[string]any` param as a `[]any`, so a nil row reaches the driver as null, and walks a `[]any` of maps and nils, at least one a map, as rows too; a row's fields are typed at `rows.<field>`, and a non-nil constraint at the rows key itself is an error. A `time.Time` is sent in a location the driver can encode: `time.Local` and any zone name the host cannot resolve become an offset; a resolvable IANA name is kept when its offset agrees with the value's, and becomes an offset otherwise.

### Label Management

```go
label := adapter.Label(ctx, "inventory", "Product")  // "inventory__Product"

// Detect label collisions (two types producing the same Neo4j label)
result := adapter.DetectLabelCollisions(ctx, s)
```

**Cypher reserved words:** the DSL reserves none as one; of the six spellings
its grammar refuses as a property name, four are Cypher keywords too (`as`,
`in`, `true` and `false`). A property named
`match` is valid yammm (and fine for JSON/CSV export) but is rejected with
`ErrReservedKeyword` wherever a generated constraint or index statement
writes it, because property names appear unquoted in generated Cypher; the
check is case-insensitive. Under `WithEdition(Community)`,
`WithScalarTypeConstraints(false)` or `WithRequiredOnlyTypeConstraints(true)`
fewer statements write a name, so fewer are checked, and `ShapeForSchema`
checks labels only. Namespaced labels usually absorb reserved type names
(`inventory__MATCH` is fine). Check early with
`ValidateIdentifier(name, context)` or by running `ConstraintsForSchema`
before write time.

### Diffing Against a Live Database

Both diffs are scoped by an `*OwnedLabels` — the labels the schema owns, a
superset of those the adapter emits for it. Build it once and pass it to both halves. The trailing
variadic argument is the **other** side's remote objects: index and constraint
names share one Neo4j namespace, and every emitted statement carries
`IF NOT EXISTS`, so a declaration whose name the database already holds is a
silent no-op rather than a create. Passing both sides lets each diff report that
as drift naming the holder.

```go
owned := adapter.OwnedLabels(ctx, s)

cDiff := adapter.DiffConstraints(desiredConstraints, actualConstraints, owned, actualIndexes...)
iDiff := adapter.DiffIndexes(desiredIndexes, actualIndexes, owned, actualConstraints...)

// Both results carry: Match, Drift, Create, Drop, Unverified, Excluded
inSync := len(cDiff.Drift) == 0 && len(cDiff.Create) == 0 && len(cDiff.Drop) == 0 &&
    len(cDiff.Unverified) == 0 // omitting Unverified reports an unchecked object as verified
```

Ownership is set membership, not a prefix test on the remote object's label:
`Label` composes a label from a caller-configurable prefix and separator around
two sanitized free-form names and is not invertible, so any rule that tries to
read a schema back out of a label can be satisfied by a sibling schema's labels.

`Excluded` counts the remote objects the comparison neither paired nor
dropped — the schema owns no label they carry, or they are of a kind this
configuration cannot declare: on the constraint side a relationship
constraint, a node constraint kind the DSL cannot express, and under
`WithEdition(Community)` every kind but UNIQUE; on the index side every row
`RemoteIndex.Declarable` refuses. A counted object that holds the name of a
declaration nothing realises is also the `Actual` of that declaration's drift. A
constraint's backing index that serves a declared index is that declaration's
`Actual` and is not counted. The count is not drift; in a
shared database a non-zero count is normal. It exists so "0 to drop" is not read
as "the database is accounted for": ownership is derived from the schema in hand,
so objects left behind by a type since deleted or renamed sit on a label no
current type declares.

### Schema Inference (from Live Database)

```go
source := adapter.InferSchema(constraints, relationships, "inventory")
// Returns .yammm source code inferred from Neo4j metadata; there is no error
```

---

## gogen Adapter (Go Code Generation)

```go
import "github.com/simon-lentz/yammm/adapter/gogen"
```

Schema-in, bytes-out: `gogen.Marshal` maps a loaded, resolved schema to formatted, type-checked Go source — one struct per type, named Enum/DataType types, a generated `Date` type and one type per custom `Timestamp` layout (each embedding `time.Time` with a JSON codec in the stored string form), `EDGE_` association structs, a Graph aggregate keyed as the JSON adapter keys its output, and the embedded schema source, reachable through `SerializedSources()` / `SerializedEntry`, which re-loads hermetically (under `schema.WithSyntheticRoot`, no filesystem participation). Generated output is stdlib-only (imports at most `time` and `encoding/json`) and byte-reproducible across checkouts. Unlike the data adapters it has no instance-data path and returns a plain `error` rather than a `diag.Result`.

Schemas with imports are flattened into one self-contained package. A name unique in the closure stays bare; a name two entities claim, or a reserved name such as `Graph` claims, gives every claimant its exact spelling, which names its kind and its identity (for a type, its schema and name): two schemas' `Region` become `Type_geo__Region` / `Type_common__Region`, and a type and a datatype `Region` of one schema become `Type_geo__Region` / `DataType_geo__Region`. No name depends on declaration order, and adding a declaration never hands a name to another entity.

Full API semantics: the gogen section of `docs/API.md`. CLI form: `yammm gen --to go` (see `cli.md`).

---

## jschema Adapter (JSON Schema Generation)

```go
import "github.com/simon-lentz/yammm/adapter/jschema"
```

Schema-in, bytes-out: `jschema.Marshal` maps a loaded, resolved schema to a JSON Schema **draft 2020-12** document describing the instance-data JSON object form `yammm check` reads (it does not reproduce yammm's validation; `adapter/jschema`'s Fidelity Caveats list where the two differ) — one top-level key per type a data file can hold (entry types bare, directly imported types alias-qualified as `common.Region`), each an array of instances; `EDGE_` defs carrying required `_target_<pk>` foreign-key fields; compositions always arrays (`minItems: 1` when required, `maxItems: 1` for to-one); named DataTypes as `$ref`ed `$defs` entries; schema doc-comments flowing through as `description` for editor hover. Association presence is deliberately NOT `required` per-file (yammm defers it to graph assembly). Output is deterministic and self-checked (valid JSON, every `$ref` resolves) before return. Options: `WithSchemaID` (the `"$id"`, omitted when unset). Plain `error`, no instance-data path, no source-backing requirement.

Wire the generated document into an editor for completion and validation while authoring data files. For a JSON data file, use a `json.schemas` mapping in the editor's settings. A JSON data file cannot carry a `"$schema"` member: the envelope admits no member but a type name, and `adapter/json` refuses the key as a type tag (`E_INVALID_TYPE_TAG`). A `# yaml-language-server: $schema=./fleet.schema.json` comment wires a YAML file, but yammm reads no YAML data file — its data commands read JSON, JSONC and CSV — so that file gets the editor's checks alone.

Full API semantics: the JSON Schema Generation section of `docs/API.md`. CLI form: `yammm gen --to jsonschema` (see `cli.md`).

---

## markdown Adapter (Markdown + Mermaid Documentation Generation)

```go
import "github.com/simon-lentz/yammm/adapter/markdown"
```

Schema-in, bytes-out: `markdown.Marshal` maps a loaded, resolved schema to one self-contained Markdown reference document covering the whole import closure — a Mermaid class diagram (each type's own members as `name KindLabel` pairs, `<<Abstract>>`/`<<Part>>` stereotypes, DSL-labeled relation edges, `Parent <|-- Child` inheritance edges), per-type sections in declaration order (flattened property tables with `from <Owner>` inherited-row markers, DSL-form constraint rendering like `String[1, 100]`, relation bullets with linked targets and edge-property sub-tables, invariant source fences extracted from the schema source), and Name | Definition | Description data-type tables. Imported schemas get their own `## Schema <Name> (imported as <alias>)` sections, a transitive import, which has no alias, a plain `## Schema <Name>`. A type is named as the entry schema names it: bare when the entry declares it, `alias.TypeName` when the entry imports it directly, and `TypeName (schemaName)` when the entry reaches it only through another import. Headings take GitHub's anchors (`-1`, `-2` on a repeated slug), allocated over every heading element in the document, doc-comment headings and raw HTML `<h1>`–`<h6>` included, as GitHub anchors them, so no schema is refused for its names and every link lands on its heading. Schema text renders literally under GitHub-flavored Markdown: an empty HTML comment splits each colon, `@` and dot after `www` in text escaped for Markdown and in a `<code>` cell, since GitHub autolinks through escapes. Output is deterministic, is a versioned surface (`docs/API.md`'s Versioning Policy), and is self-checked before return by reading it with that parser (`github.com/yuin/goldmark`): every heading and table the generator wrote must read as written, outside doc comments the links read must be exactly the internal links it wrote, and every class-diagram line must be a form the emitter writes, a subset of Mermaid's class-diagram grammar (class ids are ASCII and never hold `direction`; a class label writes every character but an ASCII letter or digit, a dot, and an underscore between two ASCII letters or digits as an entity code, and an edge label writes each character Mermaid reads as syntax as one, so `one:many` labels an edge `one#58;many`); doc-comment text is the author's Markdown and is not checked, in a description cell too, and a code fence or HTML block it leaves open is closed at the end of its block. Options: `WithClassDiagram(false)` omits the diagram, and `WithClassMembers(false)` drops the member lines inside each class. Plain `error`, no instance-data path, no source-backing requirement (on Builder-built schemas an invariant is its message line and its doc comment).

Full API semantics: the Markdown Documentation Generation section of `docs/API.md`. CLI form: `yammm gen --to md` (see `cli.md`).

---

## Adapter Refusal Classes

A write reports through an `error`, not a `diag.Result`, so the adapters carry sentinel classes a caller matches with `errors.Is` instead of matching message text.

| Sentinel | Adapter | Meaning |
| -------- | ------- | ------- |
| `csv.ErrUnrepresentable` | CSV | The snapshot holds a value CSV cannot write so that its own parser reads it back unchanged: a composed child, a lone all-empty association, a cell holding a CR LF, a list of one empty element, a null list element, a value no column renders (a value of no form, such as a map, an array, a struct, a channel or a pointer to one, or a `json.Number` whose text is no number literal, an empty one among them; or a time or a UUID its column does not render), a string that is not valid UTF-8, a number its parser does not read back as a number (a `NaN`, an infinity, a `json.Number` no finite `float64` holds) |
| `csv.ErrConfig` | CSV | The adapter holds a setting it cannot use: a list separator the parser could not find again, or a delimiter `encoding/csv` refuses. Refused before any writer is requested. The parse-side twin is `E_CSV_CONFIG` |
| `csv.ErrNilSnapshot`, `json.ErrNilResult` | CSV, JSON | A write method received a nil snapshot |
| `neo4j.ErrUnrepresentable` | Neo4j | `BatchNodeQueries`, `BatchEdgeQueries`, `Coerce`, `CoerceParams` or `CoerceRelProps` refuse a value Neo4j would not store as held: one its constraint's coercion refuses, a nested collection's (matching `ErrUnsupportedListElem` too), a map, a string that is not valid UTF-8, a value of no form, a time or a UUID its constraint does not render, a `json.Number` no `int64` or finite `float64` holds, or a primary key that is absent or holds no value; a property the schema does not declare is judged by its one form alone. No other error carries the class: a caller's own, such as a constraint `schema.CheckConstraint` refuses, a constraint at a nested map's or a rows param's own key, a `GraphShape` the adapter did not build for the snapshot's schema or a relationship type that is no valid identifier, and a cancelled context |
| `json.ErrUnrepresentable` | JSON | The snapshot holds a value JSON cannot write as itself: a non-finite float (NaN or an infinity) at any depth, which a validated `Float` never is; a string that is not valid UTF-8 at any depth; a `json.Number` no finite `float64` holds; a `time.Time` outside a `Timestamp` or a `Date`, a `uuid.UUID` outside a `UUID`, or a time at its kind whose text does not read back, such as one outside years 0 to 9999; or a value that is no scalar, list or string-keyed map (`instance.ScalarOf`), a `json.Number` whose text is no number literal among them |

An I/O failure and a cancellation match none of these classes.

## Adapter Error Codes

| Code | Adapter | Meaning |
| ---- | ------- | ------- |
| `E_ADAPTER_PARSE` | All | Format-specific parsing error |
| `E_ADAPTER_IO` | CSV | The reader a streamed parse reads from failed; Fatal, and the CLI exits 3 as for any I/O failure. Module-wide by name, as `E_ADAPTER_PARSE` is, but the CSV adapter is the one parser that streams: the JSON adapter takes bytes |
| `E_CSV_COERCE` | CSV | A cell's text does not coerce to the type its member declares |
| `E_CSV_CONFIG` | CSV | The adapter holds a setting this parse cannot use; no record is read |
| `E_NEO4J_LABEL_COLLISION` | Neo4j | Two types produce the same Neo4j label |
| `E_NEO4J_INVALID_IDENTIFIER` | Neo4j | Name not valid as Neo4j identifier |
| `E_NEO4J_UNSUPPORTED_TYPE` | Neo4j | Constraint kind has no Neo4j type mapping |
