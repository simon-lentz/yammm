package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

// declaredNamesSchema declares a property and an association on an abstract
// parent, and a composition, so every position a snapshot stores a name at is
// reachable: a root, an inherited member, a composed child, an edge.
const declaredNamesSchema = `schema "names"

type Target {
	key String primary
}

abstract type Base {
	id String primary
	note String
	--> OWNER (_:many) Target {
		since String
	}
}

type Holder extends Base {
	name String
	*-> PIECES (_:many) Piece
}

part type Piece {
	label String
}
`

func loadDeclaredNamesSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), declaredNamesSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s
}

// A rebuild holds a declared name at every position a snapshot stores one: an
// inherited member of a root, a composed child's member, an edge's, a duplicate's
// and an unresolved record's.
func TestRebuild_HoldsADeclaredNameAtEveryPosition(t *testing.T) {
	t.Parallel()
	s := loadDeclaredNamesSchema(t)
	holder, target, piece := mustTypeID(t, s, "Holder"), mustTypeID(t, s, "Target"), mustTypeID(t, s, "Piece")
	hk := immutable.WrapKey([]any{"h1"})
	snap := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{holder, target},
		Instances: []snapparts.Instance{
			{
				TypeID: target, PrimaryKey: immutable.WrapKey([]any{"t1"}),
				Properties: immutable.WrapProperties(map[string]any{"key": "t1"}),
			},
			{
				TypeID: holder, PrimaryKey: hk,
				Properties: immutable.WrapProperties(map[string]any{"id": "h1", "note": "root"}),
				Composed: map[string][]snapparts.Instance{"PIECES": {{
					TypeID:     piece,
					Properties: immutable.WrapProperties(map[string]any{"label": "child"}),
				}}},
			},
		},
		Edges: []snapparts.Edge{{
			Relation: "OWNER", SourceType: holder, SourceKey: hk,
			TargetKey: immutable.WrapKey([]any{"t1"}), Properties: immutable.WrapProperties(map[string]any{"since": "edge"}),
		}},
		Duplicates: []snapparts.Instance{
			{TypeID: holder, PrimaryKey: hk, Properties: immutable.WrapProperties(map[string]any{"id": "h1", "note": "duplicate"})},
		},
		Unresolved: []snapparts.Unresolved{{
			SourceType: holder, SourceKey: hk, Relation: "OWNER",
			TargetKey: immutable.WrapKey([]any{"t9"}), Reason: "target_missing",
			Properties: immutable.WrapProperties(map[string]any{"since": "unresolved"}),
		}},
	})
	root := slices.Collect(snap.InstancesOf(holder))[0]
	assertProp(t, root.Properties(), "note", "root")
	assertProp(t, slices.Collect(root.Composed("PIECES"))[0].Properties(), "label", "child")
	assertProp(t, slices.Collect(snap.Edges())[0].Properties(), "since", "edge")
	assertProp(t, slices.Collect(snap.Duplicates())[0].Instance().Properties(), "note", "duplicate")
	assertProp(t, slices.Collect(snap.Unresolved())[0].Properties(), "since", "unresolved")
}
