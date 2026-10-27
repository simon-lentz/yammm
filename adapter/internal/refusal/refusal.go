package refusal

import (
	"errors"
	"fmt"
)

// classified carries a refusal's own text and its class.
type classified struct {
	msg   string
	class error
}

func (e *classified) Error() string { return e.msg }
func (e *classified) Unwrap() error { return e.class }

// New returns a refusal of class whose message is format applied to a. The
// message is exactly that text: errors.Is finds class, and nothing of class's
// own text is added.
func New(class error, format string, a ...any) error {
	return &classified{msg: fmt.Sprintf(format, a...), class: class}
}

// marked carries an error another call returned and the class it is marked
// with, in that order: it wraps both, as errors.Join does, so errors.Is,
// errors.As and errors.AsType walk each by their own rules. The pair is held in
// an array, so a walk allocates nothing.
type marked struct {
	errs [2]error
}

func (e *marked) Error() string   { return e.errs[0].Error() }
func (e *marked) Unwrap() []error { return e.errs[:] }

// Mark returns err as a refusal of class: the message is err's own, errors.Is
// finds class and everything err matches, and errors.As finds what err holds
// and then class. A nil err is nil, and an err errors.Is already matches to
// class is returned as it is, so errors.As finds class there only if err holds
// it.
func Mark(class, err error) error {
	if err == nil || errors.Is(err, class) {
		return err
	}
	return &marked{errs: [2]error{err, class}}
}
