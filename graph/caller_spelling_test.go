package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

const (
	rawInstant   = "2020-01-02T03:04:05+00:00"
	canonInstant = "2020-01-02T03:04:05Z"
)

// callerSpellingSchema declares a Timestamp-keyed root that composes a part and
// associates to a Timestamp-keyed peer, so every address the graph receives
// — an edge endpoint, an unresolved target — has a
// canonical spelling distinct from the one a caller may hold.
func callerSpellingSchema(t *testing.T) schema.Schema {
	t.Helper()
	const src = `schema "caller_spelling"

type Run {
	at Timestamp primary
	--> NEXT (_:many) Run
	*-> STEPS (_:many) Step
}

part type Step {
	n String primary
}

type Tag {
	name String primary
}
`
	s, res := schema.LoadString(t.Context(), src, "caller_spelling.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// runInstance validates a Run over s, its key and its NEXT target spelled as given.
func runInstance(t *testing.T, s schema.Schema, at, next string) instance.ValidInstance {
	t.Helper()
	data := map[string]any{"at": at}
	if next != "" {
		data["next"] = []any{map[string]any{"_target_at": next}}
	}
	return instancetest.Valid(t, s, "Run", data)
}

// TestUnresolvedTarget_OneAddressOnEveryPath pins that a forward reference
// whose target key the caller spelled
// non-canonically carries the canonical target key whether the record was
// built through Add or rebuilt from a document's parts, so the two paths
// produce one record from one input.
func TestUnresolvedTarget_OneAddressOnEveryPath(t *testing.T) {
	t.Parallel()
	s := callerSpellingSchema(t)
	runID := mustTypeID(t, s, "Run")
	want := graph.FormatKey(canonInstant)

	g := graph.New(s)
	if r := g.Add(t.Context(), runInstance(t, s, "2021-01-01T00:00:00Z", rawInstant)); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	added := slices.Collect(g.Snapshot().Unresolved())
	if len(added) != 1 || added[0].TargetKey() != want {
		t.Errorf("Add path: unresolved target = %v, want one record at %s", added, want)
	}

	rebuilt := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{runID},
		Instances: []snapparts.Instance{
			{
				TypeID: runID, PrimaryKey: immutable.WrapKey([]any{"2021-01-01T00:00:00Z"}),
				Properties: immutable.WrapProperties(map[string]any{"at": "2021-01-01T00:00:00Z"}),
			},
		},
		Unresolved: []snapparts.Unresolved{{
			SourceType: runID, SourceKey: immutable.WrapKey([]any{"2021-01-01T00:00:00Z"}),
			Relation: "NEXT", TargetKey: immutable.WrapKey([]any{rawInstant}),
			Reason: "target_missing",
		}},
	})
	if got := slices.Collect(rebuilt.Unresolved()); len(got) != 1 || got[0].TargetKey() != want {
		t.Errorf("rebuild path: unresolved target = %v, want one record at %s", got, want)
	}
}

// TestRebuild_SourceAddressesMoveWithTheInstances pins two rebuild-path sites:
// an edge's source key and an unresolved record's source key are canonicalized
// before the lookup, so either spelling resolves against the rewritten index and
// the record carries the canonical form.
func TestRebuild_SourceAddressesMoveWithTheInstances(t *testing.T) {
	t.Parallel()
	s := callerSpellingSchema(t)
	runID := mustTypeID(t, s, "Run")
	const other = "2021-01-01T00:00:00Z"
	parts := func(at string) snapparts.Instance {
		return snapparts.Instance{
			TypeID: runID, PrimaryKey: immutable.WrapKey([]any{at}),
			Properties: immutable.WrapProperties(map[string]any{"at": at}),
		}
	}
	snap := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{runID},
		Instances: []snapparts.Instance{
			parts(rawInstant),
			parts(other),
		},
		Edges: []snapparts.Edge{{
			SourceType: runID, SourceKey: immutable.WrapKey([]any{rawInstant}),
			Relation: "NEXT", TargetKey: immutable.WrapKey([]any{other}),
		}},
		Unresolved: []snapparts.Unresolved{{
			SourceType: runID, SourceKey: immutable.WrapKey([]any{rawInstant}),
			Relation: "NEXT", TargetKey: immutable.WrapKey([]any{"2022-01-01T00:00:00Z"}),
			Reason: "target_missing",
		}},
	})
	if e := slices.Collect(snap.Edges()); len(e) != 1 || e[0].Source().PrimaryKey().String() != graph.FormatKey(canonInstant) {
		t.Errorf("edge source = %v, want the canonical instant", e)
	}
	if u := slices.Collect(snap.Unresolved()); len(u) != 1 || u[0].Source().PrimaryKey().String() != graph.FormatKey(canonInstant) {
		t.Errorf("unresolved source = %v, want the canonical instant", u)
	}
}
