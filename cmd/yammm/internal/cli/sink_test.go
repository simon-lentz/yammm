package cli

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// warningResult returns a result carrying one Warning with the given message.
func warningResult(message string) diag.Result {
	c := diag.NewCollectorUnlimited()
	c.Collect(diag.NewIssue(diag.Warning, diag.W_ANNOTATION_SHADOWED, message).Build())
	return c.Result()
}

// errorResult returns a result carrying one Error with the given message.
func errorResult(message string) diag.Result {
	c := diag.NewCollectorUnlimited()
	c.Collect(diag.NewIssue(diag.Error, diag.E_CONSTRAINT_FAIL, message).Build())
	return c.Result()
}

// TestDiagnosticSink_RendersEveryAddedResult is the pin the CLI's rendering of
// a warnings-only result never had: deleting any Add drops the result it
// carried, and this is what turns that red.
func TestDiagnosticSink_RendersEveryAddedResult(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatText, true, false)
	sink.Add(warningResult("first"))
	sink.Add(errorResult("second"), warningResult("third"))
	_ = sink.Close(nil)

	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("rendered output is missing %q; got:\n%s", want, out.String())
		}
	}
}

// TestDiagnosticSink_CloseIsIdempotent pins that a second Close adds nothing
// to the stream, so no path can write a second document.
func TestDiagnosticSink_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatJSON, true, false)
	sink.Add(errorResult("only once"))

	_ = sink.Close(nil)
	first := out.String()
	_ = sink.Close(nil)
	_ = sink.Close(nil)

	if out.String() != first {
		t.Errorf("a second Close wrote more:\nfirst:\n%s\nafter:\n%s", first, out.String())
	}
	assertOneJSONDocument(t, first)
}

// TestDiagnosticSink_RendersOneJSONDocument pins why the sink exists: several
// results added across an invocation's phases render as one document, where
// separate renders wrote one document per phase and a JSON reader accepts none
// of them.
func TestDiagnosticSink_RendersOneJSONDocument(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatJSON, true, false)
	sink.Add(warningResult("load phase"))
	sink.Add(errorResult("data phase"))
	_ = sink.Close(nil)

	doc := assertOneJSONDocument(t, out.String())
	for _, want := range []string{"load phase", "data phase"} {
		if !strings.Contains(doc, want) {
			t.Errorf("the one document is missing %q; got:\n%s", want, doc)
		}
	}
}

// TestDiagnosticSink_EmptyRendersNothing keeps a command that diagnosed nothing
// silent, which is what lets the wrapper render unconditionally.
func TestDiagnosticSink_EmptyRendersNothing(t *testing.T) {
	t.Parallel()

	for _, format := range []OutputFormat{FormatText, FormatJSON} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			sink := NewDiagnosticSink(&out, format, true, false)
			sink.Add(diag.OK())
			_ = sink.Close(nil)
			if out.String() != "" {
				t.Errorf("an empty sink wrote %q", out.String())
			}
		})
	}
}

// TestDiagnosticSink_ResultMergesEveryPhase pins what a command's exit code is
// derived from: the merge of every phase, not the last one added.
func TestDiagnosticSink_ResultMergesEveryPhase(t *testing.T) {
	t.Parallel()

	sink := NewDiagnosticSink(io.Discard, FormatText, true, false)
	if got := sink.Result().Len(); got != 0 {
		t.Errorf("a fresh sink merges %d issues, want 0", got)
	}

	sink.Add(errorResult("early phase"))
	sink.Add(warningResult("late phase"))

	result := sink.Result()
	if result.Len() != 2 {
		t.Errorf("merged result holds %d issues, want 2", result.Len())
	}
	if !result.HasErrors() {
		t.Error("an error added by an early phase must survive the merge")
	}
	if ExitForResult(result) != ExitValidation {
		t.Errorf("exit code = %d, want %d", ExitForResult(result), ExitValidation)
	}
}

// TestDiagnosticSink_TextFlushWritesEachResultOnce pins the incremental text
// write: a flush writes what has arrived, and Close writes only what arrived
// after it, in the order added.
func TestDiagnosticSink_TextFlushWritesEachResultOnce(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatText, true, false)
	sink.Add(warningResult("before the payload"))
	sink.Flush()
	if !strings.Contains(out.String(), "before the payload") {
		t.Fatalf("Flush wrote nothing under text; got %q", out.String())
	}
	sink.Flush()
	sink.Add(warningResult("after the payload"))
	_ = sink.Close(nil)

	got := out.String()
	for _, want := range []string{"before the payload", "after the payload"} {
		if n := strings.Count(got, want); n != 1 {
			t.Errorf("%q written %d times, want once; got:\n%s", want, n, got)
		}
	}
	if strings.Index(got, "before the payload") > strings.Index(got, "after the payload") {
		t.Errorf("results written out of order:\n%s", got)
	}
}

// TestDiagnosticSink_LateResultReachesTheStream pins the invariant a
// write-once render broke: a result added after a flush reaches the stream,
// and under JSON the one document holds every result however the command
// flushed.
func TestDiagnosticSink_LateResultReachesTheStream(t *testing.T) {
	t.Parallel()

	for _, format := range []OutputFormat{FormatText, FormatJSON} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			sink := NewDiagnosticSink(&out, format, true, false)
			sink.Add(warningResult("early"))
			sink.Flush()
			sink.Add(warningResult("late"))
			_ = sink.Close(nil)
			for _, want := range []string{"early", "late"} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("%q reached no stream; got:\n%s", want, out.String())
				}
			}
			if format == FormatJSON {
				assertOneJSONDocument(t, out.String())
			}
		})
	}
}

