package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

const managerSchema = `schema "p12"

type Person {
    id String primary
    name String required
    --> MANAGER (one) Person
}
`

// verdictFixture writes the manager schema and each named document, one
// instance per line, into a fresh directory.
func verdictFixture(t *testing.T, docs map[string]string) (dir, schemaPath string) {
	t.Helper()
	dir = t.TempDir()
	schemaPath = filepath.Join(dir, "p12.yammm")
	if err := os.WriteFile(schemaPath, []byte(managerSchema), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, doc := range docs {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, schemaPath
}

// dataCommands returns, for each command the one data pipeline serves, its
// argument list over schemaPath and dataPath under --format json.
func dataCommands(t *testing.T, schemaPath, dataPath string) map[string][]string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.ys")
	return map[string][]string{
		"check":         {"check", "--format", "json", schemaPath, dataPath},
		"load":          {"load", "--format", "json", schemaPath, dataPath},
		"export":        {"export", "--format", "json", "--to", "json", schemaPath, dataPath},
		"snapshot save": {"snapshot", "save", "--format", "json", "-o", out, schemaPath, dataPath},
	}
}

// issueLines returns the span line of every issue of the document carrying code.
func issueLines(doc diagnosticDocument, code string) []int {
	var lines []int
	for _, iss := range doc.Issues {
		if iss.Code == code && iss.Span != nil {
			lines = append(lines, iss.Span.Start.Line)
		}
	}
	slices.Sort(lines)
	return lines
}

// codesOf returns the document's issue codes, sorted.
func codesOf(doc diagnosticDocument) []string {
	var codes []string
	for _, iss := range doc.Issues {
		codes = append(codes, iss.Code)
	}
	slices.Sort(codes)
	return codes
}

// TestDataCommands_ReportNoMissingTargetTheDocumentHolds pins the verdict every
// data command gives: an association whose target the document states and the
// validator refuses draws the target's refusal alone, while a target the
// document never states is still reported missing.
func TestDataCommands_ReportNoMissingTargetTheDocumentHolds(t *testing.T) {
	t.Parallel()

	_, schemaPath := verdictFixture(t, nil)
	dir := filepath.Dir(schemaPath)
	docs := map[string]struct {
		body        string
		wantCodes   []string
		wantMissing []int
	}{
		"refused.json": {
			body: `{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "a", "name": "A", "manager": {"_target_id": "boss"}},
{"id": "b", "name": "B", "manager": {"_target_id": "boss"}}
]}`,
			wantCodes: []string{"E_MISSING_REQUIRED"},
		},
		"absent.json": {
			body: `{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "a", "name": "A", "manager": {"_target_id": "nobody"}},
{"id": "b", "name": "B", "manager": {"_target_id": "boss"}}
]}`,
			wantCodes:   []string{"E_MISSING_REQUIRED", "E_UNRESOLVED_REQUIRED"},
			wantMissing: []int{3},
		},
	}
	for name, d := range docs {
		dataPath := filepath.Join(dir, name)
		if err := os.WriteFile(dataPath, []byte(d.body), 0o600); err != nil {
			t.Fatal(err)
		}
		for command, args := range dataCommands(t, schemaPath, dataPath) {
			t.Run(name+"/"+command, func(t *testing.T) {
				t.Parallel()
				code, _, errOut := executeCmdOutput(t, args...)
				if code != cli.ExitValidation {
					t.Fatalf("exit code = %d, want %d", code, cli.ExitValidation)
				}
				doc := decodeDocument(t, errOut)
				if got := codesOf(doc); !slices.Equal(got, d.wantCodes) {
					t.Errorf("codes = %v, want %v", got, d.wantCodes)
				}
				if got := issueLines(doc, "E_UNRESOLVED_REQUIRED"); !slices.Equal(got, d.wantMissing) {
					t.Errorf("E_UNRESOLVED_REQUIRED at lines %v, want %v", got, d.wantMissing)
				}
			})
		}
	}
}

// TestDataCommands_ReportADuplicateKeyARefusedInstanceHides pins that a key the
// document states twice is a duplicate whether or not the validator refused one
// of its holders: every holder after the first draws E_DUPLICATE_PK once.
func TestDataCommands_ReportADuplicateKeyARefusedInstanceHides(t *testing.T) {
	t.Parallel()

	_, schemaPath := verdictFixture(t, nil)
	dir := filepath.Dir(schemaPath)
	docs := map[string]struct {
		body     string
		wantDups []int
	}{
		"refused_first.json": {`{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "B", "manager": {"_target_id": "boss"}}
]}`, []int{3}},
		"refused_last.json": {`{"Person": [
{"id": "boss", "name": "B", "manager": {"_target_id": "boss"}},
{"id": "boss", "manager": {"_target_id": "boss"}}
]}`, []int{3}},
		"refused_then_two_valid.json": {`{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "B", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "C", "manager": {"_target_id": "boss"}}
]}`, []int{3, 4}},
		"two_refused.json": {`{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "boss", "manager": {"_target_id": "boss"}}
]}`, []int{3}},
		"unreadable_key.json": {`{"Person": [
{"id": 7, "name": "A", "manager": {"_target_id": "boss"}},
{"id": 7, "name": "B", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "C", "manager": {"_target_id": "boss"}}
]}`, nil},
	}
	for name, d := range docs {
		dataPath := filepath.Join(dir, name)
		if err := os.WriteFile(dataPath, []byte(d.body), 0o600); err != nil {
			t.Fatal(err)
		}
		for command, args := range dataCommands(t, schemaPath, dataPath) {
			t.Run(name+"/"+command, func(t *testing.T) {
				t.Parallel()
				code, _, errOut := executeCmdOutput(t, args...)
				if code != cli.ExitValidation {
					t.Fatalf("exit code = %d, want %d", code, cli.ExitValidation)
				}
				doc := decodeDocument(t, errOut)
				if got := issueLines(doc, "E_DUPLICATE_PK"); !slices.Equal(got, d.wantDups) {
					t.Errorf("E_DUPLICATE_PK at lines %v, want %v", got, d.wantDups)
				}
				if got := issueLines(doc, "E_UNRESOLVED_REQUIRED"); len(got) != 0 {
					t.Errorf("E_UNRESOLVED_REQUIRED at lines %v, want none", got)
				}
			})
		}
	}
}

// TestSnapshotSave_TheDataFilesAreOneDocument pins that snapshot save judges
// its data files together: a target one file refuses explains the missing target
// another file names, and a key two files state is a duplicate.
func TestSnapshotSave_TheDataFilesAreOneDocument(t *testing.T) {
	t.Parallel()

	dir, schemaPath := verdictFixture(t, map[string]string{
		"first.json": `{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}}
]}`,
		"second.json": `{"Person": [
{"id": "a", "name": "A", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "B", "manager": {"_target_id": "boss"}}
]}`,
	})
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--format", "json", "-o", filepath.Join(dir, "out.ys"),
		schemaPath, filepath.Join(dir, "first.json"), filepath.Join(dir, "second.json"))
	if code != cli.ExitValidation {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitValidation)
	}
	doc := decodeDocument(t, errOut)
	if got, want := codesOf(doc), []string{"E_DUPLICATE_PK", "E_MISSING_REQUIRED"}; !slices.Equal(got, want) {
		t.Errorf("codes = %v, want %v", got, want)
	}
	for _, iss := range doc.Issues {
		if iss.Code == "E_DUPLICATE_PK" && (iss.Span == nil || !strings.HasSuffix(iss.Span.Source, "second.json") || iss.Span.Start.Line != 3) {
			t.Errorf("E_DUPLICATE_PK is not at second.json line 3: %+v", iss.Span)
		}
	}
}

