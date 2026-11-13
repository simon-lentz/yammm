# Adapter and Generator Reference

One platform adapter is provided, Neo4j, and this reference also covers the two generators (gogen, markdown).

The adapter lives in `adapter/neo4j` and the generators in `schema/{gogen,markdown}`. The library never imports either -- each imports the library. The adapter writes a validated snapshot as Neo4j queries; the generators are schema-in/bytes-out and never touch instance data.

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

Generation is **all-or-nothing**: any validation error returns `(nil, result)`, so one unemittable property withholds every constraint of the schema rather than emitting a partial script that looks complete. Beside a name Neo4j refuses as an identifier (Cypher reserved words, below) and two types that compose one label (`E_NEO4J_LABEL_COLLISION`), the one shape a *valid* schema can hit is a list whose element resolves to a collection, a List or a Vector, the list and its element each written directly or through an alias — legal yammm, but Neo4j has no nested collection property type, so `ConstraintsForSchema` and `ConstraintsStructured` report `E_NEO4J_UNSUPPORTED_TYPE` whatever the constraint options set, `WithScalarTypeConstraints(false)` included. `IndexesForSchema` and `ShapeForSchema` do not make that check. The write path does: `BatchNodeQueries` and `CoerceParams` refuse a value of such a property with an error wrapping `ErrUnsupportedListElem`, an empty list included, and read a value that holds none as nil, which `BatchNodeQueries` writes as null. The property is judged on each concrete type and part type with a valid label that declares or inherits it, so one declared on an abstract type that no such type extends is not reported. Model the inner collection as a `part type` reached by a composition. A `Vector[N]` property is fine; it maps to `LIST<FLOAT NOT NULL>`.

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

Property-level `@index` emits a single-property range index; type-level `@@index(a, b)` a composite range index (declared order significant); property-level `@vector(cosine|euclidean)` a vector index (dimension from the `Vector[N]` constraint); property-level `@fulltext` and type-level `@@fulltext(a, b)` fulltext indexes (`CREATE FULLTEXT INDEX ... ON EACH [...]`). Index names are always emitted; two indexes whose readable names would collide are disambiguated with a short deterministic digest. A load refuses an index annotation with no eligible target (`E_UNKNOWN_ANNOTATION_TARGET`, `E_INVALID_ANNOTATION_TARGET`), so no schema it returns draws the adapter's own codes for the two cases, `E_NEO4J_UNKNOWN_PROPERTY` and `E_NEO4J_INVALID_INDEX_TARGET`. Indexes emit for every edition (unlike constraints); the `CREATE VECTOR INDEX ... OPTIONS` form requires Neo4j 5.15+. Apply every constraint statement before every index statement: a range index over a primary key's properties in their order is the index a UNIQUE or NODE KEY constraint creates, so applied first it makes the server refuse the constraint with `IndexAlreadyExists`, and applied after it creates nothing.

