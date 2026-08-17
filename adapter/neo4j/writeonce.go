package neo4j

import (
	"slices"

	"github.com/simon-lentz/yammm/schema"
)

// ImmutableKeysFor returns the property names of t marked @writeOnce (own and
// inherited), sorted and deduplicated.
//
// These feed the immutable-key derivation on the node write path: a @writeOnce
// property is set when a node is first created and not rewritten on a
// subsequent MERGE. Load-time validation guarantees no @writeOnce key is a
// primary-key member (sole or composite), so a derived key can never collide
// with a MERGE match key. A subtype whose own body re-declares an annotated
// parent property (identically or by narrowing) without re-stating @writeOnce
// drops it from that subtype's derived set — the load warns via
// W_ANNOTATION_SHADOWED. Returns nil for a nil type or one with no @writeOnce
// properties.
//
// The result unions with any keys passed via [WithImmutableKeys]; see that
// option for the effective-set contract.
func ImmutableKeysFor(t *schema.Type) []string {
	if t == nil {
		return nil
	}
	var keys []string
	for p := range t.AllProperties() {
		if _, ok := p.Annotation("writeOnce"); !ok {
			continue
		}
		keys = append(keys, p.Name())
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// derivedImmutableKeys returns a type's @writeOnce keys for the write path.
//
// [Adapter.ShapeForSchema] records them on the shape, which every write entry
// point receives, so the guarantee holds even for a caller passing a nil schema
// type — the documented streaming call shape. A nil ImmutableKeys means the
// shape never computed them (hand-built, or built before the field existed)
// rather than "this type has none"; only then is the schema type consulted, so
// the common unannotated type costs no per-node walk.
func derivedImmutableKeys(shape *NodeShape, schemaType *schema.Type) []string {
	if shape != nil && shape.ImmutableKeys != nil {
		return shape.ImmutableKeys
	}
	return ImmutableKeysFor(schemaType)
}