// TestSnapshotSaveInto_TheMergedFileHoldsItsKeys pins that under --into the
// imported instances are earlier holders of their keys: a refused new instance
// at an imported key is a duplicate, and a target refused in the new data still
// explains a missing target.
func TestSnapshotSaveInto_TheMergedFileHoldsItsKeys(t *testing.T) {
	t.Parallel()

	dir, schemaPath := verdictFixture(t, map[string]string{
		"base.json": `{"Person": [{"id": "boss", "name": "Boss", "manager": {"_target_id": "boss"}}]}`,
		"more.json": `{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "z", "name": "Z", "manager": {"_target_id": "q"}},
{"id": "q", "manager": {"_target_id": "q"}}
]}`,
	})
	base := filepath.Join(dir, "base.ys")
	if code, _, errOut := executeCmdOutput(t, "snapshot", "save", "-o", base, schemaPath, filepath.Join(dir, "base.json")); code != cli.ExitOK {
		t.Fatalf("save the base: exit %d\n%s", code, errOut)
	}
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--format", "json", "-o", filepath.Join(dir, "out.ys"),
		"--into", base, schemaPath, filepath.Join(dir, "more.json"))
	if code != cli.ExitValidation {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitValidation)
	}
	doc := decodeDocument(t, errOut)
	if got := issueLines(doc, "E_DUPLICATE_PK"); !slices.Equal(got, []int{2}) {
		t.Errorf("E_DUPLICATE_PK at lines %v, want [2]", got)
	}
	if got := issueLines(doc, "E_UNRESOLVED_REQUIRED"); len(got) != 0 {
		t.Errorf("E_UNRESOLVED_REQUIRED at lines %v, want none", got)
	}
}

