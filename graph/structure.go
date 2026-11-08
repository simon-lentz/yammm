package graph

import (
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// structureRules holds a snapshot built against another schema to the rules the
// package doc's "Structural facts" section lists, under the importing schema.
//
// One break draws one issue: a unit a rule refused feeds no later rule. A
// record a rule refused feeds no (one) count, an instance whose key a rule
// refused is no second holder of an address or of a sibling's key, and the
// records at an address in refused are no count for a later rule.
type structureRules struct {
	s     schema.Schema
	canon *canonicalizer
	c     *diag.Collector

	// ones counts the records under each (one) association of a source, and is
	// allocated at the first (one) record met.
	ones  map[oneSlot]int
	order []oneSlot
	// addresses holds each root's canonical address, relation left empty, and
	// whether a sound root holds it: one of a type that can hold a root, whose key
	// the key rule accepts.
	addresses map[oneSlot]bool
	// refused holds each address two roots stand at, relation left empty: the
	// snapshot does not say which root a record there is the record of.
	refused map[oneSlot]bool
	// refusedTypes holds each identity refused as no root's type: a root of one
	// holds no address, so it owes no required association and is in no count.
	refusedTypes map[schema.TypeID]bool
	// unresolvedTypes holds each identity the identity rule refused as one s
	// cannot resolve.
	unresolvedTypes map[schema.TypeID]bool
}

// oneSlot addresses one source instance's (one) association.
type oneSlot struct {
	source   schema.TypeID
	key      string
	relation string
}

// importOp names the import in every message.
const importOp = "NewFromSnapshot"

func (r *structureRules) refuse(code diag.Code, msg string) {
	r.c.Collect(diag.NewIssue(diag.Error, code, importOp+": "+msg).Build())
}

// unjudged reports whether the records at slot are no count a rule may judge:
// two roots stand at its address.
func (r *structureRules) unjudged(slot oneSlot) bool {
	return r.refused[oneSlot{source: slot.source, key: slot.key}]
}

func (r *structureRules) refuseAddress(address oneSlot) {
	if r.refused == nil {
		r.refused = make(map[oneSlot]bool)
	}
	r.refused[address] = true
}

// checkRoot applies [checkTree] to a root and addresses it.
func checkRoot(r *structureRules, root Instance) {
	checkTree(r, "instance", root)
	if t, ok := r.s.TypeByID(root.typeID); ok {
		address(r, t, root)
	}
}

// checkTree applies every per-instance rule to n and to each composed child at
// every depth: identity, then — for an identity that resolves — each slot, the
// stored key and the declared names.
func checkTree(r *structureRules, position string, n Instance) {
	id := n.typeID
	// In a fixed order: the identity rule names the first position it meets.
	composed := n.composed
	var names []string
	if len(composed) > 0 {
		names = slices.Sorted(maps.Keys(composed))
	}
	if !r.identity(id, positionAt(position, n.primaryKey.String())) {
		for _, relName := range names {
			for _, child := range composed[relName] {
				r.keylessPartKey(child)
				checkTree(r, "composed child", child)
			}
		}
		return
	}
	t, _ := r.s.TypeByID(id)
	for _, relName := range names {
		children := composed[relName]
		checkSlot(r, t, n, relName, children)
		for _, child := range children {
			r.keylessPartKey(child)
			checkTree(r, "composed child", child)
		}
	}
	r.key(position, t, id, n.primaryKey, n.properties)
	r.names(position, t, id, n.primaryKey, n.properties)
}

// identity refuses an identity s cannot resolve, and reports whether id names a
// type of s. at names where id stands. It refuses at the first position that
// carries the identity: one rename fixes every position, so it is one break.
func (r *structureRules) identity(id schema.TypeID, at string) bool {
	if _, ok := r.s.TypeByID(id); ok {
		return true
	}
	if !r.unresolvedTypes[id] {
		if r.unresolvedTypes == nil {
			r.unresolvedTypes = make(map[schema.TypeID]bool)
		}
		r.unresolvedTypes[id] = true
		r.refuse(diag.E_GRAPH_TYPE_NOT_FOUND, fmt.Sprintf("unresolvable type identity %s at %s", identityText(r.s, id), at))
	}
	return false
}

// positionAt names an instance position and the key standing there.
func positionAt(position, key string) string {
	return position + " position, key " + diag.CutValue(key)
}

// typesEntry refuses a types entry s cannot resolve, and a type that cannot hold
// a root instance, whether or not the snapshot holds one: no constructor denotes
// a type [Graph.Add] refuses.
func (r *structureRules) typesEntry(i int, id schema.TypeID) {
	if !r.identity(id, fmt.Sprintf("types entry %d", i)) {
		return
	}
	if code, rule := rootIneligibility(r.s, id); rule != "" {
		r.refuse(code, fmt.Sprintf("types entry %d denotes %s, which %s", i, schema.TagForm(r.s, id), rule))
		r.refuseType(id)
	}
}

func (r *structureRules) refuseType(id schema.TypeID) {
	if r.refusedTypes == nil {
		r.refusedTypes = make(map[schema.TypeID]bool)
	}
	r.refusedTypes[id] = true
}

// rootIneligibility names the package doc's "Root type eligibility" rule that
// keeps id from holding a root under s, with its code, or returns ""
// when it can hold one or s cannot resolve it.
func rootIneligibility(s schema.Schema, id schema.TypeID) (diag.Code, string) {
	t, ok := s.TypeByID(id)
	if !ok {
		return diag.Code{}, ""
	}
	// [Graph.Add] checks the first two in this order, so a type breaking two
	// draws one code at every constructor.
	switch {
	case !t.HasPrimaryKey():
		return diag.E_GRAPH_MISSING_PK, "declares no primary key"
	case t.IsPart():
		return diag.E_GRAPH_INVALID_COMPOSITION, "is a part type, addressed through its parent composition"
	case t.IsAbstract():
		return diag.E_GRAPH_ABSTRACT_TYPE, "is abstract"
	}
	return diag.Code{}, ""
}

// checkSlot judges one parent's slot under relName by the rules a validator
// holds a composition to: the slot is a composition t declares, a (one) slot
// holds one child, and each child is an instance of the declared target, matched
// by identity. A child whose own identity
// does not resolve is the identity rule's to report.
func checkSlot(r *structureRules, t schema.Type, parent Instance, relName string, children []Instance) {
	rel, ok := t.Relation(relName)
	if !ok || rel.Kind() != schema.RelationComposition {
		parentName := schema.TagForm(r.s, parent.typeID)
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
			fmt.Sprintf("%s: %s[%s] holds composed children under %s, which its type does not declare as a composition",
				importOp, parentName, diag.CutValue(parent.primaryKey.String()), diag.QuoteValue(relName))).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relName).Build())
		return
	}
	if !rel.IsMany() && len(children) > 1 {
		parentName := schema.TagForm(r.s, parent.typeID)
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
			fmt.Sprintf("%s: %s[%s] composition %q: (one) cardinality violated, got %d children",
				importOp, parentName, diag.CutValue(parent.primaryKey.String()), relName, len(children))).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relName).
			WithDetail(diag.DetailKeyJSONField, rel.FieldName()).Build())
	}
	want := rel.TargetID()
	for i, child := range children {
		got := child.typeID
		if got == want {
			continue
		}
		if _, resolves := r.s.TypeByID(got); !resolves {
			continue
		}
		parentName := schema.TagForm(r.s, parent.typeID)
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("%s: %s[%s] composition %q child %d is of type %s; the composition declares %s",
				importOp, parentName, diag.CutValue(parent.primaryKey.String()), relName, i, got, want)).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relName).
			WithDetail(diag.DetailKeyExpected, want.String()).
			WithDetail(diag.DetailKeyGot, got.String()).Build())
	}
	siblingKeys(r, parent, rel, children)
}

