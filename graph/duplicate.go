package graph

import (
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
)

// Duplicate records a duplicate primary key detected during graph construction.
//
// When an instance is added with a primary key that already exists for the same
// type, a Duplicate is created to track both the new instance (which is rejected)
// and the existing instance (which remains in the graph).
//
// # Composed Children Not Included
//
// The Instance field contains the rejected instance without its composed children.
// This is because duplicate detection occurs before composition extraction, so
// composed children from the rejected instance are never processed. If you need
// to inspect composed children from duplicate data, access them from the original
// [instance.ValidInstance] passed to [Graph.Add].
//
// Duplicates are accessed via [Result.Duplicates].
type Duplicate struct {
	// Instance is the instance that was rejected due to duplicate PK.
	// This is the instance passed to Add() that was not added to the graph.
	Instance *Instance

	// Conflict is the existing instance in the graph that has the same PK.
	// This instance remains in the graph.
	Conflict *Instance

	// Diagnostic contains the E_DUPLICATE_PK or E_DUPLICATE_COMPOSED_PK issue
	// with details about the conflict.
	//
	// For snapshots loaded from persisted .ys files, this field is zero-valued
	// because construction diagnostics are transient and not persisted. Use
	// [Duplicate.HasDiagnostic] to guard access to this field in code that may
	// operate on both constructed and loaded snapshots.
	Diagnostic diag.Issue
}

// HasDiagnostic reports whether this duplicate record has an associated
// construction diagnostic.
//
// Returns false for duplicates loaded from persisted snapshots (.ys files),
// where construction diagnostics are transient and not persisted. The
// structural data (Instance, Conflict) is always available regardless of
// this method's return value.
func (d *Duplicate) HasDiagnostic() bool {
	return !d.Diagnostic.Code().IsZero()
}

// UnresolvedEdge records an association edge whose target was not found in the graph.
//
// Unresolved edges occur when:
//   - The target instance was not added yet (Reason: "target_missing")
//   - A required association field is absent from the data (Reason: "absent")
//   - A required association array is empty (Reason: "empty")
//
// For required associations (Required: true), unresolved edges cause [Graph.Check]
// to report E_UNRESOLVED_REQUIRED diagnostics. Optional associations (Required: false)
// may remain unresolved without error.
//
// Unresolved edges are accessed via [Result.Unresolved].
type UnresolvedEdge struct {
	// Source is the instance that declares the unresolved reference.
	Source *Instance

	// Relation is the DSL relation name (e.g., "OWNER").
	Relation string

	// TargetType is the expected target type name in instance tag form.
	// This is resolved from the schema's relation definition.
	TargetType string

	// TargetKey is the foreign key value in canonical string form.
	// This is the FormatKey() output of the FK values.
	// Empty for "absent" and "empty" reasons.
	TargetKey string

	// Required indicates whether this association is required by the schema.
	// When true, this unresolved edge will cause Check() to emit E_UNRESOLVED_REQUIRED.
	Required bool

	// Reason explains why the edge is unresolved:
	//   - "target_missing": the target instance is not in the graph
	//   - "absent": the association field is missing from the instance data
	//   - "empty": the association array is present but empty
	Reason string

	// properties carries the DSL-declared edge property values from the
	// forward reference. Populated only for Reason "target_missing" —
	// "absent" and "empty" describe a missing/empty target reference
	// that never had a target to attach properties to, so properties
	// is the zero-value immutable.Properties there.
	//
	// Accessed via [UnresolvedEdge.Property] and [UnresolvedEdge.Properties].
	// Survives Marshal/Load round-trips in .ys wire format v2 and later
	// (v1 documents produced before v0.3.0 have no properties field;
	// v2 readers load them as empty Properties, which is lossless —
	// v1 never carried the data).
	properties immutable.Properties
}

// Property returns the value for the given edge property name and true if it exists.
// Returns (zero Value, false) if the property does not exist or the receiver is nil.
//
// Edge properties on unresolved edges are declared on the forward reference
// via the yammm DSL; they survive Marshal/Load in .ys wire format v2 and
// later. Symmetric with [Edge.Property] for resolved edges.
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

// newDuplicate creates a Duplicate record.
func newDuplicate(instance, conflict *Instance, diagnostic diag.Issue) *Duplicate {
	return &Duplicate{
		Instance:   instance,
		Conflict:   conflict,
		Diagnostic: diagnostic,
	}
}

// newUnresolvedEdge creates an UnresolvedEdge record.
func newUnresolvedEdge(source *Instance, relation, targetType, targetKey string, required bool, reason string, properties immutable.Properties) *UnresolvedEdge {
	return &UnresolvedEdge{
		Source:     source,
		Relation:   relation,
		TargetType: targetType,
		TargetKey:  targetKey,
		Required:   required,
		Reason:     reason,
		properties: properties,
	}
}
