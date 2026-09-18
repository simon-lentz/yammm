package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// check reads a ".tsv" file by tabs, so export must write one by tabs or the
// pair stops round-tripping. A file named anything else, stdout and the
// per-type ".csv" files of --output-dir keep ','.
func TestExport_CSVDelimiterFollowsTheOutputExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "people.yammm")
	dataPath := filepath.Join(dir, "people.json")
	writeFile := func(p, text string) {
		t.Helper()
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(schemaPath, `schema "people"

type Person {
	id String primary
	name String required
}
`)
	writeFile(dataPath, `{"Person": [{"id": "p1", "name": "Smith, Ann"}]}`)

	for _, c := range []struct {
		file, want string
	}{
		{"out.tsv", "id\tname\np1\tSmith, Ann\n"},
		{"out.csv", "id,name\np1,\"Smith, Ann\"\n"},
	} {
		t.Run(c.file, func(t *testing.T) {
			t.Parallel()
			out := filepath.Join(t.TempDir(), c.file)
			if code := executeCmd(t, "export", "--to", "csv", "--output", out, schemaPath, dataPath); code != cli.ExitOK {
				t.Fatalf("export exit %d", code)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("export wrote %q, want %q", got, c.want)
			}
			if code := executeCmd(t, "check", schemaPath, out, "--type", "Person"); code != cli.ExitOK {
				t.Errorf("check of the exported %s exit %d, want %d", c.file, code, cli.ExitOK)
			}
		})
	}

	t.Run("stdout", func(t *testing.T) {
		t.Parallel()
		code, stdout, _ := executeCmdOutput(t, "export", "--to", "csv", schemaPath, dataPath)
		if code != cli.ExitOK {
			t.Fatalf("export exit %d", code)
		}
		if want := "id,name\np1,\"Smith, Ann\"\n"; stdout != want {
			t.Errorf("export wrote %q to stdout, want %q", stdout, want)
		}
	})

	t.Run("--output-dir", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if code := executeCmd(t, "export", "--to", "csv", "--output-dir", dir, schemaPath, dataPath); code != cli.ExitOK {
			t.Fatalf("export exit %d", code)
		}
		got, err := os.ReadFile(filepath.Join(dir, "Person.csv"))
		if err != nil {
			t.Fatal(err)
		}
		if want := "id,name\np1,\"Smith, Ann\"\n"; string(got) != want {
			t.Errorf("export wrote %q, want %q", got, want)
		}
	})
}
