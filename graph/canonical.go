package graph

import (
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// canonicalizer rewrites reconstructed parts into the representation each
// value's schema constraint stores, so a graph rebuilt from parts holds what a
// graph built through validation holds.
//
// It caches, per type and per relation, the declared properties whose kind has
// a canonical form at all. A schema declaring no Timestamp, Date or UUID
// therefore costs one map lookup per instance and allocates nothing.
//
// Primary keys are canonicalized too, and every position that ADDRESSES an
// instance is canonicalized with them — an instance's own key, an edge's
// endpoint keys, a duplicate's key, its conflict and parent, an unresolved
// record's endpoints. That is the whole of the rule: a key moves only if every
// reference to it moves in the same pass.
//
// It was not always so. This type once left keys untouched on the ground that
// "the wire holds canonical text, so a loaded key is already canonical" — which
// was false for a key that entered through [instance.NewValidInstance], the
// bypass constructor that runs no validation. Canonicalizing at the WRITER
// instead left the graph indexing, sorting and resolving edges by one spelling
// while its document carried another; canonicalizing here makes one value have
// one spelling in the graph, and the document inherits it.
type canonicalizer struct {
	schema   *schema.Schema
	byType   map[schema.TypeID][]*schema.Property
	byEdge   map[edgeKey][]*schema.Property
	inactive bool
}

// edgeKey addresses a relation by the source type that declares it.
type edgeKey struct {
	source   schema.TypeID
	relation string
}

func newCanonicalizer(s *schema.Schema) *canonicalizer {
	return &canonicalizer{
		schema:   s,
		byType:   make(map[schema.TypeID][]*schema.Property),
		byEdge:   make(map[edgeKey][]*schema.Property),
		inactive: s == nil,
	}
}

// typeProps returns the declared properties of id whose kind canonicalizes.
func (c *canonicalizer) typeProps(id schema.TypeID) []*schema.Property {
	if ps, ok := c.byType[id]; ok {
		return ps
	}
	var ps []*schema.Property
	if t, ok := c.schema.TypeByID(id); ok {
		for p := range t.AllProperties() {
			if value.Canonicalizes(p.Constraint()) {
				ps = append(ps, p)
			}
		}
	}
	c.byType[id] = ps
	return ps
}

// edgeProps returns the properties of the named relation on the source type
// whose kind canonicalizes.
func (c *canonicalizer) edgeProps(source schema.TypeID, relation string) []*schema.Property {
	k := edgeKey{source: source, relation: relation}
	if ps, ok := c.byEdge[k]; ok {
		return ps
	}
	var ps []*schema.Property
	if t, ok := c.schema.TypeByID(source); ok {
		if rel, ok := t.Relation(relation); ok {
			for p := range rel.Properties() {
				if value.Canonicalizes(p.Constraint()) {
					ps = append(ps, p)
				}
			}
		}
	}
	c.byEdge[k] = ps
	return ps
}

// key rewrites a primary key component-wise under the declared key
// constraints of id. A component the constraint cannot render is left as it
// arrived, for the same reason apply leaves a property: this reconstructs
// rather than validates, and a graph assembled before the rule existed must
// still build.
func (c *canonicalizer) key(id schema.TypeID, k immutable.Key) immutable.Key {
	if c.inactive || k.Len() == 0 {
		return k
	}
	t, ok := c.schema.TypeByID(id)
	if !ok {
		return k
	}
	var out []any
	i := 0
	for pk := range t.PrimaryKeys() {
		if i >= k.Len() {
			break
		}
		comp := k.Get(i).Unwrap()
		if value.Canonicalizes(pk.Constraint()) {
			if canonical, err := value.Canonical(comp, pk.Constraint()); err == nil {
				if out == nil {
					out = k.Clone()
				}
				out[i] = canonical
			}
		}
		i++
	}
	if out == nil {
		return k
	}
	return immutable.WrapKey(out)
}

// apply rewrites the properties named by ps. A value the constraint cannot
// render is left as it arrived: RebuildSnapshot reconstructs a document rather
// than validating one, and a document written before this rule existed must
// still load.
func apply(props immutable.Properties, ps []*schema.Property) immutable.Properties {
	if len(ps) == 0 || props.Len() == 0 {
		return props
	}
	var m map[string]any
	for _, p := range ps {
		if _, ok := props.Get(p.Name()); !ok {
			continue
		}
		if m == nil {
			m = props.Clone()
		}
		raw := m[p.Name()]
		if raw == nil {
			continue
		}
		canonical, err := value.Canonical(raw, p.Constraint())
		if err != nil {
			continue
		}
		m[p.Name()] = canonical
	}
	if m == nil {
		return props
	}
	return immutable.WrapProperties(m)
}

// instance rewrites an instance's properties and those of its composed
// children, which carry their own types.
func (c *canonicalizer) instance(ip InstanceParts) InstanceParts {
	if c.inactive {
		return ip
	}
	ip.Properties = apply(ip.Properties, c.typeProps(ip.TypeID))
	ip.PrimaryKey = c.key(ip.TypeID, ip.PrimaryKey)

	if len(ip.Composed) > 0 {
		composed := make(map[string][]InstanceParts, len(ip.Composed))
		for rel, children := range ip.Composed {
			out := make([]InstanceParts, len(children))
			for i, child := range children {
				out[i] = c.instance(child)
			}
			composed[rel] = out
		}
		ip.Composed = composed
	}
	return ip
}

// edge rewrites an edge's own properties, whose constraints hang off the
// source type's relation rather than off either endpoint type.
func (c *canonicalizer) edge(ep EdgeParts) EdgeParts {
	if c.inactive {
		return ep
	}
	ep.Properties = apply(ep.Properties, c.edgeProps(ep.SourceType, ep.Relation))
	// Both endpoints move with the instances they address, or the edge stops
	// resolving against an index whose keys this same pass rewrote.
	ep.SourceKey = c.key(ep.SourceType, ep.SourceKey)
	ep.TargetKey = c.key(ep.TargetType, ep.TargetKey)
	return ep
}

// duplicate rewrites a duplicate record's three addresses.
func (c *canonicalizer) duplicate(dp DuplicateParts) DuplicateParts {
	if c.inactive {
		return dp
	}
	dp.Instance = c.instance(dp.Instance)
	dp.Key = c.key(dp.Type, dp.Key)
	dp.ConflictKey = c.key(dp.ConflictType, dp.ConflictKey)
	dp.ParentKey = c.key(dp.ParentType, dp.ParentKey)
	return dp
}

// unresolved rewrites a forward reference's edge properties, which reach the
// wire through the same relation a resolved edge's do, and its source address.
// The TARGET key is left as the caller wrote it: it addresses an instance that
// does not exist, so no type's constraints could render it.
func (c *canonicalizer) unresolved(up UnresolvedParts) UnresolvedParts {
	if c.inactive {
		return up
	}
	up.Properties = apply(up.Properties, c.edgeProps(up.SourceType, up.Relation))
	up.SourceKey = c.key(up.SourceType, up.SourceKey)
	return up
}
