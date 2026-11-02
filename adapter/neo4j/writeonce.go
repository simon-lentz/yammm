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
// W_ANNOTATION_SHADOWED. Returns nil for a zero type or one with no @writeOnce
// properties.
//
// The write path selects a type's immutable-key query shape from this set.
func ImmutableKeysFor(t schema.Type) []string {
	if t.IsZero() {
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
