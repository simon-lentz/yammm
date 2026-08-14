package graph

import (
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// closureTypeIDs indexes every type in s's closure by bare name, in closure
// order — s first, then each import in declaration order. A name declared more
// than once maps to every declaration, so a caller can tell the ambiguous case
// from the unique one instead of silently taking the first.
func closureTypeIDs(s *schema.Schema) map[string][]schema.TypeID {
	index := make(map[string][]schema.TypeID)
	for _, cs := range s.Closure() {
		for _, t := range cs.TypesSlice() {
			index[t.Name()] = append(index[t.Name()], t.ID())
		}
	}
	return index
}

// bindImportedTarget resolves an unresolved edge's target tag that the entry
// schema cannot name, which is how a v2 document refers to a transitively
// imported type. The binding is reported whenever the tag is ambiguous, and a
// tag naming nothing drops the record with a warning rather than in silence.
func (g *Graph) bindImportedTarget(unres *UnresolvedEdge, closure map[string][]schema.TypeID) (schema.TypeID, bool) {
	candidates := closure[unres.TargetType]
	if len(candidates) == 0 {
		g.collector.Collect(diag.NewIssue(diag.Warning, diag.E_GRAPH_TYPE_NOT_FOUND,
			fmt.Sprintf("unresolved edge %q from %s references target type %q, which no schema in the import closure declares; the record is dropped",
				unres.Relation, unres.Source.TypeName(), unres.TargetType)).
			WithDetail(diag.DetailKeyTypeName, unres.Source.TypeName()).
			WithDetail(diag.DetailKeyRelationName, unres.Relation).
			WithDetail(diag.DetailKeyTargetType, unres.TargetType).
			Build())
		return schema.TypeID{}, false
	}
	if len(candidates) > 1 {
		g.collector.Collect(diag.NewIssue(diag.Warning, diag.W_GRAPH_AMBIGUOUS_TYPE,
			fmt.Sprintf("unresolved edge %q from %s names target type %q, which %d schemas in the import closure declare; bound to the one in %q",
				unres.Relation, unres.Source.TypeName(), unres.TargetType, len(candidates), candidates[0].SchemaPath().String())).
			WithDetail(diag.DetailKeyTypeName, unres.Source.TypeName()).
			WithDetail(diag.DetailKeyRelationName, unres.Relation).
			WithDetail(diag.DetailKeyTargetType, unres.TargetType).
			Build())
	}
	return candidates[0], true
}

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

	// Filed by carried identity, not by tag: a tag cannot name a transitively
	// imported type, and one tag group's members need not share a type.
	for _, typeName := range snap.Types() {
		for _, inst := range snap.InstancesOf(typeName) {
			typeID := inst.TypeID()
			if typeID.IsZero() {
				var ok bool
				if typeID, ok = g.resolveTypeName(typeName); !ok {
					continue
				}
			}
			if g.instances[typeID] == nil {
				g.instances[typeID] = make(map[string]*Instance)
			}
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
	var closure map[string][]schema.TypeID
	for _, unres := range snap.Unresolved() {
		srcClone := cloneMap[unres.Source]

		targetTypeID, ok := g.resolveTypeName(unres.TargetType)
		if !ok {
			if closure == nil {
				closure = closureTypeIDs(g.schema)
			}
			targetTypeID, ok = g.bindImportedTarget(unres, closure)
			if !ok {
				continue
			}
		}

		// Reverse the reason mapping from Snapshot(). Graph.Add() stores
		// forward references with reasonDetail="" (empty string). Snapshot()
		// converts "" to "target_missing" at graph.go:821-824. Restoring
		// the original form ensures pending edge resolution in Add() works
		// unchanged.
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

		pk := pendingKey{targetTypeID: targetTypeID, targetKey: unres.TargetKey}
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

		// Diagnostic is zero-value for loaded snapshots (HasDiagnostic() == false).
		g.duplicates = append(g.duplicates, newDuplicate(instClone, conflictClone, dup.Diagnostic))
	}

	// Step 5: Diagnostics — no-op.
	// Loaded snapshots have diag.OK() diagnostics. Construction diagnostics
	// are transient and not persisted.
}
