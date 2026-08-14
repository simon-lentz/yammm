package graph

import (
	"iter"
	"maps"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// Snapshot is an immutable snapshot of the graph at a point in time.
//
// Snapshot provides read-only access to all instances, edges, duplicates,
// and diagnostics. It is safe for concurrent read access from multiple
// goroutines.
//
// Types are identified by [schema.TypeID], never by name; see the package doc
// for why, and [schema.TagForm] for rendering one as a name.
//
// All slice-returning methods produce deterministically sorted output,
// independent of Add() call order or concurrency, and every ordering below
// compares types by TypeID so two types rendering one name still sort apart:
//   - [Snapshot.Types]: lexicographic by TypeID (schema path, then name)
//   - [Snapshot.InstancesOf]: lexicographic by primary key string
//   - [Snapshot.Edges]: (sourceType, sourceKey, relation, targetType, targetKey)
//   - [Snapshot.Duplicates]: (type, primaryKey)
//   - [Snapshot.Unresolved]: (sourceType, sourceKey, relation, targetType, targetKey)
//
// The [Snapshot.Instances] map has non-deterministic iteration order per Go semantics.
// For deterministic iteration, use [Snapshot.AllInstances] (iterator) or
// Types() + InstancesOf() (slice-based).
type Snapshot struct {
	// schema is the schema used for validation.
	schema *schema.Schema

	// types contains every type identity in sorted order.
	types []schema.TypeID

	// instances maps type identity to sorted instances.
	instances map[schema.TypeID][]*Instance

	// instanceIndex provides O(1) lookup by (type, key).
	instanceIndex map[schema.TypeID]map[string]*Instance

	// edges contains all resolved edges in sorted order.
	edges []*Edge

	// edgeIndex maps each instance to its outgoing edges for O(1) lookup.
	// Built eagerly in newSnapshot from the sorted edges slice.
	// Uses pointer identity: only instances from this snapshot are found.
	edgeIndex map[*Instance][]*Edge

	// duplicates contains duplicate records in sorted order.
	duplicates []*Duplicate

	// unresolved contains unresolved edge records in sorted order.
	unresolved []*UnresolvedEdge

	// diagnostics contains all issues from graph construction.
	diagnostics diag.Result
}

// Schema returns the schema used for validation.
//
// This provides access to type definitions and relation metadata,
// which is needed for schema-aware serialization (e.g., determining
// whether a relation is one-to-one or one-to-many).
//
// For snapshots loaded from persisted .ys files, this returns the schema
// provided to snapshot.Load, not the schema used at original construction
// time. The loaded schema is verified for structural compatibility via
// schema.StructuralHash.
func (r *Snapshot) Schema() *schema.Schema {
	if r == nil {
		return nil
	}
	return r.schema
}

// Types returns every type identity in the graph, ordered by TypeID: schema
// path, then name.
//
// Use with [Snapshot.InstancesOf] for deterministic iteration, and
// [schema.TagForm] to render an identity as a name.
// Returns a defensive copy.
func (r *Snapshot) Types() []schema.TypeID {
	if r == nil || len(r.types) == 0 {
		return nil
	}
	result := make([]schema.TypeID, len(r.types))
	copy(result, r.types)
	return result
}

// InstancesOf returns instances of the given type in sorted order.
//
// Instances are sorted by primary key using [FormatKey] string comparison.
// Returns nil if the type has no instances in the graph.
//
// Returns a defensive copy.
func (r *Snapshot) InstancesOf(id schema.TypeID) []*Instance {
	if r == nil || r.instances == nil {
		return nil
	}
	instances := r.instances[id]
	if len(instances) == 0 {
		return nil
	}
	result := make([]*Instance, len(instances))
	copy(result, instances)
	return result
}

// Instances returns all validated instances keyed by type identity.
//
// WARNING: Map iteration order is non-deterministic per Go semantics.
// For deterministic iteration (CLI output, tests), use [Snapshot.Types]
// with [Snapshot.InstancesOf] instead.
//
// Returns a shallow copy of the map; instance slices are shared.
func (r *Snapshot) Instances() map[schema.TypeID][]*Instance {
	if r == nil || r.instances == nil {
		return nil
	}
	// Shallow copy the map; slices are already sorted snapshots
	result := make(map[schema.TypeID][]*Instance, len(r.instances))
	maps.Copy(result, r.instances)
	return result
}

// AllInstances returns an iterator over every instance in the graph in
// deterministic order: types sorted lexicographically, instances within
// each type sorted by primary key.
//
// This is the simplest way to iterate all instances without composition
// structure. For composition-aware traversal, use [walk.Walk].
//
// Unlike [Snapshot.Instances] (which returns a map with non-deterministic
// iteration order), AllInstances guarantees stable ordering across calls.
func (r *Snapshot) AllInstances() iter.Seq[*Instance] {
	return func(yield func(*Instance) bool) {
		if r == nil {
			return
		}
		for _, typeName := range r.types {
			for _, inst := range r.instances[typeName] {
				if !yield(inst) {
					return
				}
			}
		}
	}
}

// InstanceByKey looks up a single instance by type identity and primary key.
//
// The key must be in canonical string form (use [FormatKey] to convert values).
// Returns (nil, false) if no matching instance exists.
func (r *Snapshot) InstanceByKey(id schema.TypeID, key string) (*Instance, bool) {
	if r == nil || r.instanceIndex == nil {
		return nil, false
	}
	typeIndex := r.instanceIndex[id]
	if typeIndex == nil {
		return nil, false
	}
	inst, ok := typeIndex[key]
	return inst, ok
}

// Edges returns all resolved relationship edges in sorted order.
//
// Edges are sorted by the tuple:
// (sourceTypeName, sourceKey, relationName, targetTypeName, targetKey)
//
// Returns a defensive copy.
func (r *Snapshot) Edges() []*Edge {
	if r == nil || len(r.edges) == 0 {
		return nil
	}
	result := make([]*Edge, len(r.edges))
	copy(result, r.edges)
	return result
}

// EdgesFrom returns the outgoing edges for the given instance, sorted by
// (relation, targetType, targetKey).
//
// This is the preferred way to access per-instance edges. Unlike iterating
// Edges() and filtering by source, EdgesFrom uses a precomputed index for
// O(1) lookup per instance.
//
// The instance must belong to this snapshot. Instances from other snapshots
// or from a mutable Graph always return nil (the index uses pointer identity).
//
// Returns nil if the instance has no outgoing edges.
// Returns a defensive copy; modifications do not affect the snapshot.
func (r *Snapshot) EdgesFrom(inst *Instance) []*Edge {
	if r == nil || inst == nil || r.edgeIndex == nil {
		return nil
	}
	edges := r.edgeIndex[inst]
	if len(edges) == 0 {
		return nil
	}
	result := make([]*Edge, len(edges))
	copy(result, edges)
	return result
}

// Diagnostics returns validation issues from graph construction.
//
// This includes errors and warnings from [Graph.Add] and [Graph.AddComposed] calls.
// [Graph.Check] results are returned separately per-call and are not accumulated
// here, making Check idempotent: multiple calls have no effect on snapshot diagnostics.
//
// For snapshots loaded from persisted .ys files, Diagnostics returns diag.OK()
// because construction diagnostics are transient and not persisted. Consumers
// should check [Snapshot.Duplicates] and [Snapshot.Unresolved] for structural
// issues that are preserved across serialization.
//
// Use [diag.Result.OK] to check if the graph construction succeeded.
func (r *Snapshot) Diagnostics() diag.Result {
	if r == nil {
		return diag.OK()
	}
	return r.diagnostics
}

// Duplicates returns duplicate primary key records in sorted order.
//
// Duplicates are sorted by (typeName, primaryKey).
// Returns nil if no duplicates were detected.
// Returns a defensive copy.
func (r *Snapshot) Duplicates() []*Duplicate {
	if r == nil || len(r.duplicates) == 0 {
		return nil
	}
	result := make([]*Duplicate, len(r.duplicates))
	copy(result, r.duplicates)
	return result
}

// Unresolved returns unresolved edge records in sorted order.
//
// Unresolved edges are associations whose target instances are not in the graph.
// They are sorted by:
// (sourceTypeName, sourceKey, relationName, targetTypeName, targetKey)
//
// Returns nil if all edges are resolved.
// Returns a defensive copy.
func (r *Snapshot) Unresolved() []*UnresolvedEdge {
	if r == nil || len(r.unresolved) == 0 {
		return nil
	}
	result := make([]*UnresolvedEdge, len(r.unresolved))
	copy(result, r.unresolved)
	return result
}

// OK reports whether the graph was constructed without errors.
//
// This is a convenience method equivalent to r.Diagnostics().OK().
// A graph may have warnings and still be OK.
//
// For snapshots loaded from persisted .ys files, OK always returns true
// because construction diagnostics are transient. This reflects
// construction-time diagnostics only, not structural issues — check
// [Snapshot.Duplicates] and [Snapshot.Unresolved] separately.
func (r *Snapshot) OK() bool {
	if r == nil {
		return true
	}
	return r.diagnostics.OK()
}

// HasErrors reports whether the graph has any errors.
//
// This is a convenience method equivalent to r.Diagnostics().HasErrors().
//
// For snapshots loaded from persisted .ys files, HasErrors always returns
// false because construction diagnostics are transient. See [Snapshot.OK].
func (r *Snapshot) HasErrors() bool {
	if r == nil {
		return false
	}
	return r.diagnostics.HasErrors()
}

// newSnapshot creates a Snapshot from sorted graph data.
// All slices must already be sorted according to the ordering guarantees.
func newSnapshot(
	s *schema.Schema,
	types []schema.TypeID,
	instances map[schema.TypeID][]*Instance,
	instanceIndex map[schema.TypeID]map[string]*Instance,
	edges []*Edge,
	duplicates []*Duplicate,
	unresolved []*UnresolvedEdge,
	diagnostics diag.Result,
) *Snapshot {
	snap := &Snapshot{
		schema:        s,
		types:         types,
		instances:     instances,
		instanceIndex: instanceIndex,
		edges:         edges,
		duplicates:    duplicates,
		unresolved:    unresolved,
		diagnostics:   diagnostics,
	}
	if len(edges) > 0 {
		snap.edgeIndex = make(map[*Instance][]*Edge)
		for _, e := range edges {
			snap.edgeIndex[e.source] = append(snap.edgeIndex[e.source], e)
		}
	}
	return snap
}
