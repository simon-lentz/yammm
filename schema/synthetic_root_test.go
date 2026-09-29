package schema_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// syntheticRootSources is the shared two-source fixture: an entry that imports
// its dependency module-style, which is the shape every consumer of
// WithSyntheticRoot uses and the shape both import guards reject without it.
func syntheticRootSources() map[string][]byte {
	return map[string][]byte{
		"a/b/x.yammm": []byte(`schema "core"

type Region {
	code String primary
}
`),
		"main.yammm": []byte(`schema "app"

import "a/b/x" as core

type County {
	fips String primary
	--> IN_REGION (one) core.Region
}
`),
	}
}

// firstError renders the result's diagnostics for a failure message.
func firstError(result diag.Result) string {
	var b strings.Builder
	for _, issue := range slices.Collect(result.Issues()) {
		b.WriteString(issue.Message())
		b.WriteString("; ")
	}
	return b.String()
}

// typeIDByName indexes the closure's types by name, so a test can compare two
// loads of one schema without depending on declaration order.
func typeIDByName(t *testing.T, s *schema.Schema) map[string]schema.TypeID {
	t.Helper()
	out := map[string]schema.TypeID{}
	for _, cs := range s.Closure() {
		for _, ty := range cs.TypesSlice() {
			out[ty.Name()] = ty.ID()
		}
	}
	return out
}

