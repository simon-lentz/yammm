package gogen

import "errors"

// ErrInvalidPackageName marks a [WithPackageName] value that cannot head a
// package clause: it is not a Go identifier, it is a keyword, or it is "_". It
// is the class, matched with errors.Is; the message names the value. It lets a
// caller report the refusal as its own input's fault, which a generator
// failure is not.
var ErrInvalidPackageName = errors.New("gogen: invalid package name")
