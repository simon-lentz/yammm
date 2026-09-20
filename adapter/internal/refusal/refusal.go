package refusal

import "fmt"

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
