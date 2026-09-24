package graph

import (
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// NewFromSnapshot creates a Graph pre-populated from a Snapshot's contents,
// ready for [Graph.Add] calls that resolve against the imported instances.
//
// snap need not have been built against s, but it must hold to s under every
// fact of the package doc's "Structural facts" but Records, whose records the
// import derives again under s. A snapshot that breaks one is refused with
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
// the root rule. A record's reason needs no schema and every constructor of
// snap held it, so the walk judges none; importSnapshot derives the records again.
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
		checkTree(r, "duplicate instance", dup.instance)
		// A root duplicate's conflict is the root at its own type and key, which
		// every constructor of snap held, and that root's type carries the root
		// rule through the types entries. A composed duplicate's conflict depends
		// on its slot's multiplicity and keys, which s can change.
		if dup.relation == "" {
			continue
		}
		// An identity s cannot resolve is the identity rule's to report: a
		// parent's through its type's entry, the instance's through checkTree.
		if _, ok := s.TypeByID(dup.instance.typeID); !ok {
			continue
		}
		if _, ok := s.TypeByID(dup.parent.typeID); !ok {
			continue
		}
		if _, why := derivedConflict(s, canon, nil, dup.instance, dup.parent, dup.relation); why != "" {
			r.refuse(diag.E_GRAPH_INVALID_COMPOSITION, fmt.Sprintf("duplicate record %d, %s[%s], %s",
				i, dup.instance.typeID, dup.instance.primaryKey.String(), why))
		}
	}
	for _, u := range snap.unresolved {
		// An absent record holds no data, so it names no field s can lack:
		// importSnapshot drops it where s declares no such association.
		if u.reason == "absent" && associationTarget(s, u.source.typeID, u.relation).IsZero() {
			continue
		}
		rec := associationRecord{
			position: "unresolved record of", source: u.source.typeID, sourceKey: u.source.primaryKey, relation: u.relation,
			targetKey: unresolvedTargetKey(u), reason: u.reason, properties: u.properties,
		}
		// A target_missing record names a target, which holds to the declared
		// target as an edge's does.
		if u.reason == "target_missing" {
			rec.target = u.targetType
		}
		r.association(rec)
	}
	r.oneOverflow()
	return c.Result()
}

// importSnapshot populates a mutable Graph from a Snapshot's contents,
// bypassing the normal Add() pipeline. It does not perform duplicate
// detection or composition extraction — it directly installs the snapshot's
// pre-resolved data into the graph's internal structures. Only for a snapshot
// bound to another schema does it resolve an edge: deriveRecords turns a
// target_missing record whose target it installed into one.
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

	// Step 3: Install the unresolved records. A snapshot bound to this schema
	// holds Add's records already; one built under another schema holds the
	// records Add derived there, so they are derived again here.
	if snap.schema == g.schema {
		for _, unres := range snap.unresolved {
			g.installPending(cloneMap[unres.source], unres.relation, unres.targetKey, unres.reason, unres.properties)
		}
	} else {
		g.deriveRecords(snap, cloneMap)
	}

	// Step 4: Install duplicates.
	// Duplicates are rejected instances — NOT in g.instances. They are
	// preserved in g.duplicates so Snapshot() includes them in output.
	for _, dup := range snap.Duplicates() {
		instClone := cloneMap[dup.instance]
		if instClone == nil {
			// Duplicate's Instance was rejected and is not in the snapshot's
			// instance list. Clone it directly.
			instClone = cloneInstance(dup.instance, cloneMap)
			g.rename(name, instClone)
			g.canon.reinstance(instClone)
		}

		conflictClone := cloneMap[dup.conflict]

		var parentClone *Instance
		if dup.parent != nil {
			parentClone = cloneMap[dup.parent]
			if parentClone == nil {
				parentClone = cloneInstance(dup.parent, cloneMap)
				g.rename(name, parentClone)
				g.canon.reinstance(parentClone)
			}
		}

		// Diagnostic is zero-value for loaded snapshots.
		g.duplicates = append(g.duplicates, newDuplicate(instClone, conflictClone, parentClone, dup.relation, dup.diagnostic))
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
	if u.targetKey == "" {
		return immutable.Key{}
	}
	components, err := ParseKey(u.targetKey)
	if err != nil {
		return immutable.Key{}
	}
	return immutable.WrapKey(components)
}

// installPending installs one unresolved record of source under relation as
// [Graph.Add] stages it: the target key moves with the instances the import
// re-keyed, and whether the association is required is read from this schema.
func (g *Graph) installPending(source *Instance, relation, targetKey, reason string, props immutable.Properties) {
	typ, _ := g.schema.TypeByID(source.typeID)
	rel, _ := typ.Relation(relation)
	targetType := rel.TargetID()
	// Graph.Snapshot renders the empty reason Add stores as "target_missing".
	if reason == "target_missing" {
		reason = ""
		targetKey = g.canon.address(targetType, targetKey)
	}
	pk := pendingKey{targetTypeID: targetType, targetKey: targetKey}
	g.pending[pk] = append(g.pending[pk], &pendingEdge{
		source:       source,
		relation:     relation,
		jsonField:    rel.FieldName(),
		targetType:   targetType,
		targetKey:    targetKey,
		properties:   g.canon.edgeProperties(source.typeID, relation, props),
		isRequired:   !rel.IsOptional(),
		reasonDetail: reason,
	})
}

// deriveRecords installs the records [Graph.Add] would derive from snap's data
// under this schema. A target_missing record whose target this import
// installed resolves into an edge; an absent or empty record under an optional
// association, and an absent one under an association this schema does not
// declare, is dropped; and a root with no edge and no record under a
// required association gains an absent record, what Add derives when the data
// names no target, as the snapshot's data names none.
func (g *Graph) deriveRecords(snap *Snapshot, cloneMap map[*Instance]*Instance) {
	type slot struct {
		source   *Instance
		relation string
	}
	held := make(map[slot]bool, len(g.edges)+len(snap.unresolved))
	for _, e := range g.edges {
		held[slot{e.source, e.relation}] = true
	}
	for _, unres := range snap.unresolved {
		source := cloneMap[unres.source]
		typ, _ := g.schema.TypeByID(source.typeID)
		rel, ok := typ.Relation(unres.relation)
		// validateImport let through only an absent record here.
		if !ok || !rel.IsAssociation() {
			continue
		}
		if unres.reason != "target_missing" {
			if rel.IsOptional() {
				continue
			}
		} else if target := g.findInstance(rel.TargetID(), g.canon.address(rel.TargetID(), unres.targetKey)); target != nil {
			g.edges = append(g.edges, newEdge(unres.relation, source, target,
				g.canon.edgeProperties(source.typeID, unres.relation, unres.properties)))
			held[slot{source, unres.relation}] = true
			continue
		}
		g.installPending(source, unres.relation, unres.targetKey, unres.reason, unres.properties)
		held[slot{source, unres.relation}] = true
	}
	for _, id := range snap.types {
		typ, ok := g.schema.TypeByID(id)
		if !ok {
			continue
		}
		for _, inst := range snap.instances[id] {
			source := cloneMap[inst]
			for rel := range typ.AllAssociations() {
				if rel.IsOptional() || held[slot{source, rel.Name()}] {
					continue
				}
				g.installPending(source, rel.Name(), "", "absent", immutable.Properties{})
			}
		}
	}
}
