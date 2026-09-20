package json

import "errors"

// ErrNilResult is returned when MarshalObject or WriteObject is called with a nil graph result.
var ErrNilResult = errors.New("json adapter: nil graph result")

// ErrUnrepresentable marks a write refused because the snapshot holds a shape
// this adapter cannot render as the object its own parser accepts. It is the
// class, matched with errors.Is; the message names the instance or the edge,
// and the relation or the type. It
// separates a refusal of the data from an [encoding/json] failure and from an
// I/O failure, which no message text can.
var ErrUnrepresentable = errors.New("json adapter: the snapshot holds a shape JSON cannot represent")
