package instance

// RawInstance represents unvalidated instance data.
//
// RawInstance is the input to the validation pipeline. It contains the raw
// property and relation values from the input source (typically JSON) and optional
// provenance information for error reporting.
type RawInstance struct {
	// Properties contains the raw member values keyed by name: each property's,
	// and each relation's under its field name.
	// Property names may use any casing by default; the validator normalizes
	// them. Under [WithStrictPropertyNames] a name that differs in case is not
	// matched.
	Properties map[string]any

	// Provenance optionally captures source location metadata.
	// If zero, error messages will not include source locations.
	Provenance Provenance
}
