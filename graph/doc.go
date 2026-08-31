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
// instances, edges, duplicates, unresolved records) without validation;
// rebuilt instances report Validated() == false. This is the
// deserialization entry point used by the snapshot package.
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
// # Build Then Commit
//
// [Graph.Add] and [Graph.AddComposed] walk an instance ONCE. That walk both
// checks the whole structure — edge names and multiplicities, and every slot
// and child of the composition tree at any depth — and assembles the [Instance]
// tree to install, touching no graph state. Only a walk that raised no error
// reaches the commit phase, which takes the lock and installs the tree, the
// staged association records and the attestation together.
//
// A non-OK result therefore leaves the graph unchanged, and does so
// structurally rather than by two functions agreeing. The alternative,
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
// declaring schema path plus its name. A type *name* is a rendering of that
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
// So every place that must denote a type exactly takes an identity:
//
//   - [Snapshot.Types]
//   - [Snapshot.InstancesOf]
//   - [Snapshot.InstanceByKey]
//   - [Graph.AddComposed]'s parentType parameter
//   - [SnapshotParts.Types] and [SnapshotParts.Instances] map keys
//   - the type fields on [EdgeParts], [DuplicateParts] and [UnresolvedParts]
//   - [UnresolvedEdge.TargetType]
//
// [Instance.TypeName] still carries the rendered name, because an instance
// carries its identity beside it ([Instance.TypeID]) and the name is what a
// document was written with. Use
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
// Use [FormatKey] to construct lookup keys for [Snapshot.InstanceByKey].
//
// For composed children, [FormatComposedKey] encodes an identity that handles
// all special characters safely. It has no inverse: composed identities are
// rendered by the writers and read back by nobody.
//
// # Error Handling
//
// Graph operations return [diag.Result]:
//
//   - [diag.Result.HasFatal]: context cancellation (E_CONTEXT_CANCELLED)
//   - [diag.Result.HasErrors]: semantic failure (duplicate PK, type not found)
//   - [diag.Result.OK]: success (may have warnings)
//
// Programmer errors (nil receiver, nil instance, schema mismatch) panic.
//
// [Graph.Add] emits:
//
//   - E_GRAPH_TYPE_NOT_FOUND: a root's type is not declared by this graph's
//     schema or a direct import, or a composed child's type is not in the
//     import closure at all
//   - E_GRAPH_MISSING_PK: the type declares no primary key
//   - E_GRAPH_INVALID_COMPOSITION: a part type was added directly, or a
//     composed child is not an instance of its relation's target type
//   - E_GRAPH_ABSTRACT_TYPE: the type is abstract
//   - E_GRAPH_INVALID_PK: a primary key — the instance's own, or a composed
//     child's of a keyed part type — is empty, has the wrong arity, or
//     disagrees with its own key property
//   - E_GRAPH_CARDINALITY: a (one) association carries several targets
//   - E_GRAPH_UNKNOWN_RELATION: edge data or composed children arrived under a
//     name the type does not declare in that slot
//   - E_DUPLICATE_COMPOSED_PK: a (one) composition carries several children,
//     or two children of one (many) slot share a primary key
//   - E_DUPLICATE_PK: the primary key already exists for this type
//   - E_CONTEXT_CANCELLED: the context was cancelled
//
// [Graph.AddComposed] emits E_GRAPH_PARENT_NOT_FOUND when the parent instance
// is absent, E_GRAPH_INVALID_COMPOSITION when the named relation is not a
// composition, E_DUPLICATE_COMPOSED_PK when the slot already holds the child
// it can hold, and otherwise the same codes as Add — its child runs the same
// structural guard.
//
// [Graph.Check] emits E_UNRESOLVED_REQUIRED and E_CONTEXT_CANCELLED.
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
//     conflictKey, parent slot, rejected instance's properties)
//   - [Snapshot.Unresolved]: (sourceType, sourceKey, relation, targetType,
//     targetKey, reason, required, edge properties)
//
// Each tuple is total: every arm is compared, so no two distinct records tie
// and inherit map-iteration order.
//
// Sorting is performed at [Graph.Snapshot] time, amortized across accessor calls.
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
//   - Addressing composed parents via [FormatComposedKey]
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
