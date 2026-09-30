package graph

import (
	"maps"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// indexedInstance builds a bypass instance of typ keyed id, naming each
// relation's targets by key.
func indexedInstance(t *testing.T, s *schema.Schema, typ, id string, edges map[string][]string) *instance.ValidInstance {
	t.Helper()
	tt, ok := s.Type(typ)
	if !ok {
		t.Fatalf("no type %s", typ)
	}
	data := make(map[string]*instance.ValidEdgeData, len(edges))
	for rel, keys := range edges {
		ts := make([]instance.ValidEdgeTarget, len(keys))
		for i, k := range keys {
			ts[i] = instance.NewValidEdgeTarget(immutable.WrapKey([]any{k}), immutable.WrapProperties(nil))
		}
		data[rel] = instance.NewValidEdgeData(ts)
	}
	return instancetest.VI(typ, instancetest.TypeID(tt.ID()), instancetest.PK(id),
		instancetest.Props(map[string]any{"id": id}), instancetest.Edges(data))
}

// assertIndexMatchesARebuild fails unless g.bySource holds exactly what an
// index rebuilt from g.edges and g.pending holds.
func assertIndexMatchesARebuild(t *testing.T, g *Graph, step string) {
	t.Helper()
	if g.bySource == nil {
		t.Fatalf("%s: no source index", step)
	}
	fresh := &Graph{edges: g.edges, pending: g.pending}
	fresh.sourceIndex()
	for slot := range mapsKeys(g.bySource, fresh.bySource) {
		have, want := g.bySource[slot], fresh.bySource[slot]
		if !sameElements(have.edgeList(), want.edgeList()) || !sameElements(have.pendingList(), want.pendingList()) {
			t.Errorf("%s: %s[%s] %s: index holds %d edges and %d records, the graph %d and %d", step,
				slot.source.TypeName(), slot.source.PrimaryKey(), slot.relation,
				len(have.edgeList()), len(have.pendingList()), len(want.edgeList()), len(want.pendingList()))
		}
	}
}

func (h *heldRecords) edgeList() []*Edge {
	if h == nil {
		return nil
	}
	return h.edges
}

func (h *heldRecords) pendingList() []*pendingEdge {
	if h == nil {
		return nil
	}
	return h.pending
}

func mapsKeys[K comparable, V any](a, b map[K]V) map[K]bool {
	out := make(map[K]bool, len(a)+len(b))
	for k := range a {
		out[k] = true
	}
	for k := range b {
		out[k] = true
	}
	return out
}

// sameElements reports whether a and b hold the same elements, each as many
// times, in any order.
func sameElements[T comparable](a, b []T) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[T]int, len(a))
	for _, x := range a {
		counts[x]++
	}
	for _, x := range b {
		counts[x]--
	}
	return !slices.ContainsFunc(slices.Collect(maps.Values(counts)), func(n int) bool { return n != 0 })
}

// The source index agrees with a rebuild after every Add, merge and
// resolution once the first merge has built it.
func TestSourceIndex_MatchesARebuildAfterEveryOperation(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "merge"

type Issuer {
	id String primary
}

type Page {
	id String primary
}

type Issue {
	id String primary
	--> ISSUED_BY (many) Issuer
	--> LISTED_ON (one:many) Page
	--> LEAD (_:one) Issuer
	--> HOME (one) Page
}
`, "merge.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	g := New(s)
	add := func(step, typ, id string, edges map[string][]string) {
		t.Helper()
		if r := g.Add(t.Context(), indexedInstance(t, s, typ, id, edges)); r.HasErrors() {
			t.Fatalf("%s: %s", step, r)
		}
		if g.bySource != nil {
			assertIndexMatchesARebuild(t, g, step)
		}
	}
	merge := func(step, id string, edges map[string][]string) {
		t.Helper()
		if merged, r := g.AddOrMerge(t.Context(), indexedInstance(t, s, "Issue", id, edges)); r.HasErrors() || !merged {
			t.Fatalf("%s: merged %v, %s", step, merged, r)
		}
		assertIndexMatchesARebuild(t, g, step)
	}
	add("issuer a", "Issuer", "a", nil)
	add("page p1", "Page", "p1", nil)
	add("issue i1", "Issue", "i1", map[string][]string{"HOME": {"p1"}, "LISTED_ON": {"p1"}, "ISSUED_BY": {"a", "zz"}})
	add("issue i2", "Issue", "i2", nil)
	merge("first merge", "i1", map[string][]string{"ISSUED_BY": {"late", "b"}})
	add("resolve late", "Issuer", "late", nil)
	add("issue i3", "Issue", "i3", map[string][]string{"ISSUED_BY": {"a", "q"}})
	merge("retire an absent LISTED_ON", "i2", map[string][]string{"LISTED_ON": {"p1"}})
	merge("retire an absent HOME", "i2", map[string][]string{"HOME": {"p9"}})
	add("resolve zz", "Issuer", "zz", nil)
	add("resolve p9", "Page", "p9", nil)
	merge("merge into i3", "i3", map[string][]string{"ISSUED_BY": {"q", "late"}})
	add("resolve q", "Issuer", "q", nil)
	add("resolve b", "Issuer", "b", nil)
}
