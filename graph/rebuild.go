package graph

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// SnapshotParts holds the pre-resolved data needed to construct a Snapshot
// without running the graph construction pipeline. All fields use value
// types; pointer-based cross-references (edge source/target) are resolved
// internally by [RebuildSnapshot] using the instance index.
// Every type reference below is a [schema.TypeID]. A name cannot carry a
// transitively imported type or separate two same-named types, so a
// name-keyed Instances map merges them before RebuildSnapshot sees them.
type SnapshotParts struct {
	Types      []schema.TypeID
	Instances  map[schema.TypeID][]InstanceParts
	Edges      []EdgeParts
	Duplicates []DuplicateParts
	Unresolved []UnresolvedParts
}

// InstanceParts holds the data for a single instance. Composed children
// are nested recursively.
//
// Composed children order is caller-determined and preserved as-is by
// RebuildSnapshot. For keyed children, callers must provide sorted order
// (lexicographic by key string). For keyless children, callers must provide
// insertion order. RebuildSnapshot does not re-sort composed children.
type InstanceParts struct {
	TypeName   string
	TypeID     schema.TypeID
	PrimaryKey immutable.Key
	Properties immutable.Properties
	Composed   map[string][]InstanceParts
	Provenance *location.Provenance
}

// EdgeParts holds the data for a single resolved edge. Source and target
// are identified by (type identity, primary key) rather than pointer;
// RebuildSnapshot resolves them to *Instance pointers via the instance index.
type EdgeParts struct {
	Relation   string
	SourceType schema.TypeID
	SourceKey  immutable.Key
	TargetType schema.TypeID
	TargetKey  immutable.Key
	Properties immutable.Properties
}

// DuplicateParts holds the data for a single duplicate record. Type and Key
// identify the rejected instance; ConflictType and ConflictKey address the
// instance it collided with. A root conflict resolves through the instance
// index; a composed conflict resolves through the parent slot named by
// ParentType, ParentKey and Relation — set only for a composed-child
// duplicate — where the stated key selects among siblings and an empty key
// addresses the slot's sole occupant.
type DuplicateParts struct {
	Type         schema.TypeID
	Key          immutable.Key
	Instance     InstanceParts
	ConflictType schema.TypeID
	ConflictKey  immutable.Key
	ParentType   schema.TypeID
	ParentKey    immutable.Key
	Relation     string
}

// UnresolvedParts holds the data for a single unresolved edge record.
//
// Properties carries the edge property values declared on the forward
// reference. Populated only when Reason is "target_missing"; empty
// otherwise. Loaded from the "properties" field on unresolved-edge
// entries, which every readable wire format carries.
//
// TargetType is an identity even though no instance of it is present — it is
// what a later [Graph.Add] resolves an arriving target against, so a name
// there would have to be re-resolved and could bind to the wrong type.
type UnresolvedParts struct {
	SourceType schema.TypeID
	SourceKey  immutable.Key
	Relation   string
	TargetType schema.TypeID
	TargetKey  immutable.Key
	Required   bool
	Reason     string
	Properties immutable.Properties
}