// TestWithSyntheticRoot_SameSchemaDifferentIdentities is the contract a consumer
// cutting over to embedded sources depends on: the schema is the same, and the
// type identities are deliberately not. StructuralHash is path-independent, so
// it agrees across the two loads while every SchemaPath differs.
func TestWithSyntheticRoot_SameSchemaDifferentIdentities(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	disk, res := schema.LoadSourcesWithEntry(ctx, syntheticRootSources(), "main.yammm", "/project")
	if res.HasErrors() {
		t.Fatalf("module-root load: %s", firstError(res))
	}
	embedded, res := schema.LoadSourcesWithEntry(ctx, syntheticRootSources(), "main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
	if res.HasErrors() {
		t.Fatalf("synthetic-root load: %s", firstError(res))
	}

	if got, want := schema.StructuralHash(embedded), schema.StructuralHash(disk); got != want {
		t.Errorf("StructuralHash differs across roots: got %s, want %s", got, want)
	}

	diskIDs, embeddedIDs := typeIDByName(t, disk), typeIDByName(t, embedded)
	if len(embeddedIDs) != len(diskIDs) {
		t.Fatalf("closure type counts differ: synthetic %d, module-root %d", len(embeddedIDs), len(diskIDs))
	}
	for name, embeddedID := range embeddedIDs {
		diskID, ok := diskIDs[name]
		if !ok {
			t.Fatalf("type %q absent from the module-root load", name)
		}
		if embeddedID.SchemaPath().String() == diskID.SchemaPath().String() {
			t.Errorf("type %q kept its schema path across roots: %s", name, embeddedID.SchemaPath())
		}
		if !strings.HasPrefix(embeddedID.SchemaPath().String(), "embedded://app/") {
			t.Errorf("type %q is not under the synthetic root: %s", name, embeddedID.SchemaPath())
		}
	}
}

// TestWithSyntheticRoot_DerivedIdentityIsExact pins the derived string itself.
// A consumer's persisted snapshots are keyed to it byte for byte, and three
// derivation sites must agree on it, so proving the identities merely differ is
// not enough: a later normalization change that looks harmless would re-key
// every snapshot such a consumer holds.
func TestWithSyntheticRoot_DerivedIdentityIsExact(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://root"))
	if res.HasErrors() {
		t.Fatalf("load: %s", firstError(res))
	}

	if got, want := s.SourceID().String(), "embedded://root/main.yammm"; got != want {
		t.Errorf("entry identity = %q, want %q", got, want)
	}
	ids := typeIDByName(t, s)
	if got, want := ids["Region"].SchemaPath().String(), "embedded://root/a/b/x.yammm"; got != want {
		t.Errorf("imported identity = %q, want %q", got, want)
	}
}

// TestWithSyntheticRoot_ModuleStyleImportResolves pins the guard change: at
// v0.12.2 an empty module root plus a non-file-backed entry reported
// E_IMPORT_RESOLVE before identity derivation was ever reached.
func TestWithSyntheticRoot_ModuleStyleImportResolves(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
	if res.HasErrors() {
		t.Fatalf("load: %s", firstError(res))
	}
	if len(s.ImportsSlice()) != 1 {
		t.Fatalf("expected one import, got %d", len(s.ImportsSlice()))
	}
	if s.ImportsSlice()[0].Schema() == nil {
		t.Error("the import resolved to no schema")
	}
}

// TestWithSyntheticRoot_RelativeImportResolvesByKey pins that a relative
// import under a synthetic root resolves against the importing source's key,
// as text: "./dep" from "main.yammm" is "dep.yammm", "../dep" from
// "sub/main.yammm" is "dep.yammm", and one climbing above the root keeps its
// "..".
func TestWithSyntheticRoot_RelativeImportResolvesByKey(t *testing.T) {
	t.Parallel()

	dep := []byte("schema \"core\"\n\ntype Region {\n\tcode String primary\n}\n")
	main := func(imp string) []byte {
		return []byte("schema \"app\"\n\nimport \"" + imp + "\" as core\n\n" +
			"type County {\n\tfips String primary\n\t--> IN_REGION (one) core.Region\n}\n")
	}
	for _, tc := range []struct {
		name, entry, imp, depKey, wantID string
	}{
		{"sibling", "main.yammm", "./dep", "dep.yammm", "embedded://app/dep.yammm"},
		{"parent", "sub/main.yammm", "../dep", "dep.yammm", "embedded://app/dep.yammm"},
		{"above the root", "main.yammm", "../dep", "../dep.yammm", "embedded://app/../dep.yammm"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sources := map[string][]byte{tc.depKey: dep, tc.entry: main(tc.imp)}
			s, res := schema.LoadSourcesWithEntry(t.Context(), sources, tc.entry, "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
			if res.HasErrors() {
				t.Fatalf("load: %s", firstError(res))
			}
			if got := s.ImportsSlice()[0].ResolvedSourceID().String(); got != tc.wantID {
				t.Errorf("import resolved to %q, want %q", got, tc.wantID)
			}
		})
	}
}

// TestWithSyntheticRoot_KeyResolvingToRootIsError covers all four inputs that
// clean to ".". Each reaches the root itself, which is not a source.
func TestWithSyntheticRoot_KeyResolvingToRootIsError(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"", ".", "./", "a/.."} {
		t.Run("key="+key, func(t *testing.T) {
			t.Parallel()
			sources := map[string][]byte{key: []byte("schema \"s\"\n\ntype T {\n\tid String primary\n}\n")}
			_, res := schema.LoadSourcesWithEntry(t.Context(), sources, key, "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
			if !res.HasErrors() {
				t.Fatalf("expected key %q to be rejected", key)
			}
			if msg := firstError(res); !strings.Contains(msg, "resolves to the synthetic root itself") {
				t.Errorf("expected the root-itself error, got: %s", msg)
			}
		})
	}
}

// TestWithSyntheticRoot_AbsoluteKeyIsError pins the documented exception to
// LoadSourcesWithEntry's absolute-entry-path form.
// The keys that look absolute only once cleaned or in NFC were accepted, and
// their normalized spellings refused, so one source took two verdicts.
func TestWithSyntheticRoot_AbsoluteKeyIsError(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"/abs/main.yammm", "C:/main.yammm", "./C:/main.yammm", "a/../C:/main.yammm", "\u212a:/main.yammm"} {
		sources := map[string][]byte{key: []byte("schema \"s\"\n\ntype T {\n\tid String primary\n}\n")}
		_, res := schema.LoadSourcesWithEntry(t.Context(), sources, key, "",
			schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
		if !res.HasErrors() {
			t.Errorf("expected the key %q to be rejected", key)
			continue
		}
		if msg := firstError(res); !strings.Contains(msg, "must be relative to the synthetic root") {
			t.Errorf("key %q: expected the relative-key error, got: %s", key, msg)
		}
	}
}

// TestWithSyntheticRoot_KeyNamingADirectoryAboveTheRootIsError pins that a key
// names a file: one cleaning to ".." or ending in ".." names a directory above
// the root, as "." names the root itself.
func TestWithSyntheticRoot_KeyNamingADirectoryAboveTheRootIsError(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"..", "../..", "a/../..", "../x/.."} {
		sources := map[string][]byte{key: []byte("schema \"s\"\n\ntype T {\n\tid String primary\n}\n")}
		_, res := schema.LoadSourcesWithEntry(t.Context(), sources, key, "",
			schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
		if msg := firstError(res); !strings.Contains(msg, "names a directory above the synthetic root") {
			t.Errorf("key %q: expected the directory error, got: %s", key, msg)
		}
	}
}

// TestWithSyntheticRoot_DotSlashKeyMatchesBareKey is why the key is cleaned at
// every derivation site: the pre-registered key and the import-resolved
// candidate are spelled differently and must land on one identity.
func TestWithSyntheticRoot_DotSlashKeyMatchesBareKey(t *testing.T) {
	t.Parallel()

	src := syntheticRootSources()
	dotted := map[string][]byte{
		"./a/b/x.yammm": src["a/b/x.yammm"],
		"./main.yammm":  src["main.yammm"],
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), dotted, "./main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://root"))
	if res.HasErrors() {
		t.Fatalf("load: %s", firstError(res))
	}
	if got, want := s.SourceID().String(), "embedded://root/main.yammm"; got != want {
		t.Errorf("entry identity = %q, want %q", got, want)
	}
	if got, want := typeIDByName(t, s)["Region"].SchemaPath().String(), "embedded://root/a/b/x.yammm"; got != want {
		t.Errorf("imported identity = %q, want %q", got, want)
	}
}

