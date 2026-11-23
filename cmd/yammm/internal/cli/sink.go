package cli

import (
	"errors"
	"io"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
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
	captured schema.Sources
	results  []diag.Result
	flushed  int
	closed   bool
	load     *diag.Collector // the schema load SchemaLoadOptions began
	read     *diag.Collector // the snapshot read SnapshotWarnings began
}

// callIssueLimit is the most issues the sink keeps of one schema load or one
// snapshot call, warnings and failures together, the most severe first.
const callIssueLimit = 100

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
// Under [FormatJSON] Close writes the command's one document, a clean run
// included. A failure that carries a message joins it as
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
			err = Exit(ExitForError(err))
		}
		// Under FormatJSON nothing is flushed before Close, and the one
		// document is written even when no result was added.
		renderer := NewRenderer(s.format, s.isTTY, s.noColor, s.provider, s.root)
		_ = RenderResult(s.w, renderer, s.format, MergeResults(s.results...))
		s.flushed = len(s.results)
		s.closed = true
		return err
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

// SchemaLoadOptions returns the options that keep what a schema load diagnoses
// on the sink: its sources, so a load that fails still renders excerpts though
// it returns no schema to take them from, and each warning as the load finds
// it. [DiagnosticSink.AddSchemaLoad] records the load.
func (s *DiagnosticSink) SchemaLoadOptions() []schema.Option {
	s.load = diag.NewCollector(callIssueLimit)
	return []schema.Option{schema.CaptureSources(&s.captured), schema.WithWarnings(s.load.Collect)}
}

// AddSchemaLoad records the schema load [DiagnosticSink.SchemaLoadOptions]
// began: its warnings and the failures err holds, as one result under
// callIssueLimit. It returns the exit that result earns, nil for a load
// that did not fail. An err holding no [diag.ResultError] is a runtime failure.
func (s *DiagnosticSink) AddSchemaLoad(err error) error {
	load := s.load
	s.load = nil
	return s.addCall(load, err)
}

// SnapshotWarnings returns the option that keeps each warning a snapshot read
// finds on the sink. [DiagnosticSink.AddSnapshot] records the read.
func (s *DiagnosticSink) SnapshotWarnings() snapshot.Option {
	s.read = diag.NewCollector(callIssueLimit)
	return snapshot.WithWarnings(s.read.Collect)
}

// AddSnapshot records a snapshot call: the warnings
// [DiagnosticSink.SnapshotWarnings] kept for it, if it began one, and the
// failures err holds, as one result under callIssueLimit. It returns the exit
// that result earns, nil for a call that did not fail. An err holding no
// [diag.ResultError] is a runtime failure.
func (s *DiagnosticSink) AddSnapshot(err error) error {
	read := s.read
	s.read = nil
	return s.addCall(read, err)
}

// addCall adds the result of one call: what c kept of its warnings, c nil for
// a call that kept none, and the failures err holds.
func (s *DiagnosticSink) addCall(c *diag.Collector, err error) error {
	if c == nil {
		c = diag.NewCollector(callIssueLimit)
	}
	re, ok := errors.AsType[*diag.ResultError](err)
	if err != nil && !ok {
		return Runtime(err)
	}
	if ok {
		c.Merge(re.Result)
	}
	result := c.Result()
	s.Add(result)
	return Exit(ExitForResult(result))
}

// CapturedSources returns the sources the last captured load read, or the zero Sources.
func (s *DiagnosticSink) CapturedSources() schema.Sources { return s.captured }
