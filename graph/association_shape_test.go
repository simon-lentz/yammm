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

const associationShapeSchema = `schema "assoc"

type Company {
	id String primary
}

type Pair {
	a String primary
	b String primary
}

part type Badge {
	code String primary
}

type Person {
	id String primary
	--> EMPLOYER (_:one) Company
	--> MANAGER (one:one) Person
	--> FRIENDS (_:many) Person
	--> PARTNER (_:one) Pair
	*-> BADGES (_:many) Badge
}

type Run {
	at Timestamp primary
	--> NEXT (_:one) Run
}
`

type assocIDs struct{ company, pair, badge, person, run schema.TypeID }

func loadAssociationShapes(t *testing.T) (*schema.Schema, assocIDs) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), associationShapeSchema, "assoc.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s, assocIDs{
		company: mustTypeID(t, s, "Company"), pair: mustTypeID(t, s, "Pair"), badge: mustTypeID(t, s, "Badge"),
		person: mustTypeID(t, s, "Person"), run: mustTypeID(t, s, "Run"),
	}
}

// personParts is Person p1 and Company c1, the instances every case's records
// address.
func personParts(ids assocIDs) graph.SnapshotParts {
	return graph.SnapshotParts{
		Types: []schema.TypeID{ids.person, ids.company},
		Instances: []graph.InstanceParts{
			{TypeID: ids.person, PrimaryKey: immutable.WrapKey([]any{"p1"}), Properties: immutable.WrapProperties(map[string]any{"id": "p1"})},
			{TypeID: ids.company, PrimaryKey: immutable.WrapKey([]any{"c1"}), Properties: immutable.WrapProperties(map[string]any{"id": "c1"})},
		},
	}
}

func unresolvedFromP1(ids assocIDs, relation, reason string, key ...any) graph.UnresolvedParts {
	up := graph.UnresolvedParts{
		SourceType: ids.person, SourceKey: immutable.WrapKey([]any{"p1"}), Relation: relation,
		Reason: reason,
	}
	if key != nil {
		up.TargetKey = immutable.WrapKey(key)
	}
	return up
}

// TestRebuildSnapshot_HoldsEveryAssociationRecordToWhatAddStages drives each
// association rule at RebuildSnapshot: every record [graph.Graph.Add] can stage
// is under an association of the source's type, at its declared target, with a
// target key of the target's arity, one record at most under a (one), and a
// reason from the documented set.
func TestRebuildSnapshot_HoldsEveryAssociationRecordToWhatAddStages(t *testing.T) {
	t.Parallel()
	s, ids := loadAssociationShapes(t)
	edge := func(relation string, key ...any) graph.EdgeParts {
		return graph.EdgeParts{
			Relation: relation, SourceType: ids.person, SourceKey: immutable.WrapKey([]any{"p1"}),
			TargetKey: immutable.WrapKey(key),
		}
	}
	for _, c := range []struct {
		name   string
		code   diag.Code
		phrase string
		shape  func(p *graph.SnapshotParts)
	}{
		{"an edge under an undeclared name", diag.E_GRAPH_UNKNOWN_RELATION, `under "BOSS", which its type does not declare as an association`, func(p *graph.SnapshotParts) {
			p.Edges = []graph.EdgeParts{edge("BOSS", "c1")}
		}},
		{"an edge under a composition's name", diag.E_GRAPH_UNKNOWN_RELATION, `under "BADGES", which its type does not declare as an association`, func(p *graph.SnapshotParts) {
			p.Edges = []graph.EdgeParts{edge("BADGES", "c1")}
		}},
		{"an unresolved record under a composition's name", diag.E_GRAPH_UNKNOWN_RELATION, `under "BADGES", which its type does not declare as an association`, func(p *graph.SnapshotParts) {
			p.Unresolved = []graph.UnresolvedParts{unresolvedFromP1(ids, "BADGES", "target_missing", "b1")}
		}},
		{"a (one) holding two edges", diag.E_GRAPH_CARDINALITY, `(one) association "EMPLOYER" of Person[["p1"]] holds 2 records`, func(p *graph.SnapshotParts) {
			p.Instances = append(p.Instances, graph.InstanceParts{TypeID: ids.company, PrimaryKey: immutable.WrapKey([]any{"c2"}), Properties: immutable.WrapProperties(map[string]any{"id": "c2"})})
			p.Edges = []graph.EdgeParts{edge("EMPLOYER", "c1"), edge("EMPLOYER", "c2")}
		}},
		{"a (one) holding an edge and an unresolved record", diag.E_GRAPH_CARDINALITY, `(one) association "EMPLOYER" of Person[["p1"]] holds 2 records`, func(p *graph.SnapshotParts) {
			p.Edges = []graph.EdgeParts{edge("EMPLOYER", "c1")}
			p.Unresolved = []graph.UnresolvedParts{unresolvedFromP1(ids, "EMPLOYER", "target_missing", "c9")}
		}},
		{"an unresolved target key of the wrong arity", diag.E_GRAPH_INVALID_PK, `under "PARTNER" carries a 1-part target key; the target declares 2`, func(p *graph.SnapshotParts) {
			p.Unresolved = []graph.UnresolvedParts{unresolvedFromP1(ids, "PARTNER", "target_missing", "x")}
		}},
		{"an edge target key of the wrong arity", diag.E_GRAPH_INVALID_PK, `under "EMPLOYER" carries a 2-part target key; the target declares 1`, func(p *graph.SnapshotParts) {
			p.Edges = []graph.EdgeParts{edge("EMPLOYER", "c1", "extra")}
		}},
		{"a reason outside the documented set", diag.E_INTERNAL, `states reason "lost", which is not one of`, func(p *graph.SnapshotParts) {
			p.Unresolved = []graph.UnresolvedParts{unresolvedFromP1(ids, "EMPLOYER", "lost", "c9")}
		}},
		{"an empty record carrying edge properties", diag.E_INTERNAL, `reason "empty" carries a target key or edge properties`, func(p *graph.SnapshotParts) {
			up := unresolvedFromP1(ids, "MANAGER", "empty")
			up.Properties = immutable.WrapProperties(map[string]any{"since": "x"})
			p.Unresolved = []graph.UnresolvedParts{up}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			parts := personParts(ids)
			c.shape(&parts)
			snap, res := graph.RebuildSnapshot(s, parts)
			if snap != nil {
				t.Error("a refused rebuild returned a snapshot")
			}
			found := false
			for issue := range res.Issues() {
				if issue.Code() == c.code && strings.Contains(issue.Message(), c.phrase) {
					found = true
				}
			}
			if !found {
				t.Errorf("no %s naming %q: %s", c.code, c.phrase, res)
			}
		})
	}
}

