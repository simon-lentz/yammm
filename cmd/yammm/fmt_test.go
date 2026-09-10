package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/schema"
)

const (
	fmtUnformatted = "schema \"test\"\ntype   Person   {\nid   String   primary\n}\n"
	fmtCanonical   = "schema \"test\"\n\ntype Person {\n\tid String primary\n}\n"
)

// writeFmtFixture writes content under name in a fresh temp dir and returns the
// path. Each case gets its own directory so --write cases cannot collide.
func writeFmtFixture(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// TestFmtCheck_FormattedFileIsSilent pins the hook's success shape: exit zero
// and print nothing, so a pre-commit run over a clean tree stays quiet.
func TestFmtCheck_FormattedFileIsSilent(t *testing.T) {
	t.Parallel()

	path := writeFmtFixture(t, "clean.yammm", fmtCanonical)
	code, out, _ := executeCmdOutput(t, "fmt", "--check", path)
	if code != cli.ExitOK {
		t.Errorf("exit code = %d, want %d", code, cli.ExitOK)
	}
	if out != "" {
		t.Errorf("expected no output, got %q", out)
	}
}

// TestFmtCheck_UnformattedFileIsReported pins the gofmt -l shape: the path
// alone, and the validation exit code the consumer's hook depends on.
func TestFmtCheck_UnformattedFileIsReported(t *testing.T) {
	t.Parallel()

	path := writeFmtFixture(t, "dirty.yammm", fmtUnformatted)
	code, out, _ := executeCmdOutput(t, "fmt", "--check", path)
	if code != cli.ExitValidation {
		t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
	}
	if out != path+"\n" {
		t.Errorf("output = %q, want the path alone", out)
	}
}

// TestFmtCheck_MixedListReportsOnlyOffenders pins that one run over a file list
// reports every unformatted path and no formatted one.
func TestFmtCheck_MixedListReportsOnlyOffenders(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	paths := map[string]string{
		"a_dirty.yammm": fmtUnformatted,
		"b_clean.yammm": fmtCanonical,
		"c_dirty.yammm": fmtUnformatted,
	}
	var args []string
	for name, content := range paths {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		args = append(args, p)
	}

	code, out, _ := executeCmdOutput(t, append([]string{"fmt", "--check"}, args...)...)
	if code != cli.ExitValidation {
		t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
	}
	lines := strings.Fields(out)
	if len(lines) != 2 {
		t.Fatalf("expected two reported paths, got %q", out)
	}
	for _, line := range lines {
		if !strings.HasSuffix(line, "_dirty.yammm") {
			t.Errorf("reported a formatted file: %q", line)
		}
	}
}

// TestFmt_MultiplePathsConcatenateToStdout pins the no-flag list behaviour,
// which matches gofmt: each formatted file in argument order, nothing between.
func TestFmt_MultiplePathsConcatenateToStdout(t *testing.T) {
	t.Parallel()

	first := writeFmtFixture(t, "first.yammm", fmtUnformatted)
	second := writeFmtFixture(t, "second.yammm", fmtUnformatted)

	code, out, _ := executeCmdOutput(t, "fmt", first, second)
	if code != cli.ExitOK {
		t.Errorf("exit code = %d, want %d", code, cli.ExitOK)
	}
	if want := fmtCanonical + fmtCanonical; out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// TestFmtCheck_UnreadablePathDoesNotStopTheList pins both halves of the list
// contract: an unreadable path does not suppress the offenders after it, and
// its I/O code outranks the validation code they produce.
func TestFmtCheck_UnreadablePathDoesNotStopTheList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.yammm")
	dirty := filepath.Join(dir, "dirty.yammm")
	if err := os.WriteFile(dirty, []byte(fmtUnformatted), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	code, out, _ := executeCmdOutput(t, "fmt", "--check", missing, dirty)
	if code != cli.ExitRuntime {
		t.Errorf("exit code = %d, want %d (an I/O failure outranks validation)", code, cli.ExitRuntime)
	}
	if out != dirty+"\n" {
		t.Errorf("output = %q, want the readable offender after the unreadable path", out)
	}
}

// TestFmt_CheckAndWriteAreMutuallyExclusive pins the usage code. The message
// itself goes to os.Stderr, which this harness cannot see; the assertion on it
// lives in testdata/script/fmt.txtar.
func TestFmt_CheckAndWriteAreMutuallyExclusive(t *testing.T) {
	t.Parallel()

	path := writeFmtFixture(t, "any.yammm", fmtCanonical)
	if code := executeCmd(t, "fmt", "--check", "--write", path); code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
}

// TestFmtWrite_MultiplePaths pins that the widened arity carries --write too,
// which is what removes the shell loop from a formatting hook.
func TestFmtWrite_MultiplePaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	var args []string
	for _, name := range []string{"one.yammm", "two.yammm"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(fmtUnformatted), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		args = append(args, p)
	}

	if code := executeCmd(t, append([]string{"fmt", "--write"}, args...)...); code != cli.ExitOK {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitOK)
	}
	for _, p := range args {
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if string(got) != fmtCanonical {
			t.Errorf("%s = %q, want the canonical form", p, got)
		}
	}
}

