package graph

import (
	"maps"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// Result is an immutable snapshot of the graph at a point in time.
//
// Result provides read-only access to all instances, edges, duplicates,
// and diagnostics. It is safe for concurrent read access from multiple
// goroutines.
//
// All slice-returning methods produce deterministically sorted output,
// independent of Add() call order or concurrency:
//   - [Result.Types]: lexicographic by type name
//   - [Result.InstancesOf]: lexicographic by primary key string
//   - [Result.Edges]: lexicographic tuple (sourceType, sourceKey, relation, targetType, targetKey)
//   - [Result.Duplicates]: lexicographic by (typeName, primaryKey)
//   - [Result.Unresolved]: lexicographic by (sourceType, sourceKey, relation, targetType, targetKey)
//
// The [Result.Instances] map has non-deterministic iteration order per Go semantics.
// For deterministic iteration, use Types() + InstancesOf().
type Result struct {
	// schema is the schema used for validation.
	schema *schema.Schema

	// types contains all type names in sorted order (instance tag form).
	types []string

	// instances maps type name to sorted instances.
	instances map[string][]*Instance

	// instanceIndex provides O(1) lookup by (type, key).
	instanceIndex map[string]map[string]*Instance

	// edges contains all resolved edges in sorted order.
	edges []*Edge

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
func (r *Result) Schema() *schema.Schema {
	if r == nil {
		return nil
	}
	return r.schema
}

// Types returns all type names in lexicographic order.
//
// The returned slice uses instance tag form:
//   - Local types: unqualified (e.g., "Person")
//   - Imported types: alias-qualified (e.g., "c.Entity")
//
// Use with [Result.InstancesOf] for deterministic iteration.
// Returns a defensive copy.
func (r *Result) Types() []string {
	if r == nil || len(r.types) == 0 {
		return nil
	}
	result := make([]string, len(r.types))
	copy(result, r.types)
	return result
}

// InstancesOf returns instances of the given type in sorted order.
//
// Instances are sorted by primary key using [FormatKey] string comparison.
// Returns nil if the type has no instances in the graph.
//
// The typeName must be in instance tag form:
//   - Local types: unqualified (e.g., "Person")
//   - Imported types: alias-qualified (e.g., "c.Entity")
//
// Returns a defensive copy.
func (r *Result) InstancesOf(typeName string) []*Instance {
	if r == nil || r.instances == nil {
		return nil
	}
	instances := r.instances[typeName]
	if len(instances) == 0 {
		return nil
	}
	result := make([]*Instance, len(instances))
	copy(result, instances)
	return result
}

// Instances returns all validated instances keyed by type name.
//
// WARNING: Map iteration order is non-deterministic per Go semantics.
// For deterministic iteration (CLI output, tests), use [Result.Types]
// with [Result.InstancesOf] instead.
//
// The map keys use instance tag form.
// Returns a shallow copy of the map; instance slices are shared.
func (r *Result) Instances() map[string][]*Instance {
	if r == nil || r.instances == nil {
		return nil
	}
	// Shallow copy the map; slices are already sorted snapshots
	result := make(map[string][]*Instance, len(r.instances))
	maps.Copy(result, r.instances)
	return result
}

// InstanceByKey looks up a single instance by type name and primary key.
//
// The key must be in canonical string form (use [FormatKey] to convert values).
// Returns (nil, false) if no matching instance exists.
//
// The typeName must be in instance tag form.
func (r *Result) InstanceByKey(typeName, key string) (*Instance, bool) {
	if r == nil || r.instanceIndex == nil {
		return nil, false
	}
	typeIndex := r.instanceIndex[typeName]
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
func (r *Result) Edges() []*Edge {
	if r == nil || len(r.edges) == 0 {
		return nil
	}
	result := make([]*Edge, len(r.edges))
	copy(result, r.edges)
	return result
}

// Diagnostics returns validation issues from graph construction.
//
// This includes errors and warnings from [Graph.Add] and [Graph.AddComposed] calls.
// [Graph.Check] results are returned separately per-call and are not accumulated
// here, making Check idempotent: multiple calls have no effect on snapshot diagnostics.
//
// Use [diag.Result.OK] to check if the graph construction succeeded.
func (r *Result) Diagnostics() diag.Result {
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
func (r *Result) Duplicates() []*Duplicate {
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
func (r *Result) Unresolved() []*UnresolvedEdge {
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
func (r *Result) OK() bool {
	if r == nil {
		return true
	}
	return r.diagnostics.OK()
}

// HasErrors reports whether the graph has any errors.
//
// This is a convenience method equivalent to r.Diagnostics().HasErrors().
func (r *Result) HasErrors() bool {
	if r == nil {
		return false
	}
	return r.diagnostics.HasErrors()
}

// newResult creates a Result from sorted graph data.
// All slices must already be sorted according to the ordering guarantees.
func newResult(
	s *schema.Schema,
	types []string,
	instances map[string][]*Instance,
	instanceIndex map[string]map[string]*Instance,
	edges []*Edge,
	duplicates []*Duplicate,
	unresolved []*UnresolvedEdge,
	diagnostics diag.Result,
) *Result {
	return &Result{
		schema:        s,
		types:         types,
		instances:     instances,
		instanceIndex: instanceIndex,
		edges:         edges,
		duplicates:    duplicates,
		unresolved:    unresolved,
		diagnostics:   diagnostics,
	}
}
