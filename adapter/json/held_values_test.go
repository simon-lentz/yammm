package json

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// heldRoot describes one root of typeName at the one-component key.
func heldRoot(t *testing.T, s schema.Schema, typeName string, key any, props map[string]any) snapparts.Instance {
	t.Helper()
	return snapparts.Instance{
		TypeID:     mustTypeID(t, s, typeName),
		PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(props),
	}
}

// heldEdge describes one edge from the source root under relation.
func heldEdge(t *testing.T, s schema.Schema, sourceType string, sourceKey any, relation string, targetKey any, props map[string]any) snapparts.Edge {
	t.Helper()
	return snapparts.Edge{
		Relation:   relation,
		SourceType: mustTypeID(t, s, sourceType),
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

// A whole number a document states at a Float edge property is written as the
// float its constraint stores, as at a property.
func TestMarshalObject_AWholeNumberAtAFloatEdgePropertyIsAFloat(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "w"

type Hop {
	id String primary
	--> NEXT (_:many) Hop {
		weight Float
	}
}
`, "w.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	snap := heldDocument(t, s, "Hop", `{"key":["a"],"properties":{"id":"a"},"edges":{"NEXT":[{"target_type":0,"target_key":["a"],"properties":{"weight":5}}]}}`)
	out, err := New().MarshalObject(t.Context(), snap)
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}
	if !strings.Contains(string(out), `"weight":5.0`) {
		t.Errorf("MarshalObject = %s; want the weight written as 5.0", out)
	}
}
