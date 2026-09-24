package snapshot_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

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
	*-> PIECES (_:many) Piece
}

part type Piece {
	label String
}
`

// declaredNamesDocument is a clean .ys holding a value at every position that
// stores a name, each value a marker an undeclared name can be spliced beside.
func declaredNamesDocument(t *testing.T) (*schema.Schema, string) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), declaredNamesSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	holderT, _ := s.Type("Holder")
	targetT, _ := s.Type("Target")
	pieceT, _ := s.Type("Piece")
	hk := immutable.WrapKey([]any{"h1"})
	snap, r := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{holderT.ID(), targetT.ID()},
		Instances: []graph.InstanceParts{
			{
				TypeID: targetT.ID(), PrimaryKey: immutable.WrapKey([]any{"t1"}),
				Properties: immutable.WrapProperties(map[string]any{"key": "t1"}),
			},
			{
				TypeID: holderT.ID(), PrimaryKey: hk,
				Properties: immutable.WrapProperties(map[string]any{"id": "h1", "note": "ROOT"}),
				Composed: map[string][]graph.InstanceParts{"PIECES": {{
					TypeID:     pieceT.ID(),
					Properties: immutable.WrapProperties(map[string]any{"label": "CHILD"}),
				}}},
			},
		},
		Edges: []graph.EdgeParts{{
			Relation: "OWNER", SourceType: holderT.ID(), SourceKey: hk,
			TargetType: targetT.ID(), TargetKey: immutable.WrapKey([]any{"t1"}),
			Properties: immutable.WrapProperties(map[string]any{"since": "EDGE"}),
		}},
		Duplicates: []graph.DuplicateParts{{
			Type: holderT.ID(), Key: hk,
			Instance: graph.InstanceParts{
				TypeID: holderT.ID(), PrimaryKey: hk,
				Properties: immutable.WrapProperties(map[string]any{"id": "h1", "note": "DUP"}),
			},
			ConflictType: holderT.ID(), ConflictKey: hk,
		}},
		Unresolved: []graph.UnresolvedParts{{
			SourceType: holderT.ID(), SourceKey: hk, Relation: "OWNER",
			TargetType: targetT.ID(), TargetKey: immutable.WrapKey([]any{"t9"}), Reason: "target_missing",
			Properties: immutable.WrapProperties(map[string]any{"since": "UNRES"}),
		}},
	})
	if err := r.Err(); err != nil {
		t.Fatalf("RebuildSnapshot: %v", err)
	}
	data, mres := snapshot.Marshal(t.Context(), snap)
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres)
	}
	return s, string(data)
}

// Over every combination of the five positions, a document holding an
// undeclared property name is refused by Load and Verify with one
// E_SNAPSHOT_MALFORMED per name, whatever the load options, and the clean
// document loads.
func TestLoad_RefusesAnUndeclaredNameAtEveryPosition(t *testing.T) {
	t.Parallel()
	s, clean := declaredNamesDocument(t)
	markers := []string{`"note":"ROOT"`, `"label":"CHILD"`, `"since":"EDGE"`, `"note":"DUP"`, `"since":"UNRES"`}
	for _, m := range markers {
		if strings.Count(clean, m) != 1 {
			t.Fatalf("marker %s occurs %d times in the clean document", m, strings.Count(clean, m))
		}
	}
	for mask := range 1 << len(markers) {
		doc, want := clean, 0
		for i, m := range markers {
			if mask&(1<<i) != 0 {
				doc = strings.Replace(doc, m, m+`,"ghost":"x"`, 1)
				want++
			}
		}
		loaded, lres := snapshot.Load(t.Context(), []byte(doc), s, snapshot.WithIntegrityCheck(false))
		vres := snapshot.Verify(t.Context(), []byte(doc), s, snapshot.WithIntegrityCheck(false))
		for label, r := range map[string]diag.Result{"Load": lres, "Verify": vres} {
			got := 0
			for issue := range r.Issues() {
				if issue.Code() == diag.E_SNAPSHOT_MALFORMED && strings.Contains(issue.Message(), `"ghost"`) {
					got++
				}
			}
			if got != want || (want == 0) != r.OK() {
				t.Fatalf("mask %b: %s: want %d refusals, got %d:\n%s", mask, label, want, got, r)
			}
		}
		if (want == 0) != (loaded != nil) {
			t.Fatalf("mask %b: want a snapshot only when no undeclared name, got %v", mask, loaded != nil)
		}
	}
}
