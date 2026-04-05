package json

import (
	"github.com/simon-lentz/yammm/location"
)

// adapterConfig holds JSON adapter configuration.
type adapterConfig struct {
	strictJSON     bool
	trackLocations bool
	typeField      string
}

func defaultConfig() adapterConfig {
	return adapterConfig{
		strictJSON:     false, // Use jsonc preprocessing by default
		trackLocations: false, // Don't track locations by default
		typeField:      "$type",
	}
}

// Adapter parses JSON data into RawInstance values with optional location tracking.
//
// Thread Safety: Adapter is safe for concurrent Parse* calls after construction.
// No shared mutable state exists; all context flows through parameters.
type Adapter struct {
	registry location.PositionRegistry
	config   adapterConfig
}

// Option configures Adapter behavior at construction time.
type Option func(*adapterConfig)

// New creates a new JSON adapter with the given options.
//
// If WithTrackLocations(true) is set but registry is nil, returns an error.
// The registry parameter may be nil if WithTrackLocations is not used.
func New(registry location.PositionRegistry, opts ...Option) (*Adapter, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	// Validate: can't track locations without a registry
	if cfg.trackLocations && registry == nil {
		return nil, ErrNilRegistry
	}

	// Validate: type field cannot be empty
	if cfg.typeField == "" {
		return nil, ErrEmptyTypeField
	}

	return &Adapter{registry: registry, config: cfg}, nil
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
func WithStrictJSON(strict bool) Option {
	return func(c *adapterConfig) {
		c.strictJSON = strict
	}
}

// WithTrackLocations enables source position tracking for parsed elements.
//
// When enabled, the adapter captures byte offsets during parsing and converts
// them to line/column positions via the PositionRegistry. This enables accurate
// diagnostic locations in error messages.
//
// Requires a non-nil PositionRegistry to be passed to New.
func WithTrackLocations(track bool) Option {
	return func(c *adapterConfig) {
		c.trackLocations = track
	}
}

// WithTypeField sets the field name used for type tagging in JSON objects.
//
// Default is "$type". This field is used by ParseArray to determine which
// type each object belongs to.
//
// Returns ErrEmptyTypeField from New if field is empty.
func WithTypeField(field string) Option {
	return func(c *adapterConfig) {
		c.typeField = field
	}
}
