package cli

import (
	"fmt"
	"io"

	"github.com/simon-lentz/yammm/diag"
)

// DiagnosticSink collects everything one invocation diagnoses and renders it
// once.
//
// Under [FormatJSON] a render writes one complete JSON document, so a second
// render produces a stream no JSON reader accepts, and a skipped render drops
// the warnings a loader emitted because it chose not to reject. A command adds
// results where it produces them and renders none itself, which leaves no
// return path able to skip the render and no way to write a second document.
type DiagnosticSink struct {
	w        io.Writer
	format   OutputFormat
	noColor  bool
	isTTY    bool
	provider diag.SourceProvider
	root     string
	results  []diag.Result
	rendered bool
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

// Add records results for this invocation's one render.
func (s *DiagnosticSink) Add(results ...diag.Result) {
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

// Render writes this invocation's diagnostics, once. A second call writes
// nothing.
//
// A command that emits a stdout payload calls it before the payload, so an
// operator reads the diagnostics first. Every other path renders through the
// deferred call the RunE wrapper makes, including every early return.
func (s *DiagnosticSink) Render() {
	if s.rendered {
		return
	}
	s.rendered = true
	renderer := NewRenderer(s.format, s.isTTY, s.noColor, s.provider, s.root)
	_ = RenderResult(s.w, renderer, s.format, s.Result())
}