// siblingKeys refuses two children of one (many) slot at one canonical key,
// as a validator refuses them. A child of another type has no key to compare, and
// neither has one [checkKey] refuses, as it refuses every key of a keyless part.
func siblingKeys(r *structureRules, parent Instance, rel schema.Relation, children []Instance) {
	if !rel.IsMany() || len(children) < 2 {
		return
	}
	target, ok := r.s.TypeByID(rel.TargetID())
	if !ok {
		return
	}
	sound := func(child Instance) bool {
		return checkKey(target, child.primaryKey, child.properties.Get, r.canon) == nil
	}
	seen := make(map[string]int, len(children))
	for i, child := range children {
		if child.typeID != rel.TargetID() {
			continue
		}
		key := r.canon.key(rel.TargetID(), child.primaryKey).String()
		if first, dup := seen[key]; dup {
			if !sound(child) {
				continue
			}
			if !sound(children[first]) {
				seen[key] = i
				continue
			}
			parentName := schema.TagForm(r.s, parent.typeID)
			r.c.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
				fmt.Sprintf("%s: %s[%s] composition %q holds children %d and %d at key %s",
					importOp, parentName, diag.CutValue(parent.primaryKey.String()), rel.Name(), first, i, diag.CutValue(key))).
				WithDetail(diag.DetailKeyTypeName, parentName).
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPrimaryKey, key).Build())
			continue
		}
		seen[key] = i
	}
}

