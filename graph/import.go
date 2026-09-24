package graph

import (
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// NewFromSnapshot creates a Graph pre-populated from a Snapshot's contents,
// ready for [Graph.Add] calls that resolve against the imported instances.
//
// snap need not have been built against s, but it must hold to s under the
// package doc's "Structural facts". A snapshot that breaks one is refused with
// a nil Graph and a diagnostic naming each position, under the code
// [Graph.Add] refuses it with; nothing is installed. A snapshot bound to s
// already holds to them and is not walked.
//
// Panics if s or snap is nil (programmer error).
func NewFromSnapshot(s *schema.Schema, snap *Snapshot, opts ...Option) (*Graph, diag.Result) {
	if snap == nil {
		panic("graph.NewFromSnapshot: nil Snapshot")
	}
	g := New(s, opts...)
	if res := validateImport(g.schema, g.canon, snap); res.HasErrors() {
		return nil, res
	}
	g.importSnapshot(snap)
	return g, diag.OK()
}

// validateImport holds snap to s through [structureRules], the definition
// [RebuildSnapshot] applies, so an import cannot install what the rebuild path
// refuses. snap's types include every root's type, so its types entries carry
// the root rule. A record's reason and a duplicate record's own type and key
// need no schema, and every constructor of snap held them, so the walk skips them.
func validateImport(s *schema.Schema, canon *canonicalizer, snap *Snapshot) diag.Result {
	if snap.schema == s {
		return diag.OK()
	}
	c := diag.NewCollector(0)
	r := &structureRules{s: s, canon: canon, c: c, op: "NewFromSnapshot", callerFacing: true}

	for i, id := range snap.types {
		r.typesEntry(i, id)
	}
	for _, id := range snap.types {
		for _, inst := range snap.instances[id] {
			checkTree(r, "instance", inst)
			r.address(id, inst.primaryKey)
		}
	}
	for _, e := range snap.edges {
		r.association(associationRecord{
			position: "edge from", source: e.source.typeID, sourceKey: e.source.primaryKey, relation: e.relation,
			target: e.target.typeID, targetKey: e.target.primaryKey, properties: e.properties,
		})
	}
	for i, dup := range snap.duplicates {
		if dup.Parent == nil {
			r.rootDuplicate(i, dup.Instance.typeID)
		}
		checkTree(r, "duplicate instance", dup.Instance)
	}
	for _, u := range snap.unresolved {
		r.identity(u.TargetType, positionAt("unresolved target", u.TargetKey))
		r.association(associationRecord{
			position: "unresolved record of", source: u.Source.typeID, sourceKey: u.Source.primaryKey, relation: u.Relation,
			target: u.TargetType, targetKey: unresolvedTargetKey(u), reason: u.Reason, properties: u.properties,
		})
	}
	r.oneOverflow()
	return c.Result()
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
	name := tagForms(g.schema)

	imported := 0
	for _, typeID := range snap.Types() {
		if g.instances[typeID] == nil {
			g.instances[typeID] = make(map[string]*Instance)
		}
		for _, inst := range snap.InstancesOf(typeID) {
			cloned := cloneInstance(inst, cloneMap)
			g.rename(name, cloned)
			// Re-keyed under THIS graph's canonicalizer, not the one that wrote
			// the snapshot: a document persisted before a key's constraint
			// changed carries addresses the importing schema no longer spells
			// that way, and every lookup here canonicalizes.
			g.canon.reinstance(cloned)
			g.instances[typeID][cloned.PrimaryKey().String()] = cloned
			imported++
		}
	}

	// The loaded claim joins the Values accumulator only when instances
	// were imported: importing zero instances imports no unproven data.
	// Associations needs no graph state — step 3 reinstalls the pending
	// records the derivation in Snapshot reads.
	if imported > 0 {
		// A snapshot carrying no claim cannot support one: importing it makes
		// the graph's own Values claim false, the same as importing a false one.
		att := snap.Attestation()
		g.attestValues = g.attestValues && att != nil && att.Values
	}

	// Step 2: Install resolved edges.
	// Edges reference source and target via *Instance pointers. The cloneMap
	// resolves snapshot pointers to the corresponding graph instances.
	for _, edge := range snap.Edges() {
		srcClone := cloneMap[edge.Source()]
		tgtClone := cloneMap[edge.Target()]
		g.edges = append(g.edges, newEdge(edge.Relation(), srcClone, tgtClone,
			g.canon.edgeProperties(edge.Source().TypeID(), edge.Relation(), edge.Properties())))
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

		// Read from THIS graph's schema, as Add reads them: the import rule
		// holds the relation to an association of the source's type there.
		// Resolved by identity, which reaches the whole closure where the
		// source's tag form does not.
		jsonField, required := "", false
		if typ, ok := g.schema.TypeByID(unres.Source.TypeID()); ok {
			if rel, ok := typ.Relation(unres.Relation); ok {
				jsonField, required = rel.FieldName(), !rel.IsOptional()
			}
		}

		// The pending index is read by the address Add installs, so the target
		// key moves with the instances this same pass re-keyed.
		targetKey := g.canon.address(unres.TargetType, unres.TargetKey)
		pk := pendingKey{targetTypeID: unres.TargetType, targetKey: targetKey}
		g.pending[pk] = append(g.pending[pk], &pendingEdge{
			source:       srcClone,
			relation:     unres.Relation,
			jsonField:    jsonField,
			targetType:   unres.TargetType,
			targetKey:    targetKey,
			properties:   g.canon.edgeProperties(unres.Source.TypeID(), unres.Relation, unres.Properties()),
			isRequired:   required,
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
			g.rename(name, instClone)
			g.canon.reinstance(instClone)
		}

		conflictClone := cloneMap[dup.Conflict]

		var parentClone *Instance
		if dup.Parent != nil {
			parentClone = cloneMap[dup.Parent]
			if parentClone == nil {
				parentClone = cloneInstance(dup.Parent, cloneMap)
				g.rename(name, parentClone)
				g.canon.reinstance(parentClone)
			}
		}

		// Diagnostic is zero-value for loaded snapshots.
		g.duplicates = append(g.duplicates, newDuplicate(instClone, conflictClone, parentClone, dup.Relation, dup.Diagnostic))
	}

	// Step 5: Diagnostics — no-op.
	// Loaded snapshots have diag.OK() diagnostics. Construction diagnostics
	// are transient and not persisted.
}

// rename renders inst's name, and every composed child's, under this graph's
// schema. A snapshot's names were rendered under the schema it was built
// against, whose import aliases need not be this graph's.
func (g *Graph) rename(name func(schema.TypeID) string, inst *Instance) {
	inst.typeName = name(inst.typeID)
	for _, children := range inst.composed {
		for _, child := range children {
			g.rename(name, child)
		}
	}
}

// unresolvedTargetKey recovers the key an unresolved record stores as its
// rendering. "absent" and "empty" records store none, and a rendering that
// does not parse reads as no key, which the arity rule then refuses.
func unresolvedTargetKey(u *UnresolvedEdge) immutable.Key {
	if u.TargetKey == "" {
		return immutable.Key{}
	}
	components, err := ParseKey(u.TargetKey)
	if err != nil {
		return immutable.Key{}
	}
	return immutable.WrapKey(components)
}
