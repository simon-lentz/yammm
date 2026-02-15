package graph

import (
	"github.com/simon-lentz/yammm/immutable"
)

// Edge represents a resolved association edge between two instances.
//
// Edges are created when an instance references another instance via a
// relationship (association). The edge connects the source instance to
// the target instance and may carry optional edge properties.
//
// Edge is safe for concurrent read access from multiple goroutines.
//
// Edges are accessed via [Result.Edges].
type Edge struct {
	// relation is the DSL relation name (e.g., "OWNER", "WORKS_AT").
	relation string

	// source is the instance that declares the relationship.
	source *Instance

	// target is the instance being referenced.
	target *Instance

	// properties contains optional edge property values.
	// May be empty if the relationship has no declared properties.
	properties immutable.Properties
}

// Relation returns the DSL relation name for this edge.
//
// This is the relation name as declared in the schema (e.g., "OWNER"),
// not the field name used in instance data (which uses lower_snake form).
func (e *Edge) Relation() string {
	if e == nil {
		return ""
	}
	return e.relation
}

// Source returns the instance that declares this relationship.
//
// The source instance is always non-nil for edges returned by [Result.Edges].
func (e *Edge) Source() *Instance {
	if e == nil {
		return nil
	}
	return e.source
}

// Target returns the instance being referenced.
//
// The target instance is always non-nil for resolved edges returned by
// [Result.Edges]. Unresolved edges (where the target is not in the graph)
// are reported separately via [Result.Unresolved].
func (e *Edge) Target() *Instance {
	if e == nil {
		return nil
	}
	return e.target
}

// Property returns the value for the given edge property name and true if it exists.
// Returns (zero Value, false) if the property does not exist.
//
// Edge properties are declared on relationships in the schema. Not all
// relationships have properties; use [Edge.HasProperties] to check.
func (e *Edge) Property(name string) (immutable.Value, bool) {
	if e == nil {
		return immutable.Value{}, false
	}
	return e.properties.Get(name)
}

// Properties returns all edge property values.
//
// Returns an empty Properties if the edge has no properties.
// The returned Properties is immutable.
func (e *Edge) Properties() immutable.Properties {
	if e == nil {
		return immutable.Properties{}
	}
	return e.properties
}

// HasProperties reports whether this edge has any properties.
func (e *Edge) HasProperties() bool {
	if e == nil {
		return false
	}
	return e.properties.Len() > 0
}

// newEdge creates an Edge from graph-internal data.
// This is an internal constructor; edges are created during graph construction.
func newEdge(relation string, source, target *Instance, properties immutable.Properties) *Edge {
	return &Edge{
		relation:   relation,
		source:     source,
		target:     target,
		properties: properties,
	}
}
