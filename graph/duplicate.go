package graph

import (
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Duplicate records a root the graph rejected for a primary key that already
// exists for its type.
//
// When [Graph.Add] meets such a key, a Duplicate is created to track both the
// new instance (which is rejected) and the existing instance (which remains in
// the graph). [BatchAssembler.AddValidOrMerge] creates one only for an incoming
// instance carrying composed children; a merge creates none. Its fields are read
// through methods, so no caller can set one.
//
// # Composed Children Not Included
//
// [Duplicate.Instance] is the rejected instance without its composed children.
// [Graph.Add] builds the whole tree before it meets the duplicate, and records
// a childless copy of the rejected root: a duplicate installs nothing. If you need
// to inspect composed children from duplicate data, access them from the original
// [instance.ValidInstance] passed to [Graph.Add], [BatchAssembler.AddValid] or
// [BatchAssembler.AddValidOrMerge].
//
// Duplicates are accessed via [Snapshot.Duplicates].
type Duplicate struct{ *duplicateState }

// IsZero reports whether the handle names no record.
func (d Duplicate) IsZero() bool { return d.duplicateState == nil }

// duplicateState is the record a [Duplicate] names.
type duplicateState struct {
	instance Instance
	conflict Instance
}

// Instance returns a childless copy of the rejected instance: one that an add
// did not install, or a duplicate record a document held.
func (d Duplicate) Instance() Instance {
	if d.IsZero() {
		return Instance{}
	}
	return d.instance
}

// Conflict returns the root the rejected instance collided with, which remains
// in the graph: the root at the rejected instance's type and key.
func (d Duplicate) Conflict() Instance {
	if d.IsZero() {
		return Instance{}
	}
	return d.conflict
}

// UnresolvedEdge records an association edge the graph could not resolve: one
// whose target it holds no root for, or a required one the data left absent or
// empty.
//
// Unresolved edges occur when:
//   - The target instance was not added yet (Reason: "target_missing")
//   - A required association field is absent from the data (Reason: "absent")
//   - A required association array is empty (Reason: "empty")
//
// For required associations ([UnresolvedEdge.Required]), unresolved edges cause
// [Graph.Check] to report E_UNRESOLVED_REQUIRED diagnostics. Optional
// associations may remain unresolved without error. Its fields are read through
// methods, so no caller can set one.
//
// Unresolved edges are accessed via [Snapshot.Unresolved].
type UnresolvedEdge struct{ *unresolvedEdgeState }

// IsZero reports whether the handle names no record.
func (u UnresolvedEdge) IsZero() bool { return u.unresolvedEdgeState == nil }

// unresolvedEdgeState is the record a [UnresolvedEdge] names.
type unresolvedEdgeState struct {
	source     Instance
	relation   string
	targetType schema.TypeID
	targetKey  string
	required   bool
	reason     string
	// properties is empty for the "absent" and "empty" reasons, which never
	// had a target to attach properties to.
	properties immutable.Properties
}

// Source returns the instance that declares the unresolved reference.
func (u UnresolvedEdge) Source() Instance {
	if u.IsZero() {
		return Instance{}
	}
	return u.source
}

// Relation returns the DSL relation name (e.g., "OWNER").
func (u UnresolvedEdge) Relation() string {
	if u.IsZero() {
		return ""
	}
	return u.relation
}

// TargetType returns the association's declared target type.
func (u UnresolvedEdge) TargetType() schema.TypeID {
	if u.IsZero() {
		return schema.TypeID{}
	}
	return u.targetType
}

// TargetKey returns the foreign key in [FormatKey] form, and "" for the
// "absent" and "empty" reasons, which name no target.
func (u UnresolvedEdge) TargetKey() string {
	if u.IsZero() {
		return ""
	}
	return u.targetKey
}

// Required reports whether the association is required by the schema the
// snapshot is bound to. Every constructor derives it from the relation and
// none takes it from its input; when true, [Graph.Check] reports E_UNRESOLVED_REQUIRED.
func (u UnresolvedEdge) Required() bool {
	if u.IsZero() {
		return false
	}
	return u.required
}

// Reason explains why the edge is unresolved:
//   - "target_missing": the target instance is not in the graph
//   - "absent": the association field is missing from the instance data
//   - "empty": the association array is present but empty
func (u UnresolvedEdge) Reason() string {
	if u.IsZero() {
		return ""
	}
	return u.reason
}

// Property returns the value for the given edge property name and true if it exists.
// Returns (zero Value, false) if the property does not exist or the receiver is zero.
//
// Edge properties on unresolved edges are declared on the association in the
// yammm DSL; they survive Marshal/Load in every readable .ys wire
// format. Symmetric with [Edge.Property] for resolved edges.
func (u UnresolvedEdge) Property(name string) (immutable.Value, bool) {
	if u.IsZero() {
		return immutable.Value{}, false
	}
	return u.properties.Get(name)
}

// Properties returns all edge property values.
//
// An edge with no property, or a zero receiver, holds the zero Properties, so
// [immutable.Properties.Clone] answers nil for it. The returned Properties is immutable.
func (u UnresolvedEdge) Properties() immutable.Properties {
	if u.IsZero() {
		return immutable.Properties{}
	}
	return u.properties
}

// newDuplicate creates a Duplicate record.
func newDuplicate(instance, conflict Instance) Duplicate {
	return Duplicate{&duplicateState{
		instance: instance,
		conflict: conflict,
	}}
}

// newUnresolvedEdge creates an UnresolvedEdge record.
func newUnresolvedEdge(source Instance, relation string, targetType schema.TypeID, targetKey string, required bool, reason string, properties immutable.Properties) UnresolvedEdge {
	return UnresolvedEdge{&unresolvedEdgeState{
		source:     source,
		relation:   relation,
		targetType: targetType,
		targetKey:  targetKey,
		required:   required,
		reason:     reason,
		properties: properties,
	}}
}
