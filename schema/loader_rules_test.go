package schema_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// An empty schema name is an authoring error, refused where it was written.
func TestLoad_EmptySchemaNameIsRefused(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), "schema \"\"\n\ntype T {\n    id String primary\n}\n", "s.yammm")
	if !s.IsZero() {
		t.Error("a nameless schema must not load")
	}
	if !res.HasCode(diag.E_INVALID_NAME) {
		t.Errorf("want E_INVALID_NAME; got %v", res.Err())
	}
	if res.HasCode(diag.E_INTERNAL) || res.HasCode(diag.E_DUPLICATE_TYPE) {
		t.Errorf("an authoring error must not be reported as the loader's; got %v", res.Err())
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Every sealed object is sealed once, by its creator; a second call is a bug
// and panics as the mutators it protects do.
func TestSeal_SecondCallPanics(t *testing.T) {
	t.Parallel()

	rel := schema.TestNewRelation(schema.RelationAssociation, "R", "r", schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "Owner", nil)
	schema.TestSealRelation(rel)
	yammmtest.AssertPanics(t, func() { schema.TestSealRelation(rel) })

	s := schema.TestNewSchema("s", location.SourceID{}, location.Span{}, "")
	schema.TestSealSchema(s)
	yammmtest.AssertPanics(t, func() { schema.TestSealSchema(s) })

	typ := schema.TestNewType("T", location.SourceID{}, location.Span{}, "", false, false)
	schema.TestSealType(typ)
	yammmtest.AssertPanics(t, func() { schema.TestSealType(typ) })

	dt := schema.TestNewDataType("D", schema.NewStringConstraint(), location.Span{}, "")
	schema.TestSealDataType(dt)
	yammmtest.AssertPanics(t, func() { schema.TestSealDataType(dt) })
}

// A schema source is read up to a fixed bound; a larger file is refused before
// it is parsed, whether it is the entry or an import.
func TestLoad_RefusesAnOversizedSource(t *testing.T) {
	t.Parallel()

	big := make([]byte, 16<<20+1)
	copy(big, "schema \"big\"\n")
	for i := len("schema \"big\"\n"); i < len(big); i++ {
		big[i] = ' '
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.yammm"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.yammm"), []byte("schema \"main\"\n\nimport \"./big.yammm\" as b\n\ntype T {\n    id String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, res := schema.Load(t.Context(), filepath.Join(dir, "big.yammm"), schema.WithModuleRoot(dir))
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "larger than") {
		t.Errorf("an oversized entry must be refused by size; got %v", res.Err())
	}
	_, res = schema.Load(t.Context(), filepath.Join(dir, "main.yammm"), schema.WithModuleRoot(dir))
	if res.Err() == nil || !strings.Contains(res.Err().Error(), "larger than") {
		t.Errorf("an oversized import must be refused by size; got %v", res.Err())
	}
}

// An empty source key names no file, and a file-backed identity for it would
// be the working directory's. It is refused, as the synthetic-root door
// already refused it, before any entry is selected.
func TestLoadSourcesWithEntry_EmptyKeyIsRefused(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"":        []byte("schema \"alpha\"\n\ntype A {\n    id String primary\n}\n"),
		"b.yammm": []byte("schema \"bravo\"\n\ntype B {\n    id String primary\n}\n"),
	}
	rows := []struct {
		name string
		root func(t *testing.T) string
	}{
		{"no module root", func(*testing.T) string { return "" }},
		// Joined under a root, the empty key would name the root directory.
		{"a module root", func(t *testing.T) string {
			t.Helper()
			return t.TempDir()
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root := row.root(t)
			for i := range 20 {
				s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "", root, schema.WithSourcesOnly(true))
				if res.Err() == nil {
					t.Fatalf("run %d loaded %v; want the empty key refused", i, s)
				}
				// The loader renders the cause into a diagnostic, so the sentinel
				// reaches the caller as text rather than through the error chain.
				if !res.HasCode(diag.E_LOAD_IO_FAILURE) || !strings.Contains(res.Err().Error(), location.ErrEmptyPath.Error()) {
					t.Fatalf("run %d: %v; want E_LOAD_IO_FAILURE naming %v", i, res.Err(), location.ErrEmptyPath)
				}
			}
		})
	}
}

