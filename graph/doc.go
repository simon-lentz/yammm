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
// [Result] is an immutable snapshot; it is safe for concurrent read access
// from multiple goroutines.
//
// # Basic Usage
//
//	g := graph.New(schema)
//
//	// Add validated instances (may be called concurrently)
//	result, err := g.Add(ctx, validInstance)
//	if err != nil {
//	    // Internal error (nil instance, context cancellation)
//	}
//	if !result.OK() {
//	    // Validation error (duplicate PK, type not found)
//	}
//
//	// Check completeness
//	result, err = g.Check(ctx)
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
//   - [Result.Types]
//   - [Result.InstancesOf]
//   - [Result.InstanceByKey]
//   - [Result.Instances] map keys
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
// Use [FormatKey] to construct lookup keys for [Result.InstanceByKey].
//
// For composed children, [FormatComposedKey] and [ParseComposedKey] provide
// identity encoding that handles all special characters safely.
//
// # Error Handling
//
// Graph operations follow the (Output, diag.Result, error) pattern:
//
//   - error != nil: Internal failure (nil receiver, nil instance, cancellation)
//   - error == nil && !result.OK(): Semantic failure (duplicate PK, type not found)
//   - error == nil && result.OK(): Success (may have warnings)
//
// Internal errors use the ErrInternal sentinel and related error types.
// Semantic issues use diag.Code values like E_DUPLICATE_PK, E_UNRESOLVED_REQUIRED.
//
// # Diagnostics Lifecycle
//
// [Result.Diagnostics] returns the cumulative issues from [Graph.Add] and
// [Graph.AddComposed] calls. These are construction-time diagnostics that
// accumulate as instances are added to the graph.
//
// [Graph.Check] operates differently: it returns a fresh [diag.Result] per
// call without affecting [Result.Diagnostics]. This makes Check idempotent—
// calling it multiple times returns identical results without accumulating
// issues into the snapshot.
//
// Design rationale: This separation allows users to call Check() freely
// (for logging, validation gates, or debugging) without polluting the
// snapshot's construction diagnostics.
//
// # Ordering Guarantees
//
// All slice-returning [Result] methods produce deterministically sorted output,
// independent of [Graph.Add] call order or concurrency:
//
//   - [Result.Types]: lexicographic by type name
//   - [Result.InstancesOf]: lexicographic by primary key string
//   - [Result.Edges]: lexicographic tuple (sourceType, sourceKey, relation, targetType, targetKey)
//   - [Result.Duplicates]: lexicographic by (typeName, primaryKey)
//   - [Result.Unresolved]: lexicographic by (sourceType, sourceKey, relation, targetType, targetKey)
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
package graph
