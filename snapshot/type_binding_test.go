package snapshot_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const typeBindingSchema = `schema "geo"

type County {
	fips String primary
	name String required
}
`

// loadUnderModuleRoot loads the one-source fixture with an explicit module root,
// so two loads of identical text produce identical StructuralHashes and
// different type identities — the shape a consumer hits when a snapshot outlives
// the layout it was written under.
func loadUnderModuleRoot(t *testing.T, moduleRoot string) schema.Schema {
	t.Helper()
	s, err := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{"geo.yammm": []byte(typeBindingSchema)}, "geo.yammm", schema.WithModuleRoot(moduleRoot))
	if err != nil {
		t.Fatalf("load under %q: %v", moduleRoot, err)
	}
	return s
}

// loadUnderSyntheticRoot loads the same fixture hermetically under a synthetic
// root, which is what makes its identities independent of the process.
func loadUnderSyntheticRoot(t *testing.T, root string) schema.Schema {
	t.Helper()
	s, err := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{"geo.yammm": []byte(typeBindingSchema)}, "geo.yammm",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root))
	if err != nil {
		t.Fatalf("load under %q: %v", root, err)
	}
	return s
}

// marshalCounty writes a one-instance snapshot over s.
func marshalCounty(t *testing.T, s schema.Schema) []byte {
	t.Helper()
	county, ok := s.Type("County")
	if !ok {
		t.Fatal("fixture is missing County")
	}
	id := county.ID()
	built := snappartstest.Loaded(t, s, snapparts.Parts{
		Types: []schema.TypeID{id},
		Instances: []snapparts.Instance{
			{
				TypeID:     id,
				PrimaryKey: immutable.WrapKey([]any{"01001"}),
				Properties: immutable.WrapProperties(map[string]any{"fips": "01001", "name": "Autauga"}),
			},
		},
	})
	data, err := snapshot.Marshal(t.Context(), built)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// headerOf reads back only the header, the way a dispatch caller classifies.
func headerOf(t *testing.T, data []byte) *snapshot.Header {
	t.Helper()
	header, err := snapshot.ReadHeader(t.Context(), bytes.NewReader(data))
	if err != nil {
		t.Fatalf("header-only read: %v", err)
	}
	return header
}

// mustBind asserts that s binds every row of data: the header classifies the
// document as current and the full load succeeds. Together they are the
// whole of what a dispatch caller checks — the hash covers every declared
// schema and type name, so a matching hash already implies every row binds.
func mustBind(t *testing.T, data []byte, s schema.Schema, what string) {
	t.Helper()
	if !headerOf(t, data).SchemaHashMatches(s) {
		t.Fatalf("%s: the header does not classify the document as current", what)
	}
	loaded, err := snapshot.Unmarshal(t.Context(), data, s)
	if err != nil {
		t.Fatalf("%s: full load: %v", what, err)
	}
	if loaded.IsZero() {
		t.Fatalf("%s: expected a loaded snapshot", what)
	}
}

// TestTypeTable_PathMoveDoesNotUnbindRows pins the property the types table is
// keyed by schema name for: one schema text loaded from two directories
// produces documents that read against each other.
//
// The two loads share a StructuralHash — it excludes source paths by design —
// and the rows agree with it. While the table carried source paths the two
// contradicted each other: the hash reported a match and every row was
// unbindable, so a caller that checked the hash classified the snapshot as
// resumable and then failed at Load with one E_SNAPSHOT_UNKNOWN_TYPE per row,
// with nothing to do but regenerate the document.
func TestTypeTable_PathMoveDoesNotUnbindRows(t *testing.T) {
	t.Parallel()

	written := loadUnderModuleRoot(t, "/project")
	read := loadUnderModuleRoot(t, "/elsewhere")
	mustBind(t, marshalCounty(t, written), read, "a schema loaded from another directory")
}

// TestSyntheticRoot_SnapshotReadsBackFromAnotherWorkingDirectory is I-1's
// end-to-end claim, asserted where the two packages meet: a snapshot written
// under a synthetic root binds every row when it is read from a different
// working directory.
//
// Since the types table states the declaring schema's NAME, a load under a
// working-directory-derived module root binds every row too. That is the point
// of the re-key and it is asserted here rather than left implicit: a caller no
// longer needs a synthetic root to make a .ys portable between machines. The
// synthetic root still fixes the identities a load produces; it is no longer
// what carries them across the wire.
func TestSyntheticRoot_SnapshotReadsBackFromAnotherWorkingDirectory(t *testing.T) {
	// t.Chdir forbids t.Parallel; the process-wide working directory is the
	// variable under test.
	written := loadUnderSyntheticRoot(t, "embedded://app")
	data := marshalCounty(t, written)

	t.Chdir(t.TempDir())

	mustBind(t, data, loadUnderSyntheticRoot(t, "embedded://app"), "the synthetic root from another working directory")

	// A "." module root canonicalizes against the working directory, giving
	// every type a different source path from the written document's — and the
	// rows still bind, because the wire names the schema rather than its path.
	mustBind(t, data, loadUnderModuleRoot(t, "."), "a cwd-derived load")
}

const layoutEntry = `schema "app"

import "lib/item" as x

type Item {
	id String primary
	--> HOLDS (_:one) x.Item
}
`

const layoutLib = `schema "lib"

type Item {
	id String primary
	--> NEXT (_:one) Item
}
`

// loadLayout loads layoutEntry from entryPath and layoutLib from lib/item.yammm
// under one module root. Moving the entry file moves its source path past the
// library's while both texts and both schema names stay the same. Both schemas
// declare Item, so only the schema name tells their identities apart.
func loadLayout(t *testing.T, entryPath string) schema.Schema {
	t.Helper()
	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		entryPath:        []byte(layoutEntry),
		"lib/item.yammm": []byte(layoutLib),
	}, entryPath, schema.WithSourcesOnly(true), schema.WithModuleRoot("/project"))
	if err != nil {
		t.Fatalf("load with the entry at %s: %v", entryPath, err)
	}
	return s
}

