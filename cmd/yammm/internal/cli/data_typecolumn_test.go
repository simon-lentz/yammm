package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// LoadAndParseCSV must pass the --type-column flag value into the adapter:
// ParseWithTypeColumn refuses to run when WithTypeColumn was never set, so a
// bare adapter turns every multi-type CSV invocation into E_CSV_COERCE.
func TestLoadAndParseCSV_TypeColumnReachesAdapter(t *testing.T) {
	t.Parallel()
	s, result := schema.LoadString(t.Context(), `schema "cli"

type Person {
	id String primary
}

type Company {
	id String primary
}
`, "cli.yammm")
	if result.HasErrors() {
		t.Fatalf("load schema: %s", result)
	}

	p := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(p, []byte("kind,id\nPerson,p1\nCompany,c1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	parsed, res, err := LoadAndParseCSV(t.Context(), p, "", "kind", s)
	if err != nil {
		t.Fatalf("LoadAndParseCSV: %v", err)
	}
	if res.HasErrors() {
		t.Fatalf("type-column parse failed: %s", res)
	}
	if len(parsed["Person"]) != 1 || len(parsed["Company"]) != 1 {
		t.Errorf("parsed = %d Person, %d Company; want 1 and 1",
			len(parsed["Person"]), len(parsed["Company"]))
	}
}
