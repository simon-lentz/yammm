package cli

import (
	"fmt"
	"io"

	"github.com/simon-lentz/yammm/diag"
)

// DiagnosticSink collects everything one invocation diagnoses, and writes it so
// that nothing added is lost and a JSON consumer reads one document.
//
// Text is written incrementally: [DiagnosticSink.Flush] writes what has arrived
// so it reads above a payload, and [DiagnosticSink.Close] writes the rest. JSON
// is written once, at Close, with the command's failure folded in; the document
// is on stderr and a payload on stdout, so writing it last reorders nothing.
type DiagnosticSink struct {
	w        io.Writer
	format   OutputFormat
	noColor  bool
	isTTY    bool
	provider diag.SourceProvider
	root     string
	results  []diag.Result
	flushed  int
	closed   bool
}

// NewDiagnosticSink returns a sink that renders to w.
func NewDiagnosticSink(w io.Writer, format OutputFormat, noColor, isTTY bool) *DiagnosticSink {
	return &DiagnosticSink{w: w, format: format, noColor: noColor, isTTY: isTTY}
}

// Format reports the diagnostic output format this invocation was given, for
// the commands that shape a stdout payload with the same flag.
func (s *DiagnosticSink) Format() OutputFormat { return s.format }

// SetSource fixes how rendered locations resolve: provider supplies excerpt
// text, and root is what file paths are relativized against.
//
// A command calls it as soon as a load produces them, so a command that returns
// before its own phase runs still renders locations the way a completed load
// would.
func (s *DiagnosticSink) SetSource(provider diag.SourceProvider, root string) {
	s.provider, s.root = provider, root
}

// Add records results for this invocation. It panics after
// [DiagnosticSink.Close], because a result added then could reach no stream.
func (s *DiagnosticSink) Add(results ...diag.Result) {
	if s.closed {
		panic("cli: DiagnosticSink.Add after Close")
	}
	s.results = append(s.results, results...)
}

// Result merges everything added so far.
//
// A command derives its exit code from this rather than from its last phase's
// result, so the code answers for every phase that diagnosed.
func (s *DiagnosticSink) Result() diag.Result {
	return MergeResults(s.results...)
}

// Statusf writes a progress line to the command's error stream, and nothing at
// all under [FormatJSON].
//
// A status line reports progress rather than a fact about the artefact, so a
// machine consumer is owed none of it — and one reaching stderr under
// FormatJSON would sit beside the document and stop it parsing. A fact about
// the artefact is a diagnostic instead, and goes through [DiagnosticSink.Add].
func (s *DiagnosticSink) Statusf(format string, args ...any) {
	if s.format == FormatJSON {
		return
	}
	fmt.Fprintf(s.w, format, args...)
}

// Flush writes, under [FormatText], every result added since the last flush. A
// command that emits a payload calls it first, so an operator reads the
// diagnostics above the payload. Under [FormatJSON] it writes nothing.
func (s *DiagnosticSink) Flush() {
	if s.format == FormatJSON || s.closed {
		return
	}
	s.renderFrom(s.flushed)
}

// Close writes everything not yet written and returns the error the process
// reports. The RunE wrapper calls it once, last.
//
// Under [FormatJSON] a failure that carries a message joins the document as
// [diag.E_COMMAND_FAILED], and Close returns that failure's bare exit signal,
// so nothing is printed beside the document. A second call writes nothing and
// returns err unchanged.
func (s *DiagnosticSink) Close(err error) error {
	if s.closed {
		return err
	}
	if s.format == FormatJSON {
		if failure, ok := FailureResult(err); ok {
			s.results = append(s.results, failure)
			err = &ExitError{Code: ExitForError(err)}
		}
	}
	s.renderFrom(s.flushed)
	s.closed = true
	return err
}

// renderFrom writes the results from index i on, as one result, and marks them
// written.
func (s *DiagnosticSink) renderFrom(i int) {
	s.flushed = len(s.results)
	if i >= len(s.results) {
		return
	}
	renderer := NewRenderer(s.format, s.isTTY, s.noColor, s.provider, s.root)
	_ = RenderResult(s.w, renderer, s.format, MergeResults(s.results[i:]...))
}
