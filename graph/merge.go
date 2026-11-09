package graph

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// AddOrMerge adds inst as [Graph.Add] does, save where the graph already holds a
// root of inst's type at its primary key: there an inst with no composed children
// merges, installing its association records on the held root and keeping
// nothing else of it. It reports whether it merged; a merge records no
// [Duplicate], and merged is false whenever res holds an error. The package
// doc's Merge on a Duplicate Key section states the rules, among them the
// E_GRAPH_CARDINALITY refusal of a second (one) target. It panics as
// [Graph.Add] does, naming AddOrMerge.
func (g *Graph) AddOrMerge(ctx context.Context, inst instance.ValidInstance) (merged bool, res diag.Result) {
	return g.add(ctx, inst, "graph.AddOrMerge", true)
}

// recordSlot addresses one source instance's association.
type recordSlot struct {
	source   Instance
	relation string
}

// heldRecords is what one source holds under one association. A graph holds
// one record per source, association and target, so edges and target_missing
// records are each at their target key, and the "absent" or "empty" record,
// which names no target, stands alone.
type heldRecords struct {
	edges   map[string]Edge
	pending map[string]*pendingEdge
	reason  *pendingEdge
}

// installEdge appends e to the graph's edges and to the source index.
func (g *Graph) installEdge(e Edge) {
	g.edges = append(g.edges, e)
	if g.bySource != nil {
		g.held(recordSlot{e.source, e.relation}).addEdge(e)
	}
}

// installPendingEdge files p under its target and in the source index.
func (g *Graph) installPendingEdge(p *pendingEdge) {
	pk := pendingKey{targetTypeID: p.targetType, targetKey: p.targetKey}
	set := g.pending[pk]
	if set == nil {
		set = make(map[*pendingEdge]struct{})
		g.pending[pk] = set
	}
	set[p] = struct{}{}
	if g.bySource != nil {
		g.held(recordSlot{p.source, p.relation}).addPending(p)
	}
}

// resolvePending turns every unresolved record naming target, at typeID and
// key, into an edge.
func (g *Graph) resolvePending(typeID schema.TypeID, key string, target Instance) {
	pk := pendingKey{targetTypeID: typeID, targetKey: key}
	for pend := range g.pending[pk] {
		g.unindexPending(pend)
		g.installEdge(newEdge(pend.relation, pend.source, target, pend.properties))
	}
	delete(g.pending, pk)
}

// removePending drops p from the graph's unresolved records.
func (g *Graph) removePending(p *pendingEdge) {
	pk := pendingKey{targetTypeID: p.targetType, targetKey: p.targetKey}
	delete(g.pending[pk], p)
	if len(g.pending[pk]) == 0 {
		delete(g.pending, pk)
	}
	g.unindexPending(p)
}

// unindexPending drops p from the source index.
func (g *Graph) unindexPending(p *pendingEdge) {
	if g.bySource == nil {
		return
	}
	h := g.bySource[recordSlot{p.source, p.relation}]
	switch {
	case h == nil:
	case p.reasonDetail != "":
		if h.reason == p {
			h.reason = nil
		}
	case h.pending[p.targetKey] == p:
		delete(h.pending, p.targetKey)
	}
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

func (h *heldRecords) addEdge(e Edge) {
	if h.edges == nil {
		h.edges = make(map[string]Edge)
	}
	h.edges[e.target.PrimaryKey().String()] = e
}

func (h *heldRecords) addPending(p *pendingEdge) {
	if p.reasonDetail != "" {
		h.reason = p
		return
	}
	if h.pending == nil {
		h.pending = make(map[string]*pendingEdge)
	}
	h.pending[p.targetKey] = p
}

// sourceIndex builds the source index on the first merge, so a graph that
// never merges pays nothing for it. Every install and removal keeps it after.
func (g *Graph) sourceIndex() {
	if g.bySource != nil {
		return
	}
	g.bySource = make(map[recordSlot]*heldRecords)
	for _, e := range g.edges {
		g.held(recordSlot{e.source, e.relation}).addEdge(e)
	}
	for _, set := range g.pending {
		for p := range set {
			g.held(recordSlot{p.source, p.relation}).addPending(p)
		}
	}
}

// holdsTarget reports whether h holds an edge to, or an unresolved record
// naming, the target at key, which is not empty. A nil h holds none.
func (h *heldRecords) holdsTarget(key string) bool {
	if h == nil {
		return false
	}
	_, edge := h.edges[key]
	_, pending := h.pending[key]
	return edge || pending
}

// targets counts h's edges and the unresolved records that name a target. A
// nil h holds none.
func (h *heldRecords) targets() int {
	if h == nil {
		return 0
	}
	return len(h.edges) + len(h.pending)
}

// planMerge returns the incoming records a merge into held installs: each one
// that names a target held does not already hold. It refuses with one
// issue for each (one) association the merge would give a second target, in
// relation-name order. Called under g.mu.
func (g *Graph) planMerge(typ schema.Type, held Instance, staged []stagedEdge) ([]stagedEdge, []diag.Issue) {
	g.sourceIndex()
	var plan []stagedEdge
	added := make(map[recordSlot]int)
	// A validator refuses a listing that repeats a target key, so staged names
	// each target of a slot once.
	for _, se := range staged {
		// An "absent" or "empty" record names no target, so it adds nothing.
		if se.reason != "" {
			continue
		}
		slot := recordSlot{held, se.relation}
		if g.bySource[slot].holdsTarget(se.targetKey) {
			continue
		}
		added[slot]++
		plan = append(plan, se)
	}
	var refused []diag.Issue
	// A collector with an issue limit keeps the equally severe issues that
	// arrive first, so the refusals arrive in one order on every run.
	for _, slot := range slices.SortedFunc(maps.Keys(added), func(a, b recordSlot) int {
		return cmp.Compare(a.relation, b.relation)
	}) {
		n := added[slot]
		rel, _ := typ.Relation(slot.relation)
		if rel.IsMany() {
			continue
		}
		if h := g.bySource[slot]; n+h.targets() > 1 {
			refused = append(refused, diag.NewIssue(diag.Error, diag.E_GRAPH_CARDINALITY,
				fmt.Sprintf("association %q on type %q is (one), and %s[%s] already holds a target the duplicate does not name",
					slot.relation, held.TypeName(), held.TypeName(), diag.CutValue(held.PrimaryKey().String()))).
				WithDetail(diag.DetailKeyTypeName, held.TypeName()).
				WithDetail(diag.DetailKeyPrimaryKey, held.PrimaryKey().String()).
				WithDetail(diag.DetailKeyRelationName, slot.relation).Build())
		}
	}
	if len(refused) > 0 {
		return nil, refused
	}
	return plan, nil
}

// commitMerge installs plan's records on held: an edge where the target is
// held, an unresolved record where it is not. A record under a required
// association retires held's "absent" or "empty" record there, which stands
// alone by the package doc's Records fact. Called under g.mu.
func (g *Graph) commitMerge(held Instance, plan []stagedEdge) {
	for _, se := range plan {
		if h := g.bySource[recordSlot{held, se.relation}]; h != nil && h.reason != nil {
			g.removePending(h.reason)
		}
		if target := g.findInstance(se.targetType, se.targetKey); !target.IsZero() {
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
}