// layoutGraph builds a snapshot in which each schema's type sources one edge,
// one unresolved record and one rejected duplicate, so every order that
// compares types meets both schemas.
func layoutGraph(t *testing.T, s schema.Schema) graph.Snapshot {
	t.Helper()
	g := graph.New(s)
	target := func(key string) map[string]any { return map[string]any{"_target_id": key} }
	adds := []struct {
		typeName string
		props    map[string]any
	}{
		{"Item", map[string]any{"id": "k", "holds": target("k")}},
		{"Item", map[string]any{"id": "k", "holds": target("k")}},
		{"Item", map[string]any{"id": "a2", "holds": target("gone")}},
		{"x.Item", map[string]any{"id": "k", "next": target("gone")}},
		{"x.Item", map[string]any{"id": "k", "next": target("gone")}},
		{"x.Item", map[string]any{"id": "j", "next": target("k")}},
	}
	for _, add := range adds {
		err := g.Add(t.Context(), instancetest.Valid(t, s, add.typeName, add.props))
		if err != nil && !errors.Is(err, diag.E_DUPLICATE_PK) {
			t.Fatalf("add %s %v: %v", add.typeName, add.props["id"], err)
		}
	}
	return g.Snapshot()
}

// typeNames returns the name of each of snap.Types as the entry schema renders
// it, in its order.
func typeNames(snap graph.Snapshot) []string {
	var names []string
	for _, id := range snap.Types() {
		names = append(names, schema.TagForm(snap.Schema(), id))
	}
	return names
}

// edgeSources returns the source type of each of snap.Edges as the entry
// schema renders it, in its order.
func edgeSources(snap graph.Snapshot) []string {
	var names []string
	for e := range snap.Edges() {
		names = append(names, schema.TagForm(snap.Schema(), e.Source().TypeID()))
	}
	return names
}

// TestMarshal_ClosureLayoutDoesNotMoveBytes pins that a document's bytes follow
// the schema names, not the source paths: one closure loaded with its entry
// file at two places writes one document.
func TestMarshal_ClosureLayoutDoesNotMoveBytes(t *testing.T) {
	t.Parallel()

	atRoot, inDir := loadLayout(t, "main.yammm"), loadLayout(t, "a/main.yammm")
	if atRoot.SourceID().String() < "/project/lib" == (inDir.SourceID().String() < "/project/lib") {
		t.Fatal("fixture is vacuous: the two layouts order the entry and the library alike")
	}
	a, b := layoutGraph(t, atRoot), layoutGraph(t, inDir)
	if n, u, d := len(slices.Collect(a.Edges())), len(slices.Collect(a.Unresolved())), len(slices.Collect(a.Duplicates())); n != 2 || u != 2 || d != 2 {
		t.Fatalf("fixture is vacuous: %d edges, %d unresolved, %d duplicates, want 2 of each", n, u, d)
	}
	if an, bn := typeNames(a), typeNames(b); !slices.Equal(an, bn) {
		t.Errorf("Types orders one closure in two layouts as %v and %v", an, bn)
	}
	if an, bn := edgeSources(a), edgeSources(b); !slices.Equal(an, bn) {
		t.Errorf("Edges orders one closure in two layouts by source as %v and %v", an, bn)
	}
	ab, err := snapshot.Marshal(t.Context(), a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	bb, err := snapshot.Marshal(t.Context(), b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Equal(ab, bb) {
		t.Errorf("one closure in two layouts writes two documents:\n%s\n---\n%s", ab, bb)
	}
}