`DiffIndexes` returns an `*IndexDiffResult` with **five** sets: Match/Drift/Create/Drop plus **Unverified** — indexes present on both sides whose definition could not be compared (no readable vector configuration, or still `POPULATING`). `DiffConstraints` has the same five sets, its `Unverified` covering a TYPE constraint whose remote record carries no `propertyType`, so its enforced type was not compared. A drift gate must count `Unverified` as an incomplete check, not a pass; summing only Drift+Create+Drop reports an unchecked index as in sync. Composite property order is significant, and a schema-owned remote index with no declaration reports as a drop when the schema could have declared it, and is counted in `Excluded` otherwise. See [Diffing Against a Live Database](#diffing-against-a-live-database) for the call shape.

### Graph Shape

```go
shape, result := adapter.ShapeForSchema(ctx, s)
// shape holds each non-abstract type's label, primary keys and @writeOnce keys, exports no field,
// and is the input to BatchNodeQueries and BatchEdgeQueries
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

Values passing through `BatchNodeQueries` are coerced to driver-native types automatically, save two the validator takes: a value that holds one at a list whose element is a collection, which is refused by its type first, and a `Timestamp` at a UTC offset past 18 hours. Consumers hand-building Cypher parameters call `CoerceParams`. It reads each value at a declared key as the validator reads a property value, and sends it as the write path sends the value the validator stores. A value the validator refuses is an error, a pointer included, save a `dbtype.Date` at a `Date`, which passes. A value that holds none (`instance.IsAbsent`), nil, a nil slice, a nil map or a nil `immutable` container, it returns as nil, which the driver writes as null:

```go
types := neo4jAdapter.ParamTypesForType(t)   // property-name -> constraint map for a type
params, err := neo4jAdapter.CoerceParams(params, types)  // one flat parameter map
```

The coercion turns an integer a caller holds at a `Float` into a `float64`, and a `Date` or `Timestamp` string into a driver temporal value, so values satisfy Neo4j `IS ::` type constraints; a float at an Integer is an error, whole or not, and so is a value outside a bound, an `Enum` or a `Pattern`. `CoerceParams` reads one flat map and walks no value: a map or a list of maps at a declared key is judged as the value it is, and one that is not nil passes as given under an undeclared key. A nil map or a nil list is returned as nil at every key. To coerce the fields of a row of a hand-built `$rows` param, or of a `rel_props` map inside one, call `CoerceParams` on that map. A `time.Time` goes through the text the validator stores it as. At a declared key every instant is sent as an offset and none under a zone name, a location's included: a `Z` text goes as offset zero, which is the form Cypher's `datetime()` gives the same text. The offset is the one the text carries. An instant at an offset past 18 hours is refused with an error matching `ErrUnrepresentable`, on the write path too: the server holds no wider offset. A `dbtype.Date` passes at a `Date`.

### Label Management

```go
label := adapter.Label(ctx, "inventory", "Product")  // "inventory__Product"

// Detect label collisions (two types producing the same Neo4j label)
result := adapter.DetectLabelCollisions(ctx, s)
```

**Cypher reserved words:** the DSL reserves none as one; of the six spellings
its grammar refuses as a property name, four are Cypher keywords too (`as`,
`in`, `true` and `false`). A property named
`match` is valid yammm (and fine in a `.ys` snapshot) but is rejected with
`E_NEO4J_INVALID_IDENTIFIER`, whose detail begins with `ErrReservedKeyword`'s text,
wherever a generated constraint or index statement writes it, because property names appear unquoted in generated Cypher; the
check is case-insensitive. Under `WithEdition(Community)`,
`WithScalarTypeConstraints(false)` or `WithRequiredOnlyTypeConstraints(true)`
fewer statements write a name, so fewer are checked, and `ShapeForSchema`
checks labels only. Namespaced labels usually absorb reserved type names
(`inventory__MATCH` is fine). Check early with
`ValidateIdentifier(name, context)` or by running `ConstraintsForSchema` and
`IndexesForSchema` before write time.

### Diffing Against a Live Database

Both diffs are scoped by an `*OwnedLabels` — the labels the schema owns, a
superset of those the adapter emits for it. Build it once and pass it to both halves. The trailing
variadic argument is the **other** side's remote objects: index and constraint
names share one Neo4j namespace, and every emitted statement carries
`IF NOT EXISTS`, so a declaration whose name the database already holds creates
nothing. Over a name of its own kind the statement is a silent no-op, and a
constraint's backing index counts as an index. Over the other kind's name the server
refuses it. Passing both sides lets each diff report that
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
a schema name and a type name and is not invertible, so any rule that tries to
read a schema back out of a label can be satisfied by a sibling schema's labels.

`Excluded` counts the remote objects the comparison set aside because the
schema owns no label they carry, or they are of a kind this
configuration cannot declare: on the constraint side a relationship
constraint, a node constraint kind the DSL cannot express, and under
`WithEdition(Community)` every kind but UNIQUE; on the index side every row
`RemoteIndex.Declarable` refuses. A counted object that holds the name of a
declaration nothing realises is also the `Actual` of that declaration's drift, as
is a UNIQUE or NODE KEY over the key a declaration of the other kind needs: a
NODE KEY under `WithEdition(Community)`, or one on a declared label the owned
set leaves out. A
constraint's backing index that serves a declared index is that declaration's
`Actual` and is not counted. The count is not drift; in a
shared database a non-zero count is normal. It exists so "0 to drop" is not read
as "the database is accounted for": ownership is derived from the schema in hand,
so objects left behind by a type since deleted or renamed sit on a label no
current type declares.

---

## gogen Generator (Go Code Generation)

```go
import "github.com/simon-lentz/yammm/schema/gogen"
```

Schema-in, bytes-out: `gogen.Marshal` maps a loaded, resolved schema to formatted, type-checked Go source — one struct per type, named Enum/DataType types, a generated `Date` type when a position emits `Date` (it embeds `time.Time` with a JSON codec in the stored string form), `EDGE_` association structs, a Graph aggregate keyed as an instance document keys a type's array, and the embedded schema source, reachable through `SerializedSources()` / `SerializedEntry`, which re-loads hermetically (under `schema.WithSyntheticRoot`, no filesystem participation). Generated output is stdlib-only (imports at most `time` and `encoding/json`) and byte-reproducible across checkouts. Unlike `adapter/neo4j` it has no instance-data path, and it returns a plain `error` where the Neo4j adapter's schema-derived DDL and shape builders return a `diag.Result`.

Schemas with imports are flattened into one self-contained package. A name unique in the closure stays bare; a name two entities claim, or a reserved name such as `Graph` claims, gives every claimant its exact spelling, which names its kind and its identity (for a type, its schema and name): two schemas' `Region` become `Type_geo__Region` / `Type_common__Region`, and a datatype `Uri` and a type `URI` of one schema, which the initialisms map to one Go name, become `DataType_geo__Uri` / `Type_geo__URI`. No name depends on declaration order, and adding a declaration never hands a name to another entity.

Full API semantics: the gogen section of `docs/API.md`. CLI form: `yammm gen --to go` (see `cli.md`).

---

## markdown Generator (Markdown + Mermaid Documentation Generation)

```go
import "github.com/simon-lentz/yammm/schema/markdown"
```

Schema-in, bytes-out: `markdown.Marshal` maps a loaded, resolved schema to one self-contained Markdown reference document covering the whole import closure — a Mermaid class diagram (each type's own members as `name KindLabel` pairs, `<<Abstract>>`/`<<Part>>` stereotypes, DSL-labeled relation edges, `Parent <|-- Child` inheritance edges), per-type sections in declaration order (flattened property tables with `from <Owner>` inherited-row markers, DSL-form constraint rendering like `String[1, 100]`, relation bullets with linked targets and edge-property sub-tables, invariant source fences extracted from the schema source), and Name | Definition | Description data-type tables. Imported schemas get their own `## Schema <Name> (imported as <alias>)` sections, a transitive import, which has no alias, a plain `## Schema <Name>`. A type is named as the entry schema names it: bare when the entry declares it, `alias.TypeName` when the entry imports it directly, and `TypeName (schemaName)` when the entry reaches it only through another import. Headings take GitHub's anchors (`-1`, `-2` on a repeated slug; a slug keeps U+200C and U+200D), allocated over every heading element in the document that is not inside another or lost under a `<select>`, doc-comment headings and raw HTML `<h1>`–`<h6>` included, as GitHub anchors them, so no schema is refused for its names. A link lands on its heading wherever `golang.org/x/net/html`, the HTML parser the generator allocates anchors with, reads a doc comment's HTML as GitHub does, save a link to a heading that doc comment's HTML takes, which leads nowhere. Schema text renders literally under GitHub-flavored Markdown: an empty HTML comment splits each colon, each `@` and each dot that directly follows `www`, in any case, in text escaped for Markdown and in a `<code>` cell, since GitHub autolinks through escapes, and each dollar in text escaped for Markdown is written inside a `<span>`, since GitHub reads a pair of dollars outside code as math; a `<code>` cell keeps its dollars, since GitHub reads no math inside a `<code>` element. Output is byte-identical for the same schema, options, yammm version and build (the Go release that builds the generator and the module versions the build selects), is a versioned surface (`docs/API.md`'s Versioning Policy), and is self-checked before return: the generator's own text, with each doc comment replaced, is read with a CommonMark parser (`github.com/yuin/goldmark`), every heading and table the generator wrote must read as written, the links read must be exactly the internal links it wrote, with no raw HTML but the tags it writes, and every class-diagram line must be a form the emitter writes, a subset of Mermaid's class-diagram grammar (class ids are ASCII and never hold `direction`; a class label writes every character but an ASCII letter or digit, a dot, and an underscore between two ASCII letters or digits as an entity code, and an edge label writes each character Mermaid reads as syntax as one, so `one:many` labels an edge `one#58;many`); doc-comment text is the author's Markdown and the check reads none of it but whether a description cell holds a line break, which the fold never writes, so no doc comment makes `Marshal` refuse a schema; a description cell holds it as inline Markdown on one line and takes no closer, and a code fence or HTML block that a comment written as a block leaves open is closed at the end of that block. Options: `WithClassDiagram(false)` omits the diagram, and `WithClassMembers(false)` drops the member lines inside each class. Plain `error`, no instance-data path.

Full API semantics: the Markdown Documentation Generation section of `docs/API.md`. CLI form: `yammm gen --to md` (see `cli.md`).

---

## Adapter Refusal Classes

A write reports through an `error`, not a `diag.Result`, so the adapter carries sentinel classes a caller matches with `errors.Is` instead of matching message text.

| Sentinel | Adapter | Meaning |
| -------- | ------- | ------- |
| `neo4j.ErrUnrepresentable` | Neo4j | `BatchNodeQueries` and `BatchEdgeQueries` refuse a value Neo4j would not store as held: one its constraint's coercion refuses, a nested collection's (matching `ErrUnsupportedListElem` too), a map at a property the schema declares, a list or a map a document nests more than 256 levels deep, a `json.Number` that is an integer literal past `int64` at any position but a `Float` or a `Vector` element, or that no finite `float64` holds, or a primary key that is absent or holds no value; `CoerceParams` marks its refusals of a value so too. No other error carries the class: a caller's own, such as a constraint `schema.CheckConstraint` refuses, a `GraphShape` the adapter did not build for the snapshot's schema or a relationship type that is no valid identifier, and a cancelled context |

An I/O failure and a cancellation do not match it.

## Adapter Error Codes

| Code | Adapter | Meaning |
| ---- | ------- | ------- |
| `E_NEO4J_LABEL_COLLISION` | Neo4j | Two types produce the same Neo4j label |
| `E_NEO4J_INVALID_IDENTIFIER` | Neo4j | Name not valid as Neo4j identifier |
| `E_NEO4J_UNSUPPORTED_TYPE` | Neo4j | Constraint kind has no Neo4j type mapping |
