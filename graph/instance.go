package graph

import (
	"iter"
	"slices"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Instance represents an instance node in the graph. [Instance.Validated]
// reports whether it came through the validator.
//
// Instance provides read access to instance data. It is safe for
// concurrent read access from multiple goroutines.
//
// Instances are created internally, by [Graph.Add], the snapshot package's reader
// and the snapshot copies [Graph.Snapshot] and the import take.
// A root is accessed via [Snapshot.AllInstances], [Snapshot.InstancesOf], or
// [Snapshot.InstanceByKey]; a composed child via [Instance.Composed]; and the
// instances an edge, a duplicate or an unresolved record names via that
// record's methods.
type Instance struct{ *instanceState }

// IsZero reports whether the handle names no record.
func (i Instance) IsZero() bool { return i.instanceState == nil }

// instanceState is the record a [Instance] names.
type instanceState struct {
	// typeName is the identity rendered by schema.TagForm under the bound schema.
	typeName string

	// typeID is the semantic type identity for internal indexing.
	typeID schema.TypeID

	// primaryKey is the instance's primary key components.
	primaryKey immutable.Key

	// properties contains the validated property values.
	properties immutable.Properties

	// composed holds composed children indexed by relation name.
	// Only populated for instances that have compositions.
	// nil if the instance has no compositions.
	composed map[string][]Instance

	// provenance holds source location metadata for the instance.
	// Zero if the validated instance carried none.
	provenance location.Provenance

	// validated records whether this instance entered through a
	// validator-built ValidInstance. Rebuilt instances are always false:
	// the wire carries a claim, not a proof.
	validated bool
}

// TypeName returns this instance's type rendered under the bound schema by
// [github.com/simon-lentz/yammm/schema.TagForm]: "Person" for a local type,
// "c.Entity" for a directly imported one, and "deep#Beacon" for one the schema
// reaches only through an intermediate import. Use [Instance.TypeID] to compare.
func (i Instance) TypeName() string {
	if i.IsZero() {
		return ""
	}
	return i.typeName
}

// TypeID returns the semantic type identity.
//
// Use TypeID for equality comparisons, inheritance checks, and cross-schema
// type resolution. Use [Instance.TypeName] for display alone.
func (i Instance) TypeID() schema.TypeID {
	if i.IsZero() {
		return schema.TypeID{}
	}
	return i.typeID
}

// PrimaryKey returns the instance's primary key components.
//
// The returned Key is immutable. Use [immutable.Key.String] for map key
// lookups or [immutable.Key.Clone] to get a mutable copy.
func (i Instance) PrimaryKey() immutable.Key {
	if i.IsZero() {
		return immutable.Key{}
	}
	return i.primaryKey
}

// Property returns the value for the given property name and true if it exists.
// Returns (zero Value, false) if the property does not exist.
//
// Property names are matched case-sensitively. Use the schema's canonical
// property names for reliable lookups.
func (i Instance) Property(name string) (immutable.Value, bool) {
	if i.IsZero() {
		return immutable.Value{}, false
	}
	return i.properties.Get(name)
}

// Properties returns all validated property values.
//
// The returned Properties is immutable. Use [immutable.Properties.SortedRange]
// for deterministic iteration or [immutable.Properties.Clone] for a mutable copy.
func (i Instance) Properties() immutable.Properties {
	if i.IsZero() {
		return immutable.Properties{}
	}
	return i.properties
}

// Provenance returns the source location metadata for this instance.
//
// Returns the zero Provenance when the instance's source carried none: the
// validated instance [Graph.Add] took, or the document snapshot.Load read.
// Provenance includes SourceName, Path, and Span for diagnostic reporting.
func (i Instance) Provenance() location.Provenance {
	if i.IsZero() {
		return location.Provenance{}
	}
	return i.provenance
}

// Validated reports whether this instance entered the graph through a
// validator-built ValidInstance (see [Graph.Add]). Instances rebuilt from
// a loaded snapshot report false; [Snapshot.Attestation] carries the
// writing library's claim for those.
func (i Instance) Validated() bool {
	if i.IsZero() {
		return false
	}
	return i.validated
}

// Composed returns an iterator over the composed children of the given relation
// name. It yields nothing if the relation does not exist or has no children.
func (i Instance) Composed(relationName string) iter.Seq[Instance] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var children []Instance
	if !i.IsZero() {
		children = i.composed[relationName]
	}
	return slices.Values(children)
}

// ComposedCount returns the number of composed children for the given relation.
func (i Instance) ComposedCount(relationName string) int {
	if i.IsZero() || i.composed == nil {
		return 0
	}
	return len(i.composed[relationName])
}

// ComposedRelations returns the names of all relations that have composed children.
// The returned slice is sorted lexicographically for deterministic iteration.
func (i Instance) ComposedRelations() []string {
	if i.IsZero() || len(i.composed) == 0 {
		return nil
	}
	result := make([]string, 0, len(i.composed))
	for name := range i.composed {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

// newInstance creates an Instance from graph-internal data. [Graph.Add] and
// [rebuildSnapshot] build through it; a snapshot copy is taken by cloneInstance.
func newInstance(
	typeName string,
	typeID schema.TypeID,
	primaryKey immutable.Key,
	properties immutable.Properties,
	provenance location.Provenance,
	validated bool,
) Instance {
	return Instance{&instanceState{
		typeName:   typeName,
		typeID:     typeID,
		primaryKey: primaryKey,
		properties: properties,
		provenance: provenance,
		validated:  validated,
	}}
}

// addComposed attaches a composed child to the instance.
// This is an internal method called during graph construction.
func (i Instance) addComposed(relationName string, child Instance) {
	if i.composed == nil {
		i.composed = make(map[string][]Instance)
	}
	i.composed[relationName] = append(i.composed[relationName], child)
}

// cloneInstance creates a deep copy of an Instance including its composed children.
// cloneMap receives the original-to-clone mapping of every instance in the tree,
// which is how a caller resolves a record naming one of them. orig names a record,
// and no record is cloned twice: a composition tree shares no child.
//
// dependency: This function shares primaryKey (immutable.Key),
// properties (immutable.Properties), and provenance (location.Provenance)
// by direct assignment rather than deep-copying.
// This is safe because these types guarantee structural immutability:
// all internal maps and slices are unexported and never modified after construction.
// If these types ever expose mutation methods, this function must be updated to
// deep-copy them.
//
// Fields shared directly: typeName, typeID, primaryKey, properties,
// provenance, validated
// Fields deep-copied: composed map and its child instance trees
func cloneInstance(orig Instance, cloneMap map[Instance]Instance) Instance {
	// Create the clone - immutable fields can be shared directly
	clone := &instanceState{
		typeName:   orig.typeName,
		typeID:     orig.typeID,
		primaryKey: orig.primaryKey,
		properties: orig.properties,
		provenance: orig.provenance, // Safe to share: Provenance is immutable
		validated:  orig.validated,
	}

	cloneMap[orig] = Instance{clone}

	// Deep clone composed children
	if orig.composed != nil {
		clone.composed = make(map[string][]Instance, len(orig.composed))
		for relationName, children := range orig.composed {
			clonedChildren := make([]Instance, len(children))
			for i, child := range children {
				clonedChildren[i] = cloneInstance(child, cloneMap)
			}
			clone.composed[relationName] = clonedChildren
		}
	}

	return Instance{clone}
}
