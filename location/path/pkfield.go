package path

// PKField represents a primary key field with its name and value.
//
// PKField is used with [Builder.PK] to construct PK-based path indices.
// The Value field should be one of:
//   - string (formatted as quoted: [name="Alice"])
//   - int, int64, or other integer types (formatted unquoted: [id=123])
//   - bool (formatted unquoted: [active=true])
//
// Other types will be formatted using their default string representation.
type PKField struct {
	Name  string
	Value any
}
