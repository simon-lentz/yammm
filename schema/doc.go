// Package schema provides the public API for loading and querying YAMMM schemas.
//
// # Overview
//
// This package implements the schema layer, which is responsible for:
//   - Loading and parsing schema definitions from *.yammm files
//   - Building schemas programmatically using the [Builder] API
//   - Providing an immutable, thread-safe schema representation
//   - Supporting cross-schema imports and inheritance
//
// # Key Types
//
// The schema object model consists of:
//
//   - [Schema]: Top-level container with types, data types, and imports
//   - [Type]: Named type with properties, relations, invariants, and inheritance
//   - [Property]: Typed property with optionality, primary key, and constraint metadata
//   - [Relation]: Association (reference) or composition (ownership) between types
//   - [Invariant]: Named boolean constraint expression evaluated at validation time
//   - [DataType]: Named constraint alias (e.g., "type Email = String /pattern/")
//   - [Import]: Cross-schema import declaration with alias
//   - [TypeID]: Canonical type identity tuple (SourceID, name) for cross-schema resolution
//   - [TypeRef]: Syntactic type reference preserving user's original syntax
//
// # Loading Schemas
//
// Schemas are loaded using the Load family of functions:
//
//	// Load from file
//	s, result := schema.Load(ctx, "schema.yammm")
//
//	// Load from string (imports disallowed)
//	s, result := schema.LoadString(ctx, source, "schema.yammm")
//
//	// Load from multiple in-memory sources
//	s, result := schema.LoadSources(ctx, sources, moduleRoot)
//
//	// Load with explicit entry point (useful for LSP)
//	s, result := schema.LoadSourcesWithEntry(ctx, sources, entryPath, moduleRoot)
//
// A non-OK result with a nil Schema indicates failure. Check result.HasFatal()
// for I/O or cancellation errors. Check result.HasErrors() to determine
// semantic success.
//
// # Load Options
//
// Load functions accept [LoadOption] values to configure behavior:
//
//   - [WithRegistry]: schema registry for cross-schema type resolution
//   - [WithModuleRoot]: root directory for module-style imports
//   - [WithIssueLimit]: maximum diagnostic issues to collect (default 100)
//   - [WithSourceRegistry]: source registry for position tracking
//   - [WithDisallowImports]: prevent import processing
//   - [WithLogger]: structured logger for diagnostics
//
// # Builder API
//
// [NewBuilder] provides programmatic schema construction:
//
//	s, result := schema.NewBuilder().
//	    WithName("People").
//	    AddType("Person").
//	        WithPrimaryKey("id", schema.NewUUIDConstraint()).
//	        WithProperty("name", schema.StringLenBetween(1, 100)).
//	        WithOptionalProperty("age", schema.IntegerBetween(0, 150)).
//	        Done().
//	    Build()
//
// The builder completes and seals the schema when [Builder.Build] is called.
//
// # Constraint System
//
// Properties carry typed constraints that define valid value ranges. The
// [Constraint] interface is implemented by concrete types for each DSL data
// type: [StringConstraint], [IntegerConstraint], [FloatConstraint],
// [BooleanConstraint], [TimestampConstraint], [DateConstraint],
// [UUIDConstraint], [EnumConstraint], [PatternConstraint],
// [VectorConstraint], [ListConstraint], and [AliasConstraint].
//
// Each constraint type has constructors for bounded and unbounded variants
// (e.g., [NewStringConstraint], [StringLenBetween]). Use [ResolveAlias] to
// unwrap alias chains.
//
// # Registry
//
// [Registry] provides thread-safe, append-only storage for cross-schema type
// resolution. Register schemas after loading and pass the registry to
// subsequent [Load] calls via [WithRegistry]:
//
//	reg := schema.NewRegistry()
//	reg.Register(baseSchema)
//	s, result := schema.Load(ctx, "derived.yammm", schema.WithRegistry(reg))
//
// # Shared-Registry Semantics
//
// Passing the same *Registry to multiple Load calls in one process is safe
// and efficient (since v0.3.0). Two coordinated behaviors make shared-Registry
// usage first-class:
//
//   - [Registry.Register] is idempotent for exact-match: registering the same
//     SourceID twice with identical [StructuralHash] is a no-op. Divergent
//     content under the same SourceID still errors loudly with both hashes
//     reported in the diagnostic message.
//   - loadImport short-circuits cross-Load via the shared Registry: when an
//     import's SourceID is already registered, the loader reuses the existing
//     *Schema pointer and skips the parse, compile, and re-register pipeline.
//     This is where cross-pipeline schema-caching actually pays off — the
//     idempotence contract alone only makes the final re-register a no-op
//     after an unnecessary parse has already happened.
//
// The default Load behavior (fresh Registry per call when WithRegistry is
// absent) is unchanged. See [WithRegistry] for the top-level-reparse
// asymmetry note and the SourceID-discipline caveat.
//
// # Structural Hash
//
// [StructuralHash] computes a deterministic SHA-256 hash of a schema's
// structure (types, properties, relations, constraints). Used by the
// snapshot package for compatibility verification.
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
//   - Type inheritance is computed ([Type.SuperTypes])
//   - Property collisions are detected across the inheritance hierarchy
//   - Alias constraints are resolved to their underlying types
//
// After completion, schemas are sealed to prevent further mutation. The
// [Load] family handles completion automatically. The [NewBuilder] API
// completes the schema when [Builder.Build] is called.
//
// # Type Identity
//
// Types are identified by [TypeID], a tuple of (SourceID, name). This enables
// cross-schema type resolution and proper handling of imported types.
// Two types are equal if and only if they have the same TypeID.
//
// # Thread Safety
//
// All schema types are immutable and safe for concurrent read access after
// loading. [Registry] is safe for concurrent use (read-write).
//
// # Dependencies
//
//	schema  ──imports──▶  location, diag, immutable
package schema
