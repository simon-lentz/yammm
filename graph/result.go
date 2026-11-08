package graph

import (
	"cmp"
	"iter"
	"slices"

	"github.com/simon-lentz/yammm/schema"
)

// Attestation is the writing library's claim about a snapshot's contents.
//
// Values reports that the snapshot holds at least one instance and that
// every root and composed child entered through the validator — an empty
// built snapshot attests false, so a vacuous truth cannot be re-marshalled as
// its attestation. Rejected duplicates' payloads are outside the claim.
// Associations reports that no unresolved record is Required.
//
// On a built snapshot both dimensions are computed. On a loaded snapshot
// they carry the header's claim verbatim. Nothing protects the claim: anyone
// can recompute the integrity hash, so it stops no deliberate change. The
// snapshot package's re-validation load option is the reader's remedy when a
// claim is not enough.
//
// A snapshot may carry NO claim, which [Snapshot.Attestation] reports as nil.
// A v4 document that omits the attestation carries none, and silence
// is not the same statement as a claim that both dimensions are false — the
// absence is represented as absence so it cannot be read as the stronger
// negative one.
type Attestation struct {
	Values       bool
	Associations bool
}

// Snapshot is a snapshot of the graph at a point in time.
//
// Snapshot has no method that changes it, and it is safe for concurrent read
// access from multiple goroutines. Its accessors return handles to the
// snapshot's own records, and no caller holds a record's address, so no
// assignment through a handle changes the snapshot; see the package doc's
// "Thread Safety".
//
// Types are identified by [schema.TypeID], never by name; see the package doc
// for why, and [schema.TagForm] for rendering one as a name.
//
// Every collection accessor produces deterministically sorted output
// ([Snapshot.EdgesFrom] in [Snapshot.Edges]' order), Add() call order and
// concurrency move only records that tie in every arm, and every ordering below
// compares types by TypeID:
//   - [Snapshot.Types]: lexicographic by the TypeID's String
//   - [Snapshot.InstancesOf]: lexicographic by primary key string
//   - [Snapshot.Edges]: (sourceType, sourceKey, relation, targetType,
//     targetKey, edge properties)
//   - [Snapshot.Duplicates]: (type, primaryKey, instance properties,
//     instance provenance)
//   - [Snapshot.Unresolved]: (sourceType, sourceKey, relation, targetType,
//     targetKey, reason, required, edge properties)
//
// Each sort compares every arm its tuple names. A properties arm compares a rendering that
// writes each value as %T|%#v, so records that rendering makes equal tie in that arm.
// A provenance arm compares the source name, the path a document writes, and the
// span. Among the ties are float64(1) and int64(1) inside a list or map, and a
// zero and an empty Properties.
//
// No instance map is exposed, so there is no iteration order to be surprised
// by: [Snapshot.AllInstances] yields every root instance in the order above,
// and [Snapshot.Types] with [Snapshot.InstancesOf] walks the same order a
// type at a time.
type Snapshot struct{ *snapshotState }

// IsZero reports whether the handle names no record.
func (r Snapshot) IsZero() bool { return r.snapshotState == nil }

// snapshotState is the record a [Snapshot] names.
type snapshotState struct {
	// schema is the schema used for validation.
	schema schema.Schema

	// canon rewrites a received address into the spelling instanceIndex holds,
	// as the constructor that built the index rewrote every key it installed.
	canon *canonicalizer

	// types contains every type identity in sorted order.
	types []schema.TypeID

	// instances maps type identity to sorted instances.
	instances map[schema.TypeID][]Instance

	// instanceIndex provides O(1) lookup by (type, key).
	instanceIndex map[schema.TypeID]map[string]Instance

	// edges contains all resolved edges in sorted order.
	edges []Edge

	// edgeIndex maps each instance to its outgoing edges for O(1) lookup.
	// Built eagerly in newSnapshot from the sorted edges slice.
	// Uses record identity: only instances from this snapshot are found.
	edgeIndex map[Instance][]Edge

	// duplicates contains duplicate records in sorted order.
	duplicates []Duplicate

	// unresolved contains unresolved edge records in sorted order.
	unresolved []UnresolvedEdge

	// attestation is the validity claim this snapshot carries.
	attestation *Attestation
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
func (r Snapshot) Schema() schema.Schema {
	if r.IsZero() {
		return schema.Schema{}
	}
	return r.schema
}

// Types returns every type the snapshot denotes, ordered by TypeID: each
// root's type, and each type a document holds an instances group for with no
// item. A types table row with no such group denotes no type.
// Every one can hold a root under the package doc's "Root type eligibility";
// a composed child's type is a part type and is never denoted, so walk
// [Instance.ComposedRelations] for a subtree. The slice is a copy, and a [schema.TypeID] is a value.
func (r Snapshot) Types() []schema.TypeID {
	if r.IsZero() || len(r.types) == 0 {
		return nil
	}
	result := make([]schema.TypeID, len(r.types))
	copy(result, r.types)
	return result
}

// InstancesOf returns instances of the given type in sorted order.
//
// Instances are sorted by primary key using [FormatKey] string comparison.
// It yields nothing if the type has no instances in the graph.
func (r Snapshot) InstancesOf(id schema.TypeID) iter.Seq[Instance] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var instances []Instance
	if !r.IsZero() {
		instances = r.instances[id]
	}
	return slices.Values(instances)
}

