package instance

import (
	"iter"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// ValidInstance is the immutable output of successful validation. A non-zero
// one comes from a [Validator] alone.
//
// ValidInstance contains the validated properties, extracted primary key,
// validated edge data, composed children and provenance. All values are immutable and safe to share.
type ValidInstance struct{ *validInstanceState }

// IsZero reports whether the handle names no record.
func (v ValidInstance) IsZero() bool { return v.validInstanceState == nil }

// validInstanceState is the record a [ValidInstance] names.
type validInstanceState struct {
	typeName   string
	typ        schema.Type
	primaryKey immutable.Key
	properties immutable.Properties
	edges      map[string]ValidEdgeData
	composed   map[string]immutable.Value
	provenance location.Provenance

	// scope is the instance's invariant scope, built once by
	// [Validator.scopeOf] and shared by the instance's own invariants and by
	// every parent that holds the instance as a composed child.
	scope scopeMemo
}

// scopeMemo holds one instance's invariant scope, built on first use. It
// lives on the instance so that a parent's scope can hold the child's
// scope itself rather than rebuild the child's subtree at every level.
type scopeMemo struct {
	once sync.Once
	m    immutable.Map[string]
}

// newValidInstance adopts edges and composed, which the validator owns.
func newValidInstance(
	typeName string,
	typ schema.Type,
	pk immutable.Key,
	props immutable.Properties,
	edges map[string]ValidEdgeData,
	composed map[string]immutable.Value,
	provenance location.Provenance,
) ValidInstance {
	return ValidInstance{&validInstanceState{
		typeName:   typeName,
		typ:        typ,
		primaryKey: pk,
		properties: props,
		edges:      edges,
		composed:   composed,
		provenance: provenance,
	}}
}

// TypeName returns the name of the validated type.
func (v ValidInstance) TypeName() string {
	if v.IsZero() {
		return ""
	}
	return v.typeName
}

// Type returns the type that validated this instance: one handle of the schema
// load its validator was built over. Two loads of one source hold two handles,
// so == tells them apart where a [schema.TypeID] cannot.
func (v ValidInstance) Type() schema.Type {
	if v.IsZero() {
		return schema.Type{}
	}
	return v.typ
}

// PrimaryKey returns the extracted primary key.
func (v ValidInstance) PrimaryKey() immutable.Key {
	if v.IsZero() {
		return immutable.Key{}
	}
	return v.primaryKey
}

// Property returns the value of a property by name.
// Returns (zero, false) if the property is not set.
func (v ValidInstance) Property(name string) (immutable.Value, bool) {
	if v.IsZero() {
		return immutable.Value{}, false
	}
	return v.properties.Get(name)
}

// Properties returns all validated properties.
func (v ValidInstance) Properties() immutable.Properties {
	if v.IsZero() {
		return immutable.Properties{}
	}
	return v.properties
}

// Edge returns the validated edge data for a relation.
// Returns the zero ValidEdgeData and false if the relation is absent or the
// receiver is zero. A many association given an empty array returns a
// non-zero ValidEdgeData with no targets, and true. A (one) association refuses an
// empty array (E_EDGE_SHAPE_MISMATCH), and a composition is not in Edge.
func (v ValidInstance) Edge(relationName string) (ValidEdgeData, bool) {
	if v.IsZero() {
		return ValidEdgeData{}, false
	}
	if edge, ok := v.edges[relationName]; ok {
		return edge, true
	}
	// The field-name spelling: the DSL name lower-cased, the rule every
	// name-taking surface of this package accepts.
	for name, edge := range v.edges {
		if strings.ToLower(name) == relationName {
			return edge, true
		}
	}
	return ValidEdgeData{}, false
}

// Provenance returns the source location metadata.
// Returns the zero Provenance if none was provided.
func (v ValidInstance) Provenance() location.Provenance {
	if v.IsZero() {
		return location.Provenance{}
	}
	return v.provenance
}

// Edges returns an iterator over all edges.
// The iteration order is not guaranteed to be deterministic.
func (v ValidInstance) Edges() iter.Seq2[string, ValidEdgeData] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var edges map[string]ValidEdgeData
	if !v.IsZero() {
		edges = v.edges
	}
	return maps.All(edges)
}

// Compositions returns an iterator over all compositions. Each value holds an
// [immutable.Slice] of non-zero ValidInstance values, in input order.
// The iteration order is not guaranteed to be deterministic.
func (v ValidInstance) Compositions() iter.Seq2[string, immutable.Value] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var composed map[string]immutable.Value
	if !v.IsZero() {
		composed = v.composed
	}
	return maps.All(composed)
}

// ValidEdgeData represents validated association edge data.
//
// ValidEdgeData contains the targets for an association relation,
// each with its foreign key and optional edge properties.
type ValidEdgeData struct{ *validEdgeDataState }

// IsZero reports whether the handle names no record.
func (e ValidEdgeData) IsZero() bool { return e.validEdgeDataState == nil }

// validEdgeDataState is the record a [ValidEdgeData] names.
type validEdgeDataState struct {
	targets []ValidEdgeTarget
}

// Targets returns an iterator over the edge's targets in input order.
func (e ValidEdgeData) Targets() iter.Seq[ValidEdgeTarget] {
	// One return lets the compiler resolve the closure at the call, so a range over it keeps its variables off the heap.
	var targets []ValidEdgeTarget
	if !e.IsZero() {
		targets = e.targets
	}
	return slices.Values(targets)
}

// IsEmpty returns true if there are no edge targets.
func (e ValidEdgeData) IsEmpty() bool {
	return e.IsZero() || len(e.targets) == 0
}

// ValidEdgeTarget represents a single edge target.
//
// ValidEdgeTarget contains the foreign key referencing the target instance
// and any validated edge properties.
type ValidEdgeTarget struct {
	targetKey  immutable.Key
	properties immutable.Properties
}

// TargetKey returns the foreign key referencing the target instance.
func (t *ValidEdgeTarget) TargetKey() immutable.Key {
	return t.targetKey
}

// Properties returns the edge properties.
func (t *ValidEdgeTarget) Properties() immutable.Properties {
	return t.properties
}

// Property returns a single edge property by name.
func (t *ValidEdgeTarget) Property(name string) (immutable.Value, bool) {
	return t.properties.Get(name)
}

// HasProperties reports whether this edge target has any properties.
func (t *ValidEdgeTarget) HasProperties() bool {
	return t.properties.Len() > 0
}
