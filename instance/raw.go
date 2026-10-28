package instance

import (
	"github.com/simon-lentz/yammm/location"
)

// RawInstance represents unvalidated instance data.
//
// RawInstance is the input to the validation pipeline. It contains the raw
// property values from the input source (typically JSON) and optional
// provenance information for error reporting.
type RawInstance struct {
	// Properties contains the raw member values keyed by name: each property's,
	// and each relation's under its field name.
	// Property names may use any casing by default; the validator normalizes
	// them. Under [WithStrictPropertyNames] a name that differs in case is not
	// matched. A [Refused] may stand as a property's value, and in an edge
	// object as a foreign-key field's or an edge property's value; at any
	// other position the validator judges, it is a value of no form.
	Properties map[string]any

	// Provenance optionally captures source location metadata.
	// If nil, error messages will not include source locations.
	Provenance *location.Provenance
}

// Refused is a member value its producer refused: a parser that cannot read
// an input as its member's type reports that issue and writes a Refused in
// the value's place. The validator judges a Refused it reads as a member's
// value no further and reports nothing for it, and refuses the instance that
// holds it. Value is the input as the producer read it.
type Refused struct {
	Value any
}
