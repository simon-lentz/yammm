// Package instance provides instance validation for yammm schemas.
//
// This package transforms raw, unvalidated instance data into typed, validated
// [ValidInstance] objects suitable for graph ingestion. It is the primary entry
// point for validating data against compiled schemas.
//
// # Overview
//
// The validation pipeline takes [RawInstance] objects and produces either
// [ValidInstance] (on success) or [ValidationFailure] (on error). The [Validator]
// orchestrates this process using a compiled [schema.Schema].
//
//	validator := instance.NewValidator(compiledSchema)
//	valid, failures, err := validator.Validate(ctx, "Person", raws)
//
// # Instance Types
//
// [RawInstance] represents unvalidated input data with optional provenance
// information for error reporting. [ValidInstance] is the immutable output
// containing typed properties, validated edges, and the extracted primary key.
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
// # Thread Safety
//
// [Validator] is stateless and safe for concurrent use. Multiple goroutines
// may call [Validator.Validate] simultaneously with different inputs.
//
// # Subpackages
//
//   - [instance/path] provides JSONPath-like syntax for error locations
//   - [instance/eval] provides expression evaluation for invariants
package instance