// address refuses a second root at one canonical key of its type t: an index
// holds one instance per address, so the second would be dropped or shadowed.
// Every root stands at its address, but only a root of a type that can hold one,
// whose stored key the key rule accepts, holds it, whichever comes first: a
// refused one is no duplicate. No rule judges the records at an address two roots
// stand at.
func address(r *structureRules, t schema.Type, root Instance) {
	id, key := root.typeID, root.primaryKey
	if r.addresses == nil {
		r.addresses = make(map[oneSlot]bool)
	}
	slot := oneSlot{source: id, key: r.canon.key(id, key).String()}
	sound := !r.refusedTypes[id] && checkKey(t, key, root.properties.Get, r.canon) == nil
	held, present := r.addresses[slot]
	if !present {
		r.addresses[slot] = sound
		return
	}
	// The snapshot does not say which of two roots at one address a record there
	// is the record of, so no rule judges the records at it.
	r.refuseAddress(slot)
	switch {
	case !sound:
	case !held:
		r.addresses[slot] = true
	default:
		r.refuse(diag.E_DUPLICATE_PK, fmt.Sprintf("two instances of type %s at address %s: their keys are one under this schema", schema.TagForm(r.s, id), diag.CutValue(slot.key)))
	}
}

// key holds a keyed instance's stored key to [checkKey]. An address the data
// contradicts is a forged address: an edge to it writes a foreign key that
// reads back addressing another instance, or none.
func (r *structureRules) key(position string, t schema.Type, id schema.TypeID, key immutable.Key, props immutable.Properties) {
	if !t.HasPrimaryKey() {
		return
	}
	if err := checkKey(t, key, props.Get, r.canon); err != nil {
		name := schema.TagForm(r.s, id)
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
			fmt.Sprintf("%s: %s of type %s at %s: %s", importOp, position, name, diag.CutValue(key.String()), err)).
			WithDetail(diag.DetailKeyTypeName, name).
			WithDetail(diag.DetailKeyPrimaryKey, key.String()).Build())
	}
}

