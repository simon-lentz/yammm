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
	sink.Render()

	for _, want := range []string{"first", "second", "third"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("rendered output is missing %q; got:\n%s", want, out.String())
		}
	}
}

// TestDiagnosticSink_RenderIsIdempotent pins the half of the invariant a
// deferred render cannot supply on its own: a command that renders before its
// payload is rendered again by the wrapper, and the second call must add
// nothing to the stream.
func TestDiagnosticSink_RenderIsIdempotent(t *testing.T) {
	t.Parallel()

	var out strings.Builder
	sink := NewDiagnosticSink(&out, FormatJSON, true, false)
	sink.Add(errorResult("only once"))

	sink.Render()
	first := out.String()
	sink.Render()
	sink.Render()

	if out.String() != first {
		t.Errorf("a second Render wrote more:\nfirst:\n%s\nafter:\n%s", first, out.String())
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
	sink.Render()

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
			sink.Render()
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
	sink.Render()

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
			sink.Render()
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
