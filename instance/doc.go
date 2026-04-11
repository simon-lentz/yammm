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
// # Builder
//
// [Builder] and [EdgeBuilder] provide fluent construction of [RawInstance]
// values, useful for tests and non-adapter use cases:
//
//	raw := instance.NewInstance().
//	    Prop("id", "p1").
//	    Prop("name", "Alice").
//	    Edge("WORKS_AT").Target("c1").Done().
//	    Build()
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
//   - [WithLogger]: structured logger for validation diagnostics
//   - [WithStrictPropertyNames]: reject properties not defined in the schema
//   - [WithAllowUnknownFields]: allow extra properties without diagnostics
//   - [WithMaxIssuesPerInstance]: cap diagnostics per instance
//
// # Thread Safety
//
// [Validator] is stateless and safe for concurrent use. Multiple goroutines
// may call [Validator.Validate] simultaneously with different inputs.
//
// # Dependencies
//
//	instance  ──imports──▶  schema, diag, location, location/path, immutable
package instance
