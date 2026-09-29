package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// operandFailure is one way a path operand can fail, with the exit the exit
// table gives it: a path refused before any lookup is a usage error, and
// everything the filesystem answers is a runtime error.
type operandFailure struct {
	name string
	want int
	// path returns the failing operand for a file with extension ext, or
	// false when this host cannot make it.
	path func(t *testing.T, dir, ext string) (string, bool)
}

// operandFailures lists the failures for an operand naming a file; a
// directory operand swaps "a directory" for "a regular file". A path that is
// not UTF-8 is refused before any lookup only where it names a source; any
// other operand looks it up, and this one names no file.
func operandFailures(dirOperand, source bool) []operandFailure {
	nonUTF8 := cli.ExitRuntime
	if source {
		nonUTF8 = cli.ExitUsage
	}
	failures := []operandFailure{
		{"an empty path", cli.ExitUsage, func(t *testing.T, _, _ string) (string, bool) { t.Helper(); return "", true }},
		{"a path that is not UTF-8", nonUTF8, func(t *testing.T, dir, ext string) (string, bool) {
			t.Helper()
			return filepath.Join(dir, "caf\xe9"+ext), true
		}},
		{"a missing path", cli.ExitRuntime, func(t *testing.T, dir, ext string) (string, bool) {
			t.Helper()
			return filepath.Join(dir, "missing"+ext), true
		}},
		{"a path under a regular file", cli.ExitRuntime, func(t *testing.T, dir, ext string) (string, bool) {
			t.Helper()
			file := filepath.Join(dir, "plain.txt")
			writeFixture(t, file, "x")
			return filepath.Join(file, "x"+ext), true
		}},
		{"a dangling link whose target is not UTF-8", cli.ExitRuntime, func(t *testing.T, dir, ext string) (string, bool) {
			t.Helper()
			p := filepath.Join(dir, "dangling"+ext)
			target := "missing-\xff" + ext
			if err := os.Symlink(target, p); err != nil {
				return "", false
			}
			if got, err := os.Readlink(p); err != nil || got != target {
				return "", false
			}
			return p, true
		}},
		{"a symbolic link loop", cli.ExitRuntime, func(t *testing.T, dir, ext string) (string, bool) {
			t.Helper()
			p := filepath.Join(dir, "loop"+ext)
			if err := os.Symlink(filepath.Base(p), p); err != nil {
				return "", false
			}
			return p, true
		}},
		{"an unreadable file", cli.ExitRuntime, func(t *testing.T, dir, ext string) (string, bool) {
			t.Helper()
			if runtime.GOOS == "windows" || os.Geteuid() == 0 || dirOperand {
				return "", false
			}
			p := filepath.Join(dir, "locked"+ext)
			writeFixture(t, p, "x")
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
			return p, true
		}},
	}
	if dirOperand {
		return append(failures, operandFailure{"a regular file", cli.ExitRuntime, func(t *testing.T, dir, _ string) (string, bool) {
			t.Helper()
			p := filepath.Join(dir, "plain")
			writeFixture(t, p, "x")
			return p, true
		}})
	}
	return append(failures, operandFailure{"a directory", cli.ExitRuntime, func(t *testing.T, dir, ext string) (string, bool) {
		t.Helper()
		p := filepath.Join(dir, "adir"+ext)
		if err := os.Mkdir(p, 0o750); err != nil {
			t.Fatal(err)
		}
		return p, true
	}})
}

