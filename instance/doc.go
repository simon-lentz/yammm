// Package instance provides instance validation for yammm schemas.
//
// This package transforms raw, unvalidated instance data into typed, validated
// [ValidInstance] objects suitable for graph ingestion. It is the primary entry
// point for validating data against compiled schemas.
//
// # Overview
//
// The validation pipeline takes [RawInstance] objects and produces
// [ValidInstance] objects (on success) along with a [diag.Result] containing
// any diagnostics. The [Validator] orchestrates this process using a
// compiled [schema.Schema].
//
//	validator := instance.NewValidator(compiledSchema)
//	valids, result := validator.Validate(ctx, "Person", raws)
//
// # Instance Types
//
// [RawInstance] represents unvalidated input data with optional provenance
// information for error reporting. [ValidInstance] is the immutable output
// containing typed properties, validated edges, and the extracted primary key.
//
// These two types use different access patterns by design:
//   - [RawInstance] uses public struct fields (Properties, Provenance) because
//     callers construct it directly — it is an input DTO.
//   - [ValidInstance] uses accessor methods because the library constructs it —
//     it is an immutable output type whose internals are not caller-settable.
//
// # SchemaBuilder
//
// [BuilderFor] and [SchemaBuilder] provide fluent, schema-aware construction
// of [RawInstance] values. Unlike a free-form map literal, SchemaBuilder
// validates property names, relation names, and cardinality at
// [SchemaBuilder.Build] time with call-site
// file:line locators — shifting shape errors out of ValidateOne's domain:
//
//	b, err := instance.BuilderFor(s, "Person")
//	if err != nil {
//	    return instance.RawInstance{}, err
//	}
//	raw, err := b.
//	    Property("id", "p1").
//	    Property("name", "Alice").
//	    EdgeTo("works_at", "c1").
//	    Build()
//	if err != nil {
//	    return instance.RawInstance{}, err
//	}
//
// # Validation Semantics
//
// The validator performs:
//   - Type resolution (qualified and unqualified type names)
//   - Property type validation against schema constraints
//   - Required property enforcement
//   - Primary key extraction and validation
//   - Edge object validation (associations and compositions)
//   - Invariant expression evaluation
//
// [Validator.ValidateForComposition] is a second entry point for streaming
// composition scenarios where composed children are validated separately
// from their parent.
//
// # Validator Options
//
// [NewValidator] accepts [Option] values to configure behavior:
//
//   - [WithStrictPropertyNames]: require exact-case property names
//   - [WithAllowUnknownFields]: allow extra properties without diagnostics
//   - [WithIssueLimit]: cap the issues one instance stores
//   - [WithLogger]: receive a debug record when a property name is normalized
//
// # Value Functions
//
// [CheckValue] and [CanonicalValue] are the validator's two per-value rules —
// does this value conform, and what is its stored form — exported for a
// boundary this library does not own: a hand-built export, a direct-Cypher
// parameter map, a pre-filter in front of a write the validator never sees.
// Both accept nil.
//
// # Thread Safety
//
// [Validator] is stateless and safe for concurrent use. Multiple goroutines
// may call [Validator.Validate] simultaneously with different inputs.
// [SchemaBuilder] is NOT concurrent-safe; construct one per goroutine.
//
// # Dependencies
//
//	instance  ──imports──▶  schema, diag, location, location/path, immutable,
//	                        instance/internal/eval, internal/value
package instance
