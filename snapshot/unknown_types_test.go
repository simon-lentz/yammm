package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const unknownTypesSchema = `schema "geo"

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
		map[string][]byte{"geo.yammm": []byte(unknownTypesSchema)}, "geo.yammm", moduleRoot)
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
		map[string][]byte{"geo.yammm": []byte(unknownTypesSchema)}, "geo.yammm", "",
		schema.WithSourcesOnly(), schema.WithSyntheticRoot(root))
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

// TestHeaderInfo_UnknownTypes_AllRowsResolve is the negative control: the guard
// must not fire for a snapshot read against the schema it was written under.
func TestHeaderInfo_UnknownTypes_AllRowsResolve(t *testing.T) {
	t.Parallel()

	s := loadUnderModuleRoot(t, "/project")
	header := headerOf(t, marshalCounty(t, s))
	if len(header.Types) == 0 {
		t.Fatal("fixture wrote an empty types table")
	}
	if unknown := header.UnknownTypes(s); len(unknown) != 0 {
		t.Errorf("expected every row to resolve, got %v", unknown)
	}
}

// TestHeaderInfo_UnknownTypes_PairsWithSchemaHashMatches is the whole
// justification for the method: the hash reports a match while every row is
// unbindable, so a caller checking only the hash classifies the snapshot as
// resumable and then fails at Load with one E_SNAPSHOT_UNKNOWN_TYPE per row.
func TestHeaderInfo_UnknownTypes_PairsWithSchemaHashMatches(t *testing.T) {
	t.Parallel()

	written := loadUnderModuleRoot(t, "/project")
	read := loadUnderModuleRoot(t, "/elsewhere")
	header := headerOf(t, marshalCounty(t, written))

	if !header.SchemaHashMatches(read) {
		t.Fatal("the two loads must share a StructuralHash for this test to mean anything")
	}
	unknown := header.UnknownTypes(read)
	if len(unknown) != len(header.Types) {
		t.Errorf("expected every one of %d rows to be unknown, got %d: %v",
			len(header.Types), len(unknown), unknown)
	}
}

// TestHeaderInfo_UnknownTypes_OneForeignRow pins that the method reports the
// offending row rather than an all-or-nothing verdict: those rows beside the
// closure's own paths are the whole diagnosis a caller logs.
func TestHeaderInfo_UnknownTypes_OneForeignRow(t *testing.T) {
	t.Parallel()

	s := loadUnderModuleRoot(t, "/project")
	header := headerOf(t, marshalCounty(t, s))
	foreign := snapshot.TypeRef{SchemaPath: "/elsewhere/geo.yammm", Name: "County"}
	header.Types = append(header.Types, foreign)

	unknown := header.UnknownTypes(s)
	if len(unknown) != 1 || unknown[0] != foreign {
		t.Errorf("expected exactly the foreign row, got %v", unknown)
	}
}

// TestHeaderInfo_UnknownTypes_NilSchema pins the documented nil behaviour: a
// closure that declares nothing declares no row.
func TestHeaderInfo_UnknownTypes_NilSchema(t *testing.T) {
	t.Parallel()

	header := headerOf(t, marshalCounty(t, loadUnderModuleRoot(t, "/project")))
	if unknown := header.UnknownTypes(nil); len(unknown) != len(header.Types) {
		t.Errorf("expected every row under a nil schema, got %d of %d", len(unknown), len(header.Types))
	}
}

// TestHeaderInfo_UnknownTypes_NilReceiver pins nil-safety on the same footing as
// SchemaHashMatches and CreatedAtTime.
func TestHeaderInfo_UnknownTypes_NilReceiver(t *testing.T) {
	t.Parallel()

	var header *snapshot.HeaderInfo
	if unknown := header.UnknownTypes(loadUnderModuleRoot(t, "/project")); unknown != nil {
		t.Errorf("expected nil from a nil receiver, got %v", unknown)
	}
}

// TestSyntheticRoot_SnapshotReadsBackFromAnotherWorkingDirectory is I-1's
// end-to-end claim, asserted where the two packages meet: a snapshot written
// under a synthetic root binds every row when it is read from a different
// working directory, and the module-root load it replaces does not.
func TestSyntheticRoot_SnapshotReadsBackFromAnotherWorkingDirectory(t *testing.T) {
	// t.Chdir forbids t.Parallel; the process-wide working directory is the
	// variable under test.
	written := loadUnderSyntheticRoot(t, "embedded://app")
	data := marshalCounty(t, written)

	t.Chdir(t.TempDir())

	header := headerOf(t, data)
	reread := loadUnderSyntheticRoot(t, "embedded://app")
	if unknown := header.UnknownTypes(reread); len(unknown) != 0 {
		t.Errorf("synthetic-root identities moved with the working directory: %v", unknown)
	}

	// The control: a "." module root canonicalizes against the working
	// directory, so the same document classifies as unknown from here.
	cwdLoaded := loadUnderModuleRoot(t, ".")
	if unknown := header.UnknownTypes(cwdLoaded); len(unknown) == 0 {
		t.Error("expected a cwd-derived load to disagree with the synthetic-root document")
	}

	loaded, res := snapshot.Load(t.Context(), data, reread)
	if res.HasErrors() {
		t.Fatalf("full load under the synthetic root: %v", res.Err())
	}
	if loaded == nil {
		t.Fatal("expected a loaded snapshot")
	}
}