// RebuildSnapshot constructs a Snapshot from pre-resolved parts.
//
// This is the deserialization entry point for the snapshot package.
// It accepts fully-resolved data (instances, edges, diagnostics) and
// assembles them into an immutable Snapshot without re-running validation
// or edge resolution.
//
// Most users should construct snapshots via [Graph.Add] + [Graph.Snapshot].
// RebuildSnapshot exists for [snapshot.Load] and testing.
//
// RebuildSnapshot does not accept a context.Context. Its work is
// proportional to the already-decoded Parts data and completes in bounded
// time. Context cancellation is checked during the streaming decode phase
// that precedes RebuildSnapshot; see snapshot.Load.
//
// Returns a diag.Result with Fatal-severity E_INTERNAL diagnostics if
// internal consistency checks fail (e.g., edge references to missing
// instances, or a zero [schema.TypeID] at any parts position — identity is
// total at this boundary). snapshot.Load validates these invariants before
// calling RebuildSnapshot; failures here indicate a bug in the caller.
func RebuildSnapshot(s *schema.Schema, parts SnapshotParts) (*Snapshot, diag.Result) {
	collector := diag.NewCollector(0)

	validatePartsIdentity(parts, collector)
	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Step 1: Create Instance objects.
	instances := make(map[schema.TypeID][]*Instance, len(parts.Instances))
	instanceIndex := make(map[schema.TypeID]map[string]*Instance, len(parts.Instances))

	for typeID, instParts := range parts.Instances {
		insts := make([]*Instance, 0, len(instParts))
		idx := make(map[string]*Instance, len(instParts))

		for _, ip := range instParts {
			inst := rebuildInstance(ip)
			insts = append(insts, inst)
			idx[ip.PrimaryKey.String()] = inst
		}

		instances[typeID] = insts
		instanceIndex[typeID] = idx
	}

	// Step 2: Create Edge objects, resolving pointers.
	edges := make([]*Edge, 0, len(parts.Edges))
	for _, ep := range parts.Edges {
		source := lookupInstance(instanceIndex, ep.SourceType, ep.SourceKey.String())
		target := lookupInstance(instanceIndex, ep.TargetType, ep.TargetKey.String())

		if source == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: edge source %s[%s] not found in instance index",
					ep.SourceType, ep.SourceKey.String())).Build())
			continue
		}
		if target == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: edge target %s[%s] not found in instance index",
					ep.TargetType, ep.TargetKey.String())).Build())
			continue
		}

		edges = append(edges, newEdge(ep.Relation, source, target, ep.Properties))
	}

	// Sort edges for deterministic ordering.
	slices.SortFunc(edges, compareEdges)

	// Step 3: Create Duplicate records.
	duplicates := make([]*Duplicate, 0, len(parts.Duplicates))
	for _, dp := range parts.Duplicates {
		// Defense-in-depth: duplicate instances must not have composed children.
		if len(dp.Instance.Composed) > 0 {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: duplicate instance %s[%s] has composed children",
					dp.Type, dp.Key.String())).Build())
			continue
		}

		// Resolve the conflict pointer.
		conflict := resolveDuplicateConflict(instanceIndex, dp)
		if conflict == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: duplicate conflict %s not found",
					describeDuplicateConflict(dp))).Build())
			continue
		}

		var parent *Instance
		if dp.Relation != "" {
			parent = lookupInstance(instanceIndex, dp.ParentType, dp.ParentKey.String())
		}

		dupInst := rebuildInstance(dp.Instance)
		duplicates = append(duplicates, newDuplicate(dupInst, conflict, parent, dp.Relation, diag.Issue{}))
	}

	// Step 4: Create UnresolvedEdge records.
	unresolvedEdges := make([]*UnresolvedEdge, 0, len(parts.Unresolved))
	for _, up := range parts.Unresolved {
		source := lookupInstance(instanceIndex, up.SourceType, up.SourceKey.String())
		if source == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: unresolved source %s[%s] not found in instance index",
					up.SourceType, up.SourceKey.String())).Build())
			continue
		}

		// Key.String() renders a keyless key as "[]", but UnresolvedEdge.TargetKey
		// is empty for the "absent" and "empty" reasons, which carry no target.
		targetKey := ""
		if up.TargetKey.Len() > 0 {
			targetKey = up.TargetKey.String()
		}

		unresolvedEdges = append(unresolvedEdges,
			newUnresolvedEdge(source, up.Relation, up.TargetType, targetKey, up.Required, up.Reason, up.Properties))
	}

	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Step 5: Assemble the Snapshot. Types is sorted here rather than trusted
	// from the caller, because [Snapshot.Types] documents the ordering.
	types := slices.Clone(parts.Types)
	if types == nil {
		types = []schema.TypeID{}
	}
	slices.SortFunc(types, func(a, b schema.TypeID) int {
		return cmpString(a.String(), b.String())
	})

	snap := newSnapshot(s, types, instances, instanceIndex, edges, duplicates, unresolvedEdges, diag.OK())
	return snap, diag.OK()
}

