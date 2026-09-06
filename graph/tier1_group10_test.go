package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const (
	rawInstant   = "2020-01-02T03:04:05+00:00"
	canonInstant = "2020-01-02T03:04:05Z"
)

// group10Schema declares a Timestamp-keyed root that composes a part and
// associates to a Timestamp-keyed peer, so every address the graph receives
// — a composed parent, an edge endpoint, an unresolved target — has a
// canonical spelling distinct from the one a caller may hold.
func group10Schema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "group10"

type Run {
	at Timestamp primary
	--> NEXT (_) Run
	*-> STEPS (_:many) Step
}

part type Step {
	n String primary
}
`
	s, res := schema.LoadString(t.Context(), src, "group10.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func runInstance(t *testing.T, s *schema.Schema, at string, next string) *instance.ValidInstance {
	t.Helper()
	runID := mustTypeID(t, s, "Run")
	var edges map[string]*instance.ValidEdgeData
	if next != "" {
		edges = map[string]*instance.ValidEdgeData{"NEXT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{next}), immutable.WrapProperties(nil)),
		})}
	}
	return instance.NewValidInstance("Run", runID, immutable.WrapKey([]any{at}),
		immutable.WrapProperties(map[string]any{"at": at}), edges, nil, nil)
}

// TestAddComposed_ParentAddressedByTheCallersSpelling pins that the natural
// sequence — Add an instance, then AddComposed with that instance's own key
// string — attaches whatever spelling the caller holds: the graph
// canonicalizes the parent address it receives, as it canonicalizes the key
// it installs.
func TestAddComposed_ParentAddressedByTheCallersSpelling(t *testing.T) {
	t.Parallel()
	s := group10Schema(t)
	runID := mustTypeID(t, s, "Run")
	stepID := mustTypeID(t, s, "Step")
	run := runInstance(t, s, rawInstant, "")
	step := instance.NewValidInstance("Step", stepID, immutable.WrapKey([]any{"s1"}),
		immutable.WrapProperties(map[string]any{"n": "s1"}), nil, nil, nil)

	for _, spelling := range []string{run.PrimaryKey().String(), graph.FormatKey(canonInstant)} {
		g := graph.New(s)
		if r := g.Add(t.Context(), run); !r.OK() {
			t.Fatalf("add: %s", r)
		}
		if r := g.AddComposed(t.Context(), runID, spelling, "STEPS", step); !r.OK() {
			t.Fatalf("AddComposed by %s: %s", spelling, r)
		}
		inst := g.Snapshot().InstancesOf(runID)[0]
		if got := len(inst.Composed("STEPS")); got != 1 {
			t.Errorf("AddComposed by %s attached %d children, want 1", spelling, got)
		}
	}
}

// TestAddComposed_RefusedParentKeepsTheCallersSpelling pins that a parent the
// graph does not hold is reported under the spelling the caller wrote: a
// refused address addresses nothing, so nothing canonicalizes it.
func TestAddComposed_RefusedParentKeepsTheCallersSpelling(t *testing.T) {
	t.Parallel()
	s := group10Schema(t)
	runID := mustTypeID(t, s, "Run")
	stepID := mustTypeID(t, s, "Step")
	step := instance.NewValidInstance("Step", stepID, immutable.WrapKey([]any{"s1"}),
		immutable.WrapProperties(map[string]any{"n": "s1"}), nil, nil, nil)
	g := graph.New(s)
	r := g.AddComposed(t.Context(), runID, graph.FormatKey(rawInstant), "STEPS", step)
	if r.OK() {
		t.Fatal("a parent the graph does not hold attached a child")
	}
	found := false
	for is := range r.Issues() {
		for _, d := range is.Details() {
			if d.Key == diag.DetailKeyPrimaryKey && d.Value == graph.FormatKey(rawInstant) {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("the refusal does not carry the caller's spelling: %s", r)
	}
}

// TestUnresolvedTarget_OneAddressOnEveryPath is the second implementation
// for A-352: a forward reference whose target key the caller spelled
// non-canonically carries the canonical target key whether the record was
// built through Add or through RebuildSnapshot, so the two entry points
// produce one record from one input.
func TestUnresolvedTarget_OneAddressOnEveryPath(t *testing.T) {
	t.Parallel()
	s := group10Schema(t)
	runID := mustTypeID(t, s, "Run")
	want := graph.FormatKey(canonInstant)

	g := graph.New(s)
	if r := g.Add(t.Context(), runInstance(t, s, "2021-01-01T00:00:00Z", rawInstant)); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	added := g.Snapshot().Unresolved()
	if len(added) != 1 || added[0].TargetKey != want {
		t.Errorf("Add path: unresolved target = %v, want one record at %s", added, want)
	}

	rebuilt, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{runID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			runID: {{
				TypeName: "Run", TypeID: runID, PrimaryKey: immutable.WrapKey([]any{"2021-01-01T00:00:00Z"}),
				Properties: immutable.WrapProperties(map[string]any{"at": "2021-01-01T00:00:00Z"}),
			}},
		},
		Unresolved: []graph.UnresolvedParts{{
			SourceType: runID, SourceKey: immutable.WrapKey([]any{"2021-01-01T00:00:00Z"}),
			Relation: "NEXT", TargetType: runID, TargetKey: immutable.WrapKey([]any{rawInstant}),
			Reason: "target_missing",
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	if got := rebuilt.Unresolved(); len(got) != 1 || got[0].TargetKey != want {
		t.Errorf("rebuild path: unresolved target = %v, want one record at %s", got, want)
	}
}

// TestRebuildSnapshot_SourceAddressesMoveWithTheInstances pins the two
// rebuild-path sites a mutation left green: an edge's source key and an
// unresolved record's source key are canonicalized before the lookup, so a
// caller's spelling of either resolves against the index step 1 rewrote and
// the record carries the canonical form.
func TestRebuildSnapshot_SourceAddressesMoveWithTheInstances(t *testing.T) {
	t.Parallel()
	s := group10Schema(t)
	runID := mustTypeID(t, s, "Run")
	const other = "2021-01-01T00:00:00Z"
	parts := func(at string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName: "Run", TypeID: runID, PrimaryKey: immutable.WrapKey([]any{at}),
			Properties: immutable.WrapProperties(map[string]any{"at": at}),
		}
	}
	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types:     []schema.TypeID{runID},
		Instances: map[schema.TypeID][]graph.InstanceParts{runID: {parts(rawInstant), parts(other)}},
		Edges: []graph.EdgeParts{{
			SourceType: runID, SourceKey: immutable.WrapKey([]any{rawInstant}),
			Relation: "NEXT", TargetType: runID, TargetKey: immutable.WrapKey([]any{other}),
		}},
		Unresolved: []graph.UnresolvedParts{{
			SourceType: runID, SourceKey: immutable.WrapKey([]any{rawInstant}),
			Relation: "NEXT", TargetType: runID, TargetKey: immutable.WrapKey([]any{"2022-01-01T00:00:00Z"}),
			Reason: "target_missing",
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}
	if e := snap.Edges(); len(e) != 1 || e[0].Source().PrimaryKey().String() != graph.FormatKey(canonInstant) {
		t.Errorf("edge source = %v, want the canonical instant", e)
	}
	if u := snap.Unresolved(); len(u) != 1 || u[0].Source.PrimaryKey().String() != graph.FormatKey(canonInstant) {
		t.Errorf("unresolved source = %v, want the canonical instant", u)
	}
}
