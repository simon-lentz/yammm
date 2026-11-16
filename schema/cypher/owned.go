package cypher

import "github.com/simon-lentz/yammm/schema"

// OwnedLabels is the set of Neo4j labels one schema owns: every named,
// non-abstract member type's label, a superset of the labels emitted for it.
// Construct via [Config.OwnedLabels]; the zero value and a nil pointer own
// nothing.
//
// A diff reads this rather than a schema name because ownership
// cannot be recovered from a label string. [Config.Label] composes a label from
// a caller-configurable prefix and separator around a schema name and a type
// name, which is not an invertible function: for any rule that tries to read a
// schema back out of a label there is a configuration, or a sibling schema name,
// that satisfies the rule without belonging to the schema. Membership answers
// the question directly instead.
type OwnedLabels struct {
	labels map[string]struct{}
}

// OwnedLabels returns every named, non-abstract type's label across the whole
// IMPORT CLOSURE of s, under c: the labels c emits for s, and a label it
// refuses to emit (below).
//
// The closure, not the entry schema alone: an imported type this schema writes
// nodes for gets a label, and it is emitted constraints and indexes, so it is
// the schema's to own. The emit set and the own set reach their types through
// one walk, which is what keeps the desired and owned sides widening together,
// so a newly-owned object is also newly desired and no spurious DROP arises. A
// later change must not split them. They differ only by the labels the
// emitters refuse (below).
//
// The set is every such type's label, and ownership checks no identifier.
// The neo4j package's NodeQueries and EdgeQueries, [Config.Constraints] and
// [Config.Indexes] each REFUSE the whole schema with
// [E_NEO4J_INVALID_IDENTIFIER] when a type's label is not a valid Neo4j
// identifier, emitting nothing (see [Config.Label]); this set still
// names such a label, because a database may hold objects on it from an
// earlier configuration and they are the schema's to report. The set is
// therefore not a count of what will be emitted.
//
// It is derived from the schema in hand, which fixes what a diff can see. An
// object left behind by a type that has since been DELETED or RENAMED sits on a
// label no current type produces, so it is outside this set and is never a
// Drop. Unless a drift names it as a blocker, its half's Excluded counts it. That
// is a property of deriving ownership from the schema rather than a gap to be
// closed by matching labels against a namespace prefix: [Config.Label] composes
// a label from a caller-configurable prefix and separator around a schema name
// and a type name, so for any prefix rule there is a sibling schema name or a
// separator setting that satisfies it without belonging to the schema — and a
// false positive there is a plan telling an operator to drop another
// application's constraint.
func (c Config) OwnedLabels(s schema.Schema) *OwnedLabels {
	owned := &OwnedLabels{labels: make(map[string]struct{})}
	for _, label := range c.labels().LabeledTypes(s) {
		owned.labels[label] = struct{}{}
	}
	return owned
}

// Contains reports whether the set holds label.
func (o *OwnedLabels) Contains(label string) bool {
	if o == nil {
		return false
	}
	_, ok := o.labels[label]
	return ok
}

// LabelOf returns the first label of a remote object that this schema owns, and
// whether the object is owned at all.
//
// Ownership and identity are answered together, from the same label, so an
// object carrying several labels can never be admitted under one label and then
// keyed by another. It is exported because the diff hands back bare remote
// index and constraint values, whose LabelsOrTypes a caller
// reporting on a Drop must resolve the same way the diff did — picking the
// first label instead names one the schema may not own. A drift's Blockers can
// be unowned, and LabelOf reports false for one.
func (o *OwnedLabels) LabelOf(labels []string) (string, bool) {
	for _, label := range labels {
		if o.Contains(label) {
			return label, true
		}
	}
	return "", false
}