// TestSnapshotSave_KeepsEarlierFilesFindingsWhenALaterFileCannotBeRead pins
// that a data file that cannot be read ends the command without dropping what
// the files read before it diagnosed.
func TestSnapshotSave_KeepsEarlierFilesFindingsWhenALaterFileCannotBeRead(t *testing.T) {
	t.Parallel()

	dir, schemaPath := verdictFixture(t, map[string]string{
		"first.json": `{"Person": [{"id": "a", "id": "b", "name": "A", "manager": {"_target_id": "a"}}]}`,
	})
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--format", "json", "-o", filepath.Join(dir, "out.ys"),
		schemaPath, filepath.Join(dir, "first.json"), filepath.Join(dir, "missing.json"))
	if code != cli.ExitRuntime {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitRuntime)
	}
	doc := decodeDocument(t, errOut)
	if got, want := codesOf(doc), []string{"E_ADAPTER_PARSE", "E_COMMAND_FAILED"}; !slices.Equal(got, want) {
		t.Errorf("codes = %v, want %v: the first file's finding must survive the second file's failure", got, want)
	}
}

// TestSnapshotSave_RefusesAUsageErrorBeforeReadingAnyFile pins that every data
// usage error — a format, a CSV type flag, a --type name, an operand — is
// decided before the first data file is read, so it reports nothing an earlier
// file would have drawn.
func TestSnapshotSave_RefusesAUsageErrorBeforeReadingAnyFile(t *testing.T) {
	t.Parallel()

	dir, schemaPath := verdictFixture(t, map[string]string{
		"first.json": `{"Person": [{"id": "a", "id": "b", "name": "A", "manager": {"_target_id": "a"}}]}`,
		"second.csv": "id,name\nc,C\n",
		"third.xml":  "<Person/>",
	})
	for name, args := range map[string][]string{
		"a CSV file with no type flag":  {filepath.Join(dir, "first.json"), filepath.Join(dir, "second.csv")},
		"an undetectable format":        {filepath.Join(dir, "first.json"), filepath.Join(dir, "third.xml")},
		"a --type the schema lacks":     {"--type", "Nope", filepath.Join(dir, "first.json"), filepath.Join(dir, "second.csv")},
		"an empty data path":            {"--from", "json", filepath.Join(dir, "first.json"), ""},
		"a data path that is not UTF-8": {filepath.Join(dir, "first.json"), "bad\xff.json"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			argv := append([]string{"snapshot", "save", "--format", "json", "-o", filepath.Join(t.TempDir(), "out.ys"), schemaPath}, args...)
			code, _, errOut := executeCmdOutput(t, argv...)
			if code != cli.ExitUsage {
				t.Fatalf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if got, want := codesOf(decodeDocument(t, errOut)), []string{"E_COMMAND_FAILED"}; !slices.Equal(got, want) {
				t.Errorf("codes = %v, want %v: no file may be read before a usage error", got, want)
			}
		})
	}
}

