package graph

import (
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Duplicate records a duplicate primary key detected during graph construction.
//
// When an instance is added with a primary key that already exists for the same
// type, a Duplicate is created to track both the new instance (which is rejected)
// and the existing instance (which remains in the graph). Its fields are read
// through methods, so no caller can write through a snapshot.
//
// # Composed Children Not Included
//
// [Duplicate.Instance] is the rejected instance without its composed children.
// [Graph.Add] builds the whole tree before it meets the duplicate, and records
// a childless copy of the rejected root: a duplicate installs nothing. If you need
// to inspect composed children from duplicate data, access them from the original
// [instance.ValidInstance] passed to [Graph.Add].
//
// Duplicates are accessed via [Snapshot.Duplicates].
type Duplicate struct {
	instance   *Instance
	conflict   *Instance
	parent     *Instance
	relation   string
	diagnostic diag.Issue
}

// Instance returns the instance that was rejected due to a duplicate primary
// key: the instance passed to Add that was not added to the graph.
func (d *Duplicate) Instance() *Instance {
	if d == nil {
		return nil
	}
	return d.instance
}

// Conflict returns the instance in the graph the rejected one collided with,
// which remains in the graph: the root at the rejected instance's type and key,
// or the occupant of the parent's slot at its key, or a (one) slot's sole
// occupant.
func (d *Duplicate) Conflict() *Instance {
	if d == nil {
		return nil
	}
	return d.conflict
}

// Parent returns the composing parent for a composed-child duplicate, and nil
// for a root duplicate. With [Duplicate.Relation] it locates a conflict no
// index holds.
func (d *Duplicate) Parent() *Instance {
	if d == nil {
		return nil
	}
	return d.parent
}

// Relation returns the composing relation name for a composed-child duplicate,
// and "" for a root duplicate.
func (d *Duplicate) Relation() string {
	if d == nil {
		return ""
	}
	return d.relation
}

// Diagnostic returns the E_DUPLICATE_PK or E_DUPLICATE_COMPOSED_PK issue that
// reported the conflict.
//
// For snapshots loaded from persisted .ys files the issue is zero-valued,
// because construction diagnostics are transient and not persisted. Code that
// runs on both constructed and loaded snapshots guards on [diag.Issue.IsZero].
func (d *Duplicate) Diagnostic() diag.Issue {
	if d == nil {
		return diag.Issue{}
	}
	return d.diagnostic
}

// UnresolvedEdge records an association edge whose target was not found in the graph.
//
// Unresolved edges occur when:
//   - The target instance was not added yet (Reason: "target_missing")
//   - A required association field is absent from the data (Reason: "absent")
//   - A required association array is empty (Reason: "empty")
//
// For required associations ([UnresolvedEdge.Required]), unresolved edges cause
// [Graph.Check] to report E_UNRESOLVED_REQUIRED diagnostics. Optional
// associations may remain unresolved without error. Its fields are read through
// methods, so no caller can write through a snapshot.
//
// Unresolved edges are accessed via [Snapshot.Unresolved].
type UnresolvedEdge struct {
	source     *Instance
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
func (u *UnresolvedEdge) Source() *Instance {
	if u == nil {
		return nil
	}
	return u.source
}

// Relation returns the DSL relation name (e.g., "OWNER").
func (u *UnresolvedEdge) Relation() string {
	if u == nil {
		return ""
	}
	return u.relation
}

// TargetType returns the association's declared target type.
func (u *UnresolvedEdge) TargetType() schema.TypeID {
	if u == nil {
		return schema.TypeID{}
	}
	return u.targetType
}

// TargetKey returns the foreign key in [FormatKey] form, and "" for the
// "absent" and "empty" reasons, which name no target.
func (u *UnresolvedEdge) TargetKey() string {
	if u == nil {
		return ""
	}
	return u.targetKey
}

// Required reports whether the association is required by the schema the
// snapshot is bound to. Every constructor derives it from the relation and
// none takes it from its input; when true, [Graph.Check] reports E_UNRESOLVED_REQUIRED.
func (u *UnresolvedEdge) Required() bool {
	if u == nil {
		return false
	}
	return u.required
}

// Reason explains why the edge is unresolved:
//   - "target_missing": the target instance is not in the graph
//   - "absent": the association field is missing from the instance data
//   - "empty": the association array is present but empty
func (u *UnresolvedEdge) Reason() string {
	if u == nil {
		return ""
	}
	return u.reason
}

// Property returns the value for the given edge property name and true if it exists.
// Returns (zero Value, false) if the property does not exist or the receiver is nil.
//
// Edge properties on unresolved edges are declared on the forward reference
// via the yammm DSL; they survive Marshal/Load in every readable .ys wire
// format. Symmetric with [Edge.Property] for resolved edges.
func (u *UnresolvedEdge) Property(name string) (immutable.Value, bool) {
	if u == nil {
		return immutable.Value{}, false
	}
	return u.properties.Get(name)
}

// Properties returns all edge property values.
//
// Returns an empty Properties if the edge has no properties or the receiver
// is nil. The returned Properties is immutable. Symmetric with [Edge.Properties]
// for resolved edges.
func (u *UnresolvedEdge) Properties() immutable.Properties {
	if u == nil {
		return immutable.Properties{}
	}
	return u.properties
}

// newDuplicate creates a Duplicate record. parent and relation are the
// composing coordinates, zero for a root duplicate.
func newDuplicate(instance, conflict, parent *Instance, relation string, diagnostic diag.Issue) *Duplicate {
	return &Duplicate{
		instance:   instance,
		conflict:   conflict,
		parent:     parent,
		relation:   relation,
		diagnostic: diagnostic,
	}
}

// newUnresolvedEdge creates an UnresolvedEdge record.
func newUnresolvedEdge(source *Instance, relation string, targetType schema.TypeID, targetKey string, required bool, reason string, properties immutable.Properties) *UnresolvedEdge {
	return &UnresolvedEdge{
		source:     source,
		relation:   relation,
		targetType: targetType,
		targetKey:  targetKey,
		required:   required,
		reason:     reason,
		properties: properties,
	}
}
