package graph

import (
	"iter"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// canonicalizer rewrites reconstructed parts into the representation each
// value's schema constraint stores, so a graph rebuilt from parts holds what a
// graph built through validation holds.
//
// It records, once at construction and for every type and relation in the
// schema's closure, the declared properties and key positions whose kind has a
// canonical form at all. The maps are never written after that, so any number of
// goroutines read them with no lock; a schema declaring no
// Timestamp, Date or UUID anywhere is inactive and every method returns its
// input untouched.
//
// A primary key moves only at a door that takes one from outside the graph:
// the import, which re-keys an instance and every reference to it under the
// importing schema, and an address a caller looks an instance up by. The
// rebuild takes keys as the document writes them, since the reader refuses a
// key component its kind stores as other text. One value then has one spelling
// in the graph, which indexes, sorts and resolves edges by it.
type canonicalizer struct {
	byType    map[schema.TypeID][]schema.Property
	byEdge    map[edgeKey][]schema.Property
	byKeyType map[schema.TypeID][]keyPosition
	inactive  bool
}

// keyPosition names one primary-key component whose kind canonicalizes: its
// index in the key and the property declaring its constraint.
type keyPosition struct {
	index int
	prop  schema.Property
}

// edgeKey addresses a relation by the source type that declares it.
type edgeKey struct {
	source   schema.TypeID
	relation string
}

// newCanonicalizer walks every type in s's closure once. An entry is written
// for every type and relation, empty where nothing canonicalizes, so a later
// read never misses and never writes. s is not zero: [New] and snapshot.Load
// refuse a zero schema first.
func newCanonicalizer(s schema.Schema) *canonicalizer {
	c := &canonicalizer{
		byType:    make(map[schema.TypeID][]schema.Property),
		byEdge:    make(map[edgeKey][]schema.Property),
		byKeyType: make(map[schema.TypeID][]keyPosition),
		inactive:  true,
	}
	for sc := range s.Closure() {
		for t := range sc.Types() {
			id := t.ID()
			var props []schema.Property
			for p := range t.AllProperties() {
				if value.Canonicalizes(p.Constraint()) {
					props = append(props, p)
				}
			}
			c.byType[id] = props
			var positions []keyPosition
			i := 0
			for pk := range t.PrimaryKeys() {
				if value.Canonicalizes(pk.Constraint()) {
					positions = append(positions, keyPosition{index: i, prop: pk})
				}
				i++
			}
			c.byKeyType[id] = positions
			c.inactive = c.inactive && len(props) == 0 && len(positions) == 0
			for _, rels := range []iterRelations{t.AllAssociations(), t.AllCompositions()} {
				for rel := range rels {
					var eps []schema.Property
					for p := range rel.Properties() {
						if value.Canonicalizes(p.Constraint()) {
							eps = append(eps, p)
						}
					}
					c.byEdge[edgeKey{source: id, relation: rel.Name()}] = eps
					c.inactive = c.inactive && len(eps) == 0
				}
			}
		}
	}
	return c
}

// iterRelations is the shape both relation iterators share.
type iterRelations = iter.Seq[schema.Relation]

// typeProps returns the declared properties of id whose kind canonicalizes.
func (c *canonicalizer) typeProps(id schema.TypeID) []schema.Property {
	return c.byType[id]
}

// edgeProps returns the properties of the named relation on the source type
// whose kind canonicalizes.
func (c *canonicalizer) edgeProps(source schema.TypeID, relation string) []schema.Property {
	return c.byEdge[edgeKey{source: source, relation: relation}]
}

// keyPositions returns the primary-key positions of id whose kind
// canonicalizes.
func (c *canonicalizer) keyPositions(id schema.TypeID) []keyPosition {
	return c.byKeyType[id]
}

// key rewrites a primary key component-wise under the declared key
// constraints of id. A component the constraint cannot render is left as it
// arrived, for the same reason apply leaves a property: this reconstructs
// rather than validates.
func (c *canonicalizer) key(id schema.TypeID, k immutable.Key) immutable.Key {
	if c.inactive || k.Len() == 0 {
		return k
	}
	var out []any
	for _, pos := range c.keyPositions(id) {
		if pos.index >= k.Len() {
			break
		}
		canonical, err := value.Canonical(k.Get(pos.index).Unwrap(), pos.prop.Constraint())
		if err != nil {
			continue
		}
		if out == nil {
			out = k.Clone()
		}
		out[pos.index] = canonical
	}
	if out == nil {
		return k
	}
	return immutable.WrapKey(out)
}

// address renders a FormatKey-form address as the index holds it: parsed,
// then rewritten component-wise by key. A key with no canonicalizing position
// is returned as spelled, unparsed, so a String key stays one map read; so is
// a string ParseKey refuses, which addresses nothing under any spelling.
func (c *canonicalizer) address(id schema.TypeID, key string) string {
	if c.inactive || len(c.keyPositions(id)) == 0 {
		return key
	}
	components, err := ParseKey(key)
	if err != nil {
		return key
	}
	return c.key(id, immutable.WrapKey(components)).String()
}

// canonicalOrRaw is v in the form c stores, or v as it arrived where c has no
// canonical form, where c cannot render it, or where nothing canonicalizes at
// all. It compares two values that must agree; it never decides an address.
func (c *canonicalizer) canonicalOrRaw(v immutable.Value, con schema.Constraint) any {
	raw := v.Unwrap()
	if c.inactive || !value.Canonicalizes(con) {
		return raw
	}
	canonical, err := value.Canonical(raw, con)
	if err != nil {
		return raw
	}
	return canonical
}

// properties rewrites an instance's properties under the declared constraints
// of id. The import and the rebuild path both reach it, so each stores what a
// validator stores.
func (c *canonicalizer) properties(id schema.TypeID, props immutable.Properties) immutable.Properties {
	if c.inactive {
		return props
	}
	return apply(props, c.typeProps(id))
}

// apply rewrites the properties named by ps. A value the constraint cannot
// render is left as it arrived: the reader reconstructs a document rather than
// validating one, and a document written before this rule existed must still
// load.
func apply(props immutable.Properties, ps []schema.Property) immutable.Properties {
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
// children, which carry their own types. Its key stays as the document wrote
// it.
func (c *canonicalizer) instance(ip snapparts.Instance) snapparts.Instance {
	if c.inactive {
		return ip
	}
	ip.Properties = c.properties(ip.TypeID, ip.Properties)

	if len(ip.Composed) > 0 {
		composed := make(map[string][]snapparts.Instance, len(ip.Composed))
		for rel, children := range ip.Composed {
			out := make([]snapparts.Instance, len(children))
			for i, child := range children {
				out[i] = c.instance(child)
			}
			composed[rel] = out
		}
		ip.Composed = composed
	}
	return ip
}

// reinstance rewrites a freshly cloned Instance and its composed children in
// place, as instance rewrites its parts. The clone is not yet reachable
// from the graph, so writing it is safe; key is idempotent, so an already
// canonical instance is unchanged.
func (c *canonicalizer) reinstance(inst Instance) {
	if c.inactive {
		return
	}
	inst.primaryKey = c.key(inst.typeID, inst.primaryKey)
	inst.properties = c.properties(inst.typeID, inst.properties)
	for _, children := range inst.composed {
		for _, child := range children {
			c.reinstance(child)
		}
	}
}

// edgeProperties rewrites an edge's own properties under the constraints the
// source type's relation declares. The import and the rebuild path both reach
// it, so each stores what a validator stores. An
// edge with no property holds the zero Properties on every path, because a
// document's {} cannot say which empty value the writer held.
func (c *canonicalizer) edgeProperties(source schema.TypeID, relation string, props immutable.Properties) immutable.Properties {
	if props.Len() == 0 {
		return immutable.Properties{}
	}
	if c.inactive {
		return props
	}
	return apply(props, c.edgeProps(source, relation))
}
