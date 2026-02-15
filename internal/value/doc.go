// Package value provides value comparison and kind detection utilities for the
// YAMMM library. It consolidates functionality from v1's internal/valuecmp
// (deterministic ordering) and internal/valuekind (runtime type classification).
//
// # Internal Package
//
// This package is internal to the YAMMM module and is not importable by external
// consumers per Go's internal/ package semantics. It is used by the instance
// validation layer (instance/eval) for type coercion and constraint evaluation.
//
// # Value Comparison
//
// The package implements a total order over supported types for deterministic
// comparisons in tests and constraint validation:
//
//   - [TypeStrata] classifies values into ordered strata: Nil < Bool < Numeric < String < Slice
//   - [ValueOrder] compares two values, returning -1/0/1 for ordering
//   - [Less] is a convenience wrapper for sort operations
//
// Supported types for comparison:
//   - nil
//   - bool (false < true)
//   - integers: int, int8-64, uint, uint8-64, uintptr
//   - floats: float32, float64 (with special handling: -Inf < finite < +Inf < NaN)
//   - string and *regexp.Regexp (regexp compared via String())
//   - slices of supported types (lexicographic comparison)
//
// IMPORTANT: Only predeclared scalar types are supported. Named scalar types
// (e.g., type MyInt int) return InvalidStrata and will cause ValueOrder to error.
// This is intentional for consistency across all value extraction functions.
// All slices are supported structurally (via reflect), but their elements must be
// supported types.
//
// Maps, structs, and other complex types are intentionally unsupported. Callers
// should normalize to supported primitives before ordering.
//
// # Kind Detection
//
// The [Classify] and [ClassifyWithRegistry] functions normalize runtime values
// to semantic [Kind] constants with optional value transformation:
//
//   - [IntKind]: Integer values (returns normalized int64 for json.Number)
//   - [FloatKind]: Float values (returns normalized float64 for json.Number)
//   - [VectorKind]: Float slices (coerces integer elements to float64)
//   - [BoolKind]: Boolean values
//   - [StringKind]: String values
//   - [UnspecifiedKind]: Unsupported or nil values
//
// # json.Number Handling
//
// [Classify] determines what a value IS, not what it should be per schema:
//   - json.Number("42") → IntKind (no decimal point)
//   - json.Number("3.0") → FloatKind (has decimal point)
//   - json.Number("3.14") → FloatKind
//
// Strict rejection of "3.0" for Integer schema types happens at validation time
// (Phase 3), not in this classification layer.
//
// # Float Precision Warning
//
// When large integers (> 2^53) are coerced to float64, precision may be lost.
// This is inherent to IEEE 754 floating-point representation, not a library
// limitation. For example, json.Number("9007199254740993") as Float loses
// precision because 9007199254740993 > 2^53 (JavaScript's MAX_SAFE_INTEGER).
// Schemas requiring exact large integers should use Integer type.
//
// # Large Unsigned Integer Comparison
//
// [ValueOrder] supports comparing uint64 values that exceed math.MaxInt64.
// On 64-bit platforms, uintptr values exceeding MaxInt64 are also supported
// (on 32-bit platforms, uintptr cannot hold such values).
// The comparison algorithm handles:
//   - Both unsigned: compared as uint64
//   - Mixed signed/unsigned: negative signed is always less than unsigned;
//     non-negative signed is compared as uint64
//   - Integer vs float: exact comparison via [CompareInt64Float64] or [CompareUint64Float64]
//
// # Mixed Float/Integer Comparison
//
// For mixed float/integer comparisons, [ValueOrder] uses [CompareInt64Float64] and
// [CompareUint64Float64] to preserve transitivity for values > 2^53. These functions
// convert the float to integer (not vice versa) when the float is a whole number,
// avoiding the precision loss that occurs when large integers are converted to float64.
//
// This ensures the ordering relation remains transitive across all supported values:
//   - ValueOrder(uint64(2^53+1), float64(2^53)) returns 1 (greater), not 0
//   - ValueOrder(int64(2^53+1), float64(2^53)) returns 1 (greater), not 0
//
// # Vector Coercion
//
// For []any input (the JSON pathway), all elements are coerced to float64:
//   - []any{1, 2, 3} → VectorKind, []float64{1, 2, 3}
//   - []any{1.5, 2.5} → VectorKind, []float64{1.5, 2.5}
//   - []any{1, 2.5, 3} → VectorKind, []float64{1, 2.5, 3}
//   - []any{float32(1.5), float32(2.5)} → VectorKind, []float64{1.5, 2.5}
//   - []any{1, "x", 3} → UnspecifiedKind (non-numeric element)
//
// Typed float slices are preserved as-is:
//   - []float64{1, 2, 3} → VectorKind, []float64{1, 2, 3}
//   - []float32{1, 2, 3} → VectorKind, []float32{1, 2, 3}
//
// # Empty Slice Handling
//
// Empty slices are classified based on their static type, not their contents:
//   - []float64{} → VectorKind (typed, element type known)
//   - []float32{} → VectorKind (typed, element type known)
//   - []any{} → UnspecifiedKind (untyped, no elements to inspect)
//
// This follows the "determine what it IS, not what it should be" principle.
// An empty []any{} is genuinely ambiguous—it could represent an empty vector,
// an empty string list, or an empty object list. The validator (Phase 3) has
// schema context and can properly interpret empty arrays based on the expected
// type. Note that Vector[N] constraints always require N > 0, so empty vectors
// would fail validation regardless of classification.
//
// # Registry Integration
//
// [ClassifyWithRegistry] accepts a [Registry] for custom type recognition hooks.
// The Registry hook is designed for Phase 3 (instance/eval) integration;
// see the [Registry] type documentation for details.
// A zero-value Registry falls back to built-in type detection.
//
// # Thread Safety
//
// All functions in this package are stateless and safe for concurrent use.
// No global state is maintained.
//
// # Stdlib-Only Dependencies
//
// This package depends only on stdlib. It has no dependencies on other packages
// and can be imported by any layer.
package value
