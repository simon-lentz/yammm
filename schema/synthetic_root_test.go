package schema_test

import (
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

// TestWithSyntheticRoot_RelativeImportRejected pins the documented limitation:
// a relative import resolves through the importing source's canonical path,
// which no synthetic identity has.
func TestWithSyntheticRoot_RelativeImportRejected(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{
		"dep.yammm": []byte("schema \"core\"\n\ntype Region {\n\tcode String primary\n}\n"),
		"main.yammm": []byte("schema \"app\"\n\nimport \"./dep\" as core\n\n" +
			"type County {\n\tfips String primary\n\t--> IN_REGION (one) core.Region\n}\n"),
	}
	_, res := schema.LoadSourcesWithEntry(t.Context(), sources, "main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
	if !res.HasErrors() {
		t.Fatal("expected a relative import under a synthetic root to fail")
	}
	if msg := firstError(res); !strings.Contains(msg, "relative imports require a file-based source") {
		t.Errorf("expected the file-based-source error, got: %s", msg)
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
func TestWithSyntheticRoot_AbsoluteKeyIsError(t *testing.T) {
	t.Parallel()

	sources := map[string][]byte{"/abs/main.yammm": []byte("schema \"s\"\n\ntype T {\n\tid String primary\n}\n")}
	_, res := schema.LoadSourcesWithEntry(t.Context(), sources, "/abs/main.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot("embedded://app"))
	if !res.HasErrors() {
		t.Fatal("expected an absolute key to be rejected")
	}
	if msg := firstError(res); !strings.Contains(msg, "must be relative to the synthetic root") {
		t.Errorf("expected the relative-key error, got: %s", msg)
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
// key outside the root: adapter/gogen's sourceKey emits one for an entry that
// sits outside the module root, a layout it documents as legal. The identity
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

// TestWithSyntheticRoot_TrailingSlashTrimmed pins the trim: two spellings of one
// root must give one identity, or a consumer that adds a slash re-keys its whole
// snapshot corpus.
func TestWithSyntheticRoot_TrailingSlashTrimmed(t *testing.T) {
	t.Parallel()

	for _, root := range []string{"embedded://root", "embedded://root/", "embedded://root///"} {
		t.Run("root="+root, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadSourcesWithEntry(t.Context(), syntheticRootSources(), "main.yammm", "",
				schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root))
			if res.HasErrors() {
				t.Fatalf("load: %s", firstError(res))
			}
			if got, want := s.SourceID().String(), "embedded://root/main.yammm"; got != want {
				t.Errorf("entry identity = %q, want %q", got, want)
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
