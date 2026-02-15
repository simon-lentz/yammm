// Package textlit provides text literal conversion utilities for the DSL.
//
// This package handles the conversion of DSL string literals to Go strings,
// including escape sequence processing via strconv.Unquote. It supports both
// double-quoted ("string") and single-quoted ('string') literals with standard
// Go escape sequences (\n, \t, \uXXXX, etc.).
//
// # Internal Package
//
// This package is internal to the yammm library. Its API may change without
// notice between versions. External consumers should not import this package.
//
// # Main Functions
//
//   - ConvertString: Converts DSL string literals (double or single quoted) to
//     Go strings, processing escape sequences. Returns the original string
//     alongside an error for invalid escapes to enable proper diagnostics.
//
// # Usage Notes
//
// This package is positioned in internal/ rather than as part of the schema
// parsing layer to allow both schema and internal utilities to depend on
// it without creating upward dependencies.
package textlit
