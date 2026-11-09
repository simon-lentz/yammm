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

// TestRun_MissingPathExitsRuntimeEverywhere pins one exit code for a
// nonexistent path across the eight commands that take one.
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
		{"snapshot save", []string{"snapshot", "save", "-o", filepath.Join(dir, "o.ys"), valid, missingData}},
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

// TestRun_EveryFailurePrints pins that six cobra-level and bare-error failures
// print a message. SilenceErrors discards what a command does not print itself,
// so run() must not return a code without a message.
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
		{"missing required flag", []string{"gen", "testdata/valid.yammm"}, cli.ExitUsage},
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

// TestRun_ABadFormatFlagExitsUsage pins that a --format value the CLI does not
// know is refused as a usage error, through run(), naming the flag's value.
func TestRun_ABadFormatFlagExitsUsage(t *testing.T) {
	code, _, errOut := runCLI(t, "validate", "--format", "bogus", "testdata/valid.yammm")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, `invalid output format "bogus"`) {
		t.Errorf("stderr does not name the value:\n%s", errOut)
	}
}

// TestRun_ParentCommandsRequireASubcommand pins that `yammm neo4j` and
// `yammm snapshot` fail without a subcommand. With no RunE, cobra prints help
// and exits 0, so a script that drops a subcommand reports success.
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

// TestRun_FmtReportsEveryPath pins that fmt reports every offending path in one
// invocation — that is what a pre-commit hook over a file list needs — and that
// its exit code does not let one path's failure mask another's.
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

// TestRun_FmtValidatesFormat pins that fmt refuses `--format bogus`: fmt calls
// ParseOutputFormat like every other command that takes the flag.
func TestRun_FmtValidatesFormat(t *testing.T) {
	code, _, errOut := runCLI(t, "fmt", "--format", "bogus", "testdata/valid.yammm")
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "invalid output format") {
		t.Errorf("stderr does not mention %q:\n%s", "invalid output format", errOut)
	}
}

// TestRun_DiffValidatesFormatBeforeURI pins that neo4j diff refuses a bad
// --format before its --uri guard runs. Otherwise the command exits 2 for a
// reason other than the flag under test, and the check reads as covered.
func TestRun_DiffValidatesFormatBeforeURI(t *testing.T) {
	code, _, errOut := runCLI(t, "neo4j", "diff", "--format", "bogus", shadowedSchema)
	if code != cli.ExitUsage {
		t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
	}
	if !strings.Contains(errOut, "invalid output format") {
		t.Errorf("stderr does not mention %q:\n%s", "invalid output format", errOut)
	}
}

// TestRun_Neo4jHasNoIntrospectCommand pins that `neo4j introspect` is an
// unknown command: a script that still calls it fails as a usage error naming
// the word, before any connection is opened, whatever flags follow it.
func TestRun_Neo4jHasNoIntrospectCommand(t *testing.T) {
	for name, args := range map[string][]string{
		"with --uri":         {"--uri", "bolt://127.0.0.1:1"},
		"with --help":        {"--help"},
		"with -h":            {"-h"},
		"with a former flag": {"--schema", "inventory", "--output", "inferred.yammm"},
	} {
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, append([]string{"neo4j", "introspect"}, args...)...)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitUsage, stderr)
			}
			if !strings.Contains(stderr, `unknown command "introspect" for "yammm neo4j"`) {
				t.Errorf("stderr does not name the unknown command:\n%s", stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
		})
	}
}

// TestRun_AGroupRefusesAnUnknownSubcommandBeforeHelp pins that every command
// holding subcommands refuses a word that names none of them, though cobra
// answers --help before it judges a command's words; the group's own help
// still exits 0.
func TestRun_AGroupRefusesAnUnknownSubcommandBeforeHelp(t *testing.T) {
	for _, group := range []string{"neo4j", "snapshot", "completion"} {
		t.Run(group, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, group, "bogus", "--help")
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d; stderr:\n%s", code, cli.ExitUsage, stderr)
			}
			if want := `unknown command "bogus" for "yammm ` + group + `"`; !strings.Contains(stderr, want) {
				t.Errorf("stderr does not say %s:\n%s", want, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if code, stdout, stderr := runCLI(t, group, "--help"); code != cli.ExitOK || stdout == "" {
				t.Errorf("%s --help: exit code = %d and %d bytes of help, want %d and the help; stderr:\n%s", group, code, len(stdout), cli.ExitOK, stderr)
			}
		})
	}
}

// A help flag before a subcommand's name asks for that subcommand's help, and a
// word after it that names no subcommand is still refused. --version before a
// command's name is judged on that command, as after it.
func TestRun_AHelpFlagBeforeASubcommandHelpsIt(t *testing.T) {
	for _, tt := range []struct {
		args  []string
		usage string
	}{
		{[]string{"neo4j", "--help", "diff"}, "yammm neo4j diff <schema.yammm>"},
		{[]string{"neo4j", "-h", "constraints"}, "yammm neo4j constraints <schema.yammm>"},
		{[]string{"neo4j", "--help", "--format", "json", "diff"}, "yammm neo4j diff <schema.yammm>"},
		{[]string{"--help", "neo4j"}, "yammm neo4j [flags]"},
		{[]string{"--no-color", "-h", "validate"}, "yammm validate <schema.yammm>"},
		{[]string{"snapshot", "-h", "info"}, "yammm snapshot info"},
		{[]string{"completion", "--help", "bash"}, "yammm completion bash"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.args...)
			if code != cli.ExitOK || !strings.Contains(stdout, "Usage:\n  "+tt.usage) {
				t.Errorf("exit code = %d, want %d and the usage %q; stdout:\n%s\nstderr:\n%s", code, cli.ExitOK, tt.usage, stdout, stderr)
			}
		})
	}
	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"neo4j", "--help", "bogus"}, `unknown command "bogus" for "yammm neo4j"`},
		{[]string{"neo4j", "--help", "diff", "--nosuch"}, "unknown flag: --nosuch"},
		{[]string{"--version", "check"}, "unknown flag: --version"},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.args...)
			if code != cli.ExitUsage || !strings.Contains(stderr, tt.want) || stdout != "" {
				t.Errorf("exit code = %d, want %d, stderr naming %q and no stdout; stdout:\n%s\nstderr:\n%s", code, cli.ExitUsage, tt.want, stdout, stderr)
			}
		})
	}
}

// A --format json after a help flag still asks for one document, so a refused
// word under --help is inside it.
func TestRun_AHelpRefusalAfterAHelpFlagIsOneJSONDocument(t *testing.T) {
	for _, args := range [][]string{
		{"neo4j", "bogus", "--help", "--format", "json"},
		{"neo4j", "bogus", "-h", "--format=json"},
		{"bogus", "--help", "--format", "json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, _, stderr := runCLI(t, args...)
			if code != cli.ExitUsage {
				t.Errorf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if problem := oneJSONDocumentOutcome(stderr); problem != "" || !strings.Contains(stderr, `"E_COMMAND_FAILED"`) {
				t.Errorf("want one JSON document holding E_COMMAND_FAILED: %s\n%s", problem, stderr)
			}
		})
	}
}

// TestRun_UpdateMetadataRefusesContradictoryKeys pins that --set and --unset
// naming one key are refused. Accepted, they delete the key, exit 0 and report
// "(0 keys)".
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
