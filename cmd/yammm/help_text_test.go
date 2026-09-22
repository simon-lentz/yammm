package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// helpText returns what an operator reads for the given command path.
func helpText(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errOut := executeCmdOutput(t, append(args, "--help")...)
	if code != cli.ExitOK {
		t.Fatalf("--help exit code = %d, want %d\nstderr:\n%s", code, cli.ExitOK, errOut)
	}
	return out
}

// TestRootHelp_NamesEveryCommandTakingTheDataFlags pins that the root help
// names every command that accepts --from, `snapshot save` included, and names
// --type and --type-column, the two flags a CSV input requires.
func TestRootHelp_NamesEveryCommandTakingTheDataFlags(t *testing.T) {
	t.Parallel()

	out := helpText(t)

	// The commands that register --from, read from the tree rather than listed
	// here: check, load, export and snapshot save.
	if !strings.Contains(out, "snapshot save") {
		t.Errorf("the data-flag list omits `snapshot save`, which registers --from:\n%s", out)
	}
	for _, flag := range []string{"--type", "--type-column"} {
		if !strings.Contains(out, flag) {
			t.Errorf("the root help never names %s, which a CSV input requires:\n%s", flag, out)
		}
	}
}

// TestSnapshotSaveHelp_OutputIsNotUnconditionallyRequired pins that --output
// does not read "(required)". `--into` alone satisfies the destination check,
// so that text would refuse an invocation the command accepts.
func TestSnapshotSaveHelp_OutputIsNotUnconditionallyRequired(t *testing.T) {
	t.Parallel()

	out := helpText(t, "snapshot", "save")

	line := lineContaining(t, out, "--output")
	if strings.Contains(line, "(required)") {
		t.Errorf("--output is still called unconditionally required: %q", strings.TrimSpace(line))
	}
	if !strings.Contains(line, "--into") {
		t.Errorf("--output's help does not name the flag that makes it optional: %q", strings.TrimSpace(line))
	}
}

// TestSnapshotSaveHelp_MatchesTheCommandsOwnCheck is the second implementation
// of the assertion above: the help is measured against what runSnapshotSave
// actually accepts, not against a remembered string.
func TestSnapshotSaveHelp_MatchesTheCommandsOwnCheck(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	base, more := saveInto(t)

	// --into alone, no --output: the invocation the old help called invalid.
	code, _, errOut := executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", more, "--into", base)
	if code != cli.ExitOK {
		t.Fatalf("--into alone exit code = %d, want %d — the help's claim is the one under test\nstderr:\n%s",
			code, cli.ExitOK, errOut)
	}

	// Neither flag: still refused, so "required" is not simply dropped.
	code, _, _ = executeCmdOutput(t, "snapshot", "save",
		"testdata/valid.yammm", filepath.Join(dir, "unused.json"))
	if code != cli.ExitUsage {
		t.Errorf("with neither --output nor --into the exit code = %d, want %d", code, cli.ExitUsage)
	}
}

// TestSnapshotVerifyHelp_DoesNotPromiseMemoryTheLibraryDisclaims pins the
// verify command's Long text against [snapshot.Verify]'s own contract. The
// help promised validation "without loading the full snapshot into memory"
// while the library's godoc states it decodes the instances section first, so
// peak memory scales with the document's size — a claim the operator would
// size a machine against.
func TestSnapshotVerifyHelp_DoesNotPromiseMemoryTheLibraryDisclaims(t *testing.T) {
	t.Parallel()

	out := helpText(t, "snapshot", "verify")

	if strings.Contains(out, "without loading the full snapshot into memory") {
		t.Errorf("the help promises a memory property snapshot.Verify's godoc disclaims:\n%s", out)
	}
	if !strings.Contains(out, "without materialising the snapshot") {
		t.Errorf("the help no longer states what verify actually avoids building:\n%s", out)
	}
}

// TestGenHelp_JSONSchemaWiringMatchesWhatCheckReads pins the jsonschema
// target's wiring advice against `yammm check` itself. A data file that names
// its schema in a "$schema" member fails check, because every top-level key is
// read as a type name, so the help must not recommend one.
func TestGenHelp_JSONSchemaWiringMatchesWhatCheckReads(t *testing.T) {
	t.Parallel()

	out := helpText(t, "gen")
	if !strings.Contains(out, `file cannot carry a "$schema" member`) {
		t.Errorf("the help no longer states that a data file cannot carry a \"$schema\" member:\n%s", out)
	}
	if strings.Contains(out, "$schema header") {
		t.Errorf("the help recommends a \"$schema\" header, which check refuses:\n%s", out)
	}

	data := filepath.Join(t.TempDir(), "data.json")
	doc := `{"$schema": "./valid.schema.json", "Person": [{"id": "alice", "name": "Alice"}]}`
	if err := os.WriteFile(data, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := executeCmdOutput(t, "check", "testdata/valid.yammm", data)
	if code != cli.ExitValidation || !strings.Contains(errOut, "E_INVALID_TYPE_TAG") {
		t.Errorf("check on a data file with a \"$schema\" member: exit %d, stderr:\n%s\nwant exit %d and E_INVALID_TYPE_TAG; if check now accepts the member, the help's advice is due to change",
			code, errOut, cli.ExitValidation)
	}
}
