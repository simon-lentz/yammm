package graph

import (
	"cmp"
	"iter"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// Attestation is the writing library's claim about a snapshot's contents.
//
// Values reports that the snapshot holds at least one instance and that
// every root and composed child entered through the validator — an empty
// snapshot attests false, so a vacuous truth cannot be re-marshalled as
// an attestation. Rejected duplicates' payloads are outside the claim.
// Associations reports that no unresolved record is Required.
//
// On a built snapshot both dimensions are computed. On a loaded snapshot
// they carry the header's claim verbatim — protected against tampering by
// the integrity hash and against nothing else, because [RebuildSnapshot]
// accepts caller-assembled parts. The snapshot package's re-validation
// load option is the reader's remedy when a claim is not enough.
type Attestation struct {
	Values       bool
	Associations bool
}

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
//   - [Snapshot.Edges]: (sourceType, sourceKey, relation, targetType,
//     targetKey, edge properties)
//   - [Snapshot.Duplicates]: (type, primaryKey, relation, conflictType,
//     conflictKey, parent slot, instance properties)
//   - [Snapshot.Unresolved]: (sourceType, sourceKey, relation, targetType,
//     targetKey, reason, required, edge properties)
//
// Each tuple is total: every arm is compared, so two records that differ at
// all sort apart and none inherits map-iteration order.
//
// No instance map is exposed, so there is no iteration order to be surprised
// by: [Snapshot.AllInstances] yields every root instance in the order above,
// and [Snapshot.Types] with [Snapshot.InstancesOf] walks the same order a
// slice at a time.
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

	// attestation is the validity claim this snapshot carries.
	attestation Attestation
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

// AllInstances returns an iterator over every root instance in the graph, in
// deterministic order: types by identity, instances within each type by
// primary key. The order is stable across calls.
//
// Composed children are not yielded. For the subtree beneath a root, walk
// [Instance.ComposedRelations] and [Instance.Composed].
func (r *Snapshot) AllInstances() iter.Seq[*Instance] {
	return func(yield func(*Instance) bool) {
		if r == nil {
			return
		}
		for _, typeID := range r.types {
			for _, inst := range r.instances[typeID] {
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
// Edges are sorted by the tuple (sourceType, sourceKey, relation, targetType,
// targetKey, edge properties), comparing types by identity.
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
// should check [Snapshot.Duplicates] and [Snapshot.Unresolved] for the
// structural records that survive serialization, [Snapshot.Attestation] for
// the writer's validity claim, and the snapshot package's WithRevalidation
// option to re-check a loaded document's instance data.
//
// Use [diag.Result.OK] to check if the graph construction succeeded.
func (r *Snapshot) Diagnostics() diag.Result {
	if r == nil {
		return diag.OK()
	}
	return r.diagnostics
}

// Attestation returns the validity claim this snapshot carries. See
// [Attestation] for what each dimension means and what it does not prove.
func (r *Snapshot) Attestation() Attestation {
	if r == nil {
		return Attestation{}
	}
	return r.attestation
}

// Duplicates returns duplicate primary key records in sorted order.
//
// Duplicates are sorted by the tuple (type, primaryKey, relation,
// conflictType, conflictKey, parent slot, rejected instance's properties).
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
// Unresolved edges are associations whose target instances are not in the
// graph. They are sorted by the tuple (sourceType, sourceKey, relation,
// targetType, targetKey, reason, required, edge properties).
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

// newSnapshot creates a Snapshot, establishing the ordering its accessors
// document rather than trusting a caller to have done it. Both constructors —
// [Graph.Snapshot] and [RebuildSnapshot] — reach the same shape here, so a
// third one cannot get it wrong.
//
// It takes ownership of every slice it is handed: types, each per-type instance
// slice, edges, duplicates and unresolved are sorted in place, and types is
// re-sliced to drop repeats. Pass slices nothing else holds.
//
// Composed children are deliberately untouched: their order is the caller's,
// and a keyless child's position IS its identity ([InstanceParts]).
func newSnapshot(
	s *schema.Schema,
	types []schema.TypeID,
	instances map[schema.TypeID][]*Instance,
	instanceIndex map[schema.TypeID]map[string]*Instance,
	edges []*Edge,
	duplicates []*Duplicate,
	unresolved []*UnresolvedEdge,
	diagnostics diag.Result,
	attestation Attestation,
) *Snapshot {
	// A repeated identity would make every instance of that type appear twice
	// in AllInstances and twice in the persisted document.
	slices.SortFunc(types, func(a, b schema.TypeID) int {
		return cmp.Compare(a.String(), b.String())
	})
	types = slices.CompactFunc(types, func(a, b schema.TypeID) bool { return a == b })

	for _, insts := range instances {
		slices.SortFunc(insts, func(a, b *Instance) int {
			return cmp.Compare(a.PrimaryKey().String(), b.PrimaryKey().String())
		})
	}
	slices.SortFunc(edges, compareEdges)
	slices.SortFunc(duplicates, compareDuplicates)
	slices.SortFunc(unresolved, compareUnresolved)

	snap := &Snapshot{
		schema:        s,
		types:         types,
		instances:     instances,
		instanceIndex: instanceIndex,
		edges:         edges,
		duplicates:    duplicates,
		unresolved:    unresolved,
		diagnostics:   diagnostics,
		attestation:   attestation,
	}
	if len(edges) > 0 {
		snap.edgeIndex = make(map[*Instance][]*Edge)
		for _, e := range edges {
			snap.edgeIndex[e.source] = append(snap.edgeIndex[e.source], e)
		}
	}
	return snap
}
