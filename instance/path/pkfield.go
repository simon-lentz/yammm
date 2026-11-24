package path

// PKField represents a primary key field with its name and value.
//
// PKField is used with [Path.PK] to construct PK-based path indices.
// The Value field should be one of:
//   - string (formatted as quoted: [name="Alice"])
//   - int, int8, int16, int32, int64, uint, uint8, uint16, uint32 or uint64
//     (formatted unquoted: [id=123])
//   - bool (formatted unquoted: [active=true])
//   - float32 or float64 (formatted unquoted, ".0" appended to a whole value:
//     [score=2.0])
//
// Any other value, a uintptr or a defined integer type among them, is written
// as a quoted string of its fmt.Sprint form, escaped.
type PKField struct {
	Name  string
	Value any
}
