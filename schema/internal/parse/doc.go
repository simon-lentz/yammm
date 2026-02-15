// Package parse provides AST types and parsing infrastructure for YAMMM schemas.
//
// This package contains:
//   - AST types representing parsed schema elements before semantic completion
//   - ANTLR visitor implementation for building the AST
//   - Span derivation utilities for converting ANTLR token positions
//
// # AST vs Completed Schema
//
// The AST types in this package represent the syntax-level parse result.
// They preserve exactly what was written in the schema source, including:
//   - Type references with optional import qualifiers
//   - Constraint specifications as parsed tokens
//   - Source locations for all declarations
//
// The internal/complete package transforms these AST types into the final
// immutable schema types after semantic validation and completion.
//
// # Span Derivation
//
// ANTLR tokens provide rune-based (charIndex) positions. This package converts
// them to byte offsets and full Position structs via the source registry:
//
//	ANTLR Token → charIndex (rune) → RuneToByteOffset() → byteOffset → mustPositionAt() → Position
//
// The mustPositionAt helper enforces the schema parsing invariant that all
// spans must be resolvable; it panics on failure as this indicates a bug.
package parse
