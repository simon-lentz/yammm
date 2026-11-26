package graph

import (
	"cmp"
	"slices"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/typeorder"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

func init() {
	snapparts.Hook = func(s schema.Schema, parts snapparts.Parts) any {
		return rebuildSnapshot(s, parts)
	}
}

// rebuildSnapshot constructs a Snapshot from the parts of a document the
// snapshot package's reader accepted. The reader holds a document to the package
// doc's "Structural facts" first, so every reference below resolves and this
// constructor refuses nothing. It judges no property value's constraint.
//
// Property values are rewritten into the representation their schema
// constraint stores, so a graph rebuilt from a document equals one built through
// validation and a snapshot round trip reaches a fixpoint on the first pass. A
// value the constraint cannot render is kept as it arrived. Keys are taken as
// written: the reader refuses a key component its kind stores as other text, so
// every address is already the one a validator stores.
func rebuildSnapshot(s schema.Schema, parts snapparts.Parts) Snapshot {
	canon := newCanonicalizer(s)

	// Step 1: Create Instance objects, each filed under its own identity.
	perType := make(map[schema.TypeID]int)
	for _, ip := range parts.Instances {
		perType[ip.TypeID]++
	}
	instances := make(map[schema.TypeID][]Instance, len(perType))
	instanceIndex := make(map[schema.TypeID]map[string]Instance, len(perType))
	types := slices.Clone(parts.Types)
	for typeID, n := range perType {
		instances[typeID] = make([]Instance, 0, n)
		instanceIndex[typeID] = make(map[string]Instance, n)
		types = append(types, typeID)
	}
	name := tagForms(s)

	for _, ip := range parts.Instances {
		typeID := ip.TypeID
		idx := instanceIndex[typeID]
		inst := rebuildInstance(name, canon.instance(ip))
		keyStr := inst.PrimaryKey().String()
		instances[typeID] = append(instances[typeID], inst)
		idx[keyStr] = inst
	}

	// Step 2: Create Edge records, resolving references.
	edges := make([]Edge, 0, len(parts.Edges))
	for _, ep := range parts.Edges {
		ep.Properties = canon.edgeProperties(ep.SourceType, ep.Relation, ep.Properties)
		source := lookupInstance(instanceIndex, ep.SourceType, ep.SourceKey.String())
		target := lookupInstance(instanceIndex, associationTarget(s, ep.SourceType, ep.Relation), ep.TargetKey.String())
		edges = append(edges, newEdge(ep.Relation, source, target, ep.Properties))
	}

	// Step 3: Create Duplicate records, each conflicting with the root at its own
	// type and key, as [Graph.Add] records it.
	duplicates := make([]Duplicate, 0, len(parts.Duplicates))
	for _, ip := range parts.Duplicates {
		dupInst := rebuildInstance(name, canon.instance(ip))
		conflict := lookupInstance(instanceIndex, dupInst.typeID, dupInst.primaryKey.String())
		duplicates = append(duplicates, newDuplicate(dupInst, conflict))
	}

	// Step 4: Create UnresolvedEdge records.
	unresolvedEdges := make([]UnresolvedEdge, 0, len(parts.Unresolved))
	for _, up := range parts.Unresolved {
		up.Properties = canon.edgeProperties(up.SourceType, up.Relation, up.Properties)
		source := lookupInstance(instanceIndex, up.SourceType, up.SourceKey.String())

		// Key.String() renders a keyless key as "[]", but UnresolvedEdge.TargetKey
		// is empty for the "absent" and "empty" reasons, which carry no target.
		targetKey := ""
		if up.TargetKey.Len() > 0 {
			targetKey = up.TargetKey.String()
		}

		unresolvedEdges = append(unresolvedEdges,
			newUnresolvedEdge(source, up.Relation, associationTarget(s, up.SourceType, up.Relation), targetKey,
				requiredUnder(s, up.SourceType, up.Relation), unresolvedReason(up.Reason), up.Properties))
	}

	// Step 5: Assemble the Snapshot. newSnapshot establishes every ordering
	// and drops the repeats step 1 appended.
	var claim *Attestation
	if a := parts.Attestation; a != nil {
		claim = &Attestation{Values: a.Values, Associations: a.Associations}
	}
	return newSnapshot(s, canon, types, instances, instanceIndex, edges, duplicates, unresolvedEdges, claim)
}

// rebuildInstance creates an Instance from its parts, recursing into composed
// children. Rebuilt instances never report Validated: the wire carries the
// header attestation as a claim, not a per-instance proof.
func rebuildInstance(name func(schema.TypeID) string, ip snapparts.Instance) Instance {
	inst := newInstance(name(ip.TypeID), ip.TypeID, ip.PrimaryKey, ip.Properties, ip.Provenance, false)

	for relName, children := range ip.Composed {
		for _, childParts := range children {
			child := rebuildInstance(name, childParts)
			inst.addComposed(relName, child)
		}
	}

	return inst
}

// tagForms returns [schema.TagForm] under s, rendered once per identity: a
// snapshot holds few types and many instances.
func tagForms(s schema.Schema) func(schema.TypeID) string {
	names := make(map[schema.TypeID]string)
	return func(id schema.TypeID) string {
		n, ok := names[id]
		if !ok {
			n = schema.TagForm(s, id)
			names[id] = n
		}
		return n
	}
}

// lookupInstance finds an instance in the index by type identity and key string.
func lookupInstance(index map[schema.TypeID]map[string]Instance, id schema.TypeID, keyStr string) Instance {
	typeIdx := index[id]
	if typeIdx == nil {
		return Instance{}
	}
	return typeIdx[keyStr]
}

// compareEdges orders edges by (sourceType, sourceKey, relation, targetType,
// targetKey), types as o orders them. A snapshot holds one edge per source,
// association and target, so no two of its edges tie.
func compareEdges(o typeorder.Order, a, b Edge) int {
	if c := o.Compare(a.source.typeID, b.source.typeID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.source.primaryKey.String(), b.source.primaryKey.String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.relation, b.relation); c != 0 {
		return c
	}
	if c := o.Compare(a.target.typeID, b.target.typeID); c != 0 {
		return c
	}
	return cmp.Compare(a.target.primaryKey.String(), b.target.primaryKey.String())
}

// compareProps orders two property sets as [value.OrderStored] ranks two maps:
// name by name in name order, each name before its value, so a value holding a
// separator never collides with another set. Two sets tie only when a snapshot
// writes them the same, so a set orders the same after a Marshal and an
// Unmarshal. A graph holds only values a validator builds or a reader decodes,
// which OrderStored ranks, so an error is a defect.
func compareProps(a, b immutable.Properties) int {
	c, err := value.OrderStored(a, b)
	if err != nil {
		panic("graph: a property value the total order cannot rank: " + err.Error())
	}
	return c
}

// compareUnresolved orders unresolved records by (sourceType, sourceKey,
// relation, targetType, targetKey), types as o orders them. Graph.pending is a
// map, so the records arrive in map-iteration order, and only a total order
// keeps Marshal deterministic. It is total: a source holds one record per
// association and target, and its one "absent" or "empty" record there carries
// the empty key.
func compareUnresolved(o typeorder.Order, a, b UnresolvedEdge) int {
	if c := o.Compare(a.source.TypeID(), b.source.TypeID()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.source.PrimaryKey().String(), b.source.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.relation, b.relation); c != 0 {
		return c
	}
	if c := o.Compare(a.targetType, b.targetType); c != 0 {
		return c
	}
	return cmp.Compare(a.targetKey, b.targetKey)
}

// compareDuplicates orders duplicate records by type, as o orders types, then
// key. Two rejections of one key differ only in the instance they carry, so its
// properties and its provenance are the last discriminators.
func compareDuplicates(o typeorder.Order, a, b Duplicate) int {
	if c := o.Compare(a.instance.TypeID(), b.instance.TypeID()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.instance.PrimaryKey().String(), b.instance.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := compareProps(a.instance.Properties(), b.instance.Properties()); c != 0 {
		return c
	}
	return compareProvenance(a.instance.Provenance(), b.instance.Provenance())
}

// compareProvenance orders by what a document writes first, the source name and
// the path, so a load derives the order Marshal wrote: a load's spans are zero. The
// span then separates two records no document can tell apart. No provenance sorts first.
func compareProvenance(a, b instance.Provenance) int {
	if a.IsZero() != b.IsZero() {
		if a.IsZero() {
			return -1
		}
		return 1
	}
	return cmp.Or(
		cmp.Compare(a.SourceName(), b.SourceName()),
		cmp.Compare(a.Path(), b.Path()),
		a.Span().Compare(b.Span()),
	)
}

// associationTarget returns the declared target of relation, an association of
// source under s. The reader refuses a record under any other name first.
func associationTarget(s schema.Schema, source schema.TypeID, relation string) schema.TypeID {
	t, _ := s.TypeByID(source)
	rel, _ := t.Relation(relation)
	return rel.TargetID()
}

// requiredUnder reports whether relation, an association of source under s, is
// required, as [Graph.Add] derives an unresolved record's Required.
func requiredUnder(s schema.Schema, source schema.TypeID, relation string) bool {
	t, _ := s.TypeByID(source)
	rel, _ := t.Relation(relation)
	return !rel.IsOptional()
}
