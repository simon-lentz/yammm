package graph

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/location"
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
// Property values and keys are rewritten into the representation their schema
// constraint stores, so a graph rebuilt from a document equals one built through
// validation and a snapshot round trip reaches a fixpoint on the first pass. A
// value the constraint cannot render is kept as it arrived. Every position that
// addresses an instance moves with its key, so an edge or a duplicate record
// spelled another way still resolves.
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
		// Indexed by the key the instance CARRIES, never the document's
		// spelling: every lookup below canonicalizes first, and the
		// snapshot's own accessors read this same index.
		keyStr := inst.PrimaryKey().String()
		instances[typeID] = append(instances[typeID], inst)
		idx[keyStr] = inst
	}

	// Step 2: Create Edge records, resolving references.
	edges := make([]Edge, 0, len(parts.Edges))
	for _, ep := range parts.Edges {
		// Canonicalized BEFORE the lookup: step 1 rewrote the instance keys the
		// index is built from, so an endpoint resolved from the document's
		// spelling would miss an instance that is present.
		ep = canon.edge(ep)
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
		// Both addresses move: the source with the instances step 1 rewrote,
		// the target under the relation's declared target type, as a validator
		// rendered it.
		up = canon.unresolved(up)
		source := lookupInstance(instanceIndex, up.SourceType, up.SourceKey.String())

		// Key.String() renders a keyless key as "[]", but UnresolvedEdge.TargetKey
		// is empty for the "absent" and "empty" reasons, which carry no target.
		targetKey := ""
		if up.TargetKey.Len() > 0 {
			targetKey = up.TargetKey.String()
		}

		unresolvedEdges = append(unresolvedEdges,
			newUnresolvedEdge(source, up.Relation, associationTarget(s, up.SourceType, up.Relation), targetKey,
				requiredUnder(s, up.SourceType, up.Relation), up.Reason, up.Properties))
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
// targetKey), comparing types by identity. A snapshot holds one edge per source,
// association and target, so no two of its edges tie.
func compareEdges(a, b Edge) int {
	if c := cmp.Compare(a.source.typeID.String(), b.source.typeID.String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.source.primaryKey.String(), b.source.primaryKey.String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.relation, b.relation); c != 0 {
		return c
	}
	if c := cmp.Compare(a.target.typeID.String(), b.target.typeID.String()); c != 0 {
		return c
	}
	return cmp.Compare(a.target.primaryKey.String(), b.target.primaryKey.String())
}

// compareProps orders two property sets by walking both in sorted key order.
//
// It compares field by field rather than rendering each set to one string:
// a "name=value;" rendering collides whenever a value holds the separators, so
// an ordinary String property containing ';' or '=' made two distinct sets
// compare equal and put map-iteration order back into the document. Values are
// discriminated by type before value, so an int64 1 and the string "1" do not
// tie.
//
// What this does not promise: two values of one type whose %v forms are equal
// still tie. For the scalar types a property can hold, %#v is injective.
// A composite renders by its content (see renderValue), so allocation never
// decides, and quoting keeps one string apart from two.
func compareProps(a, b immutable.Properties) int {
	// The key order is precomputed at construction, so collecting it costs one
	// allocation and no sort. An earlier rewrite walked two iter.Pull2
	// coroutines to save that allocation and paid a coroutine pair per call
	// instead, inside a comparator a sort runs O(n log n) times.
	an := slices.Collect(a.SortedKeys())
	bn := slices.Collect(b.SortedKeys())
	for i := range min(len(an), len(bn)) {
		if c := cmp.Compare(an[i], bn[i]); c != 0 {
			return c
		}
		av, _ := a.Get(an[i])
		bv, _ := b.Get(bn[i])
		if c := cmp.Compare(renderValue(av), renderValue(bv)); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(an), len(bn))
}

// renderValue renders one property value with its stored type and its content,
// so values of different types never compare equal. A Go type name holds no '|',
// so the split is unambiguous. The content is the value's clone written with
// %#v, which quotes strings and writes a nil container apart from an empty one; a
// container's own %v would print its address.
func renderValue(v immutable.Value) string {
	return fmt.Sprintf("%T|%#v", v.Unwrap(), v.Clone())
}

// compareUnresolved orders unresolved records by (sourceType, sourceKey,
// relation, targetType, targetKey). Graph.pending is a map, so the records
// arrive in map-iteration order, and only a total order keeps Marshal
// deterministic. It is total: a source holds one record per association and
// target, and its one "absent" or "empty" record there carries the empty key.
func compareUnresolved(a, b UnresolvedEdge) int {
	if c := cmp.Compare(a.source.TypeID().String(), b.source.TypeID().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.source.PrimaryKey().String(), b.source.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.relation, b.relation); c != 0 {
		return c
	}
	if c := cmp.Compare(a.targetType.String(), b.targetType.String()); c != 0 {
		return c
	}
	return cmp.Compare(a.targetKey, b.targetKey)
}

// compareDuplicates orders duplicate records. Two rejections of one key differ
// only in the instance they carry, so its properties and its provenance are the
// last discriminators.
func compareDuplicates(a, b Duplicate) int {
	if c := cmp.Compare(a.instance.TypeID().String(), b.instance.TypeID().String()); c != 0 {
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
func compareProvenance(a, b location.Provenance) int {
	if a.IsZero() != b.IsZero() {
		if a.IsZero() {
			return -1
		}
		return 1
	}
	as, bs := a.Span(), b.Span()
	return cmp.Or(
		cmp.Compare(a.SourceName(), b.SourceName()),
		cmp.Compare(writtenPath(a), writtenPath(b)),
		cmp.Compare(as.Source.String(), bs.Source.String()),
		comparePosition(as.Start, bs.Start),
		comparePosition(as.End, bs.End),
	)
}

func comparePosition(a, b location.Position) int {
	return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column), cmp.Compare(a.Byte, b.Byte))
}

// writtenPath returns the path a document holds for p, the one snapshot's marshal
// writes: a load keeps a raw path that its parsed path does not spell.
func writtenPath(p location.Provenance) string {
	if p.HasRawPath() {
		return p.RawPath()
	}
	return p.Path().String()
}
