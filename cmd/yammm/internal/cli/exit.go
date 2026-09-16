package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strconv"
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

// ReportError writes err to w as one "error: " line per line of each failure
// it carries.
//
// A bare exit signal — an [ExitError] with no wrapped error — prints nothing:
// its command has already rendered its diagnostics. Everything else prints,
// cobra's own errors included, which SilenceErrors would otherwise discard. A
// joined error prints each member carrying a message, so fmt names every
// offender and a bare member adds no line.
func ReportError(w io.Writer, err error) {
	for _, msg := range failureMessages(err) {
		for line := range strings.SplitSeq(msg, "\n") {
			fmt.Fprintf(w, "error: %s\n", line)
		}
	}
}

// failureMessages returns the message of every failure err carries, skipping
// bare exit signals and descending into a join. A trailing newline ends a
// message; it does not start an empty one.
func failureMessages(err error) []string {
	switch e := err.(type) { //nolint:errorlint // the shape of err itself decides, not an error deeper in its chain
	case nil:
		return nil
	case *ExitError:
		return failureMessages(e.Err)
	case interface{ Unwrap() []error }:
		var msgs []string
		for _, member := range e.Unwrap() {
			msgs = append(msgs, failureMessages(member)...)
		}
		return msgs
	default:
		return []string{strings.TrimRight(err.Error(), "\n")}
	}
}

// FailureResult returns err's failures as one diagnostic result — one
// [diag.E_COMMAND_FAILED] Error per message, each carrying the process exit
// code as its exit_code detail — and false when err carries no message.
//
// It is how a failure reaches a --format json consumer: inside the one
// document, as an issue matched by code, rather than as prose beside it.
func FailureResult(err error) (diag.Result, bool) {
	msgs := failureMessages(err)
	if len(msgs) == 0 {
		return diag.Result{}, false
	}
	code := strconv.Itoa(ExitForError(err))
	c := diag.NewCollectorUnlimited()
	for _, msg := range msgs {
		c.Collect(diag.NewIssue(diag.Error, diag.E_COMMAND_FAILED, msg).WithDetail(diag.DetailKeyExitCode, code).Build())
	}
	return c.Result(), true
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
//
// A bare exit signal contributes its code and no message: its command has
// already said what it had to, so a list of bare signals joins to a bare
// signal.
func JoinExitErrors(errs ...error) error {
	worst := ExitOK
	var messages []error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if code := ExitForError(err); exitRank(code) > exitRank(worst) {
			worst = code
		}
		if len(failureMessages(err)) > 0 {
			messages = append(messages, err)
		}
	}
	if worst == ExitOK {
		return nil
	}
	return &ExitError{Code: worst, Err: errors.Join(messages...)}
}