// TestDiagnosticSink_JSONFlushWritesNothing keeps the document whole: under
// JSON a flush before a payload must not write a document of its own.
func TestDiagnosticSink_JSONFlushWritesNothing(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatJSON, true, false)
	sink.Add(warningResult("held for Close"))
	sink.Flush()
	if out.Len() != 0 {
		t.Errorf("Flush wrote under JSON: %q", out.String())
	}
}

// TestDiagnosticSink_CloseFoldsAFailureIntoTheDocument pins what a JSON
// consumer receives from a failed command: the failure as an E_COMMAND_FAILED
// issue in the one document, beside every result the command added, and a
// bare exit signal so nothing is printed beside the document.
func TestDiagnosticSink_CloseFoldsAFailureIntoTheDocument(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatJSON, true, false)
	sink.Add(warningResult("a load warning"))
	err := sink.Close(Usagef("--to xml is not a target"))

	if got := ExitForError(err); got != ExitUsage {
		t.Errorf("exit code = %d, want %d", got, ExitUsage)
	}
	var reported strings.Builder
	ReportError(&reported, err)
	if reported.Len() != 0 {
		t.Errorf("Close returned a failure that still prints beside the document: %q", reported.String())
	}
	var doc struct {
		Issues []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details []struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			} `json:"details"`
		} `json:"issues"`
	}
	if err := json.Unmarshal([]byte(assertOneJSONDocument(t, out.String())), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sawWarning, sawFailure bool
	for _, iss := range doc.Issues {
		switch iss.Code {
		case diag.W_ANNOTATION_SHADOWED.String():
			sawWarning = true
		case diag.E_COMMAND_FAILED.String():
			sawFailure = strings.Contains(iss.Message, "--to xml") &&
				len(iss.Details) == 1 && iss.Details[0].Key == "exit_code" && iss.Details[0].Value == "2"
		}
	}
	if !sawWarning || !sawFailure {
		t.Errorf("the document lacks the warning (%v) or the failure with its exit code (%v):\n%s", sawWarning, sawFailure, out.String())
	}
}

// TestDiagnosticSink_TextCloseLeavesTheFailureToRun pins the text half: the
// failure is returned unchanged for run() to print, and not rendered twice.
func TestDiagnosticSink_TextCloseLeavesTheFailureToRun(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatText, true, false)
	failure := Runtimef("open data.json: no such file")
	if err := sink.Close(failure); !errors.Is(err, failure) {
		t.Errorf("Close changed the failure under text: %v", err)
	}
	if strings.Contains(out.String(), "data.json") {
		t.Errorf("Close rendered the failure under text: %q", out.String())
	}
}

// TestDiagnosticSink_AddAfterCloseIsAProgrammerError pins the guard: a result
// added after Close could reach no stream, so the sink refuses it loudly.
func TestDiagnosticSink_AddAfterCloseIsAProgrammerError(t *testing.T) {
	t.Parallel()

	sink := NewDiagnosticSink(io.Discard, FormatJSON, true, false)
	_ = sink.Close(nil)
	defer func() {
		if recover() == nil {
			t.Error("Add after Close did not panic")
		}
	}()
	sink.Add(warningResult("too late"))
}

// fixedSource answers every span with one file's content.
type fixedSource struct{ content []byte }

func (f fixedSource) Content(location.Span) ([]byte, bool) { return f.content, true }

// TestDiagnosticSink_SourceReachesTheRenderer pins the provider half of
// SetSource. It is the only place the provider changes output: excerpts render
// on a terminal and a text format, which no captured stream is.
func TestDiagnosticSink_SourceReachesTheRenderer(t *testing.T) {
	t.Parallel()

	source := location.MustNewSourceID("thing.yammm")
	c := diag.NewCollectorUnlimited()
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "unexpected token").
		WithSpan(location.Span{
			Source: source,
			Start:  location.Position{Line: 2, Column: 1},
			End:    location.Position{Line: 2, Column: 5},
		}).Build())

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatText, true, true)
	sink.SetSource(fixedSource{content: []byte("schema \"a\"\ntype Thing {\n")}, "")
	sink.Add(c.Result())
	_ = sink.Close(nil)

	if !strings.Contains(out.String(), "type Thing {") {
		t.Errorf("the excerpt the provider supplies is missing; got:\n%s", out.String())
	}
}

// TestDiagnosticSink_NoColorReachesTheRenderer pins the flag's one effect.
// Colour is emitted only on a terminal, so a captured stream shows nothing
// either way and the flag is unasserted unless the sink is told it has one.
func TestDiagnosticSink_NoColorReachesTheRenderer(t *testing.T) {
	t.Parallel()

	const escape = "\x1b["
	for _, tc := range []struct {
		name    string
		noColor bool
		want    bool
	}{
		{"colour on a terminal", false, true},
		{"--no-color on a terminal", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			sink := NewDiagnosticSink(&out, FormatText, tc.noColor, true)
			sink.Add(errorResult("coloured"))
			_ = sink.Close(nil)
			if got := strings.Contains(out.String(), escape); got != tc.want {
				t.Errorf("ANSI escapes present = %v, want %v; got:\n%q", got, tc.want, out.String())
			}
		})
	}
}

// assertOneJSONDocument reports output that is not exactly one JSON document,
// and returns it.
func assertOneJSONDocument(t *testing.T, out string) string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	var doc json.RawMessage
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("output is not a JSON document: %v\ngot:\n%s", err, out)
	}
	if err := dec.Decode(&doc); !errors.Is(err, io.EOF) {
		t.Fatalf("output carries more than one JSON document (second decode: %v); got:\n%s", err, out)
	}
	return out
}
