package cypher

import "github.com/simon-lentz/yammm/schema"

// OwnedLabels is the set of Neo4j labels one schema owns: the label of every
// non-abstract type in its import closure. Construct via [Config.OwnedLabels];
// the zero value and a nil pointer own nothing.
//
// A diff reads this rather than a schema name because ownership cannot be
// recovered from a label string: a remote label can be composed under another
// prefix or separator, or written by another tool, so any rule that reads a
// schema back out of a label is satisfied by a label that does not belong to
// the schema. Membership answers the question directly.
type OwnedLabels struct {
	labels map[string]struct{}
}

// OwnedLabels returns the label of every non-abstract type in the import
// closure of s, under c. It walks the closure as [Config.Constraints] and
// [Config.Indexes] do, so the desired and the owned sides widen together and no
// spurious DROP arises. The set judges no label: where [Config.CheckLabels]
// refuses s, the generators emit nothing and the set still names every label.
//
// The set comes from the schema in hand. An object a since deleted or renamed
// type left behind sits on a label no current type produces, so it is never a
// Drop, and unless a drift names it as a blocker, its half's Excluded counts it.
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
