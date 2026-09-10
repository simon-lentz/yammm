package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// diagnosticDocument is the part of the --format json diagnostic wire these
// tests read.
type diagnosticDocument struct {
	Issues []documentIssue `json:"issues"`
}

type documentIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Span    *struct {
		Source string `json:"source"`
		Start  struct {
			Line int `json:"line"`
		} `json:"start"`
	} `json:"span"`
	Details []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"details"`
}

// detail returns the value of the issue's detail named key.
func (iss documentIssue) detail(key string) (string, bool) {
	for _, d := range iss.Details {
		if d.Key == key {
			return d.Value, true
		}
	}
	return "", false
}

// decodeDocument returns stderr as the one diagnostic document it must be.
func decodeDocument(t *testing.T, stderr string) diagnosticDocument {
	t.Helper()
	if failure := oneJSONDocumentOutcome(stderr); failure != "" {
		t.Fatal(failure)
	}
	var doc diagnosticDocument
	if err := json.Unmarshal([]byte(stderr), &doc); err != nil {
		t.Fatalf("decode the diagnostic document: %v\n%s", err, stderr)
	}
	return doc
}

// TestRun_JSONFailureIsInTheDocument asserts what a machine consumer receives
// when a command fails for a reason that is not itself a diagnostic: the
// failure is an E_COMMAND_FAILED issue inside the one document, carrying the
// process exit code, and the process still exits with that code. The cases
// cover a failure raised inside a command, one cobra raises before any command
// runs, and one a grouping command raises.
func TestRun_JSONFailureIsInTheDocument(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nonexistent.ys")
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"export, unsupported target", []string{"export", "--to", "xml", "testdata/valid.yammm", "testdata/data.json"}, cli.ExitUsage, "xml"},
		{"check, wrong arity", []string{"check", "testdata/valid.yammm"}, cli.ExitUsage, "arg"},
		{"snapshot info, unreadable file", []string{"snapshot", "info", missing}, cli.ExitRuntime, "nonexistent.ys"},
		{"fmt, contradictory flags", []string{"fmt", "--check", "--write", "testdata/valid.yammm"}, cli.ExitUsage, "mutually exclusive"},
		{"snapshot, no subcommand", []string{"snapshot"}, cli.ExitUsage, "requires a subcommand"},
		{"an unknown command", []string{"nosuchcommand"}, cli.ExitUsage, "nosuchcommand"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := runCLI(t, append([]string{"--format", "json"}, tt.args...)...)
			if code != tt.code {
				t.Errorf("exit code = %d, want %d", code, tt.code)
			}
			doc := decodeDocument(t, stderr)
			found := false
			for _, iss := range doc.Issues {
				if iss.Code != "E_COMMAND_FAILED" || !strings.Contains(iss.Message, tt.want) {
					continue
				}
				found = true
				if exit, ok := iss.detail("exit_code"); !ok || exit != strconv.Itoa(tt.code) {
					t.Errorf("the failure's exit_code detail = %q (present %v), want %d", exit, ok, tt.code)
				}
			}
			if !found {
				t.Errorf("no E_COMMAND_FAILED issue mentions %q in the document:\n%s", tt.want, stderr)
			}
		})
	}
}

// TestRun_JSONFailureFindsTheFormatAnywhere asserts --format json is honoured
// wherever it sits on the command line, including after a flag or a command
// cobra does not know and so parses no further.
func TestRun_JSONFailureFindsTheFormatAnywhere(t *testing.T) {
	for _, args := range [][]string{
		{"validate", "--nosuchflag", "testdata/valid.yammm", "--format", "json"},
		{"nosuchcommand", "--format=json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, _, stderr := runCLI(t, args...)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			doc := decodeDocument(t, stderr)
			if !slices.ContainsFunc(doc.Issues, func(iss documentIssue) bool { return iss.Code == "E_COMMAND_FAILED" }) {
				t.Errorf("the document carries no E_COMMAND_FAILED issue:\n%s", stderr)
			}
		})
	}
}

// TestRun_FmtSyntaxErrorIsADiagnostic asserts fmt reports a syntax error the
// way validate does: a positioned E_SYNTAX diagnostic naming the file, in text
// and inside the one document under --format json.
func TestRun_FmtSyntaxErrorIsADiagnostic(t *testing.T) {
	broken := filepath.Join(t.TempDir(), "broken.yammm")
	if err := os.WriteFile(broken, []byte("schema \"test\"\ntype Person {\n\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	t.Run("text", func(t *testing.T) {
		code, _, stderr := runCLI(t, "fmt", "--check", broken)
		if code != cli.ExitValidation {
			t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
		}
		if !strings.Contains(stderr, "broken.yammm:4:1: error[E_SYNTAX]") {
			t.Errorf("stderr carries no positioned E_SYNTAX diagnostic naming the file:\n%s", stderr)
		}
	})

	t.Run("json", func(t *testing.T) {
		code, _, stderr := runCLI(t, "--format", "json", "fmt", "--check", broken)
		if code != cli.ExitValidation {
			t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
		}
		doc := decodeDocument(t, stderr)
		positioned := slices.ContainsFunc(doc.Issues, func(iss documentIssue) bool {
			return iss.Code == "E_SYNTAX" && iss.Span != nil &&
				strings.HasSuffix(iss.Span.Source, "broken.yammm") && iss.Span.Start.Line == 4
		})
		if !positioned {
			t.Errorf("the document carries no E_SYNTAX issue at broken.yammm line 4:\n%s", stderr)
		}
	})
}

// TestRun_ParentCommandsNameAnUnknownSubcommand asserts a mistyped subcommand
// is reported as what it is, naming the word the operator typed, where the
// grouping command reported a missing subcommand instead.
func TestRun_ParentCommandsNameAnUnknownSubcommand(t *testing.T) {
	for _, args := range [][]string{
		{"snapshot", "verfiy", "a.yammm", "b.ys"},
		{"neo4j", "constrants", "a.yammm"},
	} {
		t.Run(args[0], func(t *testing.T) {
			code, _, stderr := runCLI(t, args...)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if !strings.Contains(stderr, strconv.Quote(args[1])) {
				t.Errorf("stderr does not name the mistyped subcommand %q:\n%s", args[1], stderr)
			}
		})
	}
}

// diagnosticCountLine matches the count line diag.Result.String opens with, and
// the indented issue lines under it: renderings of a result, not messages.
var diagnosticCountLine = regexp.MustCompile(`\d+ error\(s\)|^error: {2,}`)

// TestRun_FailureLinesCarryAMessage asserts a failing run says why, and every
// "error: " line says something: no line is the bare prefix, and none is a
// line of a diagnostic result's String rendering printed as a message.
func TestRun_FailureLinesCarryAMessage(t *testing.T) {
	code, _, stderr := runCLI(t, "export", "--to", "cypher", "--prefix", "1bad ",
		"testdata/valid.yammm", "testdata/data.json")
	// The label is the input's fault: a validation or a usage failure, never
	// success and never an I/O failure, and the report names it.
	if code != cli.ExitValidation && code != cli.ExitUsage {
		t.Errorf("a label that is not an identifier exited %d, want %d or %d", code, cli.ExitValidation, cli.ExitUsage)
	}
	if !strings.Contains(stderr, "1bad") {
		t.Errorf("stderr does not name the label that is not an identifier:\n%s", stderr)
	}
	for line := range strings.SplitSeq(stderr, "\n") {
		rest, ok := strings.CutPrefix(line, "error:")
		if !ok {
			continue
		}
		if strings.TrimSpace(rest) == "" {
			t.Errorf("stderr carries a bare %q line:\n%s", "error: ", stderr)
		}
		if diagnosticCountLine.MatchString(line) {
			t.Errorf("stderr prints a diagnostic result's rendering as a failure message: %q\n%s", line, stderr)
		}
	}
}
