// Package neo4j builds the writes of a yammm graph to Neo4j 5 and plans a
// database's constraints and indexes against a schema: parameterized write queries with
// their coerced parameters, the introspection queries and their parsers, and
// the constraint and index drift diff.
//
// # Architectural Position
//
// Package neo4j depends on library packages (schema, graph, immutable, diag),
// on internal/value, the rule the validator, the graph and the snapshot reader
// read a value by, on internal/mapping, the label, identifier and storage rules
// it shares with schema/cypher, and on [github.com/simon-lentz/yammm/schema/cypher], whose
// [cypher.Config] is the storage mapping its write calls and its diff take. No library
// package depends on neo4j.
//
// # Neo4j Driver Dependency (Type-Only)
//
// The package imports [github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype] for
// temporal type definitions (dbtype.Date). It does not import connection,
// session, or transaction packages — no code in this package opens a
// connection or executes queries against a database. Consumers supply their own
// driver and choose how to apply the generated output: the package produces
// Cypher and driver-compatible parameters.
//
// # Write Surface
//
// [NodeQueries] and [EdgeQueries] operate on a
// complete [graph.Snapshot] for high-throughput batch writes. They are the
// snapshot's write surface, and the package has no single-item write. Hand-built
// Cypher can use [BuildBatchRelationshipMergeQuery] and [CoerceParams].
//
// Each call derives every type's label, primary keys with their constraints, and
// @writeOnce keys from the snapshot's own schema under the [cypher.Config]'s
// label settings, so a merge key and the property it names are coerced under one
// schema. A type's entry is keyed by [schema.TypeID], so two closure types
// sharing one bare name each get their own label. Two root types render one
// label only where their schema and type names compose one label, as schema "a"
// with type B__C and schema "a__B" with type C do; both calls refuse that
// closure with [cypher.E_NEO4J_LABEL_COLLISION], and a zero snapshot with
// [ErrZeroSnapshot].
//
// An error marked [ErrUnrepresentable] refuses these values: one its constraint's coercion refuses, a nested collection's, a map at a
// property the schema declares, a list or a map a document nests more than 256
// levels deep, or a [encoding/json.Number] no driver number holds. An integer literal a finite float64 holds
// passes them at a Float position and rounds to its nearest float64. No snapshot these writers accept holds a property the schema does not
// declare: the validator keeps one out of a graph, and
// [github.com/simon-lentz/yammm/snapshot.Load] refuses one. Of the values the mark refuses, a validated snapshot holds two, which
// the validator accepts: a nested collection's, and an instant at a UTC offset
// past 18 hours. Any other is held only by a snapshot whose values no
// validator refused at Error. [CoerceParams] marks its
// refusals of a value so too. No other error
// carries the class: a caller's own, such as a constraint
// [schema.CheckConstraint] refuses, a zero snapshot, a closure whose labels
// collide or are no valid identifiers, a relationship type that is no valid
// identifier, and a cancelled context.
//
// A label that is no valid identifier is refused with
// [cypher.E_NEO4J_INVALID_IDENTIFIER]. The write queries write relationship
// types unquoted too and check only an edge's ([EdgeQueries]): a composition's
// relation name, which the composition statements write, is checked nowhere.
// The write path is not a property-name gate; [cypher.Config.Constraints] and
// [cypher.Config.Indexes] check the property names their statements write.
//
// A value of a property whose List element is itself a List or a Vector is
// refused by its type, before any element is read, with
// [ErrUnsupportedListElem]: [NodeQueries] and [CoerceParams] return an error
// for a value that holds one, an empty list included, and read a value that
// holds none as nil, which NodeQueries writes as null.
//
// # Parameter Coercion
//
// One rule converts a value in the form the validator stores to the
// driver-native type its [schema.Constraint] requires, so the value satisfies
// Neo4j TYPE constraints. The write path applies it once it has read each
// json.Number as the driver's number. [CoerceParams] applies it to the
// parameters of hand-built Cypher: it first reads each value at its constraint
// as [github.com/simon-lentz/yammm/instance.Validator] reads a property value,
// so a parameter reaches the driver as the write path sends that value from a
// validated instance, and a value the validator refuses (a wrong kind, a bound,
// a member, a pattern) is an error. CoerceParams reads one flat map and walks
// no value: to coerce the fields of a nested map or of a row, call it on that
// map. The rule takes the full constraint, not just its kind; a DataType
// reference is resolved internally.
//
//   - Float: a float64. Integer: an int64. Boolean: a bool. String, Enum and
//     Pattern: a string. Each passes through, and a value of any other Go type
//     is a coercion failure and returns an error.
//   - UUID: a string -> its canonical text, lower case with hyphens. A string
//     that is no UUID is a coercion failure.
//   - Timestamp: a string the validator reads as RFC 3339 -> time.Time (Neo4j
//     ZONED DATETIME). A string it does not read, or any other value, is a
//     coercion failure.
//     The text is read in UTC, so the host's
//     zone decides nothing, and the instant goes as its offset and never under
//     a zone name: a UTC instant goes as offset zero. The server holds one
//     instant under a zone name and under that zone's offset as two unequal
//     values, and Cypher's datetime() of RFC 3339 text gives the offset form.
//     An instant at an offset past 18 hours is a coercion failure: the server
//     holds an offset of 18 hours at most.
//   - Date: a "2006-01-02" string -> dbtype.Date (Neo4j DATE). A string that
//     does not parse, or any other value, is a coercion failure. The date is
//     midnight UTC on the day the text shows, since a date carries no time of
//     day. [CoerceParams] also passes a dbtype.Date at a Date, read the same
//     way; in a list it is refused, as the validator refuses it.
//   - Vector: a []float64 passes through. At a List and at a Vector a []any is
//     element-coerced by the rules above into a typed slice. A []any at any
//     other kind is refused, since a scalar cannot hold a list.
//
// A map is refused under every constraint, whatever its key and element types:
// Neo4j stores no map as a property value. A nil map holds no value and returns
// nil.
//
// [CoerceParams] reads a value as the validator does, so a value in a form the
// validator reads is read the same way. A Go integer of any width, a named type
// and a time.Time at a Timestamp are three such forms. It refuses two values
// the validator takes: a value at a List whose element is a collection, and an
// instant at a UTC offset past 18 hours. A time.Time goes through the text the
// validator stores it as. At a Timestamp that text is RFC 3339 with fractional
// seconds, so the instant reaches the server at the offset the text carries.
// That text holds an offset in whole minutes, so a time.Time whose offset holds
// seconds moves by those seconds.
// From a key that holds a constraint no zone name reaches the server, a
// location's such as Europe/Berlin included, and there a pointer is refused, a
// nil one too.
//
// On the write path the rule refuses those two values, and beside them only a
// value no validator refused at Error: one snapshot.Load read without
// revalidating at Error. There it judges a value's
// form and never its bounds, members or pattern, nor a Vector's dimension. A
// Timestamp, a Date and a UUID are forms, so their text is parsed.
//
// The write path refuses a value at a List whose element is a List or a Vector
// by its type first: Neo4j has no nested collection property type, so a value
// there, an empty list included, is an error wrapping [ErrUnsupportedListElem],
// and [CoerceParams] refuses one so too. The write path reads every JSON number
// literal as the driver's number before the rule: at a Float its nearest
// float64, elsewhere an integer literal as the int64 it spells and a float
// literal as its nearest float64, which an Integer refuses as a float. A
// literal no finite float64 holds, or no int64 holds outside a Float and a
// Vector element, where the .ys reader left it a json.Number, is refused before
// the rule.
//
// A value that holds none ([github.com/simon-lentz/yammm/instance.IsAbsent]),
// nil, a nil slice, a nil map or a nil immutable container, is returned as nil,
// which the driver writes as null, and a nil constraint ("no type to coerce against") passes any other
// value through unchanged. A kind added to [schema.ConstraintKind] after the rule's switch was
// written surfaces as a lint failure via its //exhaustive:enforce directive, rather
// than as a silent driver-side PROPERTY_TYPE rejection in production.
//
// # Composition Ownership
//
// [NodeQueries] returns a phased slice, ordered by
// [NodeQueryKind]: every node merge precedes every composition replace,
// which precedes every composition create (parent-first by depth). The
// ordering is a documented guarantee — executing the slice in order is
// correct.
//
// The guarantee is about ORDER, not atomicity. Each query is handed to the
// caller separately, so a caller that runs each in its own transaction and
// fails partway leaves a subtree deleted by the replace phase and not yet
// rebuilt by the create phase. A caller that needs the replace and create of
// one root to be all-or-nothing runs the slice inside a single transaction.
//
// A parent write replaces its composed subtree. The replace phase deletes
// every part reachable from each written root through the schema's
// composition closure — whether or not the snapshot carries children, so a
// zero-child write still removes stale ones — and the create phase rebuilds
// the tree from the snapshot's instances. Parts are created fresh after the
// delete (SET c = ..., never SET c +=), so a stale property cannot survive
// on a part the way it can on a merged root node.
//
// Part nodes carry their identity in the _composed_key property, which this
// package mints because a part has none of its own: the library identifies one
// through its parent composition, and only a store that must address each node
// separately needs an address. For a keyless (many) part the segment is
// positional and NOT a stable identity across writes — safe under replace
// semantics, which delete a root's earlier parts first, so a key matches only a
// part node the same write created.
//
// The address is a flat JSON array: the owning root's LABEL, the root's key
// values, then one two-element segment per composition hop, except a (one)
// composition, whose segment names the relation alone.
//
//	["shop__Order",["o1"],["SECTIONS",["s1"]],["NOTES",0]]
//
// Three properties earn that shape. It is FLAT, so the address grows linearly
// with depth; nesting each level's rendering inside the next escapes the level
// above on every hop and the escapes compound. It leads with the root's LABEL,
// because the depth-2 parent MATCH is scoped by the PART label and not by the
// root, so two root types sharing a key value and a relation name would
// otherwise mint one address for children under one part label —
// the write path refuses a closure whose labels collide, which is what makes the
// label exact. And it carries NO source path: a type identity
// renders a file-backed schema's absolute path, which would put the writing
// machine's directory layout on every part node and change every address when
// the schema file moves.
//
// # Write-Once Derivation
//
// The node write path derives a type's @writeOnce-annotated properties, own and
// inherited, from the snapshot's schema on every call, and selects the type's
// immutable-key query shape from that set, so a @writeOnce property is set on
// node creation and never rewritten on a subsequent MERGE. A caller cannot
// change the set.
//
// # Cypher Builders
//
// [BuildBatchRelationshipMergeQuery] produces the UNWIND-batched relationship
// MERGE template the write surface uses internally. It is exported for
// consumers — a link engine resolving edges across datasets, for instance —
// that want the template without the surrounding param-and-chunk plumbing.
//
// It is a pure function: no execution, no driver dependency, no side effects.
// Callers pair the returned string with their own parameter map and feed both
// to a driver session at the call site. The template always ends with
// `RETURN count(*) AS matched_rows`, so a consumer implementing
// silent-failure detection has a stable column to sum; aggregation across
// chunks is the consumer's.
//
// The node-merge and composition templates are internal. A node merge ends
// with no RETURN clause: a MERGE creates the node it does not find, so it has
// no missed match to count. The composition templates are MATCH-anchored, so
// each ends with the same `RETURN count(*) AS matched_rows`.
//
// # Introspection
//
// The package holds the Cypher queries for inspecting a live Neo4j database:
//
//   - [ShowConstraints]: fetches all constraints
//   - [ShowIndexes]: fetches every index, including the index backing a
//     constraint — [RemoteIndex.OwningConstraint] identifies those, and the diff
//     needs them because they block a CREATE INDEX by name and by definition —
//     and the LOOKUP indexes, whose names block creates too
//
// Parse the results with [ParseConstraints] and [ParseIndexes]. [ParseIndexes]
// reads the index options map, so [RemoteIndex.VectorDimensions] and
// [RemoteIndex.VectorSimilarity] expose a vector index's configuration for drift
// detection.
//
// # Diff Scope
//
// [Diff] compares what a schema declares under a [cypher.Config] with a
// database's constraints and indexes and returns one [Plan] over both. It
// derives the desired constraints, the desired indexes and the owned labels from
// the Config and the schema, so the three agree by construction:
//
//	plan, result := neo4j.Diff(c, s, constraints, indexes)
//
// The result holds the constraint generation's issues. When they hold no
// error, the indexes are generated and the result holds their issues too,
// sorted with the constraints' as [diag.Collector.Result] sorts. A constraint
// error therefore returns before the index generation runs. A plan is returned
// only when the result holds no error.
//
// Ownership is set membership, not a rule applied to a remote object's label
// string: [cypher.Config.OwnedLabels] says what the set covers and why. Only an
// owned remote object is paired with a declaration or is a Drop. Any remote
// object, owned or not, can block a declaration's create, and the plan then
// drops it.
//
// # One Plan Over Both Sides
//
// Index and constraint names share one Neo4j namespace. Every emitted statement
// carries IF NOT EXISTS, so a create over a name that an object of its own kind
// holds does nothing. A UNIQUE or NODE KEY constraint's backing index holds the
// constraint's name as an index, so an index create over that name does
// nothing too. The server refuses a constraint create over an index's name, and
// an index create over a NOT NULL or TYPE constraint's name. A constraint's drop
// also removes its backing index. A drop that one side needs
// can therefore remove an object that the other side matched. [Diff]
// settles both sides in one fixed point, so its plan converges in one apply.
//
// Each remote row has a drop target, the object that one DROP removes. A
// backing index's target is its constraint. A Match or Unverified declaration
// whose realisation a drift names among its Blockers moves to Drift, with that
// realisation first, and the apply creates it again under its own name. An
// index that an online or POPULATING backing index serves becomes a Create
// when the plan drops that constraint, as a blocker or as a Drop, unless
// another blocker makes it a Drift. Where the caller leaves that Drop
// unapplied, the server takes the create as a no-op while the backing index
// stands.
//
// A row is in one place only. It is a Match or Unverified Actual, a Drop, a row
// whose drop target a drift names or a Drop removes, or it is counted in
// Excluded. A row that a drift on either side names is never a Drop. Excluded
// counts the rows the plan leaves alone: rows on a label the schema does not
// own, and rows the schema cannot declare as they stand. A non-zero count is
// normal in a shared database. It exists so that "0 to drop" does not read as
// "the database is accounted for". An object that a type deleted or renamed
// since the last apply left behind is on a label that no current type declares.
// No declaration pairs with it, and it is a blocker only where it holds a
// declared name.
//
// The plan does not depend on the order of the rows passed in. [Diff]
// sorts its own copies, and among equal candidates the first in that order
// wins. Match, Unverified, Create and Drift keep the declarations' order.
//
// # Constraint Diffing
//
// [Plan.Constraints] classifies the constraints into FIVE categories:
// matched, drifted, to-create, to-drop, and unverified. An unverified constraint
// is present, but the server did not report what the comparison needs. A caller
// that decides "is this in sync?" must read Unverified too. To treat it as
// matched reports an unchecked constraint as verified.
//
// A declaration drifts when a CREATE alone cannot realise it.
// [ConstraintDrift.Blockers] names every database object that its create needs
// dropped. These producers add objects:
//
//   - a TYPE constraint that pairs with the declaration and enforces another
//     type, or a constraint that holds the declaration's name under another
//     definition: that object;
//   - a constraint or an index, on any label, that holds the declaration's name,
//     when the declaration drifts or nothing realises it. A declaration that
//     another object realises stays a Match beside a name holder, and the plan
//     creates nothing for it;
//   - for a UNIQUE or NODE KEY declaration, a plain RANGE index over its label
//     and properties in its order, under any name;
//   - for a UNIQUE or NODE KEY declaration, a constraint of the other of those
//     two kinds over its label and properties in its order. The server refuses
//     the two side by side;
//   - the object that realises the declaration, when a drift on either side
//     needs that object dropped.
//
// Pairing goes in phases: name and identity together, then the definition with
// its properties in the declared order, then identity alone, then name alone.
//
// Unverified is reachable when the records came from a query that does not
// return propertyType, for example a caller's own Cypher in place of
// [ShowConstraints]. A server too old to report that column is also
// too old to hold a TYPE constraint. There, such a declaration is a to-create,
// or a drift when an object that holds its name blocks the create.
//
// # Index Diffing
//
// [Plan.Indexes] classifies the indexes into FIVE categories: matched,
// drifted, to-create, to-drop, and unverified. An unverified index is present,
// but its configuration was not readable or it is still populating. As with
// constraints, Unverified is neither in sync nor drifted.
//
// A declaration drifts when a CREATE alone cannot realise it.
// [IndexDrift.Blockers] names every database object that its create needs
// dropped. These producers add objects:
//
//   - a vector index that pairs with the declaration and differs in dimension
//     or similarity, an index that holds the declaration's name under another
//     definition, or a paired index in the FAILED state: that index. A
//     POPULATING index is unverified;
//   - an index of any kind, on any label, that holds the declaration's name. A
//     constraint's backing index is one, and stands for its constraint;
//   - a plain index over the same label, kind and properties in the same order,
//     under another name;
//   - a constraint with no backing index that holds the declaration's name;
//   - the index that realises the declaration, when a drift on either side
//     needs it dropped. A FAILED backing index that serves the declaration is
//     its first blocker. An online or POPULATING one is no blocker: when the
//     plan drops its constraint, the declaration is a Create unless another
//     blocker makes it a Drift.
//
// Composite property order is significant, unlike the constraint side. A
// remote index over the same properties in another order is a distinct index.
// An owned remote index that [RemoteIndex.Declarable] accepts, that no
// declaration matches, and that no drift names is a Drop. An owned index that
// it refuses is never a Drop. A constraint's backing index can still serve a
// declaration that pairing left without a row of its own. While the plan keeps
// the constraint, that row is the declaration's Actual where it serves queries
// ([RemoteIndex.IsOnline]) and where it is POPULATING (Unverified), and the
// first of its drift's Blockers where it is FAILED. Where the plan drops the
// constraint of an online or POPULATING one, the declaration is a Create
// unless another blocker makes it a Drift.
//
// # Applying a Plan
//
// A [Plan] converges in one apply, in this order:
//
//  1. Drop every object that a constraint drift or an index drift names among
//     its Blockers. Drop each drop target once, and drop each Drop that the
//     caller applies in this step too. Drop each of Blockers.Constraints with
//     [cypher.DropConstraintStatement] and each of Blockers.Indexes with
//     [cypher.DropIndexStatement]. An index with a non-empty OwningConstraint is a
//     constraint's backing index: drop that constraint.
//  2. Create every constraint in Constraints.Create and every constraint
//     drift's Desired.
//  3. Create every index in Indexes.Create and every index drift's Desired. An
//     index created before a constraint whose backing index serves its
//     definition makes the server refuse that constraint.
//
// A constraint's create succeeds only when the data meets it. A refused create
// after step 1 leaves the label without the dropped object's guarantee. Before
// step 1, compare each create's precondition with the data:
//
//   - UNIQUE: no two nodes on the label hold one tuple of the properties;
//   - NOT NULL: every node on the label holds the property;
//   - TYPE: every value of the property on the label is of the declared type;
//   - NODE KEY: both the UNIQUE and the NOT NULL preconditions, for every key
//     property.
//
// # Write Options
//
// [WithNodeChunkSize] and [WithEdgeChunkSize] set the most rows one UNWIND
// batch holds.
//
// # Neo4j Version
//
// A schema with a composition needs Neo4j 5.9+, since its replace query uses a
// quantified path pattern. [github.com/simon-lentz/yammm/schema/cypher] states
// what its DDL needs.
//
// # Sealed Schemas
//
// A schema comes only from [github.com/simon-lentz/yammm/schema.Load],
// [github.com/simon-lentz/yammm/schema.LoadString] or
// [github.com/simon-lentz/yammm/schema.LoadSourcesWithEntry], and each returns
// it sealed. Sealed schemas guarantee that alias chains are fully resolved,
// inheritance is linearized, and type identities are assigned.
//
// # Thread Safety
//
// Every function reads its arguments and writes only to values it allocates,
// so concurrent calls are safe.
//
// # Dependencies
//
//	neo4j  ──imports──▶  schema, schema/cypher, graph, immutable, diag,
//	                     internal/mapping, internal/value,
//	                     github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype
package neo4j
