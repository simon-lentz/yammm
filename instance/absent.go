package instance

import (
	"github.com/simon-lentz/yammm/internal/value"
)

// IsAbsent reports whether v holds no value: nil, a nil map, a nil slice or a
// nil immutable container. [Validator] reads such a value at a property as a
// missing key, and adapter/neo4j writes it as null. A pointer is not absent, a
// nil one too: it is a value of no form, which the validator refuses.
func IsAbsent(v any) bool {
	return value.Absent(v)
}
