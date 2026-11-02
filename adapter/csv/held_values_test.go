package csv

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

func heldTypeID(t *testing.T, s schema.Schema, typeName string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("no type %q", typeName)
	}
	return typ.ID()
}

// heldRoot describes one root of typeName at the one-component key.
func heldRoot(t *testing.T, s schema.Schema, typeName string, key any, props map[string]any) snapparts.Instance {
	t.Helper()
	return snapparts.Instance{
		TypeID:     heldTypeID(t, s, typeName),
		PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(props),
	}
}

// heldEdge describes one edge from the source root under relation.
func heldEdge(t *testing.T, s schema.Schema, sourceType string, sourceKey any, relation string, targetKey any, props map[string]any) snapparts.Edge {
	t.Helper()
	return snapparts.Edge{
		Relation:   relation,
		SourceType: heldTypeID(t, s, sourceType),
		SourceKey:  immutable.WrapKey([]any{sourceKey}),
		TargetKey:  immutable.WrapKey([]any{targetKey}),
		Properties: immutable.WrapProperties(props),
	}
}

// heldDocument reads one type's items from a hand-written .ys, which a reader
// holds to the structural facts and, with no revalidation option, judges no
// value of. items is the JSON of the type's item array, unbracketed.
func heldDocument(t *testing.T, s schema.Schema, typeName, items string) graph.Snapshot {
	t.Helper()
	doc := fmt.Sprintf(`{"yammm_snapshot":{"version":%d,"schema_name":%q,"schema_source":%q,"schema_hash":%q,"schema_hash_algorithm":%d,"integrity_hash":"","features":[]},`+
		`"types":[{"schema":%q,"name":%q}],"instances":[{"type":0,"items":[%s]}],"diagnostics":{"duplicates":[],"unresolved":[]}}`,
		snapshot.MinReadableVersion, s.Name(), s.SourceID().String(), schema.StructuralHash(s), schema.StructuralHashVersion,
		s.Name(), typeName, items)
	snap, res := snapshot.Load(t.Context(), []byte(doc), s, snapshot.WithIntegrityCheck(false))
	if res.HasErrors() {
		t.Fatalf("snapshot.Load: %s", res)
	}
	return snap
}
