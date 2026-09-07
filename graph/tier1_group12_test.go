package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// TestInstanceByKey_AnySpellingOnEveryPath is the second implementation for
// A-355: a Timestamp-keyed instance entered by a non-canonical spelling is
// found by its own key, by FormatKey of the canonical text and by FormatKey
// of the spelling the caller wrote, on a snapshot built through Add and on
// one rebuilt from parts.
func TestInstanceByKey_AnySpellingOnEveryPath(t *testing.T) {
	t.Parallel()
	s := group10Schema(t)
	runID := mustTypeID(t, s, "Run")
	want := graph.FormatKey(canonInstant)

	g := graph.New(s)
	if r := g.Add(t.Context(), runInstance(t, s, rawInstant, "")); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	built := g.Snapshot()

	rebuilt, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{runID},
		Instances: map[schema.TypeID][]graph.InstanceParts{runID: {{
			TypeName: "Run", TypeID: runID, PrimaryKey: immutable.WrapKey([]any{rawInstant}),
			Properties: immutable.WrapProperties(map[string]any{"at": rawInstant}),
		}}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}

	paths := []struct {
		name string
		snap *graph.Snapshot
	}{{"Add", built}, {"RebuildSnapshot", rebuilt}}
	for _, path := range paths {
		own := path.snap.InstancesOf(runID)[0].PrimaryKey().String()
		spellings := []struct {
			name, key string
		}{
			{"the instance's own key", own},
			{"FormatKey(canonical)", want},
			{"FormatKey(raw)", graph.FormatKey(rawInstant)},
		}
		for _, sp := range spellings {
			inst, ok := path.snap.InstanceByKey(runID, sp.key)
			if !ok {
				t.Errorf("%s path: InstanceByKey by %s (%s) missed", path.name, sp.name, sp.key)
				continue
			}
			if got := inst.PrimaryKey().String(); got != want {
				t.Errorf("%s path: InstanceByKey by %s found %s, want %s", path.name, sp.name, got, want)
			}
		}
	}
}

// TestInstanceByKey_RefusedAddressMisses pins that a string ParseKey refuses
// addresses nothing: the lookup misses under it and does not panic, on a
// canonicalizing key type and on a plain String key alike.
func TestInstanceByKey_RefusedAddressMisses(t *testing.T) {
	t.Parallel()
	s := group10Schema(t)
	runID := mustTypeID(t, s, "Run")
	stepID := mustTypeID(t, s, "Step")
	g := graph.New(s)
	if r := g.Add(t.Context(), runInstance(t, s, rawInstant, "")); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	snap := g.Snapshot()
	for _, id := range []schema.TypeID{runID, stepID} {
		if inst, ok := snap.InstanceByKey(id, "not an address"); ok || inst != nil {
			t.Errorf("InstanceByKey(%s, unparseable) = %v, %v; want nil, false", id, inst, ok)
		}
	}
}
