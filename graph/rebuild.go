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
type SnapshotParts struct {
	Types      []string
	Instances  map[string][]InstanceParts
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
// are identified by (type name, primary key) rather than pointer;
// RebuildSnapshot resolves them to *Instance pointers via the instance index.
type EdgeParts struct {
	Relation   string
	SourceType string
	SourceKey  immutable.Key
	TargetType string
	TargetKey  immutable.Key
	Properties immutable.Properties
}

// DuplicateParts holds the data for a single duplicate record.
//
// Type and Key identify the primary key that was duplicated. These fields
// serve double duty: they describe the rejected instance's identity AND
// provide the lookup key for the Conflict instance in the instance index.
type DuplicateParts struct {
	Type     string
	Key      immutable.Key
	Instance InstanceParts
}

// UnresolvedParts holds the data for a single unresolved edge record.
//
// Properties carries the edge property values declared on the forward
// reference. Populated only when Reason is "target_missing"; empty
// otherwise. Loaded from the .ys wire-format v2 "properties" field on
// unresolved-edge entries; for v1 documents this is always empty
// (v1 never carried the field).
type UnresolvedParts struct {
	SourceType string
	SourceKey  immutable.Key
	Relation   string
	TargetType string
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
	instances := make(map[string][]*Instance, len(parts.Instances))
	instanceIndex := make(map[string]map[string]*Instance, len(parts.Instances))

	for typeName, instParts := range parts.Instances {
		insts := make([]*Instance, 0, len(instParts))
		idx := make(map[string]*Instance, len(instParts))

		for _, ip := range instParts {
			inst := rebuildInstance(ip)
			insts = append(insts, inst)
			idx[ip.PrimaryKey.String()] = inst
		}

		instances[typeName] = insts
		instanceIndex[typeName] = idx
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

		// Resolve the conflict pointer via the index.
		conflict := lookupInstance(instanceIndex, dp.Type, dp.Key.String())
		if conflict == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: duplicate conflict %s[%s] not found in instance index",
					dp.Type, dp.Key.String())).Build())
			continue
		}

		dupInst := rebuildInstance(dp.Instance)
		duplicates = append(duplicates, newDuplicate(dupInst, conflict, diag.Issue{}))
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

	// Step 5: Assemble the Snapshot.
	types := parts.Types
	if types == nil {
		types = []string{}
	}

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

// lookupInstance finds an instance in the index by type name and key string.
func lookupInstance(index map[string]map[string]*Instance, typeName, keyStr string) *Instance {
	typeIdx := index[typeName]
	if typeIdx == nil {
		return nil
	}
	return typeIdx[keyStr]
}

// compareEdges provides the sorting comparator for edges.
// Sorts by (sourceType, sourceKey, relation, targetType, targetKey).
func compareEdges(a, b *Edge) int {
	if c := cmpString(a.source.typeName, b.source.typeName); c != 0 {
		return c
	}
	if c := cmpString(a.source.primaryKey.String(), b.source.primaryKey.String()); c != 0 {
		return c
	}
	if c := cmpString(a.relation, b.relation); c != 0 {
		return c
	}
	if c := cmpString(a.target.typeName, b.target.typeName); c != 0 {
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
