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
		{"table.csv", "id\na\n", cli.ExitValidation, "E_DATA_PARSE"},
		{"table.tsv", "id\ta\n", cli.ExitValidation, "E_DATA_PARSE"},
		{"list.json", `[{"id": "a"}]`, cli.ExitValidation, "E_DATA_PARSE"},
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

// No data command takes a flag that names a data format or a type for one, so
// each draws a usage refusal rather than a read.
func TestDataCommands_RefuseTheFlagsOfAnotherFormat(t *testing.T) {
	t.Parallel()
	commands := map[string][]string{
		"check":         {"check"},
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
}

// No help text names a format the commands do not read.
func TestDataCommandHelp_NamesNoOtherFormat(t *testing.T) {
	t.Parallel()
	for _, command := range [][]string{{"check"}, {"snapshot", "save"}, {}} {
		out := strings.ToLower(helpText(t, command...))
		for _, word := range []string{"csv", "tsv", "--from", "--type", "--output-dir"} {
			if strings.Contains(out, word) {
				t.Errorf("help of %q names %q:\n%s", command, word, out)
			}
		}
	}
}