// TestWithSyntheticRoot_KeyEscapingRootResolves pins the decision to permit a
// key outside the root: adapter/gogen writes one for an entry that sits
// outside the module root, a layout it documents as legal. The identity
// keeps the "..", because the joined string is never cleaned.
func TestWithSyntheticRoot_KeyEscapingRootResolves(t *testing.T) {
	t.Parallel()

	src := syntheticRootSources()
	sources := map[string][]byte{
		"a/b/x.yammm":   src["a/b/x.yammm"],
		"../main.yammm": src["main.yammm"],
	}
	s, res := schema.LoadSourcesWithEntry(t.Context(), sources, "../main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://root"))
	if res.HasErrors() {
		t.Fatalf("load: %s", firstError(res))
	}
	if got, want := s.SourceID().String(), "embedded://root/../main.yammm"; got != want {
		t.Errorf("entry identity = %q, want %q", got, want)
	}
	if len(s.ImportsSlice()) != 1 || s.ImportsSlice()[0].Schema() == nil {
		t.Error("the module-style import did not resolve from an escaping entry key")
	}
}

// TestWithSyntheticRoot_NormalizedRootMintsOneIdentity pins the root's one
// form: spellings that differ only in scheme case, slashes, dot segments or
// NFC must give one identity, or a consumer that
// adds a slash or capitalizes the scheme re-keys its whole snapshot corpus.
func TestWithSyntheticRoot_NormalizedRootMintsOneIdentity(t *testing.T) {
	t.Parallel()

	for root, want := range map[string]string{
		"embedded://root":          "embedded://root/main.yammm",
		"embedded://root/":         "embedded://root/main.yammm",
		"embedded://root///":       "embedded://root/main.yammm",
		"embedded://root/.":        "embedded://root/main.yammm",
		"EMBEDDED://root":          "embedded://root/main.yammm",
		"embedded://root/sub":      "embedded://root/sub/main.yammm",
		"embedded://root/sub///":   "embedded://root/sub/main.yammm",
		"embedded://root//sub":     "embedded://root/sub/main.yammm",
		"embedded://root/./sub":    "embedded://root/sub/main.yammm",
		"embedded://root/sub/x/..": "embedded://root/sub/main.yammm",
		"Embedded://root/sub":      "embedded://root/sub/main.yammm",
		"embedded://cafe\u0301":    "embedded://caf\u00e9/main.yammm",
		"embedded://caf\u00e9":     "embedded://caf\u00e9/main.yammm",
	} {
		t.Run("root="+root, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root))
			if res.HasErrors() {
				t.Fatalf("load: %s", firstError(res))
			}
			if got := s.SourceID().String(); got != want {
				t.Errorf("entry identity = %q, want %q", got, want)
			}
		})
	}
}

