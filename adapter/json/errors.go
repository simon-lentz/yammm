package json

import (
	"errors"
	"fmt"
)

// ErrNilResult is returned when MarshalObject or WriteObject is called with a nil graph result.
var ErrNilResult = errors.New("json adapter: nil graph result")

// ErrUnrepresentable marks a write refused because the snapshot holds a shape
// this adapter cannot render as the object its own parser accepts. It is the
// class, matched with errors.Is; the message names the edge and the type. It
// separates a refusal of the data from an [encoding/json] failure and from an
// I/O failure, which no message text can.
var ErrUnrepresentable = errors.New("json adapter: the snapshot holds a shape JSON cannot represent")

// refusalError carries a refusal's own text and the class a caller matches with
// errors.Is. Marking leaves the text alone, because the message is what names
// the edge and the type and the class is what a caller branches on.
type refusalError struct {
	msg   string
	class error
}

func (e *refusalError) Error() string { return e.msg }
func (e *refusalError) Unwrap() error { return e.class }

// refuse returns a refusal of class whose message is format applied to a.
func refuse(class error, format string, a ...any) error {
	return &refusalError{msg: fmt.Sprintf(format, a...), class: class}
}
