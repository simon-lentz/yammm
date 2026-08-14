package graph

import (
	"github.com/simon-lentz/yammm/schema"
)

// NewFromSnapshot creates a Graph pre-populated from a Snapshot's contents.
//
// The returned Graph is ready for additional [Graph.Add] calls. New instances
// interact naturally with imported data: duplicate detection, edge resolution,
// and composition extraction work as normal.
//
// NewFromSnapshot must be used instead of calling importSnapshot on an existing
// graph — it enforces the precondition that the graph is fresh.
//
// Panics if s or snap is nil (programmer error).
func NewFromSnapshot(s *schema.Schema, snap *Snapshot, opts ...Option) *Graph {
	if snap == nil {
		panic("graph.NewFromSnapshot: nil Snapshot")
	}
	g := New(s, opts...)
	g.importSnapshot(snap)
	return g
}

// importSnapshot populates a mutable Graph from a Snapshot's contents,
// bypassing the normal Add() pipeline. It does not perform duplicate
// detection, edge resolution, or composition extraction — it directly
// installs the snapshot's pre-resolved data into the graph's internal
// structures.
//
// After importSnapshot, the graph is ready for new Add() calls that
// resolve against the imported instances. New instances with the same
// (type, PK) as imported instances will be correctly flagged as duplicates.
//
// importSnapshot must be called on a fresh Graph (created via New)
// before any Add() calls.
func (g *Graph) importSnapshot(snap *Snapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Step 1: Clone and install instances.
	// Deep-clone all top-level instances from the snapshot and install them
	// into the graph's instance index. The cloneMap tracks snapshot instance
	// pointers → graph instance pointers for steps 2-4.
	cloneMap := make(map[*Instance]*Instance)

	for _, typeID := range snap.Types() {
		if g.instances[typeID] == nil {
			g.instances[typeID] = make(map[string]*Instance)
		}
		for _, inst := range snap.InstancesOf(typeID) {
			cloned := cloneInstance(inst, cloneMap)
			g.instances[typeID][cloned.PrimaryKey().String()] = cloned
		}
	}

	// Step 2: Install resolved edges.
	// Edges reference source and target via *Instance pointers. The cloneMap
	// resolves snapshot pointers to the corresponding graph instances.
	for _, edge := range snap.Edges() {
		srcClone := cloneMap[edge.Source()]
		tgtClone := cloneMap[edge.Target()]
		g.edges = append(g.edges, newEdge(edge.Relation(), srcClone, tgtClone, edge.Properties()))
	}

	// Step 3: Install unresolved edges as pending.
	// ALL reasons are reinstalled (target_missing, absent, empty) so they
	// survive the import → add → snapshot cycle without data loss.
	for _, unres := range snap.Unresolved() {
		srcClone := cloneMap[unres.Source]

		// Reverse the reason mapping from [Graph.Snapshot], which converts the
		// empty reasonDetail [Graph.Add] stores into "target_missing".
		reasonDetail := unres.Reason
		if reasonDetail == "target_missing" {
			reasonDetail = ""
		}

		// Not persisted in .ys, so rebuilt here. Resolved by identity, which
		// reaches the whole closure where the source's tag form does not.
		jsonField := ""
		if typ, ok := g.schema.TypeByID(unres.Source.TypeID()); ok {
			if rel, ok := typ.Relation(unres.Relation); ok {
				jsonField = rel.FieldName()
			}
		}

		pk := pendingKey{targetTypeID: unres.TargetType, targetKey: unres.TargetKey}
		g.pending[pk] = append(g.pending[pk], &pendingEdge{
			source:       srcClone,
			relation:     unres.Relation,
			jsonField:    jsonField,
			targetType:   unres.TargetType,
			targetKey:    unres.TargetKey,
			properties:   unres.Properties(),
			isRequired:   unres.Required,
			reasonDetail: reasonDetail,
		})
	}

	// Step 4: Install duplicates.
	// Duplicates are rejected instances — NOT in g.instances. They are
	// preserved in g.duplicates so Snapshot() includes them in output.
	for _, dup := range snap.Duplicates() {
		instClone := cloneMap[dup.Instance]
		if instClone == nil {
			// Duplicate's Instance was rejected and is not in the snapshot's
			// instance list. Clone it directly.
			instClone = cloneInstance(dup.Instance, cloneMap)
		}

		conflictClone := cloneMap[dup.Conflict]

		var parentClone *Instance
		if dup.Parent != nil {
			parentClone = cloneMap[dup.Parent]
			if parentClone == nil {
				parentClone = cloneInstance(dup.Parent, cloneMap)
			}
		}

		// Diagnostic is zero-value for loaded snapshots (HasDiagnostic() == false).
		g.duplicates = append(g.duplicates, newDuplicate(instClone, conflictClone, parentClone, dup.Relation, dup.Diagnostic))
	}

	// Step 5: Diagnostics — no-op.
	// Loaded snapshots have diag.OK() diagnostics. Construction diagnostics
	// are transient and not persisted.
}
