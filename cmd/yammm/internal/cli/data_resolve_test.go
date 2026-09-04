package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func closureSchema(t *testing.T) *schema.Schema {
	t.Helper()
	m := map[string][]byte{
		"entry.yammm": []byte(`schema "app"

import "common.yammm" as common

type Anchor {
	id String primary
}
`),
		"common.yammm": []byte(`schema "common"

type Region {
	id String primary
	population Integer
}
`),
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), m, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if !res.OK() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func writeCSV(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "data.csv")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// The CLI resolves a type name by the rule the validator applies — bare for
// an entry-schema type, alias-qualified for a directly imported one — so an
// alias-qualified --type drives coercion instead of leaving every value a
// string for the validator to refuse.
func TestLoadAndParseCSV_ResolvesAliasQualifiedType(t *testing.T) {
	t.Parallel()
	s := closureSchema(t)
	p := writeCSV(t, "id,population\nr1,42\n")

	parsed, res, err := LoadAndParseCSV(t.Context(), p, "common.Region", "", s)
	if err != nil {
		t.Fatalf("LoadAndParseCSV: %v", err)
	}
	if !res.OK() {
		t.Fatalf("parse: %s", res)
	}
	raws := parsed["common.Region"]
	if len(raws) != 1 {
		t.Fatalf("got %d rows, want 1", len(raws))
	}
	if _, isString := raws[0].Properties["population"].(string); isString {
		t.Errorf("population was not coerced: %#v", raws[0].Properties["population"])
	}
}

// An unknown --type is the caller's mistake, reported as such, not a silent
// pass-through of uncoerced strings.
func TestLoadAndParseCSV_UnknownTypeIsAnError(t *testing.T) {
	t.Parallel()
	s := closureSchema(t)
	p := writeCSV(t, "id\nr1\n")

	_, _, err := LoadAndParseCSV(t.Context(), p, "Nope", "", s)
	if err == nil || !strings.Contains(err.Error(), `"Nope"`) {
		t.Fatalf("err = %v, want one naming the type", err)
	}
}