// AllInstances returns an iterator over every root instance in the graph, in
// deterministic order: types by identity, instances within each type by
// primary key. The order is stable across calls.
//
// Composed children are not yielded. For the subtree beneath a root, walk
// [Instance.ComposedRelations] and [Instance.Composed].
func (r Snapshot) AllInstances() iter.Seq[Instance] {
	return func(yield func(Instance) bool) {
		if r.IsZero() {
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
// The key is a [FormatKey]-form address. The address is tried as spelled, then
// canonicalized under the type's key constraints and tried again, so one instant at
// two offsets is two addresses. The exact spelling is the common case, so a
// Timestamp, Date or UUID key is parsed only on a miss. [Instance.PrimaryKey]'s String is the
// address the instance carries; a string [ParseKey] refuses addresses nothing.
// Returns the zero Instance and false on a miss.
func (r Snapshot) InstanceByKey(id schema.TypeID, key string) (Instance, bool) {
	if r.IsZero() || r.instanceIndex == nil {
		return Instance{}, false
	}
	typeIndex := r.instanceIndex[id]
	if typeIndex == nil {
		return Instance{}, false
	}
	if inst, ok := typeIndex[key]; ok {
		return inst, true
	}
	inst, ok := typeIndex[r.canon.address(id, key)]
	return inst, ok
}

// Edges returns all resolved relationship edges in sorted order.
//
// Edges are sorted by the tuple (sourceType, sourceKey, relation, targetType,
// targetKey, edge properties), comparing types by identity.
func (r Snapshot) Edges() iter.Seq[Edge] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var edges []Edge
	if !r.IsZero() {
		edges = r.edges
	}
	return slices.Values(edges)
}

// EdgesFrom returns the outgoing edges for the given instance, sorted by
// (relation, targetType, targetKey).
//
// This is the preferred way to access per-instance edges. Unlike iterating
// Edges() and filtering by source, EdgesFrom uses a precomputed index for
// O(1) lookup per instance.
//
// The instance must belong to this snapshot. Instances from other snapshots
// or from a mutable Graph always yield nothing (the index uses record identity).
//
// It yields nothing if the instance has no outgoing edges.
func (r Snapshot) EdgesFrom(inst Instance) iter.Seq[Edge] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var edges []Edge
	if !r.IsZero() {
		edges = r.edgeIndex[inst]
	}
	return slices.Values(edges)
}

// Attestation returns the validity claim this snapshot carries, or nil when
// it carries none. See [Attestation] for what each dimension means, what it
// does not prove, and why absence is distinct from a false claim.
//
// The result is a COPY. Snapshot is safe for concurrent reads, and its two
// fields are exported, so returning the stored pointer would let one caller
// rewrite another's claim by a field assignment, as [Snapshot.Types] copies
// the identities it returns.
func (r Snapshot) Attestation() *Attestation {
	if r.IsZero() || r.attestation == nil {
		return nil
	}
	att := *r.attestation
	return &att
}

// Duplicates returns the [Duplicate] records of rejected instances in sorted order.
//
// Duplicates are sorted by the tuple (type, primaryKey, rejected instance's
// properties, rejected instance's provenance).
// It yields nothing if no duplicates were detected.
func (r Snapshot) Duplicates() iter.Seq[Duplicate] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var duplicates []Duplicate
	if !r.IsZero() {
		duplicates = r.duplicates
	}
	return slices.Values(duplicates)
}

// Unresolved returns unresolved edge records in sorted order.
//
// Unresolved edges are associations the graph could not resolve: a target it
// holds no root for, or a required one the data left absent or empty. They are sorted by the tuple (sourceType, sourceKey, relation,
// targetType, targetKey, reason, required, edge properties).
//
// It yields nothing if all edges are resolved.
func (r Snapshot) Unresolved() iter.Seq[UnresolvedEdge] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var unresolved []UnresolvedEdge
	if !r.IsZero() {
		unresolved = r.unresolved
	}
	return slices.Values(unresolved)
}

// newSnapshot creates a Snapshot, establishing the ordering its accessors
// document rather than trusting a caller to have done it. Both constructors —
// [Graph.Snapshot] and [rebuildSnapshot] — reach the same shape here, so a
// third one cannot get it wrong.
//
// It takes ownership of every slice it is handed: types, each per-type instance
// slice, edges, duplicates and unresolved are sorted in place, and types is
// re-sliced to drop repeats. Pass slices nothing else holds.
//
// Composed children are deliberately untouched: their order is the caller's,
// and a keyless child's position IS its identity.
func newSnapshot(
	s schema.Schema,
	canon *canonicalizer,
	types []schema.TypeID,
	instances map[schema.TypeID][]Instance,
	instanceIndex map[schema.TypeID]map[string]Instance,
	edges []Edge,
	duplicates []Duplicate,
	unresolved []UnresolvedEdge,
	attestation *Attestation,
) Snapshot {
	// A repeated identity would make every instance of that type appear twice
	// in AllInstances and twice in the persisted document.
	slices.SortFunc(types, func(a, b schema.TypeID) int {
		return cmp.Compare(a.String(), b.String())
	})
	types = slices.CompactFunc(types, func(a, b schema.TypeID) bool { return a == b })

	for _, insts := range instances {
		slices.SortFunc(insts, func(a, b Instance) int {
			return cmp.Compare(a.PrimaryKey().String(), b.PrimaryKey().String())
		})
	}
	slices.SortFunc(edges, compareEdges)
	slices.SortFunc(duplicates, compareDuplicates)
	slices.SortFunc(unresolved, compareUnresolved)

	snap := &snapshotState{
		schema:        s,
		canon:         canon,
		types:         types,
		instances:     instances,
		instanceIndex: instanceIndex,
		edges:         edges,
		duplicates:    duplicates,
		unresolved:    unresolved,
		attestation:   attestation,
	}
	if len(edges) > 0 {
		snap.edgeIndex = make(map[Instance][]Edge)
		for _, e := range edges {
			snap.edgeIndex[e.source] = append(snap.edgeIndex[e.source], e)
		}
	}
	return Snapshot{snap}
}
