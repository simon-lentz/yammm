package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// runCLI drives run() — the real entry point — and captures the process
// streams, so a test sees exactly what the shipped binary writes.
//
// executeCmdOutput cannot: it calls Execute on a command whose writers it
// injected, so it observes neither what run() prints nor the exit code run()
// derives. Every message the CLI emits on a failure path arrives here.
//
// It swaps process-global state and therefore must not run in parallel.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	origArgs, origOut, origErr := os.Args, os.Stdout, os.Stderr
	os.Args = append([]string{"yammm"}, args...)
	os.Stdout, os.Stderr = outW, errW

	var wg sync.WaitGroup
	var outBuf, errBuf strings.Builder
	// One goroutine per pipe: a single reader draining them in sequence
	// deadlocks as soon as the command fills the other pipe's buffer.
	for _, x := range []struct {
		r *os.File
		b *strings.Builder
	}{{outR, &outBuf}, {errR, &errBuf}} {
		wg.Go(func() {
			_, _ = io.Copy(x.b, x.r)
		})
	}

	code = run()

	os.Args, os.Stdout, os.Stderr = origArgs, origOut, origErr
	_ = outW.Close()
	_ = errW.Close()
	wg.Wait()
	_ = outR.Close()
	_ = errR.Close()

	if t.Failed() {
		t.Logf("stdout:\n%s\nstderr:\n%s", outBuf.String(), errBuf.String())
	}
	return code, outBuf.String(), errBuf.String()
}

// TestRun_MissingPathExitsRuntimeEverywhere pins B29 and B30: one nonexistent
// path drew four different exit codes across the eight commands that take one.
// An unreadable file is an I/O failure whichever command opened it, so every
// row is ExitRuntime and every row says why on stderr.
func TestRun_MissingPathExitsRuntimeEverywhere(t *testing.T) {
	dir := t.TempDir()
	missingSchema := filepath.Join(dir, "nonexistent.yammm")
	missingData := filepath.Join(dir, "nonexistent.json")
	missingSnap := filepath.Join(dir, "nonexistent.ys")
	valid := "testdata/valid.yammm"

	tests := []struct {
		name string
		args []string
	}{
		{"validate", []string{"validate", missingSchema}},
		{"fmt", []string{"fmt", missingSchema}},
		{"check", []string{"check", valid, missingData}},
		{"load", []string{"load", valid, missingData}},
		{"snapshot save", []string{"snapshot", "save", "-o", filepath.Join(dir, "o.ys"), valid, missingData}},
		{"export", []string{"export", "--to", "json", valid, missingData}},
		{"snapshot verify", []string{"snapshot", "verify", valid, missingSnap}},
		{"snapshot info", []string{"snapshot", "info", missingSnap}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runCLI(t, tt.args...)
			if code != cli.ExitRuntime {
				t.Errorf("exit code = %d, want %d — exit code for a missing path", code, cli.ExitRuntime)
			}
			if errOut == "" {
				t.Errorf("stderr is empty — a failure must say something")
			}
		})
	}
}

// TestRun_MissingSchemaExitsRuntime is the other half of the table above: a
// schema that cannot be read reaches these commands through reportSchemaLoad,
// which renders the load's diagnostics and turns them into an exit code. That
// path is separate from the one a missing DATA file takes, and a mutation of
// its code went unnoticed until this table existed.
func TestRun_MissingSchemaExitsRuntime(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nonexistent.yammm")
	data := "testdata/data.json"

	tests := []struct {
		name string
		args []string
	}{
		{"check", []string{"check", missing, data}},
		{"load", []string{"load", missing, data}},
		{"export", []string{"export", "--to", "json", missing, data}},
		{"gen", []string{"gen", "--to", "go", missing}},
		{"snapshot save", []string{"snapshot", "save", "-o", filepath.Join(t.TempDir(), "o.ys"), missing, data}},
		{"snapshot verify", []string{"snapshot", "verify", missing, "testdata/data.json"}},
		{"neo4j constraints", []string{"neo4j", "constraints", missing}},
		{"neo4j indexes", []string{"neo4j", "indexes", missing}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runCLI(t, tt.args...)
			if code != cli.ExitRuntime {
				t.Errorf("exit code = %d, want %d — an unreadable schema is an I/O failure", code, cli.ExitRuntime)
			}
			if !strings.Contains(errOut, "E_LOAD_IO_FAILURE") {
				t.Errorf("stderr does not mention %q:\n%s", "E_LOAD_IO_FAILURE", errOut)
			}
		})
	}
}