// TestWithSyntheticRoot_RootNamesAnAuthority pins the root's form,
// scheme://authority. Without an authority "embedded://" and "embedded:" both
// joined main.yammm as "embedded:/main.yammm", and a root with no scheme, such
// as ".", reads as a directory.
func TestWithSyntheticRoot_RootNamesAnAuthority(t *testing.T) {
	t.Parallel()

	for _, root := range []string{
		"embedded://", "embedded:///", "embedded:///app", "embedded:", "embedded:app",
		".", "app", "C:app", "://app", "1x://app", "em bedded://app", "C://app",
		`embedded://app\sub`, `embedded://a\pp`, "embedded://app/..", "embedded://app/../x", "embedded://app/sub/../../x",
	} {
		t.Run("root="+root, func(t *testing.T) {
			t.Parallel()
			_, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root))
			if !res.HasErrors() {
				t.Fatalf("expected root %q to be rejected", root)
			}
			if msg := firstError(res); !strings.Contains(msg, "invalid synthetic root") {
				t.Errorf("expected the invalid-root error, got: %s", msg)
			}
		})
	}
	for _, root := range []string{"embedded://app", "x-y.z+1://app", "test://unit/fixtures"} {
		if _, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
			schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root)); res.HasErrors() {
			t.Errorf("root %q: %s", root, firstError(res))
		}
	}
}

// TestWithSyntheticRoot_BackslashKeyIsRefusedOnEveryHost pins one reading of a
// key on every host. Windows read a\b.yammm as a/b.yammm and every other host
// as one file name, so one store named different sources on each.
func TestWithSyntheticRoot_BackslashKeyIsRefusedOnEveryHost(t *testing.T) {
	t.Parallel()

	src := syntheticRootSources()
	for _, tc := range []struct{ name, entry, dep string }{
		{"the entry", `sub\main.yammm`, "a/b/x.yammm"},
		{"an imported source's key", "main.yammm", `a\b/x.yammm`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sources := map[string][]byte{tc.entry: src["main.yammm"], tc.dep: src["a/b/x.yammm"]}
			_, res := schema.LoadSourcesWithEntry(t.Context(), sources, tc.entry, "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
			if !res.HasErrors() {
				t.Fatal("expected a key holding a backslash to be rejected")
			}
			if msg := firstError(res); !strings.Contains(msg, "holds a backslash") || !strings.Contains(msg, "under synthetic root embedded://app") {
				t.Errorf("expected the backslash error under the root, got: %s", msg)
			}
		})
	}
}

// TestWithSyntheticRoot_InvalidRoot covers the two shapes
// location.ValidateSyntheticSourceID refuses. An absolute-looking root would
// collide with file-backed identities and break wire dedup silently.
func TestWithSyntheticRoot_InvalidRoot(t *testing.T) {
	t.Parallel()

	for _, root := range []string{"", "/", "/embedded", "C:/embedded"} {
		t.Run("root="+root, func(t *testing.T) {
			t.Parallel()
			_, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root))
			if !res.HasErrors() {
				t.Fatalf("expected root %q to be rejected", root)
			}
			if msg := firstError(res); !strings.Contains(msg, "invalid synthetic root") {
				t.Errorf("expected the invalid-root error, got: %s", msg)
			}
		})
	}
}

