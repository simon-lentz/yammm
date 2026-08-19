package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
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
// its usage code outranks the validation code they produce.
func TestFmtCheck_UnreadablePathDoesNotStopTheList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.yammm")
	dirty := filepath.Join(dir, "dirty.yammm")
	if err := os.WriteFile(dirty, []byte(fmtUnformatted), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	code, out, _ := executeCmdOutput(t, "fmt", "--check", missing, dirty)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d (usage outranks validation)", code, cli.ExitUsage)
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