// Two source keys that name one source are refused, and the refusal names
// both keys: registering both would let one key's content stand for the other.
func TestLoadSourcesWithEntry_TwoKeysNamingOneSourceAreRefused(t *testing.T) {
	t.Parallel()

	src := []byte("schema \"alpha\"\n\ntype A {\n    id String primary\n}\n")
	rows := []struct {
		name string
		keys func(t *testing.T, root string) (string, string)
	}{
		{
			name: "a key with a dot segment",
			keys: func(*testing.T, string) (string, string) { return "./a.yammm", "a.yammm" },
		},
		{
			name: "an absolute key and a relative key",
			keys: func(_ *testing.T, root string) (string, string) { return filepath.Join(root, "a.yammm"), "a.yammm" },
		},
		{
			name: "a key typed in another case",
			keys: func(t *testing.T, root string) (string, string) {
				t.Helper()
				if !yammmtest.CaseFoldingFilesystem(t, root) {
					t.Skip("the filesystem is case-sensitive, so two spellings name two files")
				}
				if err := os.WriteFile(filepath.Join(root, "A.yammm"), src, 0o600); err != nil {
					t.Fatal(err)
				}
				return "A.yammm", "a.yammm"
			},
		},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			first, second := row.keys(t, root)
			sources := map[string][]byte{first: src, second: src}
			s, res := schema.LoadSourcesWithEntry(t.Context(), sources, second, root)
			if !s.IsZero() {
				t.Errorf("loaded %q; want the two keys refused", s.Name())
			}
			if !res.HasFatal() || !res.HasCode(diag.E_LOAD_IO_FAILURE) {
				t.Fatalf("want a Fatal E_LOAD_IO_FAILURE; got %v", res.Err())
			}
			msg := res.Err().Error()
			for _, key := range []string{first, second} {
				if !strings.Contains(msg, strconv.Quote(key)) {
					t.Errorf("the refusal does not name key %q: %s", key, msg)
				}
			}
		})
	}
}

// A source name that is not valid UTF-8 cannot be written as an identity, so
// LoadString refuses it before it mints one.
func TestLoadString_RefusesASourceNameThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), "schema \"s\"\n\ntype T {\n    id String primary\n}\n", "caf\xff")
	if !s.IsZero() {
		t.Errorf("loaded %q; want the source name refused", s.Name())
	}
	if !res.HasFatal() || !res.HasCode(diag.E_LOAD_IO_FAILURE) || !strings.Contains(res.Err().Error(), location.ErrInvalidUTF8Path.Error()) {
		t.Fatalf("want a Fatal E_LOAD_IO_FAILURE naming %v; got %v", location.ErrInvalidUTF8Path, res.Err())
	}
}

// The entry key chooses the schema, whether or not it is the key an empty entry
// path would select.
func TestLoadSourcesWithEntry_EntryKeyChoosesTheSchema(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"a.yammm": []byte("schema \"alpha\"\n\ntype A {\n    id String primary\n}\n"),
		"b.yammm": []byte("schema \"bravo\"\n\ntype B {\n    id String primary\n}\n"),
	}
	for _, row := range []struct{ entry, want string }{
		{"a.yammm", "alpha"},
		{"b.yammm", "bravo"},
	} {
		s, res := schema.LoadSourcesWithEntry(t.Context(), sources, row.entry, t.TempDir(), schema.WithSourcesOnly(true))
		if res.Err() != nil {
			t.Fatalf("entry %s: %v", row.entry, res.Err())
		}
		if s.IsZero() || s.Name() != row.want {
			t.Errorf("entry %s chose %v; want %q", row.entry, s, row.want)
		}
	}
}

// An empty entry path selects the lexicographically smallest key, on every
// load, whatever order the map yields its keys in.
func TestLoadSourcesWithEntry_EmptyEntrySelectsTheSmallestKey(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{}
	for _, name := range []string{"hotel", "delta", "golf", "alpha", "foxtrot", "charlie", "echo", "bravo"} {
		sources[name+".yammm"] = []byte("schema \"" + name + "\"\n\ntype T {\n    id String primary\n}\n")
	}
	for i := range 20 {
		s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "", t.TempDir(), schema.WithSourcesOnly(true))
		if res.Err() != nil {
			t.Fatalf("run %d: %v", i, res.Err())
		}
		if s.IsZero() || s.Name() != "alpha" {
			t.Fatalf("run %d chose %v; want the schema under alpha.yammm", i, s)
		}
	}
}

// A source of exactly the bound is legal; the first byte beyond it is refused.
func TestLoad_SourceBoundIsInclusive(t *testing.T) {
	t.Parallel()

	head := "schema \"edge\"\n\ntype T {\n    id String primary\n}\n"
	exact := make([]byte, 16<<20)
	copy(exact, head)
	for i := len(head); i < len(exact); i++ {
		exact[i] = ' '
	}
	dir := writeFiles(t, map[string]string{"edge.yammm": string(exact)})
	if _, res := schema.Load(t.Context(), filepath.Join(dir, "edge.yammm"), schema.WithModuleRoot(dir)); res.Err() != nil {
		t.Errorf("a source of exactly the bound must load; got %v", res.Err())
	}
}
