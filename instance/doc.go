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
// containing typed properties, validated edges and composed children, the
// extracted primary key, and the provenance. It holds the [schema.Type] that
// validated it, one handle of the validator's schema load, and
// [github.com/simon-lentz/yammm/graph.Graph.Add] panics on a handle of another load.
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
// validates property names, relation names, cardinality and each edge's shape at
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
// The validator performs, among other checks:
//   - Type resolution (qualified and unqualified type names)
//   - Property type validation against schema constraints
//   - Required property enforcement
//   - Primary key extraction and validation
//   - Edge object validation (associations and compositions)
//   - Invariant expression evaluation
//
// A key no member claims draws E_UNKNOWN_FIELD, or E_UNKNOWN_EDGE_FIELD in an
// edge object, unless [WithAllowUnknownFields] is set. Without
// [WithStrictPropertyNames], keys that fold onto one member draw
// E_CASE_FOLD_COLLISION, and the collision is its member's one issue: a
// collided required property, foreign-key field, required edge property or
// required composition is not also missing.
//
// # Validator Options
//
// [NewValidator] accepts [Option] values to configure behavior:
//
//   - [WithStrictPropertyNames]: require exact-case property names
//   - [WithAllowUnknownFields]: allow extra properties without diagnostics
//   - [WithIssueLimit]: cap the issues one instance stores
//
// # Thread Safety
//
// [Validator] is stateless and safe for concurrent use. Multiple goroutines
// may call [Validator.Validate] simultaneously with different inputs.
// [SchemaBuilder] is NOT concurrent-safe; construct one per goroutine.
//
// # Dependencies
//
//	instance  ──imports──▶  schema, diag, location, instance/path, immutable,
//	                        instance/internal/eval, internal/expr, internal/value
package instance
