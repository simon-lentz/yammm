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

// TestGenHelp_NamesTheTargetsGenWrites pins the --to flag's help to the targets
// runGen accepts.
func TestGenHelp_NamesTheTargetsGenWrites(t *testing.T) {
	t.Parallel()

	line := strings.TrimSpace(lineContaining(t, helpText(t, "gen"), "--to string"))
	if !strings.HasSuffix(line, "target: go or md (required)") {
		t.Errorf("--to's help does not name go and md alone: %q", line)
	}
}

// words returns text with every run of white space folded to one space, so a
// phrase the help wraps across lines still reads as the phrase.
func words(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// TestCheckHelp_NamesTheTargetsCheckResolves pins check's help against what
// check reports: an optional association naming no instance passes, and only a
// required one's missing target is reported.
func TestCheckHelp_NamesTheTargetsCheckResolves(t *testing.T) {
	t.Parallel()

	out := words(helpText(t, "check"))
	if !strings.Contains(out, "every required association's target") {
		t.Errorf("check's help does not say it resolves every required association's target:\n%s", out)
	}
	if !strings.Contains(out, "A target the data holds but refuses is reported by its own refusal, not as missing.") {
		t.Errorf("check's help does not say how a refused target is reported:\n%s", out)
	}
	if !strings.Contains(out, "Several data files are one document, as snapshot save reads them.") {
		t.Errorf("check's help does not say several data files are one document:\n%s", out)
	}
	if !strings.Contains(out, "check <schema.yammm> <data-file> [data-file...]") {
		t.Errorf("check's usage does not take several data files:\n%s", out)
	}

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "s.yammm")
	schemaSrc := `schema "s"

type Company {
    cid String primary
}

type Person {
    id String primary
    --> EMPLOYER (_:one) Company
    --> OWNER (one) Company
}
`
	if err := os.WriteFile(schemaPath, []byte(schemaSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		doc      string
		wantCode int
		wantErr  string
	}{
		"optional": {`{"Company": [{"cid": "c"}], "Person": [{"id": "p", "employer": {"_target_cid": "nope"}, "owner": {"_target_cid": "c"}}]}`, cli.ExitOK, ""},
		"required": {`{"Company": [{"cid": "c"}], "Person": [{"id": "p", "employer": {"_target_cid": "c"}, "owner": {"_target_cid": "nope"}}]}`, cli.ExitValidation, "E_UNRESOLVED_REQUIRED"},
	} {
		dataPath := filepath.Join(dir, name+".json")
		if err := os.WriteFile(dataPath, []byte(tc.doc), 0o600); err != nil {
			t.Fatal(err)
		}
		code, _, errOut := executeCmdOutput(t, "check", schemaPath, dataPath)
		if code != tc.wantCode || (tc.wantErr == "") != (errOut == "") || !strings.Contains(errOut, tc.wantErr) {
			t.Errorf("%s association naming no instance: exit %d, stderr %q; want exit %d and %q", name, code, errOut, tc.wantCode, tc.wantErr)
		}
	}
}