// TestDataCommands_RefuseAUsageErrorBeforeAnyRead pins that check, load and
// snapshot save decide every data file's format before they read the schema or
// a merged file: a schema that does not load and a merged file that does not
// exist draw nothing beside the usage error.
func TestDataCommands_RefuseAUsageErrorBeforeAnyRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.yammm")
	if err := os.WriteFile(broken, []byte("schema \"b\"\n\ntype {\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data.csv")
	if err := os.WriteFile(data, []byte("id,name\na,A\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.ys")
	for name, argv := range map[string][]string{
		"check":                {"check", "--format", "json", broken, data},
		"load":                 {"load", "--format", "json", broken, data},
		"snapshot save":        {"snapshot", "save", "--format", "json", "-o", out, broken, data},
		"snapshot save --into": {"snapshot", "save", "--format", "json", "--into", filepath.Join(dir, "missing.ys"), broken, data},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, _, errOut := executeCmdOutput(t, argv...)
			if code != cli.ExitUsage {
				t.Fatalf("exit code = %d, want %d", code, cli.ExitUsage)
			}
			if got, want := codesOf(decodeDocument(t, errOut)), []string{"E_COMMAND_FAILED"}; !slices.Equal(got, want) {
				t.Errorf("codes = %v, want %v: nothing may be read before a usage error", got, want)
			}
		})
	}
}

// TestCheck_ReadsATypeColumn pins that --type-column alone satisfies CSV data:
// the rows are read and judged, so a row the schema refuses exits 1, not 2.
func TestCheck_ReadsATypeColumn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	good := filepath.Join(dir, "good.csv")
	bad := filepath.Join(dir, "bad.csv")
	if err := os.WriteFile(good, []byte("kind,id,name\nPerson,a,A\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, []byte("kind,id\nPerson,a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := executeCmdOutput(t, "check", "--type-column", "kind", "testdata/valid.yammm", good); code != cli.ExitOK {
		t.Errorf("check --type-column over a valid row: exit %d\n%s", code, errOut)
	}
	if code, _, errOut := executeCmdOutput(t, "check", "--type-column", "kind", "testdata/valid.yammm", bad); code != cli.ExitValidation {
		t.Errorf("check --type-column over a refused row: exit %d, want %d\n%s", code, cli.ExitValidation, errOut)
	}
}

// TestLoad_SummaryCountsTheDocumentAndIgnoresWarnings pins the summary line's
// two counts over a document of several type names, and that a warning does
// not withhold it.
func TestLoad_SummaryCountsTheDocumentAndIgnoresWarnings(t *testing.T) {
	t.Parallel()

	dir, schemaPath := verdictFixture(t, map[string]string{
		"two.json": `{"Person": [
{"id": "a", "name": "A", "manager": {"_target_id": "a"}},
{"id": "b", "name": "B", "manager": {"_target_id": "a"}}
]}`,
	})
	companySchema := filepath.Join(dir, "company.yammm")
	if err := os.WriteFile(companySchema, []byte(managerSchema+`
type Company {
    cid String primary
}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	both := filepath.Join(dir, "both.json")
	if err := os.WriteFile(both, []byte(`{"Company": [{"cid": "c"}], "Person": [
{"id": "a", "name": "A", "manager": {"_target_id": "a"}},
{"id": "b", "name": "B", "manager": {"_target_id": "a"}}
]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"load", schemaPath, filepath.Join(dir, "two.json")}, "loaded 2 instances of 1 types\n"},
		{[]string{"load", companySchema, both}, "loaded 3 instances of 2 types\n"},
		{[]string{"load", "testdata/annotation_shadowed.yammm", "testdata/annotation_shadowed_good.json"}, "loaded 1 instances of 1 types\n"},
	} {
		code, _, errOut := executeCmdOutput(t, tc.args...)
		if code != cli.ExitOK || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: exit %d, stderr %q; want exit 0 and %q", tc.args, code, errOut, tc.want)
		}
	}
}
