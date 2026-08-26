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
	// Properties contains the raw property values keyed by property name.
	// Property names may use any casing by default; the validator normalizes
	// them. Under [WithStrictPropertyNames] a name that differs in case is not
	// matched.
	Properties map[string]any

	// Provenance optionally captures source location metadata.
	// If nil, error messages will not include source locations.
	Provenance *location.Provenance
}
