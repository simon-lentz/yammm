package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
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

func loadDeclaredNamesSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), declaredNamesSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s
}

// namePosition is one place a snapshot stores a name.
type namePosition int

const (
	atRoot namePosition = iota
	atComposedChild
	atEdge
	atDuplicate
	atUnresolved
	positionCount
)

// Every combination of undeclared names over the five positions — each a plain
// name, a case variant of a declared one, or a name spelling a field — is
// refused by RebuildSnapshot, one E_INTERNAL per name, and only the input
// holding none is rebuilt.
func TestRebuildSnapshot_RefusesAnUndeclaredNameAtEveryPosition(t *testing.T) {
	t.Parallel()
	s := loadDeclaredNamesSchema(t)
	holderT, _ := s.Type("Holder")
	targetT, _ := s.Type("Target")
	pieceT, _ := s.Type("Piece")
	spellings := []string{"ghost", "NAME", "owner._target_key"}
	for mask := range 1 << (int(positionCount) * len(spellings)) {
		props := map[namePosition]map[string]any{
			atRoot: {"id": "h1", "note": "n"}, atComposedChild: {"label": "l"},
			atEdge: {"since": "s"}, atDuplicate: {"id": "h1"}, atUnresolved: {"since": "s"},
		}
		want := 0
		for bit := range int(positionCount) * len(spellings) {
			if mask&(1<<bit) != 0 {
				props[namePosition(bit/len(spellings))][spellings[bit%len(spellings)]] = "x"
				want++
			}
		}
		hk := immutable.WrapKey([]any{"h1"})
		parts := graph.SnapshotParts{
			Types: []schema.TypeID{holderT.ID(), targetT.ID()},
			Instances: []graph.InstanceParts{
				{
					TypeID: targetT.ID(), PrimaryKey: immutable.WrapKey([]any{"t1"}),
					Properties: immutable.WrapProperties(map[string]any{"key": "t1"}),
				},
				{
					TypeID: holderT.ID(), PrimaryKey: hk,
					Properties: immutable.WrapProperties(props[atRoot]),
					Composed: map[string][]graph.InstanceParts{"PIECES": {{
						TypeID:     pieceT.ID(),
						Properties: immutable.WrapProperties(props[atComposedChild]),
					}}},
				},
			},
			Edges: []graph.EdgeParts{{
				Relation: "OWNER", SourceType: holderT.ID(), SourceKey: hk,
				TargetKey: immutable.WrapKey([]any{"t1"}), Properties: immutable.WrapProperties(props[atEdge]),
			}},
			Duplicates: []graph.DuplicateParts{{
				Instance: graph.InstanceParts{TypeID: holderT.ID(), PrimaryKey: hk, Properties: immutable.WrapProperties(props[atDuplicate])},
			}},
			Unresolved: []graph.UnresolvedParts{{
				SourceType: holderT.ID(), SourceKey: hk, Relation: "OWNER",
				TargetKey: immutable.WrapKey([]any{"t9"}), Reason: "target_missing",
				Properties: immutable.WrapProperties(props[atUnresolved]),
			}},
		}
		snap, r := graph.RebuildSnapshot(s, parts)
		got := 0
		for issue := range r.Issues() {
			if issue.Code() == diag.E_INTERNAL && strings.Contains(issue.Message(), "does not declare") {
				got++
			}
		}
		if got != want || (want == 0) != (snap != nil) {
			t.Fatalf("mask %b: want %d refusals and a snapshot only when none, got %d, snapshot %v:\n%s", mask, want, got, snap != nil, r)
		}
	}
}

// Graph.Add and AddComposed refuse a bypass-built instance storing a name its
// type or association does not declare, so a graph the API builds never holds
// one, and a .ys the writer derives from it is one the reader accepts.
func TestAdd_BypassGuard_UndeclaredNames(t *testing.T) {
	t.Parallel()
	s := loadDeclaredNamesSchema(t)
	holderT, _ := s.Type("Holder")
	targetT, _ := s.Type("Target")
	pieceT, _ := s.Type("Piece")
	piece := func(props map[string]any) *instance.ValidInstance {
		return instance.NewValidInstance("Piece", pieceT.ID(), immutable.Key{}, immutable.WrapProperties(props), nil, nil, nil)
	}
	holder := func(props, edgeProps map[string]any, child *instance.ValidInstance) *instance.ValidInstance {
		var composed map[string]immutable.Value
		if child != nil {
			composed = map[string]immutable.Value{"PIECES": immutable.Wrap([]any{child})}
		}
		return instance.NewValidInstance("Holder", holderT.ID(), immutable.WrapKey([]any{"h1"}), immutable.WrapProperties(props),
			map[string]*instance.ValidEdgeData{"OWNER": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{"t1"}), immutable.WrapProperties(edgeProps)),
			})}, composed, nil)
	}
	for _, c := range []struct {
		name string
		inst *instance.ValidInstance
		code diag.Code
	}{
		{"a root property", holder(map[string]any{"id": "h1", "ghost": 1}, nil, nil), diag.E_UNKNOWN_FIELD},
		{"a case variant", holder(map[string]any{"id": "h1", "NOTE": "n"}, nil, nil), diag.E_UNKNOWN_FIELD},
		{"a name spelling an edge column", holder(map[string]any{"id": "h1", "owner._target_key": "t2"}, nil, nil), diag.E_UNKNOWN_FIELD},
		{"an edge property", holder(map[string]any{"id": "h1"}, map[string]any{"_target_key": "t2"}, nil), diag.E_UNKNOWN_EDGE_FIELD},
		{"an inline composed child's property", holder(map[string]any{"id": "h1"}, nil, piece(map[string]any{"ghost": 1})), diag.E_UNKNOWN_FIELD},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			g := graph.New(s)
			if res := g.Add(t.Context(), instance.NewValidInstance("Target", targetT.ID(), immutable.WrapKey([]any{"t1"}),
				immutable.WrapProperties(map[string]any{"key": "t1"}), nil, nil, nil)); !res.OK() {
				t.Fatalf("add target: %s", res)
			}
			res := g.Add(t.Context(), c.inst)
			if res.OK() {
				t.Fatal("an undeclared name passed Add")
			}
			assertHasCode(t, res, c.code)
			if n := len(g.Snapshot().InstancesOf(holderT.ID())); n != 0 {
				t.Fatalf("the refused instance installed: %d", n)
			}
		})
	}
	t.Run("a streamed composed child's property", func(t *testing.T) {
		t.Parallel()
		g := graph.New(s)
		if res := g.Add(t.Context(), instance.NewValidInstance("Holder", holderT.ID(), immutable.WrapKey([]any{"h1"}),
			immutable.WrapProperties(map[string]any{"id": "h1"}), nil, nil, nil)); !res.OK() {
			t.Fatalf("add holder: %s", res)
		}
		res := g.AddComposed(t.Context(), holderT.ID(), `["h1"]`, "PIECES", piece(map[string]any{"label": "l", "ghost": 1}))
		if res.OK() {
			t.Fatal("an undeclared name passed AddComposed")
		}
		assertHasCode(t, res, diag.E_UNKNOWN_FIELD)
		inst := g.Snapshot().InstancesOf(holderT.ID())[0]
		if n := len(inst.Composed("PIECES")); n != 0 {
			t.Fatalf("the refused child attached: %d", n)
		}
	})
}
