package instance

import (
	"maps"

	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

// InstanceBuilder constructs RawInstance values for testing and programmatic use.
// It provides a fluent API for building property maps with correct edge object
// and composition structure.
//
// InstanceBuilder is NOT thread-safe; use separate builders per goroutine.
// The builder can be reused after Build(), but modifications will not affect
// previously built instances (properties are copied on build).
type InstanceBuilder struct {
	properties map[string]any
	provenance *Provenance
}

// EdgeBuilder constructs edge objects for associations.
// Use NewEdge() for building edges to pass to InstanceBuilder.Edges(),
// or use InstanceBuilder.Edge() which returns an EdgeBuilder for inline chaining.
type EdgeBuilder struct {
	parent       *InstanceBuilder // nil if standalone (created via NewEdge())
	data         map[string]any
	relationName string // stored for Done() attachment
}

// NewInstance starts building a RawInstance.
func NewInstance() *InstanceBuilder {
	return &InstanceBuilder{
		properties: make(map[string]any),
	}
}

// Prop sets a property value.
func (b *InstanceBuilder) Prop(name string, value any) *InstanceBuilder {
	b.properties[name] = value
	return b
}

// Edge starts building an edge object for a one/optional-one association.
// Returns an EdgeBuilder for fluent configuration; call Done() to return to this builder.
func (b *InstanceBuilder) Edge(relationName string) *EdgeBuilder {
	return &EdgeBuilder{
		parent:       b,
		data:         make(map[string]any),
		relationName: relationName,
	}
}

// Edges sets a many-association as an array of edge objects.
// Each EdgeBuilder should be built via NewEdge() rather than InstanceBuilder.Edge().
func (b *InstanceBuilder) Edges(relationName string, edges ...*EdgeBuilder) *InstanceBuilder {
	arr := make([]any, len(edges))
	for i, e := range edges {
		arr[i] = e.Build()
	}
	b.properties[relationName] = arr
	return b
}

// Composed embeds a single composed child instance for a one/optional-one composition.
func (b *InstanceBuilder) Composed(relationName string, child *InstanceBuilder) *InstanceBuilder {
	b.properties[relationName] = child.buildProperties()
	return b
}

// ComposedMany embeds multiple composed children for a many-composition.
func (b *InstanceBuilder) ComposedMany(relationName string, children ...*InstanceBuilder) *InstanceBuilder {
	arr := make([]any, len(children))
	for i, c := range children {
		arr[i] = c.buildProperties()
	}
	b.properties[relationName] = arr
	return b
}

// WithProvenance sets source location metadata for diagnostics.
// The path follows instance/path canonical syntax (e.g., "$.Person[0]").
// If the path cannot be parsed, it falls back to the root path.
func (b *InstanceBuilder) WithProvenance(sourceName, pathStr string) *InstanceBuilder {
	p, err := path.Parse(pathStr)
	if err != nil {
		p = path.Root()
	}
	b.provenance = NewProvenance(sourceName, p, location.Span{})
	return b
}

// WithFullProvenance sets complete provenance including span information.
func (b *InstanceBuilder) WithFullProvenance(p *Provenance) *InstanceBuilder {
	b.provenance = p
	return b
}

// Build returns the constructed RawInstance.
// The builder can be reused after Build() but modifications will not affect
// previously built instances (the properties map is copied).
func (b *InstanceBuilder) Build() RawInstance {
	return RawInstance{
		Properties: b.buildProperties(),
		Provenance: b.provenance,
	}
}

// buildProperties returns a shallow copy of the properties map.
func (b *InstanceBuilder) buildProperties() map[string]any {
	props := make(map[string]any, len(b.properties))
	maps.Copy(props, b.properties)
	return props
}

// NewEdge creates a standalone EdgeBuilder for use with InstanceBuilder.Edges().
func NewEdge() *EdgeBuilder {
	return &EdgeBuilder{
		data: make(map[string]any),
	}
}

// Target sets the _target_id field for simple (single-field) primary keys.
func (e *EdgeBuilder) Target(id any) *EdgeBuilder {
	e.data["_target_id"] = id
	return e
}

// TargetField sets a specific _target_<fieldName> for composite primary keys.
// Call multiple times for each PK component.
// The fieldName must match the schema PK property name exactly (case-sensitive).
func (e *EdgeBuilder) TargetField(fieldName string, value any) *EdgeBuilder {
	e.data["_target_"+fieldName] = value
	return e
}

// Prop sets an edge property value.
func (e *EdgeBuilder) Prop(name string, value any) *EdgeBuilder {
	e.data[name] = value
	return e
}

// Done returns to the parent InstanceBuilder (for inline chaining).
// Panics if called on a standalone EdgeBuilder (created via NewEdge()).
func (e *EdgeBuilder) Done() *InstanceBuilder {
	if e.parent == nil {
		panic("instance.EdgeBuilder.Done: cannot call Done on standalone EdgeBuilder (use Build instead)")
	}
	e.parent.properties[e.relationName] = e.Build()
	return e.parent
}

// Build returns the edge object as a map.
// This copies the internal data to ensure immutability.
func (e *EdgeBuilder) Build() map[string]any {
	result := make(map[string]any, len(e.data))
	maps.Copy(result, e.data)
	return result
}
