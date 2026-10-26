package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// A composed duplicate that names a parent no root holds is refused with the
// parent's key cut to its head, as every message that repeats a value is.
func TestRebuildSnapshot_NamesALongParentKeyByItsHead(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), recordFactsSchema, "facts.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	orderID := mustTypeID(t, s, "Order")
	tagID := mustTypeID(t, s, "Tag")
	long := strings.Repeat("z", 100)
	_, res = graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{orderID},
		Instances: []graph.InstanceParts{{
			TypeID: orderID, PrimaryKey: immutable.WrapKey([]any{"o1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "o1"}),
		}},
		Duplicates: []graph.DuplicateParts{{
			Instance: graph.InstanceParts{
				TypeID: tagID, PrimaryKey: immutable.WrapKey([]any{"t1"}),
				Properties: immutable.WrapProperties(map[string]any{"code": "t1"}),
			},
			ParentType: orderID, ParentKey: immutable.WrapKey([]any{long}), Relation: "MAIN",
		}},
	})
	named := false
	for issue := range res.Issues() {
		if strings.Contains(issue.Message(), strings.Repeat("z", 33)) {
			t.Errorf("the message repeats the whole key: %.120q", issue.Message())
		}
		named = named || strings.Contains(issue.Message(), `names parent`)
	}
	if !named {
		t.Fatalf("no issue names the parent: %s", res)
	}
}