// writeFixture writes content to path.
func writeFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestExitCodes_EveryPathOperandByFailure runs every command with each path
// operand failing each way, the other operands valid, and holds each exit to
// the exit table: 2 for a path refused before any lookup, 3 for a path the
// filesystem refuses. OP marks the operand under test in each command line.
func TestExitCodes_EveryPathOperandByFailure(t *testing.T) {
	t.Parallel()
	schemaPath, err := filepath.Abs(filepath.Join("testdata", "valid.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	dataPath, err := filepath.Abs(filepath.Join("testdata", "data.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapPath, err := filepath.Abs(filepath.Join("testdata", "valid_snapshot.ys"))
	if err != nil {
		t.Fatal(err)
	}
	const op = "OP"
	commands := []struct {
		line   string
		ext    string
		dir    bool
		source bool
	}{
		{"validate OP", ".yammm", false, true},
		{"fmt OP", ".yammm", false, false},
		{"check OP DATA", ".yammm", false, true},
		{"check SCHEMA OP", ".json", false, true},
		{"load OP DATA", ".yammm", false, true},
		{"load SCHEMA OP", ".json", false, true},
		{"export --to json OP DATA", ".yammm", false, true},
		{"export --to json SCHEMA OP", ".json", false, false},
		{"gen --to go OP", ".yammm", false, true},
		{"neo4j constraints OP", ".yammm", false, true},
		{"neo4j indexes OP", ".yammm", false, true},
		{"neo4j diff --uri bolt://127.0.0.1:1 OP", ".yammm", false, true},
		{"snapshot save -o OUT OP DATA", ".yammm", false, true},
		{"snapshot save -o OUT SCHEMA OP", ".json", false, true},
		{"snapshot save -o OUT --into OP SCHEMA DATA", ".ys", false, false},
		{"snapshot info OP", ".ys", false, false},
		{"snapshot info --header-only OP", ".ys", false, false},
		{"snapshot info --dir OP", "", true, false},
		{"snapshot verify OP SNAP", ".yammm", false, true},
		{"snapshot verify SCHEMA OP", ".ys", false, false},
		{"snapshot update-metadata -s k=v OP", ".ys", false, false},
		// The schema imports nothing, so only the root's own judgment can refuse it.
		{"validate --module-root OP SCHEMA", "", true, true},
		{"check --module-root OP SCHEMA DATA", "", true, true},
		{"load --module-root OP SCHEMA DATA", "", true, true},
		{"export --to json --module-root OP SCHEMA DATA", "", true, true},
		{"gen --to go --module-root OP SCHEMA", "", true, true},
		{"neo4j constraints --module-root OP SCHEMA", "", true, true},
		{"neo4j indexes --module-root OP SCHEMA", "", true, true},
		{"snapshot save -o OUT --module-root OP SCHEMA DATA", "", true, true},
		{"snapshot verify --module-root OP SCHEMA SNAP", "", true, true},
	}
	for _, c := range commands {
		for _, f := range operandFailures(c.dir, c.source) {
			t.Run(c.line+" with "+f.name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				bad, ok := f.path(t, dir, c.ext)
				if !ok {
					t.Skipf("this host cannot make %s", f.name)
				}
				var args []string
				for field := range strings.FieldsSeq(c.line) {
					switch field {
					case op:
						field = bad
					case "SCHEMA":
						field = schemaPath
					case "DATA":
						field = dataPath
					case "SNAP":
						field = snapPath
					case "OUT":
						field = filepath.Join(dir, "out.ys")
					}
					args = append(args, field)
				}
				if code, _, stderr := executeCmdOutput(t, args...); code != f.want {
					t.Errorf("exit %d, want %d: %s", code, f.want, stderr)
				}
			})
		}
	}
}

// TestRead_AFileNamedInBytesThatAreNotUTF8IsRead pins that an operand naming a
// file to read, not a source, takes any name the filesystem holds: each
// command reads such a file, where the host keeps the name, and exits 0.
func TestRead_AFileNamedInBytesThatAreNotUTF8IsRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	snap := filepath.Join(dir, "snap\xe9.ys")
	schemaFile := filepath.Join(dir, "f\xe9.yammm")
	sub := filepath.Join(dir, "dir\xe9")
	if err := os.WriteFile(snap, nil, 0o600); err != nil {
		t.Skipf("this host holds no name in bytes that are not UTF-8: %v", err)
	}
	if !holdsName(t, dir, filepath.Base(snap)) {
		t.Skip("this host rewrites a name in bytes that are not UTF-8")
	}
	copyFile(t, "testdata/valid_snapshot.ys", snap)
	copyFile(t, "testdata/valid.yammm", schemaFile)
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	copyFile(t, "testdata/valid_snapshot.ys", filepath.Join(sub, "s.ys"))
	schemaPath, err := filepath.Abs(filepath.Join("testdata", "valid.yammm"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"fmt", schemaFile},
		{"snapshot", "info", snap},
		{"snapshot", "info", "--header-only", snap},
		{"snapshot", "info", "--dir", sub},
		{"snapshot", "verify", schemaPath, snap},
		{"export", "--to", "json", schemaPath, snap},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			t.Parallel()
			if code, _, stderr := executeCmdOutput(t, args...); code != cli.ExitOK {
				t.Errorf("%q: exit %d, want 0: %s", args, code, stderr)
			}
		})
	}
}

// holdsName reports whether dir lists an entry named name, byte for byte.
func holdsName(t *testing.T, dir, name string) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}

// TestExitCodes_ARemovedWorkingDirectoryIsARuntimeFailure pins that a schema
// operand relative to a working directory that no longer exists exits 3: the
// filesystem answered, whether the host fails to spell the directory or to
// read the schema through it. It changes the process's directory, so it does
// not run in parallel.
func TestExitCodes_ARemovedWorkingDirectoryIsARuntimeFailure(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.Mkdir(gone, 0o750); err != nil {
		t.Fatal(err)
	}
	t.Chdir(gone)
	if err := os.Remove(gone); err != nil {
		t.Skipf("this host cannot remove the working directory: %v", err)
	}
	if code, _, stderr := executeCmdOutput(t, "validate", "s.yammm"); code != cli.ExitRuntime {
		t.Errorf("exit %d, want %d: %s", code, cli.ExitRuntime, stderr)
	}
}

// TestFmt_JudgesEveryOperandBeforeFormattingAny pins that fmt -w refuses an
// empty operand before it rewrites any file, naming the refusal, and exits 2.
// It reads run()'s own stderr through runCLI, so it does not run in parallel.
func TestFmt_JudgesEveryOperandBeforeFormattingAny(t *testing.T) {
	dir := t.TempDir()
	unformatted := filepath.Join(dir, "u.yammm")
	unformattedFixture(t, dir, "u.yammm")
	before, err := os.ReadFile(unformatted)
	if err != nil {
		t.Fatal(err)
	}
	code, _, stderr := runCLI(t, "fmt", "-w", unformatted, "")
	if code != cli.ExitUsage || !strings.Contains(stderr, "path is empty") {
		t.Errorf("exit %d, want %d naming the empty path: %s", code, cli.ExitUsage, stderr)
	}
	if after, err := os.ReadFile(unformatted); err != nil || !bytes.Equal(after, before) {
		t.Errorf("fmt -w rewrote %s before refusing an empty operand (%v)", unformatted, err)
	}
}
