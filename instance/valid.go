package instance

import (
	"iter"
	"slices"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// ValidInstance is the immutable output of successful validation.
//
// ValidInstance contains the validated properties, extracted primary key,
// and validated edge data. All values are immutable and safe to share.
type ValidInstance struct {
	typeName   string
	typeID     schema.TypeID
	primaryKey immutable.Key
	properties immutable.Properties
	edges      map[string]*ValidEdgeData
	composed   map[string]immutable.Value
	provenance *Provenance
}

// NewValidInstance creates a new ValidInstance.
// This is an internal constructor; use [Validator] to create instances.
func NewValidInstance(
	typeName string,
	typeID schema.TypeID,
	pk immutable.Key,
	props immutable.Properties,
	edges map[string]*ValidEdgeData,
	composed map[string]immutable.Value,
	provenance *Provenance,
) *ValidInstance {
	return &ValidInstance{
		typeName:   typeName,
		typeID:     typeID,
		primaryKey: pk,
		properties: props,
		edges:      edges,
		composed:   composed,
		provenance: provenance,
	}
}

// TypeName returns the name of the validated type.
func (v *ValidInstance) TypeName() string {
	return v.typeName
}

// TypeID returns the schema type ID.
func (v *ValidInstance) TypeID() schema.TypeID {
	return v.typeID
}

// PrimaryKey returns the extracted primary key.
func (v *ValidInstance) PrimaryKey() immutable.Key {
	return v.primaryKey
}

// Property returns the value of a property by name.
// Returns (zero, false) if the property is not set.
func (v *ValidInstance) Property(name string) (immutable.Value, bool) {
	return v.properties.Get(name)
}

// Properties returns all validated properties.
func (v *ValidInstance) Properties() immutable.Properties {
	return v.properties
}

// Edge returns the validated edge data for a relation.
// Returns (nil, false) if the relation has no edges.
func (v *ValidInstance) Edge(relationName string) (*ValidEdgeData, bool) {
	if v.edges == nil {
		return nil, false
	}
	edge, ok := v.edges[relationName]
	return edge, ok
}

// Composed returns the validated composed children for a composition relation.
// Returns (zero, false) if the relation has no composed children.
func (v *ValidInstance) Composed(relationName string) (immutable.Value, bool) {
	if v.composed == nil {
		return immutable.Value{}, false
	}
	val, ok := v.composed[relationName]
	return val, ok
}

// Provenance returns the source location metadata.
// Returns nil if no provenance was provided.
func (v *ValidInstance) Provenance() *Provenance {
	return v.provenance
}

// HasProvenance reports whether provenance is available.
func (v *ValidInstance) HasProvenance() bool {
	return v.provenance != nil
}

// Edges returns an iterator over all edges.
// The iteration order is not guaranteed to be deterministic.
func (v *ValidInstance) Edges() iter.Seq2[string, *ValidEdgeData] {
	return func(yield func(string, *ValidEdgeData) bool) {
		for name, edge := range v.edges {
			if !yield(name, edge) {
				return
			}
		}
	}
}

// Compositions returns an iterator over all compositions.
// The iteration order is not guaranteed to be deterministic.
func (v *ValidInstance) Compositions() iter.Seq2[string, immutable.Value] {
	return func(yield func(string, immutable.Value) bool) {
		for name, comp := range v.composed {
			if !yield(name, comp) {
				return
			}
		}
	}
}

// ValidEdgeData represents validated association edge data.
//
// ValidEdgeData contains the targets for an association relation,
// each with its foreign key and optional edge properties.
type ValidEdgeData struct {
	targets []ValidEdgeTarget
}

// NewValidEdgeData creates a new ValidEdgeData.
func NewValidEdgeData(targets []ValidEdgeTarget) *ValidEdgeData {
	return &ValidEdgeData{targets: targets}
}

// Targets returns a defensive copy of all edge targets.
// Callers may modify the returned slice without affecting the original data.
func (e *ValidEdgeData) Targets() []ValidEdgeTarget {
	if e == nil {
		return nil
	}
	return slices.Clone(e.targets)
}

// TargetsIter returns an iterator over edge targets for efficient read-only traversal.
// Unlike Targets(), this does not allocate a new slice.
//
// Example:
//
//	for target := range edge.TargetsIter() {
//	    fmt.Println(target.Key())
//	}
func (e *ValidEdgeData) TargetsIter() iter.Seq[ValidEdgeTarget] {
	return func(yield func(ValidEdgeTarget) bool) {
		if e == nil {
			return
		}
		for _, t := range e.targets {
			if !yield(t) {
				return
			}
		}
	}
}

// TargetCount returns the number of edge targets.
func (e *ValidEdgeData) TargetCount() int {
	if e == nil {
		return 0
	}
	return len(e.targets)
}

// IsEmpty returns true if there are no edge targets.
func (e *ValidEdgeData) IsEmpty() bool {
	return e == nil || len(e.targets) == 0
}

// ValidEdgeTarget represents a single edge target.
//
// ValidEdgeTarget contains the foreign key referencing the target instance
// and any validated edge properties.
type ValidEdgeTarget struct {
	targetKey  immutable.Key
	properties immutable.Properties
}

// NewValidEdgeTarget creates a new ValidEdgeTarget.
func NewValidEdgeTarget(targetKey immutable.Key, props immutable.Properties) ValidEdgeTarget {
	return ValidEdgeTarget{
		targetKey:  targetKey,
		properties: props,
	}
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
