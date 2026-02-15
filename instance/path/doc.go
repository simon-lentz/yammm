// Package path provides canonical JSONPath-like syntax for identifying
// positions within validated instance data.
//
// # Path Syntax
//
// Paths always start with "$" (the root) and consist of segments:
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
// Primary key-based indices use the format [field=value] where the value
// type is preserved:
//
//   - String: [name="Alice"] (quoted)
//   - Integer: [id=123] (unquoted)
//   - Boolean: [active=true] (unquoted)
//   - Composite: [region="us",studentId=12345] (comma-separated, mixed types)
//
// # Escaping
//
// Keys and string values use RFC 8259 JSON escape sequences:
//
//	\\ for literal backslash
//	\" for literal double quote
//	\n \r \t for whitespace
//	\uXXXX for unicode escapes
//
// # Builder Pattern
//
// The [Builder] type is immutable; each method returns a new Builder with
// the appended segment. This enables safe concurrent use:
//
//	p := path.Root().Key("Person").PK(path.PKField{Name: "id", Value: 42})
//	fmt.Println(p.String()) // $.Person[id=42]
//
// # Thread Safety
//
// All types in this package are immutable and safe for concurrent use.
// The zero value of Builder represents the root path ($); use [Root] for clarity.
package path
