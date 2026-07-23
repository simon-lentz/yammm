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
	// The dedup map is allocated lazily on the first @writeOnce hit, so the
	// overwhelmingly common unannotated type — scanned once per NodeQueryFor
	// call on the single-node write path — does an allocation-free scan and
	// returns nil.
	var keys []string
	var seen map[string]bool
	for p := range t.AllProperties() {
		if _, ok := p.Annotation("writeOnce"); !ok {
			continue
		}
		name := p.Name()
		if seen == nil {
			seen = make(map[string]bool)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		keys = append(keys, name)
	}
	slices.Sort(keys)
	return keys
}

// effectiveImmutableKeys returns the union of explicitly-passed immutable keys
// and the schema-derived @writeOnce keys of the type, deduplicated. A nil
// schemaType contributes no derived keys, so the result is the explicit set
// unchanged — matching the explicit-only pass-through contract of
// [Adapter.NodeQueryFor]. When there are no derived keys the explicit slice is
// returned as-is, so an explicit-only call is byte-for-byte unchanged.
func effectiveImmutableKeys(explicit []string, schemaType *schema.Type) []string {
	derived := ImmutableKeysFor(schemaType)
	if len(derived) == 0 {
		return explicit
	}
	if len(explicit) == 0 {
		return derived
	}
	seen := make(map[string]bool, len(explicit)+len(derived))
	union := make([]string, 0, len(explicit)+len(derived))
	for _, k := range explicit {
		if !seen[k] {
			seen[k] = true
			union = append(union, k)
		}
	}
	for _, k := range derived {
		if !seen[k] {
			seen[k] = true
			union = append(union, k)
		}
	}
	return union
}
