package graph

import (
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

// DuplicateParts holds the data for a single duplicate record.
//
// Type and Key identify the type identity and primary key that was duplicated,
// and double as the lookup key for the Conflict instance in the instance index.
//
// ParentType, ParentKey and Relation are the composing parent's coordinates,
// set only for a composed-child duplicate. A composed child is absent from the
// instance index, so a non-empty Relation directs the lookup through the
// parent's composed children instead.
type DuplicateParts struct {
	Type       schema.TypeID
	Key        immutable.Key
	Instance   InstanceParts
	ParentType schema.TypeID
	ParentKey  immutable.Key
	Relation   string
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
// instances). snapshot.Load validates these invariants before calling
// RebuildSnapshot; failures here indicate a bug in the caller.
func RebuildSnapshot(s *schema.Schema, parts SnapshotParts) (*Snapshot, diag.Result) {
	collector := diag.NewCollector(0)

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

		unresolvedEdges = append(unresolvedEdges,
			newUnresolvedEdge(source, up.Relation, up.TargetType, up.TargetKey.String(), up.Required, up.Reason, up.Properties))
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

// resolveDuplicateConflict finds the instance a duplicate collided with.
// Composed children never enter the instance index, so a composed-child
// duplicate resolves by walking parent → relation → matching child instead.
func resolveDuplicateConflict(index map[schema.TypeID]map[string]*Instance, dp DuplicateParts) *Instance {
	if dp.Relation == "" {
		return lookupInstance(index, dp.Type, dp.Key.String())
	}
	parent := lookupInstance(index, dp.ParentType, dp.ParentKey.String())
	if parent == nil {
		return nil
	}
	keyStr := dp.Key.String()
	for _, child := range parent.Composed(dp.Relation) {
		if child.TypeID() == dp.Type && child.PrimaryKey().String() == keyStr {
			return child
		}
	}
	return nil
}

// describeDuplicateConflict renders a duplicate's conflict coordinates for a
// diagnostic message.
func describeDuplicateConflict(dp DuplicateParts) string {
	if dp.Relation == "" {
		return fmt.Sprintf("%s[%s] in the instance index", dp.Type, dp.Key.String())
	}
	return fmt.Sprintf("%s[%s] under %s[%s].%s",
		dp.Type, dp.Key.String(), dp.ParentType, dp.ParentKey.String(), dp.Relation)
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
	return cmpString(a.target.primaryKey.String(), b.target.primaryKey.String())
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