// keylessPartKey refuses a key stored on a composed child whose type declares no
// primary key, as the snapshot reader refuses it: [Graph.Add] stores none there,
// and a reload would hold an address no rule reads. A root of such a type is the
// root rule's one break.
func (r *structureRules) keylessPartKey(child Instance) {
	t, ok := r.s.TypeByID(child.typeID)
	if !ok || t.HasPrimaryKey() || child.primaryKey.Len() == 0 {
		return
	}
	name := schema.TagForm(r.s, child.typeID)
	r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
		fmt.Sprintf("%s: composed child of type %s at %s: its type declares no primary key, so it stores none", importOp, name, diag.CutValue(child.primaryKey.String()))).
		WithDetail(diag.DetailKeyTypeName, name).
		WithDetail(diag.DetailKeyPrimaryKey, child.primaryKey.String()).Build())
}

// names refuses a stored property name t does not declare, own or inherited.
// The validator stores values under declared names alone.
func (r *structureRules) names(position string, t schema.Type, id schema.TypeID, key immutable.Key, props immutable.Properties) {
	for _, name := range undeclaredNames(props, func(n string) bool { _, ok := t.Property(n); return ok }) {
		r.refuse(diag.E_UNKNOWN_FIELD, fmt.Sprintf("%s %s[%s] holds property %s, which it does not declare",
			position, schema.TagForm(r.s, id), diag.CutValue(key.String()), diag.QuoteValue(name)))
	}
}

// associationRecord is one record under an association as the rules read it:
// a resolved edge, or an unresolved record, which alone holds a reason.
type associationRecord struct {
	// position names the record kind in every message.
	position   string
	unresolved bool
	source     schema.TypeID
	sourceKey  immutable.Key
	relation   string
	// target is the type of the instance an imported edge points at, and zero
	// where the record's target is the association's own.
	target     schema.TypeID
	targetKey  immutable.Key
	reason     string
	properties immutable.Properties
}

// association holds one record to what [Graph.Add] stages, as the package
// doc's "Structural facts" section lists it, and reports whether a rule has
// refused it. A record whose source or target identity does not resolve is the
// identity rule's to report. A record that names no target, by a reason that
// is not "target_missing", owes no target key. A record it reports refused is
// none [structureRules.countRecord] counts.
func (r *structureRules) association(rec associationRecord) bool {
	t, ok := r.s.TypeByID(rec.source)
	if !ok {
		return true
	}
	before := r.c.ErrorCount()
	sourceName := schema.TagForm(r.s, rec.source)
	at := fmt.Sprintf("%s %s[%s] under ", rec.position, sourceName, diag.CutValue(rec.sourceKey.String()))
	rel, ok := t.Relation(rec.relation)
	if !ok || !rel.IsAssociation() {
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
			fmt.Sprintf("%s: %s%s, which its type does not declare as an association", importOp, at, diag.QuoteValue(rec.relation))).
			WithDetail(diag.DetailKeyTypeName, sourceName).
			WithDetail(diag.DetailKeyRelationName, rec.relation).Build())
		return true
	}
	// The schema declares the association, so its name is shown whole.
	at += strconv.Quote(rec.relation)
	for _, name := range undeclaredNames(rec.properties, func(n string) bool { _, ok := rel.Property(n); return ok }) {
		r.refuse(diag.E_UNKNOWN_EDGE_FIELD, fmt.Sprintf("%s holds edge property %s, which the association does not declare", at, diag.QuoteValue(name)))
	}
	// An "empty" record stands for an empty list, which the validator refuses at
	// a (one) association, optional or not.
	if rec.reason == "empty" && !rel.IsMany() {
		r.refuse(diag.E_EDGE_SHAPE_MISMATCH, at+` states reason "empty", where Graph.Add records "empty" only under a (many) association`)
	}
	namesTarget := !rec.unresolved || rec.reason == "target_missing"
	if !rec.target.IsZero() {
		if _, ok := r.s.TypeByID(rec.target); !ok {
			return true
		}
	}
	if targetType, resolves := r.s.TypeByID(rel.TargetID()); resolves {
		if !rec.target.IsZero() && rec.target != rel.TargetID() {
			r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
				fmt.Sprintf("%s: %s targets %s; the association declares %s", importOp, at, identityText(r.s, rec.target), rel.TargetID())).
				WithDetail(diag.DetailKeyTypeName, sourceName).
				WithDetail(diag.DetailKeyRelationName, rec.relation).
				WithDetail(diag.DetailKeyExpected, rel.TargetID().String()).
				WithDetail(diag.DetailKeyGot, rec.target.String()).Build())
		} else if namesTarget {
			if declared := keyArity(targetType); rec.targetKey.Len() != declared {
				r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
					fmt.Sprintf("%s: %s carries a %d-part target key; the target declares %d", importOp, at, rec.targetKey.Len(), declared)).
					WithDetail(diag.DetailKeyTypeName, sourceName).
					WithDetail(diag.DetailKeyRelationName, rec.relation).Build())
			}
		}
	}
	return r.c.ErrorCount() > before
}

