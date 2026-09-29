package main

import (
	"encoding/json"
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

// TestLoadHelp_NamesWhenTheSummaryIsPrinted pins load's help against what load
// and check print: load's summary line appears in text output on data that
// loads without error and nowhere else, and check prints no summary.
func TestLoadHelp_NamesWhenTheSummaryIsPrinted(t *testing.T) {
	t.Parallel()

	out := words(helpText(t, "load"))
	for _, phrase := range []string{
		"in text output when the data loads without error, a summary line",
		"'yammm check' runs the same validation and prints no summary",
	} {
		if !strings.Contains(out, phrase) {
			t.Errorf("load's help does not say %q:\n%s", phrase, out)
		}
	}

	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.json")
	failing := filepath.Join(dir, "failing.json")
	if err := os.WriteFile(clean, []byte(`{"Person": [{"id": "a", "name": "A"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failing, []byte(`{"Person": [{"id": "a"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	const summary = "loaded 1 instances of 1 types\n"
	if _, _, errOut := executeCmdOutput(t, "load", "testdata/valid.yammm", clean); errOut != summary {
		t.Errorf("load in text on clean data wrote %q, want the summary %q alone", errOut, summary)
	}
	for _, argv := range [][]string{
		{"check", "testdata/valid.yammm", clean},
		{"check", "--format", "json", "testdata/valid.yammm", clean},
		{"load", "--format", "json", "testdata/valid.yammm", clean},
		{"load", "testdata/valid.yammm", failing},
	} {
		if _, _, errOut := executeCmdOutput(t, argv...); strings.Contains(errOut, "loaded ") {
			t.Errorf("%v printed a summary:\n%s", argv, errOut)
		}
	}
}

// TestGenHelp_JSONSchemaKeysAreTheTopLevelKeysCheckReads pins the jsonschema
// target's key rule against check: a type name is a key of the emitted envelope
// exactly when check reads a data file holding it at the top level.
func TestGenHelp_JSONSchemaKeysAreTheTopLevelKeysCheckReads(t *testing.T) {
	t.Parallel()

	out := words(helpText(t, "gen"))
	for _, phrase := range []string{
		"describing the JSON object form 'yammm check' reads",
		"one key per type a data file can hold at its top level",
		"It does not reproduce yammm's validation; 'yammm check' gives the verdict.",
	} {
		if !strings.Contains(out, phrase) {
			t.Errorf("the jsonschema target's help does not say %q:\n%s", phrase, out)
		}
	}

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "s.yammm")
	schemaSrc := `schema "s"

abstract type Geo {
    gid String primary
}

type Site extends Geo {
    name String
}

type Car {
    id String primary
    *-> WHEELS (many) Wheel
}

part type Wheel {
    pos String
}
`
	if err := os.WriteFile(schemaPath, []byte(schemaSrc), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, errOut := executeCmdOutput(t, "gen", "--to", "jsonschema", schemaPath)
	if code != cli.ExitOK {
		t.Fatalf("gen --to jsonschema: exit %d\n%s", code, errOut)
	}
	var envelope struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode the emitted schema: %v", err)
	}
	for _, name := range []string{"Geo", "Site", "Car", "Wheel"} {
		dataPath := filepath.Join(dir, name+".json")
		if err := os.WriteFile(dataPath, []byte(`{"`+name+`": []}`), 0o600); err != nil {
			t.Fatal(err)
		}
		code, _, errOut := executeCmdOutput(t, "check", schemaPath, dataPath)
		_, keyed := envelope.Properties[name]
		if (code == cli.ExitOK) != keyed {
			t.Errorf("%s: check exit %d, envelope key %v: the envelope's keys are not the names check reads\n%s", name, code, keyed, errOut)
		}
	}
}
