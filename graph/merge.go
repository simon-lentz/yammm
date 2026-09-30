package graph

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/trace"
	"github.com/simon-lentz/yammm/schema"
)

// AddOrMerge adds inst as [Graph.Add] does, or, when the graph already holds an
// instance of its type at its primary key and inst carries no composed
// children, installs inst's association records on the held instance and keeps
// nothing else of inst. It reports whether it merged; a merge records no
// [Duplicate] and raises no error, and one that would give a (one) association
// a second target is refused with E_GRAPH_CARDINALITY. The package doc's Merge
// on a Duplicate Key section states the rules. Panics as [Graph.Add] does.
func (g *Graph) AddOrMerge(ctx context.Context, inst *instance.ValidInstance) (merged bool, res diag.Result) {
	return g.add(ctx, inst, "graph.AddOrMerge", "yammm.graph.add_or_merge", true)
}

// recordSlot addresses one source instance's association.
type recordSlot struct {
	source   *Instance
	relation string
}

// heldRecords is what one source holds under one association: its edges and
// its unresolved records.
type heldRecords struct {
	edges   []*Edge
	pending []*pendingEdge
}

// installEdge appends e to the graph's edges and to the source index.
func (g *Graph) installEdge(e *Edge) {
	g.edges = append(g.edges, e)
	if g.bySource != nil {
		h := g.held(recordSlot{e.source, e.relation})
		h.edges = append(h.edges, e)
	}
}

// installPendingEdge files p under its target and in the source index.
func (g *Graph) installPendingEdge(p *pendingEdge) {
	pk := pendingKey{targetTypeID: p.targetType, targetKey: p.targetKey}
	g.pending[pk] = append(g.pending[pk], p)
	if g.bySource != nil {
		h := g.held(recordSlot{p.source, p.relation})
		h.pending = append(h.pending, p)
	}
}

// resolvePending turns every unresolved record naming target, at typeID and
// key, into an edge, and returns how many it resolved.
func (g *Graph) resolvePending(typeID schema.TypeID, key string, target *Instance) int {
	pk := pendingKey{targetTypeID: typeID, targetKey: key}
	list := g.pending[pk]
	for _, pend := range list {
		g.unindexPending(pend)
		g.installEdge(newEdge(pend.relation, pend.source, target, pend.properties))
	}
	delete(g.pending, pk)
	return len(list)
}

// removePending drops p from the graph's unresolved records.
func (g *Graph) removePending(p *pendingEdge) {
	pk := pendingKey{targetTypeID: p.targetType, targetKey: p.targetKey}
	list := slices.DeleteFunc(g.pending[pk], func(q *pendingEdge) bool { return q == p })
	if len(list) == 0 {
		delete(g.pending, pk)
	} else {
		g.pending[pk] = list
	}
	g.unindexPending(p)
}

// unindexPending drops p from the source index.
func (g *Graph) unindexPending(p *pendingEdge) {
	if g.bySource == nil {
		return
	}
	h := g.held(recordSlot{p.source, p.relation})
	h.pending = slices.DeleteFunc(h.pending, func(q *pendingEdge) bool { return q == p })
}

// held returns slot's records in the source index, creating the entry.
func (g *Graph) held(slot recordSlot) *heldRecords {
	h := g.bySource[slot]
	if h == nil {
		h = &heldRecords{}
		g.bySource[slot] = h
	}
	return h
}

// sourceIndex builds the source index on the first merge, so a graph that
// never merges pays nothing for it. Every install and removal keeps it after.
func (g *Graph) sourceIndex() {
	if g.bySource != nil {
		return
	}
	g.bySource = make(map[recordSlot]*heldRecords)
	for _, e := range g.edges {
		h := g.held(recordSlot{e.source, e.relation})
		h.edges = append(h.edges, e)
	}
	for _, list := range g.pending {
		for _, p := range list {
			h := g.held(recordSlot{p.source, p.relation})
			h.pending = append(h.pending, p)
		}
	}
}

