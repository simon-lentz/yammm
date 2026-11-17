# Neo4j and Generator Reference

This reference covers the Neo4j packages and the two other generators (gogen, markdown).

`schema/cypher` maps a schema to Neo4j labels and constraint and index DDL. `neo4j` writes a validated snapshot as Neo4j queries and diffs a database against a schema. The generators live in `schema/{cypher,gogen,markdown}`. The library never imports any of them -- each imports the library. The generators read a schema and never touch instance data: gogen and markdown return bytes, and `schema/cypher` returns its DDL as values.

---

## Neo4j

```go
import (
    "github.com/simon-lentz/yammm/neo4j"
    "github.com/simon-lentz/yammm/schema/cypher"
)
```

The Neo4j packages produce Cypher statements and parameters. They do **not** manage a database connection -- use a Neo4j driver to execute the generated queries.

### Configuration

```go
c := cypher.Config{
    Edition:         cypher.Enterprise,    // or cypher.Community
    LabelSeparator:  "__",                 // schema__TypeName; "" means "__"
    LabelPrefix:     "",                   // optional global prefix
    TypeConstraints: cypher.AllProperties, // or RequiredProperties, NoProperties
    NodeKeys:        false,                // NODE KEY instead of UNIQUE+NOT NULL
}
```

The zero `cypher.Config` is the default mapping. The DDL, the write path and the diff all take one `Config`, so a graph written under it is diffed under it too.

**Edition differences:**

- `Enterprise`: Supports UNIQUE, NOT NULL, property type, and NODE KEY constraints
- `Community`: UNIQUE constraints only; the NOT NULL and PROPERTY_TYPE constraints it cannot hold are reported once per call as `W_NEO4J_EDITION_CONSTRAINT_OMITTED` with a count per kind

### Constraint Generation

```go
constraints, result := c.Constraints(s)
// Each Constraint has: Name, Kind, Label, Properties, TypeExpr, Statement
// Statement is CREATE CONSTRAINT <name> IF NOT EXISTS ...
```

Every constraint carries a name. Generation is **all-or-nothing**: any validation error returns `(nil, result)`, so one unemittable property withholds every constraint of the schema rather than emitting a partial script that looks complete. Beside a name Neo4j refuses as an identifier (Cypher reserved words, below) and a label that breaks the label rule (Label Management, below), the one shape a *valid* schema can hit is a list whose element resolves to a collection, a List or a Vector, the list and its element each written directly or through an alias — legal yammm, but Neo4j has no nested collection property type, so `Constraints` reports `E_NEO4J_UNSUPPORTED_TYPE` whatever the `Config`'s other settings, `NoProperties` included, once its labels keep the label rule. `Indexes` does not make that check. The write path does: `neo4j.NodeQueries` and `neo4j.CoerceParams` refuse a value of such a property with an error wrapping `neo4j.ErrUnsupportedListElem`, an empty list included, and read a value that holds none as nil, which `NodeQueries` writes as null. The property is judged on each concrete type and part type that declares or inherits it, so one declared on an abstract type that no such type extends is not reported; a label that breaks the label rule withholds every such judgment, since `Constraints` then returns the label rule's issues alone. Model the inner collection as a `part type` reached by a composition. A `Vector[N]` property is fine; it maps to `LIST<FLOAT NOT NULL>`.

### Index Generation

```go
// Index Cypher from @index / @@index / @vector / @fulltext / @@fulltext annotations
indexes, result := c.Indexes(s)
// Each Index has: Name, Kind (IndexRange|IndexVector|IndexFulltext), Label, Properties,
// VectorDimensions, VectorSimilarity, Statement
```

