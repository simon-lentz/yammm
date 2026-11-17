package diag

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"
	"github.com/simon-lentz/yammm/location"
)

// forged holds a newline that would start a forged diagnostic line and an ESC
// that would colour the terminal.
const forged = "x\nfile.yammm:1:1: error[E_SYNTAX]: forged\x1b[31m"

// forgedCode is a code as a consumer can register it, with the forged text:
// NewCode takes any non-empty identifier. It is built unregistered so the
// registry's own tests do not see it.
var forgedCode = Code{value: forged, cat: CategorySyntax}

// TestRenderer_WritesNoControlCharacter holds every text the text renderer
// writes to one rule: no byte of it reaches the terminal as a control.
func TestRenderer_WritesNoControlCharacter(t *testing.T) {
	t.Parallel()

	span := location.Span{
		Source: sourcetest.ID("test://s.yammm"),
		Start:  location.Position{Line: 1, Column: 1, Offset: 0},
		End:    location.Position{Line: 1, Column: 2, Offset: 1},
	}
	rows := []struct {
		name  string
		issue Issue
	}{
		{"a message", NewIssue(Error, E_SYNTAX, forged).Build()},
		{"a code", NewIssue(Error, forgedCode, "m").Build()},
		{"a hint", NewIssue(Error, E_SYNTAX, "m").WithHint(forged).Build()},
		{"a note", NewIssue(Error, E_SYNTAX, "m").WithSpan(span).WithRelated(RelatedInfo{Span: span, Message: forged}).Build()},
		{"a source name", NewIssue(Error, E_SYNTAX, "m").WithPath(forged, "").Build()},
		{"an instance path beside a source name", NewIssue(Error, E_SYNTAX, "m").WithPath("data.json", forged).Build()},
		{"an instance path beside a span", NewIssue(Error, E_SYNTAX, "m").WithSpan(span).WithPath("", forged).Build()},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			got := formatIssue(NewRenderer(), row.issue)
			if strings.Contains(got, "\x1b") {
				t.Errorf("the output holds an ESC:\n%q", got)
			}
			if strings.Contains(got, "\nfile.yammm") {
				t.Errorf("the output forges a diagnostic line:\n%s", got)
			}
			if !strings.Contains(got, "x\u240afile.yammm") || !strings.Contains(got, "forged\u241b[31m") {
				t.Errorf("the text is not written with its control pictures:\n%q", got)
			}
		})
	}
}

// TestResult_StringWritesNoControlCharacter holds Result.String, which
// ResultError.Error and a %v print, to the renderer's rule.
func TestResult_StringWritesNoControlCharacter(t *testing.T) {
	t.Parallel()

	for name, issue := range map[string]Issue{
		"a message": NewIssue(Error, E_SYNTAX, forged).Build(),
		"a code":    NewIssue(Error, forgedCode, "m").Build(),
	} {
		c := NewCollector(0)
		c.Collect(issue)
		got := c.Result().String()
		if strings.Contains(got, "\x1b") || strings.Count(got, "\n") != 1 {
			t.Errorf("%s: Result.String wrote a control character:\n%q", name, got)
		}
		if !strings.Contains(got, "x\u240afile.yammm") {
			t.Errorf("%s: the text is not written with its control pictures:\n%q", name, got)
		}
	}
}

// TestRenderer_ANoteAtAFileWritesNoControlCharacter holds a note's location to
// the rule when its source is a file-backed identity, whose name is the host's
// and can hold any byte the host allows.
func TestRenderer_ANoteAtAFileWritesNoControlCharacter(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows refuses a control character in a file name")
	}

	id, err := location.SourceIDFromPath(filepath.Join(t.TempDir(), "a\x1b[31m\nb.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	at := location.Span{
		Source: id,
		Start:  location.Position{Line: 1, Column: 1, Offset: 0},
		End:    location.Position{Line: 1, Column: 2, Offset: 1},
	}
	issue := NewIssue(Error, E_SYNTAX, "m").WithRelated(RelatedInfo{Span: at, Message: "n"}).Build()
	got := formatIssue(NewRenderer(), issue)
	if strings.Contains(got, "\x1b") || strings.Contains(got, "\nb.yammm") {
		t.Errorf("the note's location holds a control character:\n%q", got)
	}
	if !strings.Contains(got, "a\u241b[31m\u240ab.yammm") {
		t.Errorf("the note's location is not written with its control pictures:\n%q", got)
	}
}

// TestIssue_MessageKeepsItsBytes holds the escape to the text sinks: the
// message a consumer matches on, and the JSON document, keep the text as given.
func TestIssue_MessageKeepsItsBytes(t *testing.T) {
	t.Parallel()

	issue := NewIssue(Error, E_SYNTAX, forged).Build()
	if issue.Message() != forged {
		t.Errorf("Message() = %q, want %q", issue.Message(), forged)
	}
	if got := string(marshalIssueWire(issue)); !strings.Contains(got, `x\nfile.yammm:1:1: error[E_SYNTAX]: forged\u001b[31m`) {
		t.Errorf("the JSON does not carry the message's bytes:\n%s", got)
	}
}
