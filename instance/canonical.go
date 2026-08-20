package instance

import (
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// CanonicalValue returns val in the single stored representation c defines.
//
// Timestamp, UUID and Date each accept more than one Go representation and
// store one: a Timestamp renders through its declared format when it has one
// and through RFC 3339 with fractional seconds otherwise, a UUID through its
// canonical lowercase form, and a Date through "2006-01-02" in the value's own
// location. Every other kind, an unresolved alias and a nil value pass through
// untouched.
//
// This is the rule the validator applies, exported for a caller that renders
// schema-typed values at a boundary this library does not own — a hand-built
// export, a direct-Cypher parameter map. Values reaching a graph through
// [Validator] are already canonical.
//
// On error the returned value is val unchanged, so a caller that heals what it
// can and passes through what it cannot may ignore the error.
func CanonicalValue(val any, c schema.Constraint) (any, error) {
	//nolint:wrapcheck // the delegate's errors are the contract; wrapping them adds a layer naming this package for no gain
	return value.Canonical(val, c)
}