// validatePartsIdentity rejects a zero [schema.TypeID] at every parts
// position. Zero means unresolved: accepting one would file data under an
// identity no schema type owns, so nothing downstream re-resolves a name.
func validatePartsIdentity(parts SnapshotParts, collector *diag.Collector) {
	zero := func(position, key string) {
		collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
			fmt.Sprintf("RebuildSnapshot: zero type identity at %s position, key %s", position, key)).Build())
	}

	for i, id := range parts.Types {
		if id.IsZero() {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: zero type identity at types entry %d", i)).Build())
		}
	}

	var checkInstance func(position string, ip InstanceParts)
	checkInstance = func(position string, ip InstanceParts) {
		if ip.TypeID.IsZero() {
			zero(position, ip.PrimaryKey.String())
		}
		for _, children := range ip.Composed {
			for _, child := range children {
				checkInstance("composed child", child)
			}
		}
	}

	for typeID, instParts := range parts.Instances {
		if typeID.IsZero() {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: zero type identity keys an instance group of %d instances", len(instParts))).Build())
		}
		for _, ip := range instParts {
			checkInstance("instance", ip)
		}
	}

	for _, ep := range parts.Edges {
		if ep.SourceType.IsZero() {
			zero("edge source", ep.SourceKey.String())
		}
		if ep.TargetType.IsZero() {
			zero("edge target", ep.TargetKey.String())
		}
	}

	for _, dp := range parts.Duplicates {
		if dp.Type.IsZero() {
			zero("duplicate", dp.Key.String())
		}
		if dp.ConflictType.IsZero() {
			zero("duplicate conflict", dp.ConflictKey.String())
		}
		if dp.Relation != "" && dp.ParentType.IsZero() {
			zero("duplicate parent", dp.ParentKey.String())
		}
		checkInstance("duplicate instance", dp.Instance)
	}

	for _, up := range parts.Unresolved {
		if up.SourceType.IsZero() {
			zero("unresolved source", up.SourceKey.String())
		}
		if up.TargetType.IsZero() {
			zero("unresolved target", up.TargetKey.String())
		}
	}
}

// rebuildInstance creates an Instance from InstanceParts, recursing into composed children.
func rebuildInstance(ip InstanceParts) *Instance {
	inst := newInstance(ip.TypeName, ip.TypeID, ip.PrimaryKey, ip.Properties, ip.Provenance)

	for relName, children := range ip.Composed {
		for _, childParts := range children {
			child := rebuildInstance(childParts)
			inst.addComposed(relName, child)
		}
	}

	return inst
}

// resolveDuplicateConflict finds the instance a duplicate collided with, at
// the stated conflict address. Composed children never enter the instance
// index, so a composed conflict resolves through the parent's relation slot;
// slot-alone addressing needs exactly one occupant.
func resolveDuplicateConflict(index map[schema.TypeID]map[string]*Instance, dp DuplicateParts) *Instance {
	if dp.Relation == "" {
		return lookupInstance(index, dp.ConflictType, dp.ConflictKey.String())
	}
	parent := lookupInstance(index, dp.ParentType, dp.ParentKey.String())
	if parent == nil {
		return nil
	}
	occupants := parent.Composed(dp.Relation)
	if dp.ConflictKey.Len() > 0 {
		keyStr := dp.ConflictKey.String()
		for _, child := range occupants {
			if child.TypeID() == dp.ConflictType && child.PrimaryKey().String() == keyStr {
				return child
			}
		}
		return nil
	}
	if len(occupants) == 1 && occupants[0].TypeID() == dp.ConflictType {
		return occupants[0]
	}
	return nil
}

// describeDuplicateConflict renders a duplicate's stated conflict address
// for a diagnostic message.
func describeDuplicateConflict(dp DuplicateParts) string {
	if dp.Relation == "" {
		return fmt.Sprintf("%s[%s] in the instance index", dp.ConflictType, dp.ConflictKey.String())
	}
	return fmt.Sprintf("%s[%s] under %s[%s].%s",
		dp.ConflictType, dp.ConflictKey.String(), dp.ParentType, dp.ParentKey.String(), dp.Relation)
}

// lookupInstance finds an instance in the index by type identity and key string.
func lookupInstance(index map[schema.TypeID]map[string]*Instance, id schema.TypeID, keyStr string) *Instance {
	typeIdx := index[id]
	if typeIdx == nil {
		return nil
	}
	return typeIdx[keyStr]
}

