package json

import (
	"github.com/simon-lentz/yammm/location"
)

// Adapter parses JSON data into RawInstance values with optional location tracking.
//
// Thread Safety: Adapter is safe for concurrent Parse* calls after construction.
// No shared mutable state exists; all context flows through parameters.
type Adapter struct {
	registry       location.PositionRegistry
	strictJSON     bool
	trackLocations bool
	typeField      string
}

// ParseOption configures Adapter behavior.
type ParseOption func(*Adapter)

// NewAdapter creates a new JSON adapter with the given options.
//
// If WithTrackLocations(true) is set but registry is nil, returns an error.
// The registry parameter may be nil if WithTrackLocations is not used.
func NewAdapter(registry location.PositionRegistry, opts ...ParseOption) (*Adapter, error) {
	a := &Adapter{
		registry:       registry,
		strictJSON:     false, // Use jsonc preprocessing by default
		trackLocations: false, // Don't track locations by default
		typeField:      "$type",
	}

	for _, opt := range opts {
		opt(a)
	}

	// Validate: can't track locations without a registry
	if a.trackLocations && a.registry == nil {
		return nil, ErrNilRegistry
	}

	// Validate: type field cannot be empty
	if a.typeField == "" {
		return nil, ErrEmptyTypeField
	}

	return a, nil
}

// WithStrictJSON configures whether to use strict JSON parsing (no comments/trailing commas).
//
// When strict is true:
//   - Parses input directly with encoding/json
//   - No jsonc preprocessing at runtime
//   - Comments and trailing commas are parse errors
//
// When strict is false (default):
//   - Preprocesses input with tidwall/jsonc
//   - Strips comments and trailing commas before parsing
//   - Preserves byte offsets for accurate diagnostics
func WithStrictJSON(strict bool) ParseOption {
	return func(a *Adapter) {
		a.strictJSON = strict
	}
}

// WithTrackLocations enables source position tracking for parsed elements.
//
// When enabled, the adapter captures byte offsets during parsing and converts
// them to line/column positions via the PositionRegistry. This enables accurate
// diagnostic locations in error messages.
//
// Requires a non-nil PositionRegistry to be passed to NewAdapter.
func WithTrackLocations(track bool) ParseOption {
	return func(a *Adapter) {
		a.trackLocations = track
	}
}

// WithTypeField sets the field name used for type tagging in JSON objects.
//
// Default is "$type". This field is used by ParseArray to determine which
// type each object belongs to.
//
// Returns ErrEmptyTypeField from NewAdapter if field is empty.
func WithTypeField(field string) ParseOption {
	return func(a *Adapter) {
		a.typeField = field
	}
}
