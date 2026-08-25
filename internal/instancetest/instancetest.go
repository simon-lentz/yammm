package instancetest

import (
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// viConfig collects the optional fields of a built ValidInstance.
type viConfig struct {
	typeID     schema.TypeID
	pk         immutable.Key
	props      immutable.Properties
	edges      map[string]*instance.ValidEdgeData
	composed   map[string]immutable.Value
	provenance *location.Provenance
}

// VIOption customizes a [VI]-built instance.
type VIOption func(*viConfig)

// TypeID sets the schema type identity (defaults to the zero TypeID).
func TypeID(id schema.TypeID) VIOption {
	return func(c *viConfig) { c.typeID = id }
}

// PK sets the primary key from its component values
// (defaults to the single-component key [1]).
func PK(parts ...any) VIOption {
	return func(c *viConfig) { c.pk = immutable.WrapKey(parts) }
}

// Props sets the instance properties (defaults to none).
func Props(m map[string]any) VIOption {
	return func(c *viConfig) { c.props = immutable.WrapProperties(m) }
}

// Edges sets the association edge data (defaults to none).
func Edges(edges map[string]*instance.ValidEdgeData) VIOption {
	return func(c *viConfig) { c.edges = edges }
}

// Composed sets the composition children (defaults to none).
func Composed(composed map[string]immutable.Value) VIOption {
	return func(c *viConfig) { c.composed = composed }
}

// Provenance sets the source provenance (defaults to none).
func Provenance(p *location.Provenance) VIOption {
	return func(c *viConfig) { c.provenance = p }
}

// VI builds a *instance.ValidInstance for tests, defaulting every field a
// scenario does not name: zero TypeID, primary key [1], and nil
// properties, edges, compositions, and provenance. It wraps the 7-argument
// [instance.NewValidInstance] so call sites state only what they test.
func VI(typeName string, opts ...VIOption) *instance.ValidInstance {
	c := viConfig{
		pk:    immutable.WrapKey([]any{int64(1)}),
		props: immutable.WrapProperties(nil),
	}
	for _, opt := range opts {
		opt(&c)
	}
	return instance.NewValidInstance(
		typeName, c.typeID, c.pk, c.props, c.edges, c.composed, c.provenance,
	)
}
