package neo4j

import (
	"errors"
	"fmt"
)

// unrepresentableError is a refusal built from its site's own text: the
// message is that text alone, and errors.Is finds [ErrUnrepresentable].
type unrepresentableError struct{ msg string }

func (e *unrepresentableError) Error() string { return e.msg }
func (e *unrepresentableError) Unwrap() error { return ErrUnrepresentable }

// unrepresentable returns a refusal whose message is format applied to a,
// with nothing of [ErrUnrepresentable]'s own text added.
func unrepresentable(format string, a ...any) error {
	return &unrepresentableError{msg: fmt.Sprintf(format, a...)}
}

// markedError carries an error another call returned and [ErrUnrepresentable],
// in that order: it wraps both, as errors.Join does, so errors.Is, errors.As and
// errors.AsType walk each by their own rules. The pair is held in an array, so
// a walk allocates nothing.
type markedError struct {
	errs [2]error
}

func (e *markedError) Error() string   { return e.errs[0].Error() }
func (e *markedError) Unwrap() []error { return e.errs[:] }

// markUnrepresentable returns err as a refusal: the message is err's own, and
// errors.Is finds [ErrUnrepresentable] and everything err matches. A nil err is
// nil, and an err that already matches [ErrUnrepresentable] is returned as it is.
func markUnrepresentable(err error) error {
	if err == nil || errors.Is(err, ErrUnrepresentable) {
		return err
	}
	return &markedError{errs: [2]error{err, ErrUnrepresentable}}
}
