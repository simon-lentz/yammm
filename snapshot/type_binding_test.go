package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
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
func loadUnderModuleRoot(t *testing.T, moduleRoot string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{"geo.yammm": []byte(typeBindingSchema)}, "geo.yammm", moduleRoot)
	if res.HasErrors() {
		t.Fatalf("load under %q: %v", moduleRoot, res.Err())
	}
	return s
}

// loadUnderSyntheticRoot loads the same fixture hermetically under a synthetic
// root, which is what makes its identities independent of the process.
func loadUnderSyntheticRoot(t *testing.T, root string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadSourcesWithEntry(t.Context(),
		map[string][]byte{"geo.yammm": []byte(typeBindingSchema)}, "geo.yammm", "",
		schema.WithSourcesOnly(true), schema.WithSyntheticRoot(root))
	if res.HasErrors() {
		t.Fatalf("load under %q: %v", root, res.Err())
	}
	return s
}

// marshalCounty writes a one-instance snapshot over s.
func marshalCounty(t *testing.T, s *schema.Schema) []byte {
	t.Helper()
	county, ok := s.Type("County")
	if !ok {
		t.Fatal("fixture is missing County")
	}
	id := county.ID()
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{id},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			id: {{
				TypeName:   "County",
				TypeID:     id,
				PrimaryKey: immutable.WrapKey([]any{"01001"}),
				Properties: immutable.WrapProperties(map[string]any{"fips": "01001", "name": "Autauga"}),
			}},
		},
	})
	if res.HasErrors() {
		t.Fatalf("rebuild: %v", res.Err())
	}
	data, res := snapshot.Marshal(t.Context(), built)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res.Err())
	}
	return data
}

// headerOf reads back only the header, the way a dispatch caller classifies.
func headerOf(t *testing.T, data []byte) *snapshot.HeaderInfo {
	t.Helper()
	header, res := snapshot.HeaderOnly(t.Context(), data)
	if res.HasErrors() {
		t.Fatalf("header-only read: %v", res.Err())
	}
	return header
}

// mustBind asserts that s binds every row of data: the header classifies the
// document as current and the full load succeeds. Together they are the
// whole of what a dispatch caller checks — the hash covers every declared
// schema and type name, so a matching hash already implies every row binds.
func mustBind(t *testing.T, data []byte, s *schema.Schema, what string) {
	t.Helper()
	if !headerOf(t, data).SchemaHashMatches(s) {
		t.Fatalf("%s: the header does not classify the document as current", what)
	}
	loaded, res := snapshot.Load(t.Context(), data, s)
	if res.HasErrors() {
		t.Fatalf("%s: full load: %v", what, res.Err())
	}
	if loaded == nil {
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
