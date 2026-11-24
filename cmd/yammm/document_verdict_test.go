package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
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
// argument list over schemaPath and dataPath under --json.
func dataCommands(t *testing.T, schemaPath, dataPath string) map[string][]string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.ys")
	return map[string][]string{
		"check":         {"check", "--json", schemaPath, dataPath},
		"snapshot save": {"snapshot", "save", "--json", "-o", out, schemaPath, dataPath},
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

// TestDataCommands_ADuplicateKeyNamesItsEarlierHolder pins that every
// E_DUPLICATE_PK names the key's first holder as a related location, whether
// the graph refused the later holder, the validator refused the first one or
// every one, or the holders sit in two files, so one key names one first holder.
func TestDataCommands_ADuplicateKeyNamesItsEarlierHolder(t *testing.T) {
	t.Parallel()

	dir, schemaPath := verdictFixture(t, map[string]string{
		"twice.json": `{"Person": [
{"id": "boss", "name": "A", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "B", "manager": {"_target_id": "boss"}}
]}`,
		"refused_first.json": `{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "B", "manager": {"_target_id": "boss"}}
]}`,
		"refused_then_two.json": `{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "B", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "C", "manager": {"_target_id": "boss"}}
]}`,
		"refused_thrice.json": `{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "boss", "manager": {"_target_id": "boss"}}
]}`,
		"first.json": `{"Person": [
{"id": "boss", "name": "A", "manager": {"_target_id": "boss"}}
]}`,
		"second.json": `{"Person": [
{"id": "boss", "name": "B", "manager": {"_target_id": "boss"}}
]}`,
	})
	cases := []struct {
		name        string
		files       []string
		wantSource  string
		wantLines   []int
		wantRelated string
		relatedLine int
	}{
		{"one file", []string{"twice.json"}, "twice.json", []int{3}, "twice.json", 2},
		{"a refused earlier holder", []string{"refused_first.json"}, "refused_first.json", []int{3}, "refused_first.json", 2},
		{"a refused first holder of three", []string{"refused_then_two.json"}, "refused_then_two.json", []int{3, 4}, "refused_then_two.json", 2},
		{"three refused holders", []string{"refused_thrice.json"}, "refused_thrice.json", []int{3, 4}, "refused_thrice.json", 2},
		{"two files", []string{"first.json", "second.json"}, "second.json", []int{2}, "first.json", 2},
	}
	for _, tc := range cases {
		paths := make([]string, len(tc.files))
		for i, f := range tc.files {
			paths[i] = filepath.Join(dir, f)
		}
		commands := map[string][]string{
			"check":         append([]string{"check", "--json", schemaPath}, paths...),
			"snapshot save": append([]string{"snapshot", "save", "--json", "-o", filepath.Join(t.TempDir(), "out.ys"), schemaPath}, paths...),
		}
		for command, args := range commands {
			t.Run(tc.name+"/"+command, func(t *testing.T) {
				t.Parallel()
				code, _, errOut := executeCmdOutput(t, args...)
				if code != cli.ExitValidation {
					t.Fatalf("exit code = %d, want %d", code, cli.ExitValidation)
				}
				var dups []documentIssue
				for _, iss := range decodeDocument(t, errOut).Issues {
					if iss.Code == "E_DUPLICATE_PK" {
						dups = append(dups, iss)
					}
				}
				if len(dups) != len(tc.wantLines) {
					t.Fatalf("E_DUPLICATE_PK drew %d issues, want %d", len(dups), len(tc.wantLines))
				}
				for i, iss := range dups {
					if iss.Span == nil || !strings.HasSuffix(iss.Span.Source, tc.wantSource) || iss.Span.Start.Line != tc.wantLines[i] {
						t.Errorf("E_DUPLICATE_PK %d span = %+v, want %s line %d", i, iss.Span, tc.wantSource, tc.wantLines[i])
					}
					if len(iss.Related) != 1 {
						t.Fatalf("E_DUPLICATE_PK %d has %d related locations, want 1", i, len(iss.Related))
					}
					rel := iss.Related[0]
					if rel.Message != "first declared here" {
						t.Errorf("related message = %q, want %q", rel.Message, "first declared here")
					}
					if rel.Span == nil || !strings.HasSuffix(rel.Span.Source, tc.wantRelated) || rel.Span.Start.Line != tc.relatedLine {
						t.Errorf("E_DUPLICATE_PK %d related span = %+v, want %s line %d", i, rel.Span, tc.wantRelated, tc.relatedLine)
					}
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
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--json", "-o", filepath.Join(dir, "out.ys"),
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
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--json", "-o", filepath.Join(dir, "out.ys"),
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

// TestSnapshotSaveInto_ADuplicateOfAMergedKeyNamesNoLocation pins that under
// --into a key the merged file holds draws E_DUPLICATE_PK at each new holder,
// a refused one and a valid one, with no related location: the merged file's
// instance has no span to name.
func TestSnapshotSaveInto_ADuplicateOfAMergedKeyNamesNoLocation(t *testing.T) {
	t.Parallel()

	dir, schemaPath := verdictFixture(t, map[string]string{
		"base.json": `{"Person": [{"id": "boss", "name": "Boss", "manager": {"_target_id": "boss"}}]}`,
		"more.json": `{"Person": [
{"id": "boss", "manager": {"_target_id": "boss"}},
{"id": "boss", "name": "B2", "manager": {"_target_id": "boss"}}
]}`,
	})
	base := filepath.Join(dir, "base.ys")
	if code, _, errOut := executeCmdOutput(t, "snapshot", "save", "-o", base, schemaPath, filepath.Join(dir, "base.json")); code != cli.ExitOK {
		t.Fatalf("save the base: exit %d\n%s", code, errOut)
	}
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--json", "-o", filepath.Join(dir, "out.ys"),
		"--into", base, schemaPath, filepath.Join(dir, "more.json"))
	if code != cli.ExitValidation {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitValidation)
	}
	doc := decodeDocument(t, errOut)
	if got := issueLines(doc, "E_DUPLICATE_PK"); !slices.Equal(got, []int{2, 3}) {
		t.Errorf("E_DUPLICATE_PK at lines %v, want [2 3]", got)
	}
	for _, iss := range doc.Issues {
		if iss.Code == "E_DUPLICATE_PK" && iss.Related != nil {
			t.Errorf("the E_DUPLICATE_PK at %+v names related locations %+v, want none", iss.Span, iss.Related)
		}
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
	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--json", "-o", filepath.Join(dir, "out.ys"),
		schemaPath, filepath.Join(dir, "first.json"), filepath.Join(dir, "missing.json"))
	if code != cli.ExitRuntime {
		t.Fatalf("exit code = %d, want %d", code, cli.ExitRuntime)
	}
	doc := decodeDocument(t, errOut)
	if got, want := codesOf(doc), []string{"E_COMMAND_FAILED", "E_DATA_PARSE"}; !slices.Equal(got, want) {
		t.Errorf("codes = %v, want %v: the first file's finding must survive the second file's failure", got, want)
	}
}

// TestSnapshotSave_RefusesAUsageErrorBeforeReadingAnyFile pins that every data
// operand a read could not answer is refused before the first data file is read,
// so it reports nothing an earlier file would have drawn.
func TestSnapshotSave_RefusesAUsageErrorBeforeReadingAnyFile(t *testing.T) {
	t.Parallel()

	dir, schemaPath := verdictFixture(t, map[string]string{
		"first.json": `{"Person": [{"id": "a", "id": "b", "name": "A", "manager": {"_target_id": "a"}}]}`,
	})
	for name, args := range map[string][]string{
		"an empty data path":            {filepath.Join(dir, "first.json"), ""},
		"a data path that is not UTF-8": {filepath.Join(dir, "first.json"), "bad\xff.json"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			argv := append([]string{"snapshot", "save", "--json", "-o", filepath.Join(t.TempDir(), "out.ys"), schemaPath}, args...)
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

// TestDataCommands_RefuseAUsageErrorBeforeAnyRead pins that check and snapshot
// save judge every data operand before they read the schema or a merged
// file: a schema that does not load and a merged file that does not exist draw
// nothing beside the usage error.
func TestDataCommands_RefuseAUsageErrorBeforeAnyRead(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.yammm")
	if err := os.WriteFile(broken, []byte("schema \"b\"\n\ntype {\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const data = "bad\xff.json"
	out := filepath.Join(dir, "out.ys")
	for name, argv := range map[string][]string{
		"check":                {"check", "--json", broken, data},
		"snapshot save":        {"snapshot", "save", "--json", "-o", out, broken, data},
		"snapshot save --into": {"snapshot", "save", "--json", "--into", filepath.Join(dir, "missing.ys"), broken, data},
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

// TestSnapshotSaveInto_ReportsAMissingTargetOfARootTheEntryCannotName pins that
// a base snapshot's root of a type the entry schema reaches only through another
// import keeps its E_UNRESOLVED_REQUIRED when the document refuses an entry type
// of the target's name and key: the refused root is another type.
func TestSnapshotSaveInto_ReportsAMissingTargetOfARootTheEntryCannotName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	for name, src := range map[string]string{
		"entry.yammm": `schema "entry"

import "./base" as base

type Beacon {
	id String primary
	--> LINK (one) Target
}

type Target {
	id String primary
	n Integer[0, 5]
}
`,
		"base.yammm": `schema "base"

import "./deep" as deep

type Basin {
	id String primary
	--> LIGHT (_:one) deep.Beacon
}
`,
		"deep.yammm": `schema "deep"

type Beacon {
	id String primary
	--> LINK (one) Target
}

type Target {
	id String primary
}
`,
		"refused.json": `{"Target": [{"id": "t1", "n": 99}]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entryPath := filepath.Join(dir, "entry.yammm")
	s, loadErr := schema.Load(ctx, entryPath)
	if loadErr != nil {
		t.Fatalf("load schema: %s", loadErr)
	}
	var deep schema.Schema
	for member := range s.Closure() {
		if member.Name() == "deep" {
			deep = member
		}
	}
	beacon, validateErr := instance.NewValidator(deep).ValidateOne(ctx, "Beacon",
		instance.RawInstance{Properties: map[string]any{"id": "b1", "link": map[string]any{"_target_id": "t1"}}})
	if validateErr != nil {
		t.Fatalf("validate deep Beacon: %s", validateErr)
	}
	g := graph.New(s)
	if err := g.Add(ctx, beacon); err != nil {
		t.Fatalf("add deep Beacon: %s", err)
	}
	data, readErr := snapshot.Marshal(ctx, g.Snapshot())
	if readErr != nil {
		t.Fatalf("marshal: %s", readErr)
	}
	base := filepath.Join(dir, "base.ys")
	if err := os.WriteFile(base, data, 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, errOut := executeCmdOutput(t, "snapshot", "save", "--json", "--into", base,
		"-o", filepath.Join(dir, "out.ys"), entryPath, filepath.Join(dir, "refused.json"))
	if code != cli.ExitValidation {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", code, cli.ExitValidation, errOut)
	}
	doc := decodeDocument(t, errOut)
	if got, want := codesOf(doc), []string{"E_CONSTRAINT_FAIL", "E_UNRESOLVED_REQUIRED"}; !slices.Equal(got, want) {
		t.Errorf("codes = %v, want %v", got, want)
	}
	for _, iss := range doc.Issues {
		if iss.Code != "E_UNRESOLVED_REQUIRED" {
			continue
		}
		if name, _ := iss.detail("type"); name != "deep#Beacon" {
			t.Errorf("the missing target's source is named %q, want %q", name, "deep#Beacon")
		}
	}
}