// tightOperators removes the spaces around a logical operator.
var tightOperators = regexp.MustCompile(`[ \t]*(&&|\|\|)[ \t]*`)

// fmtHazards returns schemas the formatter has been measured to rewrite into
// something else: one loses an enum value and still loads, one no longer loads.
func fmtHazards(t *testing.T) map[string]string {
	t.Helper()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "format", "testdata", "roundtrip", name))
		if err != nil {
			t.Fatalf("read hazard: %v", err)
		}
		return string(b)
	}
	return map[string]string{
		"an enum value on the opening line": read("enum_value_on_opening_line.yammm"),
		"an unspaced logical operator":      tightOperators.ReplaceAllString(read("g4_logical_op_in_trailing_comment.yammm"), "$1"),
	}
}

// fmtSchemaHash loads src and returns its structural hash, reporting whether it
// loaded.
func fmtSchemaHash(src string) (string, bool) {
	s, result := schema.LoadString(context.Background(), src, "hazard.yammm")
	if result.Err() != nil || s == nil {
		return "", false
	}
	return schema.StructuralHash(s), true
}

// TestFmt_NeverLeavesAChangedSchema asserts what an operator is left with, in
// every mode: output that means what the source meant, or a refusal that exits
// 3, writes nothing and leaves the file as it was.
func TestFmt_NeverLeavesAChangedSchema(t *testing.T) {
	t.Parallel()

	for name, src := range fmtHazards(t) {
		before, ok := fmtSchemaHash(src)
		if !ok {
			t.Fatalf("%s: the hazard does not load, so meaning cannot be compared", name)
		}
		for _, mode := range []string{"--write", "stdout", "--check"} {
			t.Run(name+"/"+mode, func(t *testing.T) {
				t.Parallel()

				path := writeFmtFixture(t, "hazard.yammm", src)
				args := []string{"fmt", mode, path}
				if mode == "stdout" {
					args = []string{"fmt", path}
				}
				code, out, _ := executeCmdOutput(t, args...)
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read back: %v", err)
				}

				if code == cli.ExitRuntime {
					if string(got) != src {
						t.Error("a refusal rewrote the file")
					}
					if out != "" {
						t.Errorf("a refusal wrote %q to stdout", out)
					}
					return
				}
				formatted := string(got)
				switch mode {
				case "--check":
					if code != cli.ExitOK && code != cli.ExitValidation {
						t.Fatalf("exit %d, want 0, 1 or 3", code)
					}
					return
				case "stdout":
					formatted = out
				}
				if code != cli.ExitOK {
					t.Fatalf("exit %d, want 0 or 3", code)
				}
				after, ok := fmtSchemaHash(formatted)
				if !ok {
					t.Fatalf("exit 0 with a schema that does not load:\n%s", formatted)
				}
				if after != before {
					t.Fatalf("exit 0 with a schema that means something else (%s -> %s):\n%s", before, after, formatted)
				}
			})
		}
	}
}

// TestFmtFailure_RefusalIsTheFormattersFault pins each formatter error's exit
// code: a refusal exits 3 so a hook blocks the commit, a parse failure exits 1.
func TestFmtFailure_RefusalIsTheFormattersFault(t *testing.T) {
	t.Parallel()

	refusal := formatFailure("a.yammm", fmt.Errorf("%w: token 3", format.ErrNotPreserved))
	if code := cli.ExitForError(refusal); code != cli.ExitRuntime {
		t.Errorf("refusal exit = %d, want %d", code, cli.ExitRuntime)
	}
	if msg := refusal.Error(); !strings.HasPrefix(msg, "a.yammm: ") || !strings.Contains(msg, "left unchanged") {
		t.Errorf("refusal message = %q, want the path and that the file is left unchanged", msg)
	}

	_, parseErr := format.TokenStream("schema \"s\"\ntype T {\n")
	if parseErr == nil {
		t.Fatal("the unparseable input parsed")
	}
	if code := cli.ExitForError(formatFailure("b.yammm", parseErr)); code != cli.ExitValidation {
		t.Errorf("parse failure exit = %d, want %d", code, cli.ExitValidation)
	}
}

// TestFmtCheck_LineEndingDifferenceIsUnformatted pins the behaviour the flag's
// help text warns about: TokenStream normalizes CRLF before formatting and never
// converts back, so a CRLF file is unformatted even when nothing else differs.
func TestFmtCheck_LineEndingDifferenceIsUnformatted(t *testing.T) {
	t.Parallel()

	crlf := strings.ReplaceAll(fmtCanonical, "\n", "\r\n")
	path := writeFmtFixture(t, "crlf.yammm", crlf)

	code, out, _ := executeCmdOutput(t, "fmt", "--check", path)
	if code != cli.ExitValidation {
		t.Errorf("exit code = %d, want %d", code, cli.ExitValidation)
	}
	if out != path+"\n" {
		t.Errorf("output = %q, want the path reported", out)
	}
}