// compareEdges provides the sorting comparator for edges.
// Sorts by (sourceType, sourceKey, relation, targetType, targetKey), comparing
// types by identity so two types rendering one name do not interleave.
func compareEdges(a, b *Edge) int {
	if c := cmpString(a.source.typeID.String(), b.source.typeID.String()); c != 0 {
		return c
	}
	if c := cmpString(a.source.primaryKey.String(), b.source.primaryKey.String()); c != 0 {
		return c
	}
	if c := cmpString(a.relation, b.relation); c != 0 {
		return c
	}
	if c := cmpString(a.target.typeID.String(), b.target.typeID.String()); c != 0 {
		return c
	}
	if c := cmpString(a.target.primaryKey.String(), b.target.primaryKey.String()); c != 0 {
		return c
	}
	return compareProps(a.properties, b.properties)
}

// compareProps orders two property sets by walking both in sorted key order.
//
// It compares field by field rather than rendering each set to one string:
// a "name=value;" rendering collides whenever a value holds the separators, so
// an ordinary String edge property containing ';' or '=' made two distinct sets
// compare equal and put map-iteration order back into the document. Values are
// discriminated by type before value, so an int64 1 and the string "1" do not
// tie.
//
// What this does not promise: two values of one type whose %v forms are equal
// still tie. For the scalar types an edge property can hold, %v is injective;
// for a composite it may not be, and that residue is stated rather than
// claimed away.
func compareProps(a, b immutable.Properties) int {
	an := slices.Sorted(a.Keys())
	bn := slices.Sorted(b.Keys())
	for i := range min(len(an), len(bn)) {
		if c := cmpString(an[i], bn[i]); c != 0 {
			return c
		}
		av, _ := a.Get(an[i])
		bv, _ := b.Get(bn[i])
		if c := cmpString(renderValue(av), renderValue(bv)); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(an), len(bn))
}

// renderValue renders one property value with its type, so values of different
// types never compare equal. A Go type name holds no '|', so the split is
// unambiguous.
func renderValue(v immutable.Value) string {
	u := v.Unwrap()
	return fmt.Sprintf("%T|%v", u, u)
}

func cmpString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

// compareUnresolved orders unresolved records. Graph.pending is a map,
// so the slice these are collected into arrives in map-iteration order; an
// ordering that leaves any two distinct records equal hands their positions to
// that order, and Marshal's documented determinism fails across process runs.
// Reproduced before this comparator was completed: sixty tied pairs spread
// across sixty pending buckets produced ten distinct documents in ten runs.
func compareUnresolved(a, b *UnresolvedEdge) int {
	if c := cmpString(a.Source.TypeID().String(), b.Source.TypeID().String()); c != 0 {
		return c
	}
	if c := cmpString(a.Source.PrimaryKey().String(), b.Source.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := cmpString(a.Relation, b.Relation); c != 0 {
		return c
	}
	if c := cmpString(a.TargetType.String(), b.TargetType.String()); c != 0 {
		return c
	}
	if c := cmpString(a.TargetKey, b.TargetKey); c != 0 {
		return c
	}
	if c := cmpString(a.Reason, b.Reason); c != 0 {
		return c
	}
	if a.Required != b.Required {
		if a.Required {
			return 1
		}
		return -1
	}
	return compareProps(a.properties, b.properties)
}

// compareDuplicates orders duplicate records. Two rejections of one
// key against one conflict differ only in the instance they carry, so the
// rejected instance's properties are the last discriminator.
func compareDuplicates(a, b *Duplicate) int {
	if c := cmpString(a.Instance.TypeID().String(), b.Instance.TypeID().String()); c != 0 {
		return c
	}
	if c := cmpString(a.Instance.PrimaryKey().String(), b.Instance.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := cmpString(a.Relation, b.Relation); c != 0 {
		return c
	}
	if c := cmpString(a.Conflict.TypeID().String(), b.Conflict.TypeID().String()); c != 0 {
		return c
	}
	if c := cmpString(a.Conflict.PrimaryKey().String(), b.Conflict.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := cmpString(parentKeyOf(a), parentKeyOf(b)); c != 0 {
		return c
	}
	return compareProps(a.Instance.Properties(), b.Instance.Properties())
}

// parentKeyOf renders a duplicate's parent slot, or the empty string for a root
// duplicate. The wire carries the parent coordinates, so two composed
// duplicates rejected from different slots are different records and an
// ordering that ignores the parent leaves their positions to the input order.
func parentKeyOf(d *Duplicate) string {
	if d.Parent == nil {
		return ""
	}
	return d.Parent.TypeID().String() + "\x00" + d.Parent.PrimaryKey().String()
}