// TestWithSyntheticRoot_RequiresSourcesOnly pins the refusal rather than a
// documented hazard: without hermetic resolution an import miss reads from disk
// and mints a file-backed identity into the same closure.
func TestWithSyntheticRoot_RequiresSourcesOnly(t *testing.T) {
	t.Parallel()

	_, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
		schema.WithSyntheticRoot("embedded://app"))
	if !res.HasErrors() {
		t.Fatal("expected a synthetic root without WithSourcesOnly to be rejected")
	}
	if msg := firstError(res); !strings.Contains(msg, "requires WithSourcesOnly") {
		t.Errorf("expected the WithSourcesOnly error, got: %s", msg)
	}
}

// TestWithSyntheticRoot_RejectsModuleRoot pins the other refusal: the two name
// one concept and the load can honor only one.
func TestWithSyntheticRoot_RejectsModuleRoot(t *testing.T) {
	t.Parallel()

	_, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "/project",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
	if !res.HasErrors() {
		t.Fatal("expected a synthetic root with a module root to be rejected")
	}
	if msg := firstError(res); !strings.Contains(msg, "cannot be combined with module root") {
		t.Errorf("expected the module-root conflict error, got: %s", msg)
	}
}

// TestWithSyntheticRoot_RejectedByLoadAndLoadString pins the rejection on the
// two entry points a synthetic root could only ever be a no-op for.
func TestWithSyntheticRoot_RejectedByLoadAndLoadString(t *testing.T) {
	t.Parallel()

	t.Run("Load", func(t *testing.T) {
		t.Parallel()
		_, res := schema.Load(t.Context(), filepath.Join(t.TempDir(), "absent.yammm"),
			schema.WithSyntheticRoot("embedded://app"))
		if msg := firstError(res); !strings.Contains(msg, "WithSyntheticRoot applies to LoadSourcesWithEntry only") {
			t.Errorf("expected the option rejection, got: %s", msg)
		}
	})

	t.Run("LoadString", func(t *testing.T) {
		t.Parallel()
		_, res := schema.LoadString(t.Context(), "schema \"s\"\n", "s.yammm",
			schema.WithSyntheticRoot("embedded://app"))
		if msg := firstError(res); !strings.Contains(msg, "WithSyntheticRoot applies to LoadSourcesWithEntry only") {
			t.Errorf("expected the option rejection, got: %s", msg)
		}
	})
}

