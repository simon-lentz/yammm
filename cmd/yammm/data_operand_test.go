package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// A data operand reads as JSON whatever its name: no ending selects a format, so
// a JSON document passes under any name and any other text draws a parse failure
// at exit 1, never a usage refusal.
func TestDataOperand_ReadsAsJSONWhateverItsName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "s.yammm")
	if err := os.WriteFile(schemaPath, []byte("schema \"s\"\n\ntype T {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const doc = `{"T": [{"id": "a"}]}`
	for _, c := range []struct {
		name, content string
		want          int
		wantCode      string
	}{
		{"data", doc, cli.ExitOK, ""},
		{"data.txt", doc, cli.ExitOK, ""},
		{"data.csv", doc, cli.ExitOK, ""},
		{"table.csv", "id\na\n", cli.ExitValidation, "E_ADAPTER_PARSE"},
		{"table.tsv", "id\ta\n", cli.ExitValidation, "E_ADAPTER_PARSE"},
		{"list.json", `[{"id": "a"}]`, cli.ExitValidation, "E_ADAPTER_PARSE"},
		{"saved.ys", `{"yammm_snapshot": {}, "types": []}`, cli.ExitValidation, "E_INVALID_TYPE_TAG"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(dir, c.name)
			if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			code, _, errOut := executeCmdOutput(t, "check", schemaPath, path)
			if code != c.want {
				t.Fatalf("exit %d, want %d\n%s", code, c.want, errOut)
			}
			if !strings.Contains(errOut, c.wantCode) {
				t.Errorf("stderr does not name %q:\n%s", c.wantCode, errOut)
			}
		})
	}
}

// No data command takes a flag that names a data format or a type for one, and
// csv is no export target, so each draws a usage refusal rather than a read.
func TestDataCommands_RefuseTheFlagsOfAnotherFormat(t *testing.T) {
	t.Parallel()
	commands := map[string][]string{
		"check":         {"check"},
		"load":          {"load"},
		"export":        {"export", "--to", "json"},
		"snapshot save": {"snapshot", "save", "-o", filepath.Join(t.TempDir(), "o.ys")},
	}
	for name, command := range commands {
		for _, flag := range [][]string{{"--from", "json"}, {"--type", "Person"}, {"--type-column", "kind"}} {
			t.Run(name+" "+flag[0], func(t *testing.T) {
				t.Parallel()
				argv := slices.Concat(command, flag, []string{"testdata/valid.yammm", "testdata/data.json"})
				if code, _, errOut := executeCmdOutput(t, argv...); code != cli.ExitUsage {
					t.Errorf("exit %d, want %d\n%s", code, cli.ExitUsage, errOut)
				}
			})
		}
	}
	for name, argv := range map[string][]string{
		"export --output-dir": {"export", "--to", "json", "--output-dir", t.TempDir(), "testdata/valid.yammm", "testdata/data.json"},
		"export --to csv":     {"export", "--to", "csv", "testdata/valid.yammm", "testdata/data.json"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if code, _, errOut := executeCmdOutput(t, argv...); code != cli.ExitUsage {
				t.Errorf("exit %d, want %d\n%s", code, cli.ExitUsage, errOut)
			}
		})
	}
}

// export reads a snapshot under any name, and a data document only under a name an
// identity can hold, so the refusal of a name that is not valid UTF-8 follows the read.
func TestExport_ANameThatIsNotUTF8RefusesADocumentAndReadsASnapshot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad\xff.json")
	if err := os.WriteFile(bad, []byte(`{"Person": []}`), 0o600); err != nil {
		t.Skipf("the host holds no such name: %v", err)
	}
	if code, _, errOut := executeCmdOutput(t, "export", "--to", "json", "testdata/valid.yammm", bad); code != cli.ExitUsage {
		t.Errorf("a data document: exit %d, want %d\n%s", code, cli.ExitUsage, errOut)
	}
	snap, err := os.ReadFile(filepath.Join("testdata", "valid_snapshot.ys"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad, snap, 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := executeCmdOutput(t, "export", "--to", "json", "testdata/valid.yammm", bad); code != cli.ExitOK {
		t.Errorf("a snapshot: exit %d, want %d\n%s", code, cli.ExitOK, errOut)
	}
}

// A snapshot the sniff sees past a byte order mark or a comment is refused by the
// snapshot reader, which says so, rather than read as a data document.
func TestExport_ASnapshotWithAPrefixIsRefusedAsASnapshot(t *testing.T) {
	t.Parallel()
	snap, err := os.ReadFile(filepath.Join("testdata", "valid_snapshot.ys"))
	if err != nil {
		t.Fatal(err)
	}
	for name, prefix := range map[string]string{"a byte order mark": "\xef\xbb\xbf", "a comment": "// c\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "s.ys")
			if err := os.WriteFile(path, append([]byte(prefix), snap...), 0o600); err != nil {
				t.Fatal(err)
			}
			code, _, errOut := executeCmdOutput(t, "export", "--to", "json", "testdata/valid.yammm", path)
			if code != cli.ExitValidation || !strings.Contains(errOut, "E_SNAPSHOT_MALFORMED") || strings.Contains(errOut, "E_INVALID_TYPE_TAG") {
				t.Errorf("exit %d, want %d and E_SNAPSHOT_MALFORMED alone\n%s", code, cli.ExitValidation, errOut)
			}
		})
	}
}

// export reads a data document after sniffing it, so its parse names the operand
// and its success reports the summary line, as load's do.
func TestExport_ADataDocumentNamesItsOperandAndReportsItsSummary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := filepath.Join(dir, "table.csv")
	if err := os.WriteFile(bad, []byte("id\na\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := executeCmdOutput(t, "export", "--to", "json", "testdata/valid.yammm", bad)
	if code != cli.ExitValidation || !strings.Contains(errOut, "table.csv") || !strings.Contains(errOut, "E_ADAPTER_PARSE") {
		t.Errorf("a parse failure: exit %d, want %d naming table.csv and E_ADAPTER_PARSE\n%s", code, cli.ExitValidation, errOut)
	}
	code, _, errOut = executeCmdOutput(t, "export", "--to", "json", "testdata/valid.yammm", "testdata/data.json")
	if code != cli.ExitOK || !strings.Contains(errOut, "loaded ") {
		t.Errorf("a valid document: exit %d, want %d and a summary line\n%s", code, cli.ExitOK, errOut)
	}
}

// A snapshot's export honours --output as a data document's does.
func TestExport_SnapshotWritesToOutput(t *testing.T) {
	t.Parallel()
	out := filepath.Join(t.TempDir(), "o.json")
	code, stdout, errOut := executeCmdOutput(t, "export", "--to", "json", "--output", out,
		"testdata/valid.yammm", filepath.Join("testdata", "valid_snapshot.ys"))
	if code != cli.ExitOK {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	if data, err := os.ReadFile(out); err != nil || len(data) == 0 {
		t.Errorf("output file: %d bytes, %v", len(data), err)
	}
}

// No help text names a format the commands do not read.
func TestDataCommandHelp_NamesNoOtherFormat(t *testing.T) {
	t.Parallel()
	for _, command := range [][]string{{"check"}, {"load"}, {"export"}, {"snapshot", "save"}, {}} {
		out := strings.ToLower(helpText(t, command...))
		for _, word := range []string{"csv", "tsv", "--from", "--type", "--output-dir"} {
			if strings.Contains(out, word) {
				t.Errorf("help of %q names %q:\n%s", command, word, out)
			}
		}
	}
}
