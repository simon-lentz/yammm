package json

import "errors"

// ErrZeroSnapshot is returned when MarshalObject or WriteObject is called with a zero graph snapshot.
var ErrZeroSnapshot = errors.New("json adapter: zero graph snapshot")

// ErrUnrepresentable marks a write refused because the snapshot holds a value
// JSON cannot write as itself, which only a snapshot whose values no validator
// refused at Error holds:
// a non-finite float, a string that is not valid UTF-8, a json.Number that does
// not read back as the number it spells, a time or a UUID its constraint does
// not render as text that reads back, or a value of no form the module's
// writers write, at any depth. It is the class, matched with errors.Is; the
// message names the instance or the edge, and the property or the target key
// component. It separates a refusal of the data from an I/O failure, which no
// message text can.
var ErrUnrepresentable = errors.New("json adapter: the snapshot holds a value JSON cannot represent")
