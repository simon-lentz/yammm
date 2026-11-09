package graph

import (
	"encoding/json"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// NewFromSnapshot creates a Graph pre-populated from a Snapshot's contents,
// ready for [Graph.Add] calls that resolve against the imported instances.
//
// snap need not have been built against s. The import derives its records again
// under s, refusing an "empty" record under a (one) association, and refuses one
// that breaks another fact of the package doc's "Structural facts" under s, save
// an "absent" record under a relation that is no association of s, which it drops. It stores a number at a Float of s as the
// validator does, as its nearest float64. A refusal returns a nil Graph and one
// diagnostic for each break, naming a position, and nothing is installed. A break
// that reads a unit another diagnostic refused can draw none. A snapshot bound to s already holds to the facts
// and is not walked.
//
// Panics if s or snap is zero (programmer error).
func NewFromSnapshot(s schema.Schema, snap Snapshot) (*Graph, diag.Result) {
	if s.IsZero() {
		panic("graph.NewFromSnapshot: zero Schema")
	}
	if snap.IsZero() {
		panic("graph.NewFromSnapshot: zero Snapshot")
	}
	g := New(s)
	if res := validateImport(g.schema, g.canon, snap); res.HasErrors() {
		return nil, res
	}
	g.importSnapshot(snap)
	return g, diag.OK()
}

// validateImport holds snap to s through [structureRules]. snap's types include
// every root's type, so its types entries carry the root rule. A record's reason
// needs no schema and every constructor of snap held it, so the walk judges one
// only where s makes it a record [Graph.Add] never makes, an "empty" one under a
// (one) association; importSnapshot derives the records again.
func validateImport(s schema.Schema, canon *canonicalizer, snap Snapshot) diag.Result {
	if snap.schema == s {
		return diag.OK()
	}
	c := diag.NewCollector(0)
	r := &structureRules{s: s, canon: canon, c: c}

	for i, id := range snap.types {
		r.typesEntry(i, id)
	}
	for _, id := range snap.types {
		for _, inst := range snap.instances[id] {
			checkRoot(r, inst)
		}
	}
	for _, e := range snap.edges {
		rec := associationRecord{
			position: "edge from", source: e.source.typeID, sourceKey: e.source.primaryKey, relation: e.relation,
			target: e.target.typeID, targetKey: e.target.primaryKey, properties: e.properties,
		}
		if !r.association(rec) {
			r.countRecord(rec)
		}
	}
	// A duplicate's conflict is the root at its own type and key, which every
	// constructor of snap held, and that root's type carries the root rule
	// through the types entries.
	for _, dup := range snap.duplicates {
		checkTree(r, "duplicate instance", dup.instance)
	}
	for _, u := range snap.unresolved {
		// An absent record holds no data, so it names no field s can lack:
		// importSnapshot drops it where s declares no such association.
		if u.reason == "absent" && associationTarget(s, u.source.typeID, u.relation).IsZero() {
			continue
		}
		rec := associationRecord{
			position: "unresolved record of", unresolved: true, source: u.source.typeID, sourceKey: u.source.primaryKey, relation: u.relation,
			targetKey: unresolvedTargetKey(u), reason: u.reason, properties: u.properties,
		}
		// A target_missing record names a target, which holds to the declared
		// target as an edge's does. The record is the target identity's own
		// position, so the identity rule judges it here.
		if u.reason == "target_missing" {
			rec.target = u.targetType
			r.identity(rec.target, positionAt("unresolved target", rec.targetKey.String()))
		}
		if !r.association(rec) {
			r.countRecord(rec)
		}
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
func (g *Graph) importSnapshot(snap Snapshot) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Step 1: Clone and install instances.
	// Deep-clone all top-level instances from the snapshot and install them
	// into the graph's instance index. The cloneMap tracks snapshot instances
	// → graph instances for steps 2-4.
	cloneMap := make(map[Instance]Instance)
	name := tagForms(g.schema)
	var floats *floatPositions
	if snap.schema != g.schema {
		floats = newFloatPositions(g.schema)
	}

	imported := 0
	for _, typeID := range snap.Types() {
		if g.instances[typeID] == nil {
			g.instances[typeID] = make(map[string]Instance)
		}
		for inst := range snap.InstancesOf(typeID) {
			cloned := cloneInstance(inst, cloneMap)
			g.rename(name, cloned)
			// Re-keyed under THIS graph's canonicalizer, not the one that wrote
			// the snapshot: a document persisted before a key's constraint
			// changed carries addresses the importing schema no longer spells
			// that way, and every lookup here canonicalizes.
			g.canon.reinstance(cloned)
			floats.instance(cloned)
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
	// Edges reference source and target via Instance records. The cloneMap
	// resolves snapshot records to the corresponding graph instances.
	for edge := range snap.Edges() {
		srcClone := cloneMap[edge.Source()]
		tgtClone := cloneMap[edge.Target()]
		props := floats.edge(edge.Source().TypeID(), edge.Relation(), edge.Properties())
		g.installEdge(newEdge(edge.Relation(), srcClone, tgtClone,
			g.canon.edgeProperties(edge.Source().TypeID(), edge.Relation(), props)))
	}

	// Step 3: Install the unresolved records. A snapshot bound to this schema
	// holds Add's records already; one built under another schema holds the
	// records Add derived there, so they are derived again here.
	if snap.schema == g.schema {
		for _, unres := range snap.unresolved {
			g.installPending(cloneMap[unres.source], unres.relation, unres.targetKey, unres.reason, unres.properties)
		}
	} else {
		g.deriveRecords(snap, cloneMap, floats)
	}

	// Step 4: Install duplicates.
	// Duplicates are rejected instances — NOT in g.instances. They are
	// preserved in g.duplicates so Snapshot() includes them in output.
	for dup := range snap.Duplicates() {
		// Duplicate's Instance was rejected and is not in the snapshot's
		// instance list. Clone it directly.
		instClone := cloneInstance(dup.instance, cloneMap)
		g.rename(name, instClone)
		g.canon.reinstance(instClone)
		floats.instance(instClone)

		// The conflict is a root, which step 1 cloned.
		g.duplicates = append(g.duplicates, newDuplicate(instClone, cloneMap[dup.conflict]))
	}
}

// rename renders inst's name, and every composed child's, under this graph's
// schema. A snapshot's names were rendered under the schema it was built
// against, whose import aliases need not be this graph's.
func (g *Graph) rename(name func(schema.TypeID) string, inst Instance) {
	inst.typeName = name(inst.typeID)
	for _, children := range inst.composed {
		for _, child := range children {
			g.rename(name, child)
		}
	}
}

// unresolvedTargetKey recovers the key an unresolved record stores as its
// rendering. A rendering that does not parse, as the empty one of an "absent" or
// "empty" record, reads as no key, which the arity rule then refuses where a
// record names a target.
func unresolvedTargetKey(u UnresolvedEdge) immutable.Key {
	components, err := ParseKey(u.targetKey)
	if err != nil {
		return immutable.Key{}
	}
	return immutable.WrapKey(components)
}

// installPending installs one unresolved record of source under relation as
// [Graph.Add] stages it: the target key moves with the instances the import
// re-keyed, and whether the association is required is read from this schema.
func (g *Graph) installPending(source Instance, relation, targetKey, reason string, props immutable.Properties) {
	typ, _ := g.schema.TypeByID(source.typeID)
	rel, _ := typ.Relation(relation)
	targetType := rel.TargetID()
	// Graph.Snapshot renders the empty reason Add stores as "target_missing".
	if reason == "target_missing" {
		reason = ""
		targetKey = g.canon.address(targetType, targetKey)
	}
	g.installPendingEdge(&pendingEdge{
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
// installed resolves into an edge; an absent record under an optional
// association or one this schema does not declare, and an empty one under an
// optional (many) association, is dropped; and a root with no edge and no record under a
// required association gains an absent record, what Add derives when the data
// names no target, as the snapshot's data names none.
func (g *Graph) deriveRecords(snap Snapshot, cloneMap map[Instance]Instance, floats *floatPositions) {
	type slot struct {
		source   Instance
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
		props := floats.edge(source.typeID, unres.relation, unres.properties)
		if unres.reason != "target_missing" {
			if rel.IsOptional() {
				continue
			}
		} else if target := g.findInstance(rel.TargetID(), g.canon.address(rel.TargetID(), unres.targetKey)); !target.IsZero() {
			g.installEdge(newEdge(unres.relation, source, target,
				g.canon.edgeProperties(source.typeID, unres.relation, props)))
			held[slot{source, unres.relation}] = true
			continue
		}
		g.installPending(source, unres.relation, unres.targetKey, unres.reason, props)
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

// floatPositions holds, for each type and relation of a schema, the declared
// properties at which a number is a Float ([value.ReadsAsFloat]). An import
// under another schema reads each through it, so it stores what the validator
// stores: the snapshot's schema can declare an Integer where this one declares a
// Float. A nil table reads nothing, as a snapshot bound to the importing schema
// already holds a float64 at every Float.
type floatPositions struct {
	byType map[schema.TypeID][]schema.Property
	byEdge map[edgeKey][]schema.Property
}

func newFloatPositions(s schema.Schema) *floatPositions {
	f := &floatPositions{
		byType: make(map[schema.TypeID][]schema.Property),
		byEdge: make(map[edgeKey][]schema.Property),
	}
	floatProps := func(ps func(func(schema.Property) bool)) []schema.Property {
		var out []schema.Property
		for p := range ps {
			if value.ReadsAsFloat(p.Constraint()) {
				out = append(out, p)
			}
		}
		return out
	}
	for sc := range s.Closure() {
		for t := range sc.Types() {
			f.byType[t.ID()] = floatProps(t.AllProperties())
			for _, rels := range []iterRelations{t.AllAssociations(), t.AllCompositions()} {
				for rel := range rels {
					f.byEdge[edgeKey{source: t.ID(), relation: rel.Name()}] = floatProps(rel.Properties())
				}
			}
		}
	}
	return f
}

// instance reads inst's properties, and each composed child's, in place. inst
// is a fresh clone no graph reaches yet.
func (f *floatPositions) instance(inst Instance) {
	if f == nil {
		return
	}
	inst.properties = readFloats(inst.properties, f.byType[inst.typeID])
	for _, children := range inst.composed {
		for _, child := range children {
			f.instance(child)
		}
	}
}

// edge reads the properties of a record under the relation source declares.
func (f *floatPositions) edge(source schema.TypeID, relation string, props immutable.Properties) immutable.Properties {
	if f == nil {
		return props
	}
	return readFloats(props, f.byEdge[edgeKey{source: source, relation: relation}])
}

// readFloats returns props with each property ps names read by [value.ReadAt]
// at its constraint, in a copy; props is returned as it is when no property ps
// names holds a number ReadAt can rewrite.
func readFloats(props immutable.Properties, ps []schema.Property) immutable.Properties {
	if len(ps) == 0 || props.Len() == 0 {
		return props
	}
	var m map[string]any
	for _, p := range ps {
		v, ok := props.Get(p.Name())
		if !ok || !mayReadFloat(v, p.Constraint()) {
			continue
		}
		if m == nil {
			m = props.Clone()
		}
		m[p.Name()] = value.ReadAt(m[p.Name()], p.Constraint())
	}
	if m == nil {
		return props
	}
	return immutable.WrapProperties(m)
}

// floatElement is the constraint of a Vector's elements.
var floatElement schema.Constraint = schema.NewFloatConstraint()

// mayReadFloat reports whether v holds an int64 or a json.Number at a position
// c reads as a Float, the only numbers [value.ReadAt] rewrites in a snapshot:
// the reader and the validator have read every other number as ReadAt would.
func mayReadFloat(v immutable.Value, c schema.Constraint) bool {
	rc := schema.ResolveAlias(c)
	switch x := v.Unwrap().(type) {
	case int64, json.Number:
		_, ok := rc.(schema.FloatConstraint)
		return ok
	case immutable.Slice:
		var elem schema.Constraint
		switch rc := rc.(type) {
		case schema.VectorConstraint:
			elem = floatElement
		case schema.ListConstraint:
			elem = rc.Element()
		default:
			return false
		}
		for e := range x.Iter() {
			if mayReadFloat(e, elem) {
				return true
			}
		}
	}
	return false
}