// TestWithSourcesOnly_RefusedByLoadAndLoadString pins the refusal on the two
// entry points that cannot serve the option: under it an import of a Load
// failed as not pre-registered unless a shared registry already held it, and
// on a LoadString it was a no-op.
func TestWithSourcesOnly_RefusedByLoadAndLoadString(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.yammm")
	if err := os.WriteFile(path, []byte("schema \"s\"\n\ntype T {\n\tid String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const refusal = "WithSourcesOnly applies to LoadSourcesWithEntry only"
	if _, res := schema.Load(t.Context(), path, schema.WithSourcesOnly(true)); !strings.Contains(firstError(res), refusal) {
		t.Errorf("Load: expected the option rejection, got: %s", firstError(res))
	}
	if _, res := schema.LoadString(t.Context(), "schema \"s\"\n", "s.yammm", schema.WithSourcesOnly(true)); !strings.Contains(firstError(res), refusal) {
		t.Errorf("LoadString: expected the option rejection, got: %s", firstError(res))
	}

	// false is the default, so passing it changes nothing and is accepted.
	if _, res := schema.Load(t.Context(), path, schema.WithSourcesOnly(false)); res.HasErrors() {
		t.Errorf("Load with WithSourcesOnly(false): %s", firstError(res))
	}
	if _, res := schema.LoadString(t.Context(), "schema \"s\"\n", "s.yammm", schema.WithSourcesOnly(false)); res.HasErrors() {
		t.Errorf("LoadString with WithSourcesOnly(false): %s", firstError(res))
	}
}

// TestWithSyntheticRoot_EmptyRootIsNotSilentlyIgnored pins that passing an empty
// root is an error rather than a no-op, which is what separates "the option was
// not given" from "the option was given a bad value".
func TestWithSyntheticRoot_EmptyRootIsNotSilentlyIgnored(t *testing.T) {
	t.Parallel()

	_, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot(""))
	if !res.HasErrors() {
		t.Fatal("expected an empty synthetic root to be rejected")
	}
}

// TestSyntheticImportKey_IsTheLoadersLookup holds SyntheticImportKey to the
// loader it states: for each import, a load under a synthetic root registers
// the imported source under exactly the key the function returns, and where the
// function refuses, the load refuses too. The importer key is read as the
// loader reads it.
func TestSyntheticImportKey_IsTheLoadersLookup(t *testing.T) {
	t.Parallel()

	if got, err := schema.SyntheticImportKey("a/main.yammm", "./x.yammm/.."); err != nil || got != "a.yammm" {
		t.Errorf(`SyntheticImportKey("a/main.yammm", "./x.yammm/..") = (%q, %v), want "a.yammm"`, got, err)
	}

	const root = "embedded://app"
	dep := []byte("schema \"core\"\n\ntype Region {\n\tcode String primary\n}\n")
	for _, tc := range []struct {
		name, importer, imp, want string
	}{
		{"module-style", "main.yammm", "dep", "dep.yammm"},
		{"module-style with the extension", "main.yammm", "lib/dep.yammm", "lib/dep.yammm"},
		{"module-style from a nested importer", "a/b/main.yammm", "lib/dep", "lib/dep.yammm"},
		{"a sibling", "a/b/main.yammm", "./dep", "a/b/dep.yammm"},
		{"a parent", "a/b/main.yammm", "../dep", "a/dep.yammm"},
		{"above the root", "a/main.yammm", "../../dep", "../dep.yammm"},
		{"from an importer above the root", "../x/main.yammm", "./dep", "../x/dep.yammm"},
		{"cleaned", "main.yammm", "lib/./x/../dep", "lib/dep.yammm"},
		{"a decomposed name", "main.yammm", "café/dep", "café/dep.yammm"},
		{"a backslash", "main.yammm", `lib\dep`, ""},
		{"an importer key the loader cleans", "sub/main.yammm/", "./dep", "sub/dep.yammm"},
		{"an importer key with a dot segment", "sub/./main.yammm", "../dep", "dep.yammm"},
		{"an importer key holding a backslash", `sub\main.yammm`, "./dep", ""},
		{"an absolute importer key", "/abs/main.yammm", "./dep", ""},
		{"an importer key that cleans to a drive", "./C:/main.yammm", "./dep", ""},
		{"an import NFC turns into a drive", "main.yammm", "\u212a:/x", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := schema.SyntheticImportKey(tc.importer, tc.imp)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("SyntheticImportKey = %q, want an error", got)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("SyntheticImportKey = (%q, %v), want %q", got, err, tc.want)
			}

			main := []byte("schema \"app\"\n\nimport \"" + strings.ReplaceAll(tc.imp, `\`, `\\`) + "\" as core\n\n" +
				"type County {\n\tfips String primary\n\t--> IN_REGION (one) core.Region\n}\n")
			sources := map[string][]byte{tc.importer: main}
			if tc.want != "" {
				sources[tc.want] = dep
			} else {
				sources[tc.imp+".yammm"] = dep
			}
			s, res := schema.LoadSourcesWithEntry(t.Context(), sources, tc.importer, "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root))
			if tc.want == "" {
				if !res.HasErrors() {
					t.Fatal("the loader accepted an import SyntheticImportKey refuses")
				}
				return
			}
			if res.HasErrors() {
				t.Fatalf("load: %s", firstError(res))
			}
			if id := s.ImportsSlice()[0].ResolvedSourceID().String(); id != root+"/"+tc.want {
				t.Errorf("the loader resolved %q to %q, want %q", tc.imp, id, root+"/"+tc.want)
			}
		})
	}
}