// TestRun_EveryFailurePrints pins B28: six cobra-level and bare-error failures
// exited 2 with zero bytes of output, because SilenceErrors discards what the
// command never printed itself and run() returned a code without a message.
func TestRun_EveryFailurePrints(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"unknown command", []string{"nosuchcommand"}, cli.ExitUsage},
		{"unknown flag", []string{"validate", "--nosuchflag", "testdata/valid.yammm"}, cli.ExitUsage},
		{"too few args", []string{"validate"}, cli.ExitUsage},
		{"too many args", []string{"validate", "a.yammm", "b.yammm"}, cli.ExitUsage},
		{"bad --format", []string{"validate", "--format", "bogus", "testdata/valid.yammm"}, cli.ExitUsage},
		{"missing required flag", []string{"export", "testdata/valid.yammm", "testdata/data.json"}, cli.ExitUsage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, errOut := runCLI(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit code = %d, want %d", code, tt.want)
			}
			if errOut == "" {
				t.Errorf("stderr is empty — a failure must not exit silently")
			}
		})
	}
}

// TestRun_UnclassifiedErrorExitsUsage pins run()'s fallback. A bad --format is
// a plain error carrying no exit code, so it exercises the arm a mutation of
// the fallback constant changes.
func TestRun_UnclassifiedErrorExitsUsage(t *testing.T) {
	code, _, errOut := runCLI(t, "validate", "--format", "bogus", "testdata/valid.yammm")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "invalid output format") {
		t.Errorf("stderr does not mention %q:\n%s", "invalid output format", errOut)
	}
}

// TestRun_ParentCommandsRequireASubcommand pins B18: `yammm neo4j` and
// `yammm snapshot` have no RunE, so cobra printed help and exited 0 — a script
// that drops a subcommand reports success having done nothing.
func TestRun_ParentCommandsRequireASubcommand(t *testing.T) {
	for _, parent := range []string{"neo4j", "snapshot"} {
		t.Run(parent, func(t *testing.T) {
			code, _, errOut := runCLI(t, parent)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if errOut == "" {
				t.Error("stderr is empty — a parent that refuses must say so")
			}
		})
	}
}

// TestRun_ParentCommandsStillHelp keeps --help preempting the usage error the
// test above requires.
func TestRun_ParentCommandsStillHelp(t *testing.T) {
	for _, parent := range []string{"neo4j", "snapshot"} {
		t.Run(parent, func(t *testing.T) {
			code, out, _ := runCLI(t, parent, "--help")
			if code != cli.ExitOK {
				t.Errorf("exit code = %d, want %d", code, cli.ExitOK)
			}
			if !strings.Contains(out, "Usage:") {
				t.Errorf("stdout does not mention %q:\n%s", "Usage:", out)
			}
		})
	}
}