// associationTarget returns the declared target of relation, an association of
// source under s, or the zero identity when s declares no such association.
func associationTarget(s schema.Schema, source schema.TypeID, relation string) schema.TypeID {
	t, ok := s.TypeByID(source)
	if !ok {
		return schema.TypeID{}
	}
	rel, ok := t.Relation(relation)
	if !ok || !rel.IsAssociation() {
		return schema.TypeID{}
	}
	return rel.TargetID()
}

// requiredUnder reports whether relation, an association of source under s, is
// required, as [Graph.Add] derives an unresolved record's Required.
func requiredUnder(s schema.Schema, source schema.TypeID, relation string) bool {
	t, ok := s.TypeByID(source)
	if !ok {
		return false
	}
	rel, ok := t.Relation(relation)
	return ok && !rel.IsOptional()
}

// countOne counts one record under a (one) association of a source.
func (r *structureRules) countOne(slot oneSlot) {
	if r.ones == nil {
		r.ones = make(map[oneSlot]int)
	}
	if r.ones[slot] == 0 {
		r.order = append(r.order, slot)
	}
	r.ones[slot]++
}

// countRecord counts a record no rule refused toward its (one) association: a
// (one) association of a sound root holds its source's address.
func (r *structureRules) countRecord(source schema.TypeID, sourceKey immutable.Key, relation string) {
	t, ok := r.s.TypeByID(source)
	if !ok {
		return
	}
	if rel, ok := t.Relation(relation); !ok || !rel.IsAssociation() || rel.IsMany() {
		return
	}
	key := r.canon.key(source, sourceKey).String()
	if r.addresses[oneSlot{source: source, key: key}] {
		r.countOne(oneSlot{source: source, key: key, relation: relation})
	}
}

// oneOverflow refuses each (one) association holding more than one record,
// edges and unresolved records together: [Graph.Add] stages at most one record
// per (one) association. A slot [structureRules.unjudged] names is not judged.
func (r *structureRules) oneOverflow() {
	for _, slot := range r.order {
		if n := r.ones[slot]; n > 1 && !r.unjudged(slot) {
			name := schema.TagForm(r.s, slot.source)
			r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_CARDINALITY,
				fmt.Sprintf("%s: (one) association %q of %s[%s] holds %d records",
					importOp, slot.relation, name, diag.CutValue(slot.key), n)).
				WithDetail(diag.DetailKeyTypeName, name).
				WithDetail(diag.DetailKeyRelationName, slot.relation).Build())
		}
	}
}

// keyArity returns the number of primary-key components t declares.
func keyArity(t schema.Type) int {
	n := 0
	for range t.PrimaryKeys() {
		n++
	}
	return n
}

// undeclaredNames returns the names of props that declared rejects, in sorted
// order. The clean case allocates nothing.
func undeclaredNames(props immutable.Properties, declared func(string) bool) []string {
	var out []string
	for name := range props.SortedKeys() {
		if !declared(name) {
			out = append(out, name)
		}
	}
	return out
}
