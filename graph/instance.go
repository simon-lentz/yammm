package graph

import (
	"slices"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// Instance represents a validated instance node in the graph.
//
// Instance provides read-only access to instance data. It is safe for
// concurrent read access from multiple goroutines.
//
// Instances are created internally by [Graph.Add] and [Graph.AddComposed].
// They are accessed via [Result.Instances], [Result.InstancesOf], or
// [Result.InstanceByKey].
type Instance struct {
	// typeName is the canonical instance tag form.
	// Local types: unqualified (e.g., "Person")
	// Imported types: alias-qualified (e.g., "c.Entity")
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
	composed map[string][]*Instance

	// provenance holds source location metadata for the instance.
	// nil if no provenance was provided by the adapter.
	provenance *instance.Provenance
}

// TypeName returns the canonical instance tag form for this instance's type.
//
// For local types, returns the unqualified name (e.g., "Person").
// For imported types, returns the alias-qualified name (e.g., "c.Entity")
// using the bound schema's import alias.
//
// This matches the string used in [Result.Types] and [Result.Instances] keys.
func (i *Instance) TypeName() string {
	if i == nil {
		return ""
	}
	return i.typeName
}

// TypeID returns the semantic type identity.
//
// Use TypeID for equality comparisons, inheritance checks, and cross-schema
// type resolution. Use [Instance.TypeName] for display and map lookups.
func (i *Instance) TypeID() schema.TypeID {
	if i == nil {
		return schema.TypeID{}
	}
	return i.typeID
}

// PrimaryKey returns the instance's primary key components.
//
// The returned Key is immutable. Use [immutable.Key.String] for map key
// lookups or [immutable.Key.Clone] to get a mutable copy.
func (i *Instance) PrimaryKey() immutable.Key {
	if i == nil {
		return immutable.Key{}
	}
	return i.primaryKey
}

// Property returns the value for the given property name and true if it exists.
// Returns (zero Value, false) if the property does not exist.
//
// Property names are matched case-sensitively. Use the schema's canonical
// property names for reliable lookups.
func (i *Instance) Property(name string) (immutable.Value, bool) {
	if i == nil {
		return immutable.Value{}, false
	}
	return i.properties.Get(name)
}

// Properties returns all validated property values.
//
// The returned Properties is immutable. Use [immutable.Properties.SortedRange]
// for deterministic iteration or [immutable.Properties.Clone] for a mutable copy.
func (i *Instance) Properties() immutable.Properties {
	if i == nil {
		return immutable.Properties{}
	}
	return i.properties
}

// Provenance returns the source location metadata for this instance.
//
// Returns nil if no provenance was provided by the adapter.
// Provenance includes SourceName, Path, and Span for diagnostic reporting.
func (i *Instance) Provenance() *instance.Provenance {
	if i == nil {
		return nil
	}
	return i.provenance
}

// HasProvenance reports whether provenance is available for this instance.
func (i *Instance) HasProvenance() bool {
	return i != nil && i.provenance != nil
}

// Composed returns composed children for the given relation name.
//
// Returns nil if the relation does not exist or has no children.
// The returned slice is a defensive copy; modifications do not affect
// the graph.
func (i *Instance) Composed(relationName string) []*Instance {
	if i == nil || i.composed == nil {
		return nil
	}
	children := i.composed[relationName]
	if len(children) == 0 {
		return nil
	}
	// Return defensive copy
	result := make([]*Instance, len(children))
	copy(result, children)
	return result
}

// ComposedCount returns the number of composed children for the given relation.
func (i *Instance) ComposedCount(relationName string) int {
	if i == nil || i.composed == nil {
		return 0
	}
	return len(i.composed[relationName])
}

// HasComposed reports whether the instance has any composed children
// for the given relation.
func (i *Instance) HasComposed(relationName string) bool {
	return i.ComposedCount(relationName) > 0
}

// ComposedRelations returns the names of all relations that have composed children.
// The returned slice is sorted lexicographically for deterministic iteration.
func (i *Instance) ComposedRelations() []string {
	if i == nil || len(i.composed) == 0 {
		return nil
	}
	result := make([]string, 0, len(i.composed))
	for name := range i.composed {
		result = append(result, name)
	}
	slices.Sort(result)
	return result
}

// newInstance creates an Instance from graph-internal data.
// This is an internal constructor; instances are created by Graph.Add/AddComposed.
func newInstance(
	typeName string,
	typeID schema.TypeID,
	primaryKey immutable.Key,
	properties immutable.Properties,
	provenance *instance.Provenance,
) *Instance {
	return &Instance{
		typeName:   typeName,
		typeID:     typeID,
		primaryKey: primaryKey,
		properties: properties,
		provenance: provenance,
	}
}

// addComposed attaches a composed child to the instance.
// This is an internal method called during graph construction.
func (i *Instance) addComposed(relationName string, child *Instance) {
	if i.composed == nil {
		i.composed = make(map[string][]*Instance)
	}
	i.composed[relationName] = append(i.composed[relationName], child)
}

// cloneInstance creates a deep copy of an Instance including its composed children.
// The cloneMap tracks original-to-clone mappings to handle the full instance tree.
// Returns the cloned instance and updates cloneMap with the new mapping.
//
// dependency: This function shares primaryKey (immutable.Key),
// properties (immutable.Properties), and provenance (*instance.Provenance)
// by direct assignment rather than deep-copying.
// This is safe because these types guarantee structural immutability:
// all internal maps and slices are unexported and never modified after construction.
// If these types ever expose mutation methods, this function must be updated to
// deep-copy them.
//
// Fields shared directly: typeName, typeID, primaryKey, properties, provenance
// Fields deep-copied: composed map and its child instance trees
func cloneInstance(orig *Instance, cloneMap map[*Instance]*Instance) *Instance {
	if orig == nil {
		return nil
	}

	// Check if already cloned (handles potential reference cycles)
	if clone, exists := cloneMap[orig]; exists {
		return clone
	}

	// Create the clone - immutable fields can be shared directly
	clone := &Instance{
		typeName:   orig.typeName,
		typeID:     orig.typeID,
		primaryKey: orig.primaryKey,
		properties: orig.properties,
		provenance: orig.provenance, // Safe to share: Provenance is immutable
	}

	// Register clone before recursing to handle any reference cycles
	cloneMap[orig] = clone

	// Deep clone composed children
	if orig.composed != nil {
		clone.composed = make(map[string][]*Instance, len(orig.composed))
		for relationName, children := range orig.composed {
			clonedChildren := make([]*Instance, len(children))
			for i, child := range children {
				clonedChildren[i] = cloneInstance(child, cloneMap)
			}
			clone.composed[relationName] = clonedChildren
		}
	}

	return clone
}