Property-level `@index` emits a single-property range index; type-level `@@index(a, b)` a composite range index (declared order significant); property-level `@vector(cosine|euclidean)` a vector index (dimension from the `Vector[N]` constraint); property-level `@fulltext` and type-level `@@fulltext(a, b)` fulltext indexes (`CREATE FULLTEXT INDEX ... ON EACH [...]`). Index names are always emitted; two indexes whose readable names would collide are disambiguated with a short deterministic digest. A load refuses an index annotation with no eligible target (`E_UNKNOWN_ANNOTATION_TARGET`, `E_INVALID_ANNOTATION_TARGET`), so no schema it returns draws the generator's own codes for the two cases, `E_NEO4J_UNKNOWN_PROPERTY` and `E_NEO4J_INVALID_INDEX_TARGET`. Indexes emit for every edition (unlike constraints); every statement runs on the supported floor, Neo4j 5.26. A `@vector` names a `Vector` of 1 to 4,096 dimensions. Apply every constraint statement before every index statement: a range index over a primary key's properties in their order is the index a UNIQUE or NODE KEY constraint creates, so applied first it makes the server refuse the constraint with `IndexAlreadyExists`, and applied after it creates nothing.

`neo4j.Diff` returns a `Plan` whose `Indexes` has **five** sets: Match/Drift/Create/Drop plus **Unverified** — indexes present on both sides whose definition could not be compared (a vector dimension or similarity the database did not report, or still `POPULATING`). Its `Constraints` has the four sets Match/Drift/Create/Drop: `ParseConstraints` and `neo4j.Diff` refuse a TYPE record that reports no `propertyType`, so every constraint is compared whole. A drift gate must count `Unverified` as an incomplete check, not a pass; summing only Drift+Create+Drop reports an unchecked index as in sync. Composite property order is significant. A schema-owned remote index that the schema could have declared, that no declaration takes and that no drift names reports as a drop. See [Diffing Against a Live Database](#diffing-against-a-live-database) for the call shape.

### Query Generation

```go
// Batch node queries for an entire snapshot
nodeQueries, err := neo4j.NodeQueries(ctx, c, snapshot,
    neo4j.WithNodeChunkSize(5000), // max nodes per UNWIND batch
)

// Batch edge queries
edgeQueries, err := neo4j.EdgeQueries(ctx, c, snapshot,
    neo4j.WithEdgeChunkSize(5000),
)
```

Each `NodeQuery` / `EdgeQuery` contains a Cypher statement and parameters map ready for driver execution. Each call derives every non-abstract closure type's label, primary keys and `@writeOnce` keys from the snapshot's own schema under `c`'s label settings, refuses a closure whose labels break the label rule with an error wrapping `c.CheckLabels`' result, and refuses a zero snapshot with `ErrZeroSnapshot`.

A type's write-once properties come from its `@writeOnce` annotations, own and inherited; the derived set per type drives the ON CREATE / ON MATCH split, selected per type in a batch. Node merges only — relationship merges have no ON CREATE / ON MATCH split.

### Parameter Coercion (Direct-Cypher Path)

Values passing through `NodeQueries` are coerced to driver-native types automatically, save two the validator takes: a value that holds one at a list whose element is a collection, which is refused by its type first, and a `Timestamp` at a UTC offset past 18 hours. Consumers hand-building Cypher parameters call `CoerceParams`. It reads each value at a declared key as the validator reads a property value, and sends it as the write path sends the value the validator stores. A value the validator refuses is an error, a pointer included: a `Date` takes a `"2006-01-02"` string or a `time.Time`, and a `dbtype.Date` is refused. A value that holds none (`instance.IsAbsent`), nil, a nil slice, a nil map or a nil `immutable` container, it returns as nil, which the driver writes as null:

```go
types := neo4j.ParamTypesForType(t)   // property-name -> constraint map for a type
params, err := neo4j.CoerceParams(params, types)  // one flat parameter map
```

The coercion turns an integer a caller holds at a `Float` into a `float64`, and a `Date` or `Timestamp` string into a driver temporal value, so values satisfy Neo4j `IS ::` type constraints; a float at an Integer is an error, whole or not, and so is a value outside a bound, an `Enum` or a `Pattern`. `CoerceParams` reads one flat map and walks no value: a map or a list of maps at a declared key is judged as the value it is, and one that is not nil passes as given under an undeclared key. A nil map or a nil list is returned as nil at every key. To coerce the fields of a row of a hand-built `$rows` param, or of a `rel_props` map inside one, call `CoerceParams` on that map. A `time.Time` goes through the text the validator stores it as. At a declared key every instant is sent as an offset and none under a zone name, a location's included: a `Z` text goes as offset zero, which is the form Cypher's `datetime()` gives the same text. The offset is the one the text carries. An instant at an offset past 18 hours is refused with an error matching `ErrUnrepresentable`, on the write path too: the server holds no wider offset.

### Label Management

```go
label := c.Label(t)  // "inventory__Product" for type Product of schema inventory

// Every break of the label rule across the import closure
result := c.CheckLabels(s)
```

A label is `LabelPrefix` + schema name + `LabelSeparator` + type name, as written; nothing is sanitized. `CheckLabels` reports each break with `E_NEO4J_INVALID_IDENTIFIER`: a prefix that does not start an identifier or a separator that is not ASCII letters, digits and underscores (once per call), a schema name that holds the separator or runs into it from its end, as `"a__B"` and `"geo_"` do under `__` (once per schema, and its types' labels are then not judged), and a label that is a Cypher reserved keyword (once per type). A label that keeps the rule decodes to one (schema, type) pair, so two types never share one. Every generator and both write calls run it first.

**Cypher reserved words:** the DSL reserves none as one; of the six spellings
its grammar refuses as a property name, four are Cypher keywords too (`as`,
`in`, `true` and `false`). A property named
`match` is valid yammm (and fine in a `.ys` snapshot) but is rejected with
`E_NEO4J_INVALID_IDENTIFIER`, whose detail reads "the name is a Cypher reserved keyword",
wherever a generated constraint or index statement writes it, because property names appear unquoted in generated Cypher; the
check is case-insensitive. Under `Community`, `NoProperties` or
`RequiredProperties` fewer statements write a name, so fewer are checked, and
the write path checks labels and each edge's relationship type only. Namespaced
labels usually absorb reserved type names (`inventory__MATCH` is fine). Check
early with `cypher.IsIdentifier(name)` or by running
`Constraints` and `Indexes` before write time.

### Diffing Against a Live Database

`neo4j.Diff` returns one plan over both sides. It derives the desired
constraints, the desired indexes and the labels the schema owns from the
`Config` and the schema, so the three agree by construction. Index and
constraint names share one Neo4j namespace, every emitted statement carries
`IF NOT EXISTS`, and a constraint's drop also removes its backing index. A drop
that one side needs can therefore remove an object the other side matched, so
one fixed point settles both sides. Pass every row `ShowConstraints` and
`ShowIndexes` return, parsed with `ParseConstraints` and `ParseIndexes`. A plan
pairs, blocks and drops a row by its name, so `Diff` refuses, as
`E_NEO4J_INVALID_REMOTE_OBJECT` and with no plan, a row with no name, two rows
of one kind under one name, and a TYPE row with no `propertyType`; one read
returns none of them.

```go
plan, result := neo4j.Diff(c, s, actualConstraints, actualIndexes)
cDiff, iDiff := plan.Constraints, plan.Indexes

// Both halves carry Match, Drift, Create, Drop and Excluded; Indexes adds Unverified
inSync := len(cDiff.Drift)+len(cDiff.Create)+len(cDiff.Drop) == 0 &&
    len(iDiff.Drift)+len(iDiff.Create)+len(iDiff.Drop)+len(iDiff.Unverified) == 0 // omitting Unverified reports an unchecked index as verified
```

Each drift carries `Blockers`: every database object its create needs dropped,
as `Blockers.Constraints` and `Blockers.Indexes`, each drop target once. The
plan converges in one apply in this order:

1. Drop every drift's blockers on both sides, once for each drop target. Drop
   each `Drop` the caller applies in this step too.
2. Create every constraint in `Constraints.Create` and every constraint drift's
   `Desired`.
3. Create every index in `Indexes.Create` and every index drift's `Desired`.

Drop each of `Blockers.Constraints` with `cypher.DropConstraintStatement` and
each of `Blockers.Indexes` with `cypher.DropIndexStatement`. An index with an
`OwningConstraint` goes with that constraint's drop. Before the drops, compare each constraint
create with the data: UNIQUE needs no duplicate tuple, NOT NULL needs every
node to hold the property, TYPE needs every value of the type, and NODE KEY
needs both UNIQUE's and NOT NULL's. A refused create leaves the label without
the dropped object's guarantee.

A declared index that a constraint's backing index serves, and that pairing
left without a row of its own, is matched by it while it is online, `Unverified` while it
is `POPULATING`, and drifted while it is `FAILED`. When the plan drops the
constraint of an online or `POPULATING` one, as a blocker or as a `Drop`, the
index is a `Create` unless another blocker makes it a drift. Where the caller
leaves that `Drop` unapplied, the server takes the create as a no-op while the
constraint stands. A `FAILED` backing
index is dropped with its constraint; where a declaration realises that
constraint, it drifts too, and the apply creates both again.

Ownership is set membership (`c.OwnedLabels(s)`), not a prefix test on the
remote object's label: a remote object can carry a label the label rule
refuses, such as `app__legacy__Foo` written by another tool, and a prefix test
would claim it for schema `app`; the set holds only the labels the schema
composes.

`Excluded` counts the remote objects the plan leaves alone: no `Actual`, no
`Drop`, and no object whose drop target a drift on either side names or a
`Drop` removes. Each is on a label the
schema does not own, or of a kind this configuration cannot declare: on the
constraint side a relationship constraint, a node constraint kind the DSL
cannot express, and under the `Community` edition every kind but UNIQUE; on
the index side every row `RemoteIndex.Declarable` refuses. Every remote row is
in one place only. The count is not drift: in a shared database a non-zero
count is normal. It exists so "0 to drop" is not read as "the database is
accounted for": ownership is derived from the schema in hand, so objects left
behind by a type since deleted or renamed sit on a label no current type
declares.

---

## gogen Generator (Go Code Generation)

```go
import "github.com/simon-lentz/yammm/schema/gogen"
```

Schema-in, bytes-out: `gogen.Marshal` maps a loaded, resolved schema to formatted, type-checked Go source — one struct per type, named Enum/DataType types, a generated `Date` type when a position emits `Date` (it embeds `time.Time` with a JSON codec in the stored string form), `EDGE_` association structs, a Graph aggregate keyed as an instance document keys a type's array, the embedded schema source with its `SchemaHash`, reachable through `SerializedSources()` / `SerializedEntry`, which re-loads hermetically (under `schema.WithSyntheticRoot`, no filesystem participation). Generated output is stdlib-only (imports at most `time` and `encoding/json`) and byte-reproducible across checkouts for a generator one Go toolchain builds. Like `schema/cypher` and unlike `neo4j` it has no instance-data path, and it returns a plain `error` where `schema/cypher`'s `Constraints`, `Indexes` and `CheckLabels` return a `diag.Result`.

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

## Neo4j Refusal Classes

A write reports through an `error`, not a `diag.Result`, so `neo4j` carries sentinel classes a caller matches with `errors.Is` instead of matching message text.

| Sentinel | Meaning |
| -------- | ------- |
| `neo4j.ErrUnrepresentable` | `NodeQueries` and `EdgeQueries` refuse a value Neo4j would not store as held: one its constraint's coercion refuses, a nested collection's (matching `ErrUnsupportedListElem` too), a map at a property the schema declares, a list or a map a document nests more than 256 levels deep, a `json.Number` that is an integer literal past `int64` at any position but a `Float` or a `Vector` element, or that no finite `float64` holds; `CoerceParams` marks its refusals of a value so too. No other error carries the class: a caller's own, such as a constraint `schema.CheckConstraint` refuses, a zero snapshot, a closure whose labels break the label rule, a relationship type that is no valid identifier, and a cancelled context |

An I/O failure and a cancellation do not match it.

## Neo4j Error Codes

| Code | Meaning |
| ---- | ------- |
| `E_NEO4J_INVALID_IDENTIFIER` | A label setting, schema name or label that breaks the label rule, or a property name that is no Neo4j identifier |
| `E_NEO4J_UNSUPPORTED_TYPE` | Constraint kind has no Neo4j type mapping |
