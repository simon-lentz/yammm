package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// A tab is named by the extension alone and only by ".tsv", folded the way
// DetectFormat folds it: every name that selects a tab is one DetectFormat
// reads as CSV, so --from is never needed to reach the tab.
func TestCSVDelimiter(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		path string
		want rune
	}{
		{"data.tsv", '\t'},
		{"DATA.TSV", '\t'},
		{"dir.tsv/data.csv", ','},
		{"data.tsv.csv", ','},
		{"datatsv", ','},
		{"data.tab", ','},
		{"data.t\u017fv", ','}, // LATIN SMALL LETTER LONG S folds to "s" under Unicode folding alone
		{"data.csv", ','},
		{"data.txt", ','},
		{"", ','},
	} {
		got := CSVDelimiter(c.path)
		if got != c.want {
			t.Errorf("CSVDelimiter(%q) = %q, want %q", c.path, got, c.want)
		}
		if format, err := DetectFormat(c.path); got == '\t' && (err != nil || format != "csv") {
			t.Errorf("CSVDelimiter(%q) names a tab, but DetectFormat reads %q, %v", c.path, format, err)
		}
	}
}

// DetectFormat reads a ".tsv" file as CSV, so both CSV entry points must split
// it on tabs. The comma inside the name is what a comma split would break.
func TestLoadAndParseCSV_ReadsATSVFileByTabs(t *testing.T) {
	t.Parallel()
	s, result := schema.LoadString(t.Context(), `schema "cli"

type Person {
	id String primary
	name String required
}
`, "cli.yammm")
	if result.HasErrors() {
		t.Fatalf("load schema: %s", result)
	}

	for _, c := range []struct {
		name, data, typeName, typeColumn string
	}{
		{"typed", "id\tname\np1\tSmith, Ann\n", "Person", ""},
		{"type column", "kind\tid\tname\nPerson\tp1\tSmith, Ann\n", "", "kind"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "data.tsv")
			if err := os.WriteFile(p, []byte(c.data), 0o600); err != nil {
				t.Fatal(err)
			}

			parsed, res, err := LoadAndParseCSV(t.Context(), p, c.typeName, c.typeColumn, s)
			if err != nil {
				t.Fatalf("LoadAndParseCSV: %v", err)
			}
			if res.HasErrors() {
				t.Fatalf("parse: %s", res)
			}
			if len(parsed["Person"]) != 1 {
				t.Fatalf("parsed %v, want one Person", parsed)
			}
			props := parsed["Person"][0].Properties
			if props["id"] != "p1" || props["name"] != "Smith, Ann" || len(props) != 2 {
				t.Errorf("properties = %#v, want id p1 and name %q", props, "Smith, Ann")
			}
		})
	}
}

// The delimiter follows the name the caller typed, which is the name
// DetectFormat reads, not the name a symlink resolves to.
func TestLoadAndParseCSV_DelimiterFollowsTheCallersSpelling(t *testing.T) {
	t.Parallel()
	s, result := schema.LoadString(t.Context(), `schema "cli"

type Person {
	id String primary
	name String required
}
`, "cli.yammm")
	if result.HasErrors() {
		t.Fatalf("load schema: %s", result)
	}

	for _, c := range []struct {
		name, typed, target, data, typeColumn string
	}{
		{"typed .tsv to a .csv file", "data.tsv", "real.csv", "id\tname\np1\tSmith, Ann\n", ""},
		{"typed .csv to a .tsv file", "data.csv", "real.tsv", "id,name\np1,\"Smith, Ann\"\n", ""},
		{"type column, typed .tsv to a .csv file", "data.tsv", "real.csv", "kind\tid\tname\nPerson\tp1\tSmith, Ann\n", "kind"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			target := filepath.Join(dir, c.target)
			if err := os.WriteFile(target, []byte(c.data), 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, c.typed)
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("this host cannot create a symlink: %v", err)
			}

			typeName := "Person"
			if c.typeColumn != "" {
				typeName = ""
			}
			parsed, res, err := LoadAndParseCSV(t.Context(), link, typeName, c.typeColumn, s)
			if err != nil {
				t.Fatalf("LoadAndParseCSV: %v", err)
			}
			if res.HasErrors() {
				t.Fatalf("parse: %s", res)
			}
			if len(parsed["Person"]) != 1 || parsed["Person"][0].Properties["name"] != "Smith, Ann" {
				t.Errorf("parsed %v, want one Person named %q", parsed, "Smith, Ann")
			}
		})
	}
}