// TestRun_FmtReportsEveryPath pins B31. fmt must report every offending path in
// one invocation — that is what a pre-commit hook over a file list needs — and
// its exit code must not let one path's failure mask another's.
func TestRun_FmtReportsEveryPath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "gone.yammm")

	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit, so the unwritable half cannot be set up")
	}
	readOnly := filepath.Join(dir, "readonly.yammm")
	unformatted := "schema  \"A\"\ntype T {\n      id UUID primary\n}\n"
	if err := os.WriteFile(readOnly, []byte(unformatted), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// WriteFile keeps an existing file's mode, so the mode is set after the
	// content: passing a read-only mode to the write above would leave the
	// file writable.
	if err := os.Chmod(readOnly, 0o400); err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}

	code, _, errOut := runCLI(t, "fmt", "-w", missing, readOnly)

	if code != cli.ExitRuntime {
		t.Errorf("exit code = %d, want %d — both paths fail on I/O", code, cli.ExitRuntime)
	}
	if !strings.Contains(errOut, "gone.yammm") {
		t.Errorf("stderr does not mention %q (the unreadable path must be named):\n%s", "gone.yammm", errOut)
	}
	if !strings.Contains(errOut, "readonly.yammm") {
		t.Errorf("stderr does not mention %q (the unwritable path must not be masked by the first failure):\n%s", "readonly.yammm", errOut)
	}
}

// TestRun_FmtValidatesFormat pins B44. fmt is one of exactly two command files
// that never called ParseOutputFormat, so `--format bogus` was accepted.
func TestRun_FmtValidatesFormat(t *testing.T) {
	code, _, errOut := runCLI(t, "fmt", "--format", "bogus", "testdata/valid.yammm")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "invalid output format") {
		t.Errorf("stderr does not mention %q:\n%s", "invalid output format", errOut)
	}
}

// TestRun_IntrospectValidatesFormatBeforeURI pins B44's second site. The --uri
// guard fired first, so the command exited 2 for a reason other than the flag
// under test and the site read as covered when it was not.
func TestRun_IntrospectValidatesFormatBeforeURI(t *testing.T) {
	code, _, errOut := runCLI(t, "neo4j", "introspect", "--format", "bogus")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "invalid output format") {
		t.Errorf("stderr does not mention %q:\n%s", "invalid output format", errOut)
	}
}

// TestRun_ExportRefusesBeforeWork pins B43 and B20. export validated --to after
// load, parse, validate, build and render, and silently ignored flag pairs that
// contradict each other.
func TestRun_ExportRefusesBeforeWork(t *testing.T) {
	dir := t.TempDir()

	t.Run("bad --to refuses before rendering diagnostics", func(t *testing.T) {
		code, _, errOut := runCLI(t, "export", "--to", "xml", "testdata/valid.yammm", "testdata/data.json")
		if code != cli.ExitUsage {
			t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
		}
		if !strings.Contains(errOut, "xml") {
			t.Errorf("stderr does not mention %q:\n%s", "xml", errOut)
		}
		if strings.Contains(errOut, "loaded") {
			t.Errorf("stderr mentions %q but no work runs before the flag set is refused:\n%s", "loaded", errOut)
		}
	})

	t.Run("--output-dir with --to json is refused", func(t *testing.T) {
		code, _, _ := runCLI(t, "export", "--to", "json", "--output-dir", dir,
			"testdata/valid.yammm", "testdata/data.json")
		if code != cli.ExitUsage {
			t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
		}
	})

	t.Run("--output with --output-dir is refused", func(t *testing.T) {
		code, _, _ := runCLI(t, "export", "--to", "csv",
			"--output", filepath.Join(dir, "o.csv"), "--output-dir", dir,
			"testdata/valid.yammm", "testdata/data.json")
		if code != cli.ExitUsage {
			t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
		}
	})
}

// TestRun_UpdateMetadataRefusesContradictoryKeys pins B45: --set and --unset
// naming one key deleted it, exited 0, and reported "(0 keys)".
func TestRun_UpdateMetadataRefusesContradictoryKeys(t *testing.T) {
	code, _, errOut := runCLI(t, "snapshot", "update-metadata",
		"-s", "env=staging", "--unset", "env", "testdata/valid.yammm")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "env") {
		t.Errorf("stderr does not mention %q:\n%s", "env", errOut)
	}
}