// TestRebuildSnapshot_CountsAOneSlotByItsCanonicalSourceKey pins that two
// spellings of one source instant fill one (one) association, as they address
// one instance.
func TestRebuildSnapshot_CountsAOneSlotByItsCanonicalSourceKey(t *testing.T) {
	t.Parallel()
	s, ids := loadAssociationShapes(t)
	record := func(source string, target string) graph.UnresolvedParts {
		return graph.UnresolvedParts{
			SourceType: ids.run, SourceKey: immutable.WrapKey([]any{source}), Relation: "NEXT",
			TargetKey: immutable.WrapKey([]any{target}), Reason: "target_missing",
		}
	}
	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{ids.run},
		Instances: []graph.InstanceParts{{
			TypeID: ids.run, PrimaryKey: immutable.WrapKey([]any{canonInstant}),
			Properties: immutable.WrapProperties(map[string]any{"at": canonInstant}),
		}},
		Unresolved: []graph.UnresolvedParts{record(rawInstant, "2030-01-01T00:00:00Z"), record(canonInstant, "2031-01-01T00:00:00Z")},
	})
	if !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("two spellings of one source filled a (one) twice: %s", res)
	}
}

// TestRebuildSnapshot_DerivesRequiredFromTheRelation pins that an unresolved
// record's Required is the relation's, as Add derives it: the parts cannot
// state it.
func TestRebuildSnapshot_DerivesRequiredFromTheRelation(t *testing.T) {
	t.Parallel()
	s, ids := loadAssociationShapes(t)
	parts := personParts(ids)
	parts.Unresolved = []graph.UnresolvedParts{
		unresolvedFromP1(ids, "MANAGER", "absent"),
		unresolvedFromP1(ids, "FRIENDS", "target_missing", "p9"),
	}
	snap, res := graph.RebuildSnapshot(s, parts)
	if res.HasErrors() {
		t.Fatalf("rebuild: %s", res)
	}
	got := map[string]bool{}
	for _, u := range snap.Unresolved() {
		got[u.Relation()] = u.Required()
	}
	if !got["MANAGER"] || got["FRIENDS"] {
		t.Errorf("Required = %v, want MANAGER required and FRIENDS not", got)
	}
}

// TestAdd_RefusesATargetKeyOfTheWrongArity pins the rule Add shares with the
// other constructors: no instance can carry a key of another arity, so the
// record would stay unresolved for the life of the graph.
func TestAdd_RefusesATargetKeyOfTheWrongArity(t *testing.T) {
	t.Parallel()
	s, ids := loadAssociationShapes(t)
	target := func(key ...any) *instance.ValidEdgeData {
		return instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey(key), immutable.Properties{}),
		})
	}
	for _, c := range []struct {
		relation string
		key      []any
		phrase   string
	}{
		{"PARTNER", []any{"x"}, `association "PARTNER" on type "Person" carries a 1-part target key; the target declares 2`},
		{"EMPLOYER", []any{"c1", "extra"}, `association "EMPLOYER" on type "Person" carries a 2-part target key; the target declares 1`},
	} {
		inst := instance.NewValidInstance("Person", ids.person, immutable.WrapKey([]any{"p1"}),
			immutable.WrapProperties(map[string]any{"id": "p1"}),
			map[string]*instance.ValidEdgeData{c.relation: target(c.key...)}, nil, nil)
		res := graph.New(s).Add(t.Context(), inst)
		found := false
		for issue := range res.Issues() {
			if issue.Code() == diag.E_GRAPH_INVALID_PK && strings.Contains(issue.Message(), c.phrase) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no E_GRAPH_INVALID_PK naming %q: %s", c.relation, c.phrase, res)
		}
	}
}
