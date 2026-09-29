package json

import "errors"

// ErrNilResult is returned when MarshalObject or WriteObject is called with a nil graph result.
var ErrNilResult = errors.New("json adapter: nil graph result")

// ErrUnrepresentable marks a write refused because the snapshot holds a value
// JSON cannot write: a non-finite float at any depth, or a Go value
// encoding/json refuses, which only a bypass-built snapshot holds. It is the
// class, matched with errors.Is; the message names the instance or the edge
// and the property. It separates a refusal of the data from an I/O
// failure, which no message text can.
var ErrUnrepresentable = errors.New("json adapter: the snapshot holds a value JSON cannot represent")
