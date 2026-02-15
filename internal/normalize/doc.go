// Package normalize provides data normalization utilities for the YAMMM library.
// It converts complex Go values (maps, slices, structs) into plain serializable trees
// of map[string]any and []any. Primitives and other non-normalizable values (including
// pointers to primitives) pass through unchanged.
//
// # Internal Package
//
// This package is internal to the YAMMM module per Go's internal/ package semantics.
// It is used by instance layers for producing serializable output (logging, JSON output).
//
// # Normalization
//
// The [Normalize] function recursively converts:
//   - Structs to map[string]any (using exported fields, respecting yammm tags)
//   - Maps with string keys to map[string]any (non-string-key maps returned
//     unchanged, including values; such maps cannot occur in validated instance data)
//   - Slices/Arrays to []any
//   - encoding.TextMarshaler to string (via MarshalText)
//   - fmt.Stringer to string (via String())
//   - Other values (primitives, pointers to primitives, channels, etc.) returned as-is
//
// # Struct Field Handling
//
// Struct fields are processed according to standard Go struct tag conventions:
//   - Unexported fields are skipped
//   - Fields with tag `yammm:"-"` are skipped
//   - Fields with tag `yammm:"name"` use the specified name
//   - Fields without tags use the decapitalized field name (FirstName becomes firstName)
//   - Anonymous embedded structs are flattened into the parent
//   - Nil embedded pointers result in omitted promoted fields (matching encoding/json)
//   - Named pointer fields that are nil produce nil values
//
// # Marshaler Precedence
//
// When normalizing a value, the following order of precedence applies:
//  1. encoding.TextMarshaler (MarshalText result becomes a string)
//  2. fmt.Stringer (String() result becomes a string)
//  3. Structural normalization (struct, map, slice)
//  4. Value passthrough (primitives returned as-is)
//
// # Thread Safety
//
// Normalize is stateless and safe for concurrent use.
//
// # Limitations
//
// Cyclic pointer graphs are not supported and will cause stack overflow.
// This is acceptable because the package is designed for tree-structured
// instance data (maps, slices, structs from validated instances), which
// cannot contain cycles by construction.
//
// # Stdlib-Only Dependencies
//
// This package depends only on stdlib. It has no dependencies on other packages.
package normalize
