package json

import "errors"

// ErrZeroSnapshot is returned when MarshalObject or WriteObject is called with a zero graph snapshot.
var ErrZeroSnapshot = errors.New("json adapter: zero graph snapshot")

// ErrUnrepresentable marks a write refused because the snapshot holds a
// json.Number that does not read back as the number it spells, at any depth.
// Only a document read without revalidating at Error states one. It is the class,
// matched with errors.Is; the message names the instance or the edge, and the
// property. It separates a refusal of the data from an I/O failure.
var ErrUnrepresentable = errors.New("json adapter: the snapshot holds a value JSON cannot represent")
