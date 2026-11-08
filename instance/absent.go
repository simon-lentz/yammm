package instance

import (
	"github.com/simon-lentz/yammm/internal/value"
)

// IsAbsent reports whether v holds no value: nil, or a pointer, an interface, a
// map, a slice, a channel, a function or an unsafe pointer that is nil, bare or
// behind at most 64 pointers and interfaces, or a nil immutable container.
// [Validator] reads such a value at a property as a missing key: schema/gogen
// emits a typed nil pointer for an absent optional scalar, and a nil slice for
// an absent optional list. adapter/neo4j writes such a value as null.
func IsAbsent(v any) bool {
	return value.Absent(v)
}
