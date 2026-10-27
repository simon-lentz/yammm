package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/diag"
)

// The yammm CLI's exit codes.
const (
	ExitOK         = 0 // Success (possibly with warnings)
	ExitValidation = 1 // Validation errors found
	ExitUsage      = 2 // Usage error (bad flags, missing arguments)
	ExitRuntime    = 3 // Runtime error (connection failure, I/O error)
)

// runtimeCodes are the diagnostic codes that report a failure outside the
// input: an I/O failure (a filesystem failure, or the reader a streamed parse
// reads from failing), an internal fault, or a cancelled context.
//
// diag/code.go introduces one I/O code per category by a stated convention
// rather than a CategoryIO constant, so membership is this list and not a
// category test. A new per-category I/O code belongs here.
var runtimeCodes = [...]diag.Code{
	diag.E_LOAD_IO_FAILURE, diag.E_SNAPSHOT_IO, diag.E_ADAPTER_IO,
	diag.E_INTERNAL, diag.E_CONTEXT_CANCELLED,
}

// ExitForResult returns the exit code a diagnostic result earns, and is the
// one place a result becomes an exit code.
//
// A failure outside the input outranks a refusal of it: 3 over 1. schema.Load
// reports an unreadable file as a diagnostic rather than an error return, so
// without this rule one missing path exits 1 through a command that loads a
// schema and 3 through one that opens the file itself.
//
// It reads [diag.Result.CodeCounts], which counts issues a full collector
// dropped, where [diag.Result.HasCode] sees only the ones it kept.
func ExitForResult(result diag.Result) int {
	for sev := diag.Fatal; sev <= diag.Hint; sev++ {
		counts := result.CodeCounts(sev)
		for _, code := range runtimeCodes {
			if counts[code] > 0 {
				return ExitRuntime
			}
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
// An error that carries a failure this package built keeps that failure's
// code, and anything else is ExitRuntime: a path the filesystem answers, or a
// failure the command did not classify. A usage refusal is built by [Usagef],
// so a path refused before any lookup reaches here as one ([CheckOperand],
// [CheckSourceOperand]); a location sentinel reaching here unmarked came from a
// resolution the filesystem answered.
func ExitForError(err error) int {
	if err == nil {
		return ExitOK
	}
	if exitErr, ok := errors.AsType[*exitError](err); ok {
		return exitErr.code
	}
	return ExitRuntime
}

// exitError is a failure of the CLI: its exit code, which is ExitValidation,
// ExitUsage or ExitRuntime, and the error that caused it. One with no cause is
// a bare exit signal ([Exit]); no exitError's cause is itself an exitError.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return codeText(e.code)
}

func (e *exitError) Unwrap() error { return e.err }

// codeText names a failing exit code: the text of a failure that has none of
// its own.
func codeText(code int) string {
	switch code {
	case ExitValidation:
		return "validation errors found"
	case ExitUsage:
		return "usage error"
	default:
		return "runtime error"
	}
}

// Exit returns a bare exit signal carrying code, or nil when code is ExitOK.
// It panics on a code the CLI does not name.
func Exit(code int) error {
	switch code {
	case ExitOK:
		return nil
	case ExitValidation, ExitUsage, ExitRuntime:
		return &exitError{code: code}
	}
	panic(fmt.Sprintf("cli: %d is not an exit code of the CLI", code))
}

// Usage returns err as a usage failure under its own message. A nil err is nil,
// and an err that already carries an exit code is returned as it is.
func Usage(err error) error { return classify(ExitUsage, err) }

// Runtime returns err as a runtime failure under its own message, as [Usage]
// does.
func Runtime(err error) error { return classify(ExitRuntime, err) }

func classify(code int, err error) error {
	if _, ok := errors.AsType[*exitError](err); ok || err == nil {
		return err
	}
	return &exitError{code: code, err: err}
}

// Usagef returns a failure carrying ExitUsage and a formatted message.
//
// A command returns one instead of printing: the message reaches the operator
// through [ReportError] at the top level, so the CLI has one write site for
// every failure rather than one per site.
func Usagef(format string, args ...any) error {
	return &exitError{code: ExitUsage, err: fmt.Errorf(format, args...)}
}

// Runtimef returns a failure carrying ExitRuntime and a formatted message.
func Runtimef(format string, args ...any) error {
	return &exitError{code: ExitRuntime, err: fmt.Errorf(format, args...)}
}

// Validationf returns a failure carrying ExitValidation and a formatted message.
func Validationf(format string, args ...any) error {
	return &exitError{code: ExitValidation, err: fmt.Errorf(format, args...)}
}

// ReportError writes err to w as one "error: " line per line of each failure it
// carries, skipping a blank line: one of spaces, tabs and carriage returns.
//
// A bare exit signal ([Exit]) prints nothing:
// its command has already rendered its diagnostics. Everything else prints,
// cobra's own errors included, which SilenceErrors would otherwise discard. An
// error [JoinExitErrors] returns prints each member as its own failure and a
// bare member adds no line; any other error is one failure, whatever it wraps.
func ReportError(w io.Writer, err error) {
	for _, msg := range failureMessages(err) {
		for line := range strings.SplitSeq(msg, "\n") {
			if !blank(line) {
				fmt.Fprintf(w, "error: %s\n", line)
			}
		}
	}
}

// failureMessages returns the message of every failure err carries, skipping
// bare exit signals and descending into the failures [JoinExitErrors] joins.
// Any other error is one failure under its own message, whatever it wraps. The
// blank lines a text ends with are no part of its message, and a failure whose
// text is only blank lines is reported under the text of the code it exits with.
func failureMessages(err error) []string {
	return messagesAt(err, ExitForError(err))
}

// messagesAt is failureMessages for an error that exits with code.
func messagesAt(err error, code int) []string {
	switch e := err.(type) { //nolint:errorlint // the shape of err itself decides, not an error deeper in its chain
	case nil:
		return nil
	case *exitError:
		return messagesAt(e.err, code)
	case joined:
		var msgs []string
		for _, member := range e {
			msgs = append(msgs, failureMessages(member)...)
		}
		return msgs
	}
	lines := strings.Split(err.Error(), "\n")
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return []string{codeText(code)}
	}
	return []string{strings.Join(lines, "\n")}
}

// blank reports whether line holds only spaces, tabs and carriage returns,
// which print no text.
func blank(line string) bool { return strings.Trim(line, " \t\r") == "" }

// joined holds the failures [JoinExitErrors] folds together, each reported as
// its own failure. An error that wraps several causes, as errors.Join or
// fmt.Errorf with several %w build, is one failure.
type joined []error

func (j joined) Error() string {
	texts := make([]string, len(j))
	for i, err := range j {
		texts[i] = err.Error()
	}
	return strings.Join(texts, "\n")
}

func (j joined) Unwrap() []error { return j }

// FailureResult returns err's failures as one diagnostic result — one
// [diag.E_COMMAND_FAILED] Error per message, each carrying the process exit
// code as its exit_code detail — and false when err is nil or a bare exit
// signal.
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
	if len(messages) == 0 {
		return Exit(worst)
	}
	return &exitError{code: worst, err: joined(messages)}
}
