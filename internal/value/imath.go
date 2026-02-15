package value

// IntType is a type constraint for all integer types (signed and unsigned).
//
// This custom constraint is used instead of the standard library's cmp.Ordered
// because it restricts to integer types only, preventing accidental use with
// floats or strings in integer-specific contexts (e.g., byte offset arithmetic).
type IntType interface {
	int | int8 | int16 | int32 | int64 | uint | uint8 | uint16 | uint32 | uint64
}

// Min returns the smaller of the two given integer values.
//
// This uses the custom IntType constraint rather than stdlib cmp.Min to ensure
// type safety for integer-only operations. cmp.Min accepts any cmp.Ordered type.
func Min[T IntType](a, b T) T {
	if a < b {
		return a
	}
	return b
}

// Max returns the greater of the two given integer values.
func Max[T IntType](a, b T) T {
	if a > b {
		return a
	}
	return b
}
