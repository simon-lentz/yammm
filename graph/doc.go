// Package graph builds an in-memory data structure from validated instances.
//
// The graph package is the final layer in the YAMMM validation pipeline:
//
//	Schema → Instance Validation → Graph
//
// It handles:
//   - Primary key uniqueness (duplicate detection)
//   - Association edge resolution (forward references)
//   - Composition child extraction and indexing
//   - Completeness checking (required association validation)
//
// # Thread Safety
//
// [Graph] is safe for concurrent use. Multiple goroutines may call [Graph.Add]
// and [Graph.AddComposed] concurrently. The graph handles forward references
// and duplicate detection atomically using internal synchronization.
//
// [Snapshot] is an immutable snapshot; it is safe for concurrent read access
// from multiple goroutines.
//
// # Basic Usage
//
//	g := graph.New(s)
//
//	// Add validated instances (may be called concurrently)
//	result := g.Add(ctx, validInstance)
//	if !result.OK() {
//	    // Semantic error (duplicate PK, type not found)
//	}
//
//	// Check completeness
//	result = g.Check(ctx)
//	if !result.OK() {
//	    // Required associations are missing
//	}
//
//	// Get snapshot for inspection
//	snap := g.Snapshot()
//	for _, typeID := range snap.Types() {
//	    for _, inst := range snap.InstancesOf(typeID) {
//	        // Process instances
//	    }
//	}
//
// # Batch Assembly
//
// [BatchAssembler] composes a [github.com/simon-lentz/yammm/instance.Validator],
// a [Graph], and a [Snapshot] into a single call surface for the common
// pipeline pattern (validate → add → check → snapshot). The assembler
// encodes the ordering invariant so consumers cannot run Snapshot before
// Check, and is concurrent-safe by default — multiple goroutines may
// share one assembler:
//
//	ba := graph.NewBatchAssembler(ctx, s)
//	for i, rec := range records {
//	    if err := ba.Add("TypeName", buildRawInstance(rec)); err != nil {
//	        return fmt.Errorf("record %d: %w", i, err)
//	    }
//	}
//	res, err := ba.Finalize(ctx)
//	if err != nil {
//	    return fmt.Errorf("batch: %w", err)
//	}
//	// res.Snapshot is always non-nil; pass to Marshal / WriteFile / etc.
//
// One validator serves every goroutine, serialized through an internal
// mutex. See [BatchAssembler] for the full thread-safety contract.
//
// To resume assembly on top of a previously-persisted snapshot,
// construct the assembler with [NewBatchAssemblerFromSnapshot]: the
// underlying graph starts pre-populated via [NewFromSnapshot] semantics,
// and new Add calls resolve against — and may complete — the seeded
// state.
//
// # Alternative Constructors
//
// [NewFromSnapshot] creates a [Graph] pre-populated from a [Snapshot],
// for incremental building on top of persisted or previously-constructed
// state; new Add calls resolve against the imported instances.
// [NewBatchAssemblerFromSnapshot] is its assembler-level counterpart.
//
// [RebuildSnapshot] constructs a [Snapshot] from asserted parts (types,
// instances, edges, duplicates, unresolved records). It does not validate
// values, and rebuilt instances report Validated() == false, but it holds the
// parts to the structural facts below. This is the deserialization entry point
// used by the snapshot package.
//
// # Type Resolution
//
// Two lookups, and they answer different questions:
//
//   - [Graph.Add] resolves a root's type from its identity, restricted to the
//     same set as a matter of OWNERSHIP: a graph bound to a schema holds
//     instances of the types that schema declares or directly imports, and the
//     diagnostic's hint says so.
//   - A composed child resolves across the WHOLE import closure. Its type comes
//     from a relation the schema already resolved, and no ownership question
//     arises because the child arrived inside a parent this graph does own.
//
// A schema where an imported type composes a part type from a further import
// therefore loads, validates and builds. Resolving the child on the root's rule
// instead dropped its subtree and disabled sibling duplicate detection with it.
//
// No lookup in this package takes a rendered type name. Where two identities
// render alike, a diagnostic naming them falls back to the full identity rather
// than reading "X does not match X".
//
// # Root type eligibility
//
// A root instance's type must satisfy four members, and every entry point that
// installs or asserts a root applies the same four: [Graph.Add],
// [RebuildSnapshot], [NewFromSnapshot] and the snapshot reader. A type is
// ineligible when it
//
//   - is abstract, because an abstract type has no instances;
//   - is a part type, because a part instance is addressed through its parent
//     composition;
//   - declares no primary key, because it then has no address at all;
//   - is one the entry schema cannot name, because adapter/json and
//     adapter/csv key a root's output by that name and a type reached only
//     through an intermediate import has no name form.
//
// [schema.Addressable] is the fourth member, and [schema.AddressableTag] is the
// name it grants. Sharing one rule is what lets a writer read a root's tag
// without checking for a collision: two types the entry schema can name never
// render alike, so a name-keyed document can always separate the roots a
// snapshot holds.
//
// A composed child is held to none of the four. It is addressed through its
// parent, its type comes from a relation the schema already resolved, and it
// stays legal for any type in the closure — a transitively imported part type
// included.
//
// # Denoted type eligibility
//
// A snapshot DENOTES every type in [Snapshot.Types]. That set holds every
// root's type, and [SnapshotParts.Types] may add a type with no instances.
// Every denoted type is held to all four members above, whether or not the
// snapshot holds an instance of it.
//
// The reason is the writers. adapter/json and adapter/csv key their output by
// the name of each type the snapshot denotes, so a denoted type needs a name,
// and a key they emit must be one the instance validator and the generated
// aggregate read back. The validator refuses an abstract or a part type's name even for
// an empty batch, so a snapshot that denoted one would write a document its own
// readers refuse.
//
// # Structural facts
//
// [Graph.Add] never builds a snapshot that breaks the facts below, and every
// other constructor of a snapshot refuses one that does: [RebuildSnapshot] over
// parts, and the snapshot package's reader over a document. [NewFromSnapshot]
// and [NewBatchAssemblerFromSnapshot], over a snapshot bound to another schema,
// refuse one that breaks any fact but Records, and derive its records again.
// The facts are the whole promise: a shape they do not name, such as a
// property value its constraint refuses, can pass a constructor. The facts are:
//
//   - Identity. Every type identity resolves in the schema.
//   - Root and denoted types. See the two sections above.
//   - Composition slots. A slot is a composition its parent's type declares,
//     holds instances of its declared target, holds one child at most under a
//     (one) composition, and holds each canonical key of a keyed part once.
//   - Keys. A keyed instance's stored key has one component per declared
//     primary key, each a scalar [ParseKey] reads back, and each agrees with
//     its key property, which is present and not null. Two roots of one type never share a canonical
//     address.
//   - Names. Every stored property and edge property name is declared.
//   - Associations. Every edge and unresolved record is under an association
//     its source's type declares. An edge's target is an instance of that
//     association's declared target, and a record's target type is that
//     declared target, derived from the association, never taken from a
//     constructor's input. A record carries a target key of
//     the target's arity, each component a scalar [ParseKey] reads back, where
//     it names a target, and states a reason [UnresolvedEdge] documents; an
//     "absent" or "empty" record carries no target key and no edge
//     properties. A (one) association holds one record at most, edges and
//     unresolved records together.
//   - Records. The records are the ones [Graph.Add] derives from the data.
//     Every root holds an edge or a record under each required association
//     of its type. An "absent" or "empty" record stands alone, once, and only
//     under a required association. A "target_missing" record names a target
//     no root holds.
//   - Duplicates. A duplicate's instance holds no composed children, and a
//     root duplicate's type can hold a root. Its conflict is derived from its
//     position, never taken from a constructor's input: for a root duplicate, the root at its own type and key; for a
//     composed duplicate, whose type is its composition's declared target,
//     the sole child of a (one) slot or the child of a keyed (many) slot at
//     its own key. A duplicate with no such conflict, as under a keyless
//     (many) slot, breaks the fact.
//
// Whether an association is required is read from the schema, never taken from
// a constructor's input:
// [UnresolvedEdge.Required] is derived by every constructor, so a snapshot
// imported under a schema that relaxes an association reports it under that
// schema's rule.
//
// A snapshot bound to the importing schema already holds to every fact, so
// the import walks only a snapshot built against another schema. That import
// derives the records again, as [Graph.Add] derives them from the snapshot's
// data under the importing schema. A "target_missing" record whose target the
// snapshot holds becomes an edge. An "absent" or "empty" record under an
// association the schema makes optional, or an "absent" record under one it
// drops, is dropped. A root holding no edge and no record under an association
// the schema makes required gains an "absent" record there. A snapshot records
// nothing for an empty list under an optional association, so that gained
// record reads "absent" where [Graph.Add], given the list, records "empty". A
// "target_missing" record names a target as an edge does, so under an
// association the schema retargets both are refused.
//
// # Build Then Commit
//
// [Graph.Add] and [Graph.AddComposed] walk an instance ONCE. That walk both
// checks the whole structure — edge names and multiplicities, and every slot
// and child of the composition tree at any depth — and assembles the [Instance]
// tree to install, touching no graph state. Only a walk that raised no error
// reaches the commit phase, which installs the tree, the staged association
// records and the attestation together under the graph's lock.
//
// A non-OK result therefore installs nothing of the record — no instance, child
// or edge — and does so structurally rather than by two functions agreeing. A
// rejection is itself recorded: a root [Graph.Add] refuses at its key, or a
// child [Graph.AddComposed] refuses at its slot, in [Snapshot.Duplicates], and
// every refusal's issue in [Snapshot.Diagnostics]. The alternative,
// installing first and rejecting during the walk, left a record in the graph
// that the caller had been told had failed: [BatchAssembler.Count]
// under-reported against a snapshot that held the instance, and a retry of that
// record drew E_DUPLICATE_PK. The shape after that, checking in one walk and
// attaching in another, kept the same four predicates in two places and let the
// attaching walk skip in silence exactly what the checking walk rejected.
//
// The check runs for every instance, not only for one reporting
// [instance.ValidInstance.Validated] == false. Two reasons: the graph cannot
// verify that bit, since no exported constructor sets it and a validator
// defect would set it on malformed data; and the validator has held a hole in
// one of these very rules — a (one) composition accepted an array of any
// length — which this guard was the only thing to catch. A trust boundary
// that deletes the check which found the last defect is not a trust boundary.
//
// # Composed Children
//
// A composed child is checked as a root is — its edge names and
// multiplicities, its own key, and its own composition tree — but its
// association edges are never staged, so their target keys go unjudged, and
// never installed. A part type that declares or
// inherits an association therefore produces no [Edge], no
// [UnresolvedEdge], and no effect on [Attestation]'s Associations dimension.
// The check still runs, so data filed under a name the part type does not
// declare is reported rather than dropped in silence; what does not happen is
// the edge resolution a root gets.
//
// # Key Types
//
// [BatchAssembler] composes Validator + Graph + Snapshot for the
// validate→add→check→snapshot pipeline pattern; constructed via
// [NewBatchAssembler] (or [NewBatchAssemblerFromSnapshot], seeding from
// a prior snapshot) and finalized via [BatchAssembler.Finalize], which
// returns a [FinalizeResult] whose Snapshot field is always non-nil.
// [ErrAssemblerFinalized] is the sentinel returned from Add / AddValid
// after Finalize.
//
// [Instance] provides immutable access to a graph node's data:
// [Instance.TypeName], [Instance.PrimaryKey], [Instance.Properties],
// [Instance.Composed], [Instance.ComposedRelations], [Instance.Provenance].
//
// [Edge] represents a resolved association between two instances:
// [Edge.Relation], [Edge.Source], [Edge.Target], [Edge.Properties].
//
// [UnresolvedEdge] records an association whose target was not found at
// graph-construction time (absent, empty, or target_missing). Edge
// properties declared on the forward reference survive Marshal/Load
// symmetric with [Edge.Properties] and are accessed via
// [UnresolvedEdge.Property] and [UnresolvedEdge.Properties]. The .ys
// wire format carries these through the version-2 "properties" field
// on unresolved-edge wire entries; see the snapshot package for
// format-version semantics.
//
// # Graph Options
//
// [New] and [NewFromSnapshot] accept [Option] values. [WithLogger] attaches a
// structured logger to the graph's operation boundaries — Add, AddComposed and
// Check each open and close a traced operation, and edge resolution, forward
// references, duplicate primary keys and unresolved required associations are
// logged as they occur. With no logger, every trace call returns immediately.
// Symmetric with [github.com/simon-lentz/yammm/schema.WithLogger].
//
// # Type Identity and Type Names
//
// A type is identified by [github.com/simon-lentz/yammm/schema.TypeID] — its
// declaring schema path plus its name. A type NAME is a rendering of that
// identity, in canonical instance tag form:
//
//   - Local types: unqualified name (e.g., "Person")
//   - Imported types: alias-qualified name (e.g., "c.Entity")
//
// The rendering is lossy, and only ever suitable for display. A type reachable
// only through an intermediate import has no alias to qualify with and renders
// bare, so it can collide with a local type of the same name; two same-named
// types in different schemas render identically. Keying anything by a rendering
// therefore merges types that are not the same type, silently and before any
// diagnostic can see it.
//
// A snapshot's ROOTS are the exception, and by construction rather than by
// coincidence: each is a type the bound schema can name, so no two of them
// render alike and an output document may key them by name. See "Root type
// eligibility" above, and [schema.AddressableTag] for the name itself.
//
// So every place that must denote a type exactly takes an identity:
//
//   - [Snapshot.Types]
//   - [Snapshot.InstancesOf]
//   - [Snapshot.InstanceByKey]
//   - [Graph.AddComposed]'s parentType parameter
//   - [SnapshotParts.Types], and [InstanceParts.TypeID], which files a root
//   - the type fields on [EdgeParts], [DuplicateParts] and [UnresolvedParts]
//   - [UnresolvedEdge.TargetType]
//
// [Instance.TypeName] still carries the rendered name beside the identity
// ([Instance.TypeID]). Every constructor renders it from the identity under the
// bound schema with [github.com/simon-lentz/yammm/schema.TagForm], which is
// lossy as above: a root's name names only its type, and a composed child's
// may match another type's. Use
// [github.com/simon-lentz/yammm/schema.TagForm] to render an identity where
// output needs a name — Cypher labels, CSV filenames, JSON object keys.
//
// # Key Formatting
//
// Primary keys are represented as canonical JSON array strings for
// map indexing and diagnostic messages:
//
//	graph.FormatKey("ABC123")       // ["ABC123"]
//	graph.FormatKey("us", 12345)    // ["us",12345]
//
// Use [FormatKey] to construct lookup keys for [Snapshot.InstanceByKey]. A
// Timestamp, Date or UUID component may be spelled any way its constraint
// accepts: every address the graph receives is canonicalized under the type's
// key constraints before the lookup, as the key was at entry.
//
// A part type may declare a primary key, which a composed child carries and
// the graph checks and uses to tell siblings apart. It is not an address: a
// part instance is found through its parent composition, so
// [Snapshot.InstanceByKey] never returns one and this package mints none; a
// store that must give each part node an identity derives it, and
// [github.com/simon-lentz/yammm/adapter/neo4j] is the only one that does.
//
// # Error Handling
//
// Graph operations return [diag.Result]:
//
//   - [diag.Result.HasFatal]: context cancellation (E_CONTEXT_CANCELLED), or
//     a broken invariant (E_INTERNAL)
//   - [diag.Result.HasErrors]: semantic failure (duplicate PK, type not found)
//   - [diag.Result.OK]: success (may have warnings)
//
// Programmer errors (nil receiver, nil instance, schema mismatch) panic.
//
// [Graph.Add] emits the following. The first four judge a root's type in the
// order listed, and a type breaking two draws the first:
//
//   - E_GRAPH_TYPE_NOT_FOUND: a root's type is not declared by this graph's
//     schema or a direct import
//   - E_GRAPH_MISSING_PK: the type declares no primary key
//   - E_GRAPH_INVALID_COMPOSITION: a part type was added directly, or a
//     composed child is not an instance of its relation's target type
//   - E_GRAPH_ABSTRACT_TYPE: the type is abstract
//   - E_GRAPH_INVALID_PK: a primary key — the instance's own, or a composed
//     child's of a keyed part type — is empty, has the wrong arity, holds a
//     component [ParseKey] cannot read back, has a key
//     property that is absent or null, or disagrees with its own key
//     property; or an association target key has the wrong arity or a
//     component [ParseKey] cannot read back
//   - E_GRAPH_CARDINALITY: a (one) association carries several targets
//   - E_GRAPH_UNKNOWN_RELATION: edge data or composed children arrived under a
//     name the type does not declare in that slot
//   - E_UNKNOWN_FIELD, E_UNKNOWN_EDGE_FIELD: a property or edge property name
//     the type or association does not declare
//   - E_DUPLICATE_COMPOSED_PK: a (one) composition carries several children,
//     or two children of one (many) slot share a primary key
//   - E_DUPLICATE_PK: the primary key already exists for this type
//   - E_CONTEXT_CANCELLED: the context was cancelled
//
// [Graph.AddComposed] emits:
//
//   - E_GRAPH_TYPE_NOT_FOUND: the PARENT's identity is not in the schema's
//     import closure, or is the zero TypeID
//   - E_GRAPH_PARENT_NOT_FOUND: no instance carries that identity and key
//   - E_GRAPH_INVALID_COMPOSITION: the named relation is not a composition, or
//     the child is not an instance of its target type
//   - E_GRAPH_INVALID_PK: the child's key, or a descendant's, is empty, has
//     the wrong arity, holds a component [ParseKey] cannot read back, has a
//     key property that is absent or null, or disagrees with its own key
//     property
//   - E_DUPLICATE_COMPOSED_PK: the parent's (one) slot already holds a child,
//     or its keyed (many) slot holds a sibling at the child's key
//   - E_GRAPH_CARDINALITY, E_GRAPH_UNKNOWN_RELATION, E_DUPLICATE_COMPOSED_PK,
//     E_GRAPH_INVALID_COMPOSITION, E_UNKNOWN_FIELD, E_UNKNOWN_EDGE_FIELD: from
//     the child's own structure and its descendants', judged by the walk a
//     root's composition tree takes
//   - E_CONTEXT_CANCELLED: the context was cancelled
//
// It does NOT emit E_GRAPH_MISSING_PK, E_GRAPH_ABSTRACT_TYPE or
// E_DUPLICATE_PK; those three belong to a root.
//
// A composed child whose identity is not in the import closure is an
// invariant guard on both paths, Fatal E_INTERNAL: the child must already
// equal its relation's target, which the schema resolved at load, and a
// child from outside the closure is refused first: by the target check on the
// inline path, and by [Graph.AddComposed]'s schema guard on its own. No public
// constructor reaches it.
//
// [Graph.Check] emits E_UNRESOLVED_REQUIRED and E_CONTEXT_CANCELLED.
//
// [RebuildSnapshot] and the import report a broken structural fact with the
// code [Graph.Add] reports it with. The one exception is at [RebuildSnapshot]:
// parts that break the identity, root, denoted-type, name, address, reason,
// records or duplicate rule come only from a broken caller, so it reports them
// as Fatal E_INTERNAL. [RebuildSnapshot] panics on a nil schema, as [New]
// does.
//
// # Diagnostics Lifecycle
//
// [Snapshot.Diagnostics] returns the cumulative issues from [Graph.Add] and
// [Graph.AddComposed] calls. These are construction-time diagnostics that
// accumulate as instances are added to the graph.
//
// [Graph.Check] operates differently: it returns a fresh [diag.Result] per
// call without affecting [Snapshot.Diagnostics]. This makes Check idempotent—
// calling it multiple times returns identical results without accumulating
// issues into the snapshot.
//
// Design rationale: This separation allows users to call Check() freely
// (for logging, validation gates, or debugging) without polluting the
// snapshot's construction diagnostics.
//
// # Ordering Guarantees
//
// All slice-returning [Snapshot] methods produce deterministically sorted output,
// independent of [Graph.Add] call order or concurrency:
//
//   - [Snapshot.Types]: lexicographic by TypeID (schema path, then name)
//   - [Snapshot.InstancesOf]: lexicographic by primary key string
//   - [Snapshot.Edges]: (sourceType, sourceKey, relation, targetType,
//     targetKey, edge properties)
//   - [Snapshot.Duplicates]: (type, primaryKey, relation, conflictType,
//     conflictKey, parent slot, rejected instance's properties, rejected
//     instance's provenance)
//   - [Snapshot.Unresolved]: (sourceType, sourceKey, relation, targetType,
//     targetKey, reason, required, edge properties)
//
// Each tuple is total: every arm is compared, so no two distinct records tie
// and inherit map-iteration order.
//
// Sorting is established when the Snapshot is constructed — by [Graph.Snapshot]
// and by [RebuildSnapshot] alike, which reach it through one place — and is
// amortized across accessor calls. Neither constructor asks its caller to
// pre-sort, and neither can skip it.
//
// # Streaming Scenarios
//
// For streaming scenarios where compositions arrive after their parent,
// [Graph.AddComposed] attaches children to existing parents. There are
// important limitations to understand:
//
// Supported:
//
//   - Adding children to any top-level parent (added via [Graph.Add])
//   - Mixed inline and streamed children (inline added first, streamed later)
//   - Nested inline compositions (grandchildren included in streamed child)
//
// Not Supported:
//
//   - Streaming grandchildren to composed children (nested streaming)
//   - Addressing a composed parent by a key of its own
//
// For nested compositions, include the full composition tree inline in the
// [instance.ValidInstance] passed to AddComposed. The graph will recursively
// extract and attach all nested children.
//
// Example of supported pattern:
//
//	// Parent added to graph
//	g.Add(ctx, parentInstance)
//
//	// Child streamed later, with nested GrandChild inline
//	childInstance := ... // includes GrandChild in composed property
//	g.AddComposed(ctx, parentTypeID, parentKey, "children", childInstance)
//	// Both Child and GrandChild are now attached
//
// # Dependencies
//
//	graph  ──imports──▶  schema, instance, diag, location, immutable
//	graph  ──imports──▶  internal/trace, internal/value
package graph
