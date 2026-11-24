// Package graph builds an in-memory data structure from validated instances.
//
// The graph package is the final layer in the YAMMM validation pipeline:
//
//	Schema → Instance Validation → Graph
//
// Among what it handles:
//   - Primary key uniqueness (duplicate detection, or a merge under
//     [Graph.AddOrMerge])
//   - The key rule (which components a key may hold) and root type eligibility
//   - Association edge resolution (forward references)
//   - Composition child extraction and indexing
//   - Completeness checking (required association validation)
//
// # Thread Safety
//
// [Graph] is safe for concurrent use. Multiple goroutines may call [Graph.Add]
// and [Graph.AddOrMerge] concurrently. The graph handles forward references, duplicate detection and
// merges atomically using internal synchronization.
//
// [Snapshot] has no method that changes it, and it is safe for concurrent read
// access from multiple goroutines. A record it returns is a handle, so no caller
// holds the record's address.
//
// # Basic Usage
//
//	g := graph.New(s)
//
//	// Add validated instances (may be called concurrently)
//	if err := g.Add(ctx, validInstance); err != nil {
//	    // A refused instance (a duplicate PK, a keyless part read out of a composition, ...; see Error Handling)
//	}
//
//	// Check required associations and each grown root's invariants
//	if err := g.Check(ctx); err != nil {
//	    // Required associations are missing, a root a merge grew draws a
//	    // validator failure such as E_INVARIANT_FAIL, or ctx was cancelled
//	}
//
//	// Get snapshot for inspection
//	snap := g.Snapshot()
//	for _, typeID := range snap.Types() {
//	    for inst := range snap.InstancesOf(typeID) {
//	        // Process instances
//	    }
//	}
//
// # Validate, Then Add
//
// A pipeline validates each record with an [instance.Validator] and adds what
// the validator returns. Both are safe for concurrent use, so one validator and
// one graph serve every goroutine, each call taking its own context:
//
//	v := instance.NewValidator(s)
//	g := graph.New(s)
//	for i, rec := range records {
//	    valid, err := v.ValidateOne(ctx, "TypeName", buildRawInstance(rec))
//	    if err != nil {
//	        return fmt.Errorf("record %d: validate: %w", i, err)
//	    }
//	    if err := g.Add(ctx, valid); err != nil {
//	        return fmt.Errorf("record %d: add: %w", i, err)
//	    }
//	}
//	if err := g.Check(ctx); err != nil {
//	    return fmt.Errorf("batch: check: %w", err)
//	}
//	snap := g.Snapshot() // pass it to snapshot.Marshal
//
// [Graph.AddOrMerge] takes Add's place for a source that lists one record under
// several parents. To resume a persisted batch, seed the graph with
// [NewFromSnapshot] and add the records still outstanding: they resolve against,
// and may complete, the seeded state. Each refusal is in the error of the call
// that drew it, so a caller that needs a record of every refusal keeps those
// errors; a [Snapshot] holds no diagnostics.
//
// # Alternative Constructors
//
// [NewFromSnapshot] creates a [Graph] over a [Snapshot]'s own schema,
// pre-populated from it, for incremental building on top of persisted or
// previously-constructed state; new Add calls resolve against the imported
// instances. Every constructor of a Snapshot holds it to the structural facts
// below under its schema, so the import installs the records as they stand and
// refuses nothing.
//
// [github.com/simon-lentz/yammm/snapshot.Unmarshal] constructs a [Snapshot] from a .ys
// document. It holds the document to the structural facts below and refuses a
// string of invalid UTF-8 anywhere in it, and it judges values against their
// constraints only under a load option that asks for it. Its instances report
// Validated() == false under every option.
//
// # Type Resolution
//
// [Graph.Add] judges an instance by the schema object that validated it. The
// instance holds its [schema.Type] ([instance.ValidInstance.Type]), and Add compares
// that handle with the one [schema.Schema.TypeByID] returns for its identity. A
// mismatch panics: the type is outside this graph's import closure, or another
// load validated the instance. One text loaded twice is two loads, so an instance
// crossed between them panics whether or not the texts differ.
//
// A load holds one handle per type however many import paths reach it, so a
// diamond import gives one handle. A validator over any member of a closure serves
// a graph over any schema of that load whose own closure holds the type.
//
// Past that check, a root's type is any type of the closure that can hold a root
// (Root type eligibility, below), one the schema reaches only through an
// intermediate import included. The graph holds it by identity, apart from an
// entry type of its name. A composed child reaches the whole import closure too:
// its type comes from a relation the schema already resolved.
//
// A schema where an imported type composes a part type from a further import
// therefore loads, validates and builds.
//
// No lookup in this package takes a rendered type name.
//
// # Root type eligibility
//
// A root instance's type must satisfy three members. The snapshot reader applies
// all three. [Graph.Add]
// takes an instance a validator of its own load built, and that validator
// refuses an abstract or a part type at a root, so Add checks the key and the
// part rule, which a child read out of [instance.ValidInstance.Composed] can
// still break. [Graph.AddOrMerge] runs the code [Graph.Add] runs, and
// [NewFromSnapshot] takes a snapshot that was held to them where it was built. A
// type is
// ineligible when it
//
//   - is abstract, because an abstract type has no instances;
//   - is a part type, because a part instance is addressed through its parent
//     composition;
//   - declares no primary key, because it then has no address at all.
//
// Any type of the import closure that passes them can hold a root, one the entry
// schema reaches only through an intermediate import included. A root is held
// by its identity, so such a type stays apart from an entry type of its name.
//
// A composed child is held only to the abstract member, since a load refuses an
// abstract composition target. It is addressed through its parent, its type comes
// from a relation the schema already resolved, and it is legal only as that
// relation's declared target, which can be a transitively imported part type.
//
// # Denoted type eligibility
//
// A snapshot DENOTES every type in [Snapshot.Types]. That set holds every
// root's type, and a document's instances section may add a type through a group
// with no item. A types table row with no group denotes no type.
// Every denoted type is held to the three members above, whether or not the
// snapshot holds an instance of it, so no constructor denotes a type [Graph.Add]
// refuses.
//
// # Structural facts
//
// [Graph.Add] never builds a snapshot that breaks the facts below, and the
// snapshot package's reader refuses a document that does.
// The facts are the whole promise: a shape they do not name, such as a
// property value its constraint refuses, can pass the reader. A type that
// cannot hold a root is one break, however many positions name it, and a root of
// it holds no address: it owes no required association, it is in no (one) count,
// and a second root at its key is no duplicate. A break its records or its slots
// show by themselves stays its own.
// The facts are:
//
//   - Identity. Every type identity resolves in the schema.
//   - Root and denoted types. See the two sections above.
//   - Composition slots. A slot is a composition its parent's type declares,
//     holds instances of its declared target, holds one child at most under a
//     (one) composition, and holds each canonical key of a keyed part once.
//   - Keys. Every stored key holds only scalars that render as every writer
//     writes them and that [ParseKey] reads back at that rendering, so a reload
//     keeps every address. A keyless part stores no key. A keyed instance's
//     stored key has one component per declared primary key, and each agrees
//     with its key property, which is present and not null: both of one kind,
//     so the integer 1 and the float 1.0 disagree. So a list or a map, an
//     integer literal no int64 holds, a literal no finite float64 holds, and a
//     number ParseKey reads back at another rendering (one spelled
//     1.0, or a negative zero) are refused. A Timestamp, Date or UUID component
//     of a stored key, a target key, a record's source key, or a duplicate's key
//     or conflict key that its kind can render is the text its kind stores, so
//     every address compares as written; a component its kind cannot render is
//     no address of an instance of that kind, which revalidation judges. Two roots of one type never share a canonical address.
//   - Names. Every stored property and edge property name is declared.
//   - Associations. Every edge and unresolved record is under an association
//     its source's type declares. An edge's target is an instance of that
//     association's declared target, and a record's target type is that
//     declared target, derived from the association, never taken from a
//     constructor's input. A record carries a target key of
//     the target's arity, each component one the Keys fact admits, where
//     it names a target, and states a reason [UnresolvedEdge] documents; an
//     "absent" or "empty" record carries no target key and no edge
//     properties. A (one) association holds one record at most, edges and
//     unresolved records together, and no two of a source's edges and
//     "target_missing" records under one association name one target.
//   - References. An edge's source and target and a record's source each name
//     a root. A reference to the address
//     a root's key properties state, where the key rule refuses its stored key,
//     is that root's break, and draws no diagnostic of its own.
//   - Records. The records are the ones [Graph.Add] derives from the data, and
//     the ones [Graph.AddOrMerge] merges into a held root from a duplicate's
//     data. Every root holds an edge or a record under each required
//     association of its type. An "absent" or "empty" record stands alone,
//     once, and only under a required association, and an "empty" record only
//     under a (many) association. A "target_missing" record
//     names a target no root holds.
//   - Duplicates. A duplicate's instance holds no composed children, and its
//     type can hold a root. Its conflict is the root at its own type and key. A
//     duplicate with no such root breaks the fact.
//
// The values are another matter. A snapshot holds only what a validator stored or
// the reader decoded: nil, a bool, a string of valid UTF-8, an int64, a finite
// float64, a json.Number (an integer literal past int64 at a position that is
// neither a Float nor a Vector element, or a literal no finite float64 holds),
// and lists and string-keyed maps of those. Each has a
// form on the .ys wire, so [github.com/simon-lentz/yammm/snapshot.Marshal] meets no
// value it cannot write. A data writer can still refuse one: the neo4j package
// has no spelling for some, and its doc names them.
//
// Whether an association is required is read from the schema, never taken from
// a constructor's input: [UnresolvedEdge.Required] is derived by every
// constructor from the snapshot's schema.
//
// # Build Then Commit
//
// [Graph.Add] and [Graph.AddOrMerge] check the instance's load and its root type, then assemble the [Instance] tree to install and stage its
// association records, touching no graph state. They check no structure below
// the root type: the validator of the same load already judged the property names,
// a root's edge names, edge property names, multiplicities and target keys, and
// every slot and child of the composition tree at any depth. The commit phase
// installs the tree and the staged association records together under the
// graph's lock. A merge's commit installs no tree: it installs the records the merge plans on
// the held root and retires the held root's "absent" or "empty" records they
// replace. The commit can
// still refuse, before it installs anything of the record: a root at a key the
// graph holds with E_DUPLICATE_PK, and a merge with E_GRAPH_CARDINALITY.
//
// A call that returns an error therefore installs nothing of the record — no
// instance, child or edge. A root [Graph.Add] refuses at its key, and a root
// [Graph.AddOrMerge] refuses at its key for its composed children, is itself
// recorded in [Snapshot.Duplicates]. Every refusal's issue is in the call's
// error alone.
//
// A validator defect reaches the graph: Add does not judge the record again.
// [github.com/simon-lentz/yammm/snapshot.WithRevalidation] runs a validator over a
// document's roots when it loads.
//
// # Merge on a Duplicate Key
//
// [Graph.AddOrMerge] adds an instance as [Graph.Add] does. When
// the graph already holds a root of the instance's type at its primary key, it
// merges instead: the incoming
// instance's association records go to the held root, and its properties and
// provenance are dropped, so the held root's stand. A merge records no
// [Duplicate] and raises no error. It is for a source that lists one record
// under several parents, each listing naming its own association targets.
//
// The merge makes the checks [Graph.Add] makes before it reads the held key, and
// it refuses what they refuse under the same codes. Then, under the graph's lock:
//
//   - A record naming a target the held root already names under that
//     association, by an edge or an unresolved record, adds nothing. The
//     validator refuses a listing that names one target twice, so the incoming
//     data names each target once.
//   - A record under a (one) association that would leave the held root two
//     targets there refuses the whole merge, before anything installs, with an
//     E_GRAPH_CARDINALITY for each such association.
//   - Every other record naming a target installs on the held root: an edge
//     when the target is held, carrying the incoming edge properties, and an
//     unresolved record otherwise, which a later Add of the target resolves.
//   - An "absent" or "empty" record of the incoming instance adds nothing: the
//     held root already holds an edge or a record under every required
//     association. A record the merge installs under a required association
//     retires the held root's "absent" or "empty" record there.
//
// An incoming instance carrying composed children is not merged: it is refused
// as [Graph.Add] refuses a duplicate, with a [Duplicate] and E_DUPLICATE_PK.
//
// A validator judges each listing alone, so a merge can grow a root past an
// invariant no listing breaks: two listings of one vendor each pass
// `VENDORS -> Len <= 1`, and the root holds two. An invariant is a fact of the
// held root, so [Graph.Check] judges each root a merge grew whose type declares
// or inherits one. It rebuilds the root's held form as the snapshot package's revalidation
// does, runs it through the validator the reader would choose, and returns what
// that draws, each issue carrying the root's type name and key. A lower bound
// needs no such check: the validator refuses a listing under it before any
// graph sees it. Until Check runs, a grown root stands unjudged: a caller that
// never calls Check writes it under the Values attestation. Check judges only
// the roots a merge in this graph grew: a root seeded by [NewFromSnapshot]
// stands on the seed's claim, one an earlier graph grew included, until a
// merge in this graph grows it, and
// [github.com/simon-lentz/yammm/snapshot.WithRevalidation] judges a loaded
// document's roots. Each call validates each such root again.
// A merge finds the held root's records through an index by source. The first
// call to reach these rules builds it, whether that call commits or is
// refused, so a graph keeps none until an AddOrMerge without composed children
// meets a held key.
//
// # Composed Children
//
// A validator judges a composed child as it judges a root — its property names,
// its own key, and its own composition tree — and [Graph.Add] judges none of it
// again. A load refuses a part type that declares or
// inherits an association, so a child holds no edge. A composed child
// therefore produces no [Edge], no [UnresolvedEdge], and no effect on
// [Attestation]'s Associations dimension.
//
// # Key Types
//
// [Instance] provides read access to a graph node's data, through
// accessors such as [Instance.TypeName], [Instance.PrimaryKey], [Instance.Properties],
// [Instance.Composed], [Instance.ComposedRelations], [Instance.Provenance].
//
// [Edge] represents a resolved association between two instances, read
// through accessors such as [Edge.Relation], [Edge.Source], [Edge.Target], [Edge.Properties].
//
// [UnresolvedEdge] records an association the graph could not resolve at
// construction time: a target it holds no root for (target_missing), or a
// required one the data left absent or empty. Edge
// properties declared on the forward reference survive Marshal/Unmarshal
// symmetric with [Edge.Properties] and are accessed via
// [UnresolvedEdge.Property] and [UnresolvedEdge.Properties]. The .ys
// wire format carries these in the "properties" field of each
// unresolved-edge wire entry; see the snapshot package for
// format-version semantics.
//
// # Type Identity and Type Names
//
// A type is identified by [github.com/simon-lentz/yammm/schema.TypeID] — its
// declaring schema path plus its name. A type NAME is a rendering of that
// identity under one schema:
//
//   - Local types: unqualified name (e.g., "Person")
//   - Types of a schema the schema imports directly: alias-qualified name (e.g.,
//     "c.Entity")
//   - Types the schema reaches only through an intermediate import: the declaring
//     schema's name and the type's name (e.g., "deep#Beacon")
//
// Within one load each rendering denotes one type, since a load refuses two
// schemas of one name. [github.com/simon-lentz/yammm/schema.Schema.ResolveTypeName]
// resolves the first two forms and not the third.
//
// So every place that must denote a type exactly takes or returns an identity:
//
//   - [Snapshot.Types]
//   - [Snapshot.InstancesOf]
//   - [Snapshot.InstanceByKey]
//   - [Instance.TypeID]
//   - [UnresolvedEdge.TargetType]
//
// [Instance.TypeName] still carries the rendered name beside the identity
// ([Instance.TypeID]). Every constructor renders it from the identity under the
// bound schema with [github.com/simon-lentz/yammm/schema.TagForm], and so does
// the type-name detail of every graph diagnostic.
// Where output needs a name the bound schema resolves, render the identity with
// [github.com/simon-lentz/yammm/schema.AddressableTag], which refuses an
// identity the bound schema cannot name.
//
// # Key Formatting
//
// Primary keys are represented as canonical JSON array strings for
// map indexing and diagnostic messages:
//
//	graph.FormatKey("ABC123")       // ["ABC123"]
//	graph.FormatKey("us", 12345)    // ["us",12345]
//
// Use [FormatKey] to construct lookup keys for [Snapshot.InstanceByKey]. The
// graph tries an address it receives as spelled, then with each Timestamp, Date
// or UUID component canonicalized under the type's key constraints, as the key
// was at entry. One instant at two offsets is two addresses.
//
// A part type can declare a primary key, and a composed child carries it. A
// validator and the snapshot package's reader compare it to tell siblings apart. It is not an address: a
// part instance is found through its parent composition, so
// [Snapshot.InstanceByKey] never returns one and this package mints none; a
// store that must give each part node an identity derives it, and
// [github.com/simon-lentz/yammm/neo4j] is the only one that does.
//
// # Error Handling
//
// [Graph.Add] and [Graph.Check] return an error, and [Graph.AddOrMerge] returns
// one beside whether it merged. A refusal is a *[diag.ResultError] holding each
// failure as an issue with a stable code: read it with errors.AsType, and test
// it for a code with errors.Is. A cancellation is a Fatal E_CONTEXT_CANCELLED,
// and a validator defect [Graph.Check] meets on a grown root a Fatal
// E_INTERNAL; every other refusal is an Error. The graph draws no issue below failure severity,
// so its calls take no warnings callback.
//
// Programmer errors panic: a nil receiver at a method that adds or checks, a
// zero instance at [Graph.Add] or [Graph.AddOrMerge], a nil context, a zero
// schema or snapshot at a constructor, an instance whose type this graph's
// schema does not hold under its identity (see Type Resolution), or a key value
// [FormatKey] cannot encode. A
// validator returns a zero instance for a record it refuses, so a caller checks
// [instance.ValidInstance.IsZero] before it adds.
//
// [Graph.Add] emits the following. The first two judge a root's type in the
// order listed, and a type breaking both draws the first:
//
//   - E_GRAPH_MISSING_PK: the type declares or inherits no primary key, so the root is a
//     keyless part read out of [instance.ValidInstance.Composed]
//   - E_GRAPH_INVALID_COMPOSITION: the root is a keyed part read out of
//     [instance.ValidInstance.Composed]
//   - E_DUPLICATE_PK: the primary key already exists for this type; its related
//     location is the held instance's span, when that instance carries one
//   - E_CONTEXT_CANCELLED: the context was cancelled
//
// [Graph.AddOrMerge] emits what [Graph.Add] emits, with two
// differences at a held key: E_DUPLICATE_PK only for an incoming instance
// carrying composed children, and E_GRAPH_CARDINALITY for a merge that would
// give a (one) association a second target.
//
// [Graph.Check] emits E_UNRESOLVED_REQUIRED, E_CONTEXT_CANCELLED, each failure
// a validator draws on a root a merge grew, such as E_INVARIANT_FAIL (Merge on
// a Duplicate Key), and E_INTERNAL for a validator defect.
//
// The snapshot package's reader reports a broken fact under its own codes, such as
// E_SNAPSHOT_DANGLING_REFERENCE and E_SNAPSHOT_INVALID_ROOT, or under a graph
// code, such as E_GRAPH_CARDINALITY and E_GRAPH_UNKNOWN_RELATION.
//
// # Diagnostics Lifecycle
//
// Each call returns the issues it drew, and the graph keeps none of them: a
// [Snapshot] carries the structural records ([Snapshot.Duplicates],
// [Snapshot.Unresolved]) and the [Attestation], never a diagnostic. [Graph.Check]
// judges the graph as it stands at each call, so calling it again returns the
// same error over the same graph.
//
// # Ordering Guarantees
//
// Every collection accessor of [Snapshot] produces deterministically sorted
// output. [Graph.Add] call order and concurrency move only records that tie in
// every arm of the tuple:
//
//   - [Snapshot.Types]: by schema name, then type name
//   - [Snapshot.InstancesOf]: lexicographic by primary key string
//   - [Snapshot.Instances]: types in [Snapshot.Types] order, then each type's
//     primary keys
//   - [Snapshot.Edges]: (sourceType, sourceKey, relation, targetType,
//     targetKey)
//   - [Snapshot.EdgesFrom]: the order of [Snapshot.Edges], over one source
//   - [Snapshot.Duplicates]: (type, primaryKey, rejected instance's properties,
//     rejected instance's provenance)
//   - [Snapshot.Unresolved]: (sourceType, sourceKey, relation, targetType,
//     targetKey)
//
// Every arm that names a type compares the declaring schema's name, then the type
// name. A [schema.TypeID] holds the schema's source path, so an order by it would
// move when one schema text is loaded from another directory.
// A graph holds one edge or record per source, association and target, so no two
// edges and no two unresolved records tie. Each sort compares every arm its tuple
// names. A properties arm compares a rendering that writes each value as %T|%#v,
// so records that rendering makes equal tie in that arm.
// A provenance arm compares the source name, the path a document writes, and the
// span. Among the ties are float64(1) and int64(1) inside a list or map, and a
// zero and an empty Properties.
//
// Sorting is established when the Snapshot is constructed — by [Graph.Snapshot]
// and by the snapshot package's reader alike, which reach it through one place —
// and is amortized across accessor calls. Neither asks its caller to pre-sort, and
// neither can skip it.
//
// # Dependencies
//
//	graph  ──imports──▶  schema, instance, diag, immutable
//	graph  ──imports──▶  internal/value, internal/snapparts, internal/typeorder
package graph
