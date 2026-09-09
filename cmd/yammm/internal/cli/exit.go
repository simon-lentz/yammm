package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"github.com/simon-lentz/yammm/diag"
)

// Exit codes for the yammm CLI.
const (
	ExitOK         = 0 // Success (possibly with warnings)
	ExitValidation = 1 // Validation errors found
	ExitUsage      = 2 // Usage error (bad flags, missing arguments)
	ExitRuntime    = 3 // Runtime error (connection failure, I/O error)
)

// ioCodes are the diagnostic codes that report a filesystem failure.
//
// diag/code.go introduces one per category by a stated convention rather than a
// CategoryIO constant, so membership is this list and not a category test. A new
// per-category I/O code belongs here.
var ioCodes = [...]diag.Code{diag.E_LOAD_IO_FAILURE, diag.E_SNAPSHOT_IO}

// ExitForResult returns the exit code a diagnostic result earns.
//
// An I/O failure outranks a validation failure. schema.Load reports an
// unreadable file as a diagnostic rather than an error return, so without this
// rule one missing path exits 1 through a command that loads a schema and 3
// through one that opens the file itself.
func ExitForResult(result diag.Result) int {
	for _, code := range ioCodes {
		if result.HasCode(code) {
			return ExitRuntime
		}
	}
	if result.HasErrors() {
		return ExitValidation
	}
	return ExitOK
}

// ExitForError returns the exit code an error earns — the error-side twin of
// [ExitForResult].
//
// A filesystem failure is ExitRuntime, which is both what the exit table's own
// wording says and what the commands that open a file themselves already
// answer. Anything unclassified stays ExitUsage.
func ExitForError(err error) int {
	if err == nil {
		return ExitOK
	}
	if exitErr, ok := errors.AsType[*ExitError](err); ok {
		return exitErr.Code
	}
	if _, ok := errors.AsType[*fs.PathError](err); ok {
		return ExitRuntime
	}
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
		return ExitRuntime
	}
	return ExitUsage
}

// ExitError carries a process exit code and, where the failure has a message of
// its own, the error that caused it.
//
// A nil Err makes it a bare exit signal: the command has already rendered its
// diagnostics and [ReportError] adds nothing.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	switch e.Code {
	case ExitValidation:
		return "validation errors found"
	case ExitUsage:
		return "usage error"
	case ExitRuntime:
		return "runtime error"
	default:
		return "exit"
	}
}

func (e *ExitError) Unwrap() error { return e.Err }

// Usagef returns an ExitError carrying ExitUsage and a formatted message.
//
// A command returns one instead of printing: the message reaches the operator
// through [ReportError] at the top level, so the CLI has one write site for
// every failure rather than one per site.
func Usagef(format string, args ...any) error {
	return &ExitError{Code: ExitUsage, Err: fmt.Errorf(format, args...)}
}

// Runtimef returns an ExitError carrying ExitRuntime and a formatted message.
func Runtimef(format string, args ...any) error {
	return &ExitError{Code: ExitRuntime, Err: fmt.Errorf(format, args...)}
}

// Validationf returns an ExitError carrying ExitValidation and a formatted message.
func Validationf(format string, args ...any) error {
	return &ExitError{Code: ExitValidation, Err: fmt.Errorf(format, args...)}
}

// ReportError writes err to w as one "error: " line per message.
//
// A bare exit signal — an [ExitError] with no wrapped error — prints nothing:
// the command has already rendered its diagnostics, and "validation errors
// found" beneath them says the same thing twice. Everything else prints,
// cobra's own errors included, which SilenceErrors would otherwise discard: a
// command that exits 2 having written no bytes tells the operator nothing.
//
// Each line prints separately so a command reporting several paths — fmt over a
// file list, joined with [errors.Join] — names every one of them.
func ReportError(w io.Writer, err error) {
	if err == nil {
		return
	}
	if exitErr, ok := errors.AsType[*ExitError](err); ok && exitErr.Err == nil {
		return
	}
	for line := range strings.SplitSeq(err.Error(), "\n") {
		fmt.Fprintf(w, "error: %s\n", line)
	}
}

// exitRank orders exit codes by severity for a command that reports several
// failures at once and must return the worst rather than the last.
//
// The numeric values do not rank: 2 is a usage error and 3 an I/O failure, so
// taking the maximum puts "this file could not be read" above "these flags
// contradict each other", which is backwards.
func exitRank(code int) int {
	switch code {
	case ExitUsage:
		return 3
	case ExitRuntime:
		return 2
	case ExitValidation:
		return 1
	default:
		return 0
	}
}

// JoinExitErrors folds several failures into one error carrying every message
// and the most severe exit code, or nil when none of them failed.
//
// It exists for the commands that take a list and must not stop at the first
// bad entry: one invocation names every offender, and no entry's failure hides
// another's in the exit code.
func JoinExitErrors(errs ...error) error {
	joined := errors.Join(errs...)
	if joined == nil {
		return nil
	}
	worst := ExitOK
	for _, err := range errs {
		if err == nil {
			continue
		}
		if code := ExitForError(err); exitRank(code) > exitRank(worst) {
			worst = code
		}
	}
	return &ExitError{Code: worst, Err: joined}
}
