package json

import "errors"

// ErrNilRegistry is returned when WithTrackLocations(true) is set but no registry was provided.
var ErrNilRegistry = errors.New("json adapter: WithTrackLocations(true) requires a non-nil PositionRegistry")

// ErrNilResult is returned when MarshalObject or WriteObject is called with a nil graph result.
var ErrNilResult = errors.New("json adapter: nil graph result")

// ErrEmptyTypeField is returned when WithTypeField is called with an empty field name.
var ErrEmptyTypeField = errors.New("json adapter: WithTypeField requires non-empty field name")
