package instance

// RawInstance represents unvalidated instance data.
//
// RawInstance is the input to the validation pipeline. It contains the raw
// property and relation values from the input source (typically JSON) and optional
// provenance information for error reporting.
type RawInstance struct {
	// Properties contains the raw member values keyed by name: each property's
	// under its declared name, and each relation's under its field name. A key
	// spelled in any other case is an unknown field.
	Properties map[string]any

	// Provenance optionally captures source location metadata.
	// If zero, error messages will not include source locations.
	Provenance Provenance
}
