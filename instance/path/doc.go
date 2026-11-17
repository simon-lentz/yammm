// Package path holds an instance path: the JSONPath-like text that says where
// in instance data, validated or not, a value sits.
//
// The validator writes one for each diagnostic it raises about a value, and a
// [github.com/simon-lentz/yammm/instance.Provenance] carries the path of the
// instance it describes. A path read from a document is kept as the document
// wrote it, so a writer that writes it again writes the same bytes.
//
// # Path Syntax
//
// A path this package builds starts at [Root], "$", and appends segments. A
// path read from a document need not; the validator reports an empty one from
// "$".
//
//	$              — root document
//	$.name         — property access (dot notation for identifier-safe keys)
//	$["a.b"]       — property access (bracket notation for other keys)
//	$[0]           — array index (numeric)
//	$[id=123]      — PK-based index (numeric PK)
//	$[name="Alice"] — PK-based index (string PK)
//	$[region="us",studentId=12345] — composite PK index
//
// # PK-Based Indexing
//
// Primary key-based indices use the format [field=value], where the value's
// written form records its kind:
//
//   - String: [name="Alice"] (quoted)
//   - Integer of any of Go's sized or unsized integer types, int through
//     int64 and uint through uint64: [id=123] (unquoted). A uintptr, and a
//     defined type whose underlying type is an integer, take the quoted form
//     below
//   - Float: [score=2.5], [score=2.0] (unquoted, ".0" appended to a whole
//     value)
//   - Boolean: [active=true] (unquoted)
//   - Any other value: the quoted, escaped text of its fmt.Sprint form
//   - Composite: [region="us",studentId=12345] (comma-separated, mixed types)
//
// # Escaping
//
// Keys and string values use RFC 8259 JSON escape sequences:
//
//	\\ for literal backslash
//	\" for literal double quote
//	\n \r \t \b \f for those control characters
//	\uXXXX for any other control character
//
// Invalid UTF-8 in a key or a string value is written as U+FFFD.
//
// # Building a path
//
// [Path.Key], [Path.Index] and [Path.PK] return the path with the segment
// appended, and [Path.PK] with no fields appends none:
//
//	p := path.Root.Key("Person").PK(path.PKField{Name: "id", Value: 42})
//	fmt.Println(p.String()) // $.Person[id=42]
//
// Each writes only text the grammar above spells. It panics on the three inputs
// the grammar does not spell: a negative index, a PK field name that is not an
// identifier, and a NaN or infinite float PK value. Each is a programmer error
// rather than user input — no producer in this module makes one, and the
// grammar is not extended to carry them.
package path
