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
//	for _, typeName := range snap.Types() {
//	    for _, inst := range snap.InstancesOf(typeName) {
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
//	ba := graph.NewBatchAssembler(ctx, s,
//	    graph.WithValidatorOptions(instance.RecommendedOptions()...))
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
// CPU-bound consumers that profile validation as the hot path can opt
// into [WithValidatorPool] for goroutine-parallel validator access; the
// default mutex-serialized mode is correct for I/O-bound consumers.
// See [BatchAssembler] for the full thread-safety contract.
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
// [RebuildSnapshot] constructs a [Snapshot] from pre-validated parts
// (types, instances, edges, duplicates, unresolved records). This is the
// deserialization entry point used by the snapshot package.
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
// [graph.New] accepts [Option] values:
//
//   - [WithLogger]: structured logger for graph construction diagnostics
//
// # Type Names
//
// All string-based type name parameters and return values use the
// canonical instance tag form:
//
//   - Local types: unqualified name (e.g., "Person")
//   - Imported types: alias-qualified name (e.g., "c.Entity")
//
// This applies to:
//
//   - [Snapshot.Types]
//   - [Snapshot.InstancesOf]
//   - [Snapshot.InstanceByKey]
//   - [Snapshot.Instances] map keys
//   - [Instance.TypeName]
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
// For composed children, [FormatComposedKey] and [ParseComposedKey] provide
// identity encoding that handles all special characters safely.
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
// Semantic issues use diag.Code values like E_DUPLICATE_PK, E_UNRESOLVED_REQUIRED.
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
//   - [Snapshot.Types]: lexicographic by type name
//   - [Snapshot.InstancesOf]: lexicographic by primary key string
//   - [Snapshot.Edges]: lexicographic tuple (sourceType, sourceKey, relation, targetType, targetKey)
//   - [Snapshot.Duplicates]: lexicographic by (typeName, primaryKey)
//   - [Snapshot.Unresolved]: lexicographic by (sourceType, sourceKey, relation, targetType, targetKey)
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
//	g.AddComposed(ctx, "Parent", parentKey, "children", childInstance)
//	// Both Child and GrandChild are now attached
//
// # Dependencies
//
//	graph  ──imports──▶  schema, instance, diag, location, immutable
package graph