// holdsTarget reports whether h holds an edge to, or an unresolved record
// naming, the target at key. A nil h holds none.
func (h *heldRecords) holdsTarget(key string) bool {
	if h == nil {
		return false
	}
	for _, e := range h.edges {
		if e.target.PrimaryKey().String() == key {
			return true
		}
	}
	for _, p := range h.pending {
		if p.reasonDetail == "" && p.targetKey == key {
			return true
		}
	}
	return false
}

// targets counts h's edges and the unresolved records that name a target. A
// nil h holds none.
func (h *heldRecords) targets() int {
	if h == nil {
		return 0
	}
	n := len(h.edges)
	for _, p := range h.pending {
		if p.reasonDetail == "" {
			n++
		}
	}
	return n
}

// planMerge returns the incoming records a merge into held installs: each one
// that names a target held does not already hold, once. It refuses a (one)
// association the merge would give a second target. Called under g.mu.
func (g *Graph) planMerge(typ *schema.Type, held *Instance, staged []stagedEdge) ([]stagedEdge, *diag.Issue) {
	g.sourceIndex()
	var plan []stagedEdge
	added := make(map[recordSlot]int)
	seen := make(map[recordSlot]map[string]bool)
	for _, se := range staged {
		// A required association's "absent" or "empty" record adds nothing:
		// held already holds an edge or a record under it.
		if se.reason != "" {
			continue
		}
		slot := recordSlot{held, se.relation}
		if seen[slot][se.targetKey] {
			continue
		}
		if g.bySource[slot].holdsTarget(se.targetKey) {
			continue
		}
		if seen[slot] == nil {
			seen[slot] = make(map[string]bool)
		}
		seen[slot][se.targetKey] = true
		added[slot]++
		plan = append(plan, se)
	}
	for slot, n := range added {
		rel, _ := typ.Relation(slot.relation)
		if rel.IsMany() {
			continue
		}
		if h := g.bySource[slot]; n+h.targets() > 1 {
			issue := diag.NewIssue(diag.Error, diag.E_GRAPH_CARDINALITY,
				fmt.Sprintf("association %q on type %q is (one), and %s[%s] already holds a target the duplicate does not name",
					slot.relation, held.TypeName(), held.TypeName(), held.PrimaryKey().String())).
				WithDetail(diag.DetailKeyTypeName, held.TypeName()).
				WithDetail(diag.DetailKeyPrimaryKey, held.PrimaryKey().String()).
				WithDetail(diag.DetailKeyRelationName, slot.relation).Build()
			return nil, &issue
		}
	}
	return plan, nil
}

// commitMerge installs plan's records on held: an edge where the target is
// held, an unresolved record where it is not. A record under a required
// association retires held's "absent" or "empty" record there, which stands
// alone by the package doc's Records fact. Called under g.mu.
func (g *Graph) commitMerge(ctx context.Context, held *Instance, plan []stagedEdge) {
	for _, se := range plan {
		if h := g.bySource[recordSlot{held, se.relation}]; h != nil {
			for _, p := range slices.Clone(h.pending) {
				if p.reasonDetail != "" {
					g.removePending(p)
				}
			}
		}
		if target := g.findInstance(se.targetType, se.targetKey); target != nil {
			g.installEdge(newEdge(se.relation, held, target, se.properties))
			continue
		}
		g.installPendingEdge(&pendingEdge{
			source:     held,
			relation:   se.relation,
			jsonField:  se.jsonField,
			targetType: se.targetType,
			targetKey:  se.targetKey,
			properties: se.properties,
			isRequired: se.isRequired,
		})
	}
	trace.Debug(
		ctx, g.config.logger, "merged on duplicate primary key",
		slog.String("type", held.TypeName()),
		slog.String("pk", held.PrimaryKey().String()),
		slog.Int("records", len(plan)),
	)
}
