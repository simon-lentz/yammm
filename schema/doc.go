// Package schema provides the public API for loading and querying YAMMM schemas.
//
// # Overview
//
// This package implements the schema layer, which is responsible for:
//   - Loading and parsing schema definitions from *.yammm files
//   - Building schemas programmatically using the Builder API
//   - Providing an immutable, thread-safe schema representation
//   - Supporting cross-schema imports and inheritance
//
// # Loading Schemas
//
// Schemas are loaded using the schema/load package:
//
//	// Load from file
//	s, result, err := load.Load(ctx, "schema.yammm")
//
//	// Load from string
//	s, result, err := load.LoadString(ctx, source, "schema.yammm")
//
//	// Load from multiple sources
//	s, result, err := load.LoadSources(ctx, sources, moduleRoot)
//
// The triple-return pattern ensures diagnostics are always available via
// diag.Result, while err signals catastrophic failures (I/O, cancellation).
// Check result.HasErrors() to determine semantic success.
//
// # Immutability
//
// All schema types are immutable after loading. This provides:
//   - Thread-safety for concurrent access
//   - Predictable behavior (no hidden mutations)
//   - Safe sharing across goroutines
//
// Slice accessors return defensive copies. Use iterators (iter.Seq) for
// zero-allocation traversal when possible.
//
// # Completion and Sealing
//
// Schemas undergo a completion phase that resolves all internal references:
//   - Import declarations are resolved to their target schemas
//   - Type inheritance is computed (LinearizedAncestors)
//   - Property collisions are detected across the inheritance hierarchy
//   - Alias constraints are resolved to their underlying types
//
// After completion, schemas are sealed to prevent further mutation. The
// schema/load package handles completion automatically. The schema/build
// package completes the schema when Build() is called. All types and
// constraints are immutable and thread-safe after sealing.
//
// # Type Identity
//
// Types are identified by TypeID, a tuple of (SourceID, name). This enables
// cross-schema type resolution and proper handling of imported types.
// Two types are equal if and only if they have the same TypeID.
package schema
