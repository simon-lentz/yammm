package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// writeDataFile writes content under a fresh directory and returns its path.
func writeDataFile(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// wantSourceID is the identity the loader mints for path.
func wantSourceID(t *testing.T, path string) location.SourceID {
	t.Helper()
	id, _, err := location.ResolveSourcePath(path)
	if err != nil {
		t.Fatalf("resolve %q: %v", path, err)
	}
	return id
}

// TestLoadAndParseJSON_MintsTheLoadersIdentity pins that a data file gets the
// file-backed identity the schema loader gives a schema file, not a synthetic
// one built by concatenation.
func TestLoadAndParseJSON_MintsTheLoadersIdentity(t *testing.T) {
	t.Parallel()
	p := writeDataFile(t, "data.json", `{"Anchor": [{"id": "a1"}]}`)

	parsed, res, err := LoadAndParseJSON(t.Context(), p)
	if err != nil {
		t.Fatalf("LoadAndParseJSON: %v", err)
	}
	if !res.OK() {
		t.Fatalf("parse: %s", res)
	}

	want := wantSourceID(t, p)
	span := parsed["Anchor"][0].Provenance.Span()
	if span.Source != want {
		t.Errorf("span source %q, want %q", span.Source, want)
	}
	if !span.Source.IsFilePath() {
		t.Errorf("identity %q is not file-backed", span.Source)
	}
}

// TestLoadAndParseCSV_MintsTheLoadersIdentity pins the same rule on the CSV
// entry point.
func TestLoadAndParseCSV_MintsTheLoadersIdentity(t *testing.T) {
	t.Parallel()
	s := closureSchema(t)
	p := writeDataFile(t, "data.csv", "id\na1\n")

	parsed, res, err := LoadAndParseCSV(t.Context(), p, "Anchor", "", s)
	if err != nil {
		t.Fatalf("LoadAndParseCSV: %v", err)
	}
	if !res.OK() {
		t.Fatalf("parse: %s", res)
	}

	want := wantSourceID(t, p)
	span := parsed["Anchor"][0].Provenance.Span()
	if span.Source != want {
		t.Errorf("span source %q, want %q", span.Source, want)
	}
	if !span.Source.IsFilePath() {
		t.Errorf("identity %q is not file-backed", span.Source)
	}
}

// TestLoadAndParse_OneSpellingOfOneFileGivesOneIdentity pins what a synthetic
// identity built by concatenation cannot give: two spellings of one path are
// one identity, so a diagnostic and a snapshot agree on which file they name.
func TestLoadAndParse_OneSpellingOfOneFileGivesOneIdentity(t *testing.T) {
	t.Parallel()
	p := writeDataFile(t, "data.json", `{"Anchor": [{"id": "a1"}]}`)
	// An uncleaned spelling of the same file: a synthetic identity built by
	// concatenation keeps it verbatim and gives a second identity.
	dir := filepath.Dir(p)
	indirect := dir + "/../" + filepath.Base(dir) + "/data.json"

	first, _, err := LoadAndParseJSON(t.Context(), p)
	if err != nil {
		t.Fatalf("LoadAndParseJSON: %v", err)
	}
	second, _, err := LoadAndParseJSON(t.Context(), indirect)
	if err != nil {
		t.Fatalf("LoadAndParseJSON: %v", err)
	}

	a := first["Anchor"][0].Provenance.Span().Source
	b := second["Anchor"][0].Provenance.Span().Source
	if a != b {
		t.Errorf("two spellings gave two identities: %q and %q", a, b)
	}
}

// TestLoadAndParse_RefusesAPathTheResolverRejects drives the resolve arm both
// entry points gained.
//
// A MISSING file does not reach it: [location.ResolveSourcePath] resolves a path
// whose leaf does not exist yet, and the read that follows is what fails. The
// arm fires for a path that cannot name a file at all — an empty one, and one
// that descends through a regular file.
func TestLoadAndParse_RefusesAPathTheResolverRejects(t *testing.T) {
	t.Parallel()
	regular := writeDataFile(t, "data.json", `{"Anchor": [{"id": "a1"}]}`)

	for _, c := range []struct {
		name string
		path string
	}{
		{"empty path", ""},
		{"a path under a regular file", filepath.Join(regular, "inner.json")},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := LoadAndParseJSON(t.Context(), c.path)
			if err == nil {
				t.Errorf("LoadAndParseJSON accepted %q", c.path)
			} else if !strings.Contains(err.Error(), "resolve data file") {
				t.Errorf("LoadAndParseJSON: %v, want the resolve arm", err)
			}

			_, _, err = LoadAndParseCSV(t.Context(), c.path, "Anchor", "", closureSchema(t))
			if err == nil {
				t.Errorf("LoadAndParseCSV accepted %q", c.path)
			} else if !strings.Contains(err.Error(), "resolve data file") {
				t.Errorf("LoadAndParseCSV: %v, want the resolve arm", err)
			}
		})
	}
}

// TestLoadAndParse_ReadsAMissingFileThroughTheReadArm pins the boundary the test
// above depends on: the resolver accepts a path whose leaf is absent, so a
// missing file is still reported by the read, not by the resolve.
func TestLoadAndParse_ReadsAMissingFileThroughTheReadArm(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "nope", "data.json")

	_, _, err := LoadAndParseJSON(t.Context(), missing)
	if err == nil {
		t.Fatalf("LoadAndParseJSON accepted a missing file")
	}
	if !strings.Contains(err.Error(), "read data file") {
		t.Errorf("LoadAndParseJSON: %v, want the read arm", err)
	}

	_, _, err = LoadAndParseCSV(t.Context(), missing, "Anchor", "", closureSchema(t))
	if err == nil {
		t.Fatalf("LoadAndParseCSV accepted a missing file")
	}
	if !strings.Contains(err.Error(), "open data file") {
		t.Errorf("LoadAndParseCSV: %v, want the open arm", err)
	}
}
