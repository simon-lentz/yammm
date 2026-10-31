package graph

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// structureRules holds a snapshot to the structural rules [Graph.Add] applies
// as it builds, which the package doc's "Structural facts" section lists.
// [RebuildSnapshot] and [NewFromSnapshot] both apply this one definition, so no
// constructor installs what another refuses.
//
// One break draws one issue: a unit a rule refused feeds no later rule. A
// record a rule refused feeds no (one) count and no record fact, an instance
// whose key a rule refused is no second holder of an address or of a sibling's
// key, and the records at an address in refused are no count for a later rule.
// A reference to an address a refused root states is that root's break, so it
// draws no issue of its own.
type structureRules struct {
	s     *schema.Schema
	canon *canonicalizer
	c     *diag.Collector
	// op names the constructor in every message.
	op string
	// callerFacing reports Add's code at Error for the rules [structureRules.refuse]
	// carries, which [RebuildSnapshot], whose parts break them only through a
	// broken caller, reports as Fatal E_INTERNAL.
	callerFacing bool

	// ones counts the records under each (one) association of a source, and is
	// allocated at the first (one) record met.
	ones  map[oneSlot]int
	order []oneSlot
	// addresses holds each root's canonical address, relation left empty, and
	// whether a sound root holds it: one of a type that can hold a root, whose key
	// the key rule accepts.
	addresses map[oneSlot]bool
	// stated holds each address the key properties of a root state where the key
	// rule refused its stored key, relation left empty.
	stated map[oneSlot]bool
	// refused holds each address two roots stand at, relation left empty: the
	// parts do not say which root a record there is the record of.
	refused map[oneSlot]bool
	// refusedRecords holds the index of each unresolved record a rule refused,
	// and refusedEdges each edge's.
	refusedRecords map[int]bool
	refusedEdges   map[int]bool
	// refusedTypes holds each identity refused as no root's type: a root of one
	// holds no address, so it owes no required association and is in no count.
	refusedTypes map[schema.TypeID]bool
	// unresolvedTypes holds each identity the identity rule refused as one s
	// cannot resolve.
	unresolvedTypes map[schema.TypeID]bool
	// refusedCompositions holds each composition slot [checkSlot] refused of a
	// root that holds its address, by its parent's type and canonical key.
	// unheldCompositions holds the same of a root whose key the key rule refused,
	// at both its addresses; [structureRules.slotRefused] reads both.
	refusedCompositions map[oneSlot]bool
	unheldCompositions  map[oneSlot]bool
}

// oneSlot addresses one source instance's (one) association.
type oneSlot struct {
	source   schema.TypeID
	key      string
	relation string
}

// refuse reports a rule the caller-facing mode reports under code and
// [RebuildSnapshot] reports as Fatal E_INTERNAL.
func (r *structureRules) refuse(code diag.Code, msg string) {
	sev := diag.Error
	if !r.callerFacing {
		sev, code = diag.Fatal, diag.E_INTERNAL
	}
	r.c.Collect(diag.NewIssue(sev, code, r.op+": "+msg).Build())
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

// structureNode is one instance as the rules read it: an [InstanceParts]
// before a rebuild, or an [*Instance] of a snapshot being imported.
type structureNode[N any] interface {
	nodeType() schema.TypeID
	nodeKey() immutable.Key
	nodeProperties() immutable.Properties
	nodeComposed() map[string][]N
}

func (ip InstanceParts) nodeType() schema.TypeID              { return ip.TypeID }
func (ip InstanceParts) nodeKey() immutable.Key               { return ip.PrimaryKey }
func (ip InstanceParts) nodeProperties() immutable.Properties { return ip.Properties }
func (ip InstanceParts) nodeComposed() map[string][]InstanceParts {
	return ip.Composed
}

func (i *Instance) nodeType() schema.TypeID              { return i.typeID }
func (i *Instance) nodeKey() immutable.Key               { return i.primaryKey }
func (i *Instance) nodeProperties() immutable.Properties { return i.properties }
func (i *Instance) nodeComposed() map[string][]*Instance { return i.composed }

// The rules reach these methods only through a type parameter, which the
// unused analyzer does not follow; these assertions are the use it sees.
var (
	_ structureNode[InstanceParts] = InstanceParts{}
	_ structureNode[*Instance]     = (*Instance)(nil)
)

// checkRoot applies [checkTree] to a root and addresses it.
func checkRoot[N structureNode[N]](r *structureRules, root N) {
	refusedSlots := checkTree(r, "instance", root)
	if t, ok := r.s.TypeByID(root.nodeType()); ok {
		address(r, t, root, refusedSlots)
	}
}

// checkTree applies every per-instance rule to n and to each composed child at
// every depth: identity, then — for an identity that resolves — each slot, the
// stored key and the declared names. It returns the names of n's own slots
// [checkSlot] refused.
func checkTree[N structureNode[N]](r *structureRules, position string, n N) []string {
	id := n.nodeType()
	// In a fixed order: the identity rule names the first position it meets.
	composed := n.nodeComposed()
	var names []string
	if len(composed) > 0 {
		names = slices.Sorted(maps.Keys(composed))
	}
	if !r.identity(id, positionAt(position, n.nodeKey().String())) {
		for _, relName := range names {
			for _, child := range composed[relName] {
				checkTree(r, "composed child", child)
			}
		}
		return nil
	}
	t, _ := r.s.TypeByID(id)
	var refusedSlots []string
	for _, relName := range names {
		children := composed[relName]
		if checkSlot(r, t, n, relName, children) {
			refusedSlots = append(refusedSlots, relName)
		}
		for _, child := range children {
			checkTree(r, "composed child", child)
		}
	}
	r.key(position, t, id, n.nodeKey(), n.nodeProperties())
	r.names(position, t, id, n.nodeKey(), n.nodeProperties())
	return refusedSlots
}

// identity refuses the zero identity and one s cannot resolve, and reports
// whether id names a type of s. at names where id stands. An identity s cannot
// resolve is refused at the first position that carries it: one rename fixes
// every position, so it is one break.
func (r *structureRules) identity(id schema.TypeID, at string) bool {
	if id.IsZero() {
		r.refuse(diag.E_GRAPH_TYPE_NOT_FOUND, "zero type identity at "+at)
		return false
	}
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
	return position + " position, key " + diag.ValueText(key)
}

// typesEntry refuses a types entry that is zero or that s cannot resolve, and a
// type that cannot hold a root instance where no other position refused it,
// whether or not the snapshot holds one:
// adapter/json and adapter/csv key their output by a denoted type's name.
func (r *structureRules) typesEntry(i int, id schema.TypeID) {
	if !r.identity(id, fmt.Sprintf("types entry %d", i)) {
		return
	}
	if code, rule := rootIneligibility(r.s, id); rule != "" && !r.refusedTypes[id] {
		r.refuse(code, fmt.Sprintf("types entry %d denotes %s, which %s", i, id, rule))
		r.refuseType(id)
	}
}

func (r *structureRules) refuseType(id schema.TypeID) {
	if r.refusedTypes == nil {
		r.refusedTypes = make(map[schema.TypeID]bool)
	}
	r.refusedTypes[id] = true
}

// rootType refuses n root instances of a type that cannot hold one, unless a
// types entry denoting it drew the refusal.
func (r *structureRules) rootType(id schema.TypeID, n int) {
	if r.refusedTypes[id] {
		return
	}
	if code, rule := rootIneligibility(r.s, id); rule != "" {
		r.refuse(code, fmt.Sprintf("%d root instance(s) of type %s, which %s", n, id, rule))
		r.refuseType(id)
	}
}

// slotIndex holds the children of each keyed (many) slot a duplicate names, by
// canonical key. Each slot is read once, however many duplicates name it.
type slotIndex[N structureNode[N]] struct {
	canon  *canonicalizer
	slots  map[oneSlot]map[string]N
	stated map[oneSlot]map[string]bool
}

func newSlotIndex[N structureNode[N]](canon *canonicalizer) *slotIndex[N] {
	return &slotIndex[N]{canon: canon, slots: make(map[oneSlot]map[string]N), stated: make(map[oneSlot]map[string]bool)}
}

// composedConflict returns the instance a composed duplicate of inst collided
// with in the slot of parent at at under rel, as [Graph.AddComposed] records it,
// or the zero value and the reason no child there is one: the sole occupant of a
// (one) slot, or the child of a keyed (many) slot at inst's key. Keys compare in
// the canonicalizer's form, so it judges an imported snapshot's records under the
// importing schema. rel is not a keyless (many) composition, which holds no
// conflict.
func (x *slotIndex[N]) composedConflict(at oneSlot, rel *schema.Relation, inst, parent N) (N, string) {
	var none N
	occupants := parent.nodeComposed()[rel.Name()]
	if !rel.IsMany() {
		if len(occupants) == 1 {
			return occupants[0], ""
		}
		return none, fmt.Sprintf("is under the (one) composition %q, which holds %d children", rel.Name(), len(occupants))
	}
	slot := oneSlot{source: at.source, key: at.key, relation: rel.Name()}
	children, indexed := x.slots[slot]
	if !indexed {
		children = make(map[string]N, len(occupants))
		for _, o := range occupants {
			children[x.canon.key(rel.TargetID(), o.nodeKey()).String()] = o
		}
		x.slots[slot] = children
	}
	if o, ok := children[x.canon.key(rel.TargetID(), inst.nodeKey()).String()]; ok {
		return o, ""
	}
	return none, fmt.Sprintf("holds no child of the composition %q at its key", rel.Name())
}

// composedDuplicate holds composed duplicate record i to the position
// [Graph.AddComposed] records one at, and resolves its conflict where a root at
// at holds the slot alone. A slot [checkSlot] refused for its name is that
// rule's one issue; one it refused for its children still judges the record's
// own position, but holds no occupants the conflict is resolved against. Neither
// does a duplicate whose key the key rule refuses, a parent that holds no
// address, or an address two roots stand at. A child whose key properties state
// the duplicate's key is the refusal's break. Both types resolve under r.s, and
// parent is read only where a root holds at.
func composedDuplicate[N structureNode[N]](r *structureRules, x *slotIndex[N], i int, inst N, at oneSlot, relation string, parent N) {
	refuse := func(why string) {
		r.refuse(diag.E_GRAPH_INVALID_COMPOSITION, fmt.Sprintf("duplicate record %d, %s[%s], %s",
			i, inst.nodeType(), diag.ValueText(r.canon.key(inst.nodeType(), inst.nodeKey()).String()), why))
	}
	slotRefused := r.slotRefused(at, relation)
	rel, why := compositionOf(r.s, inst.nodeType(), at.source, relation)
	if rel == nil {
		if !slotRefused || declaresComposition(r.s, at.source, relation) {
			refuse(why)
		}
		return
	}
	target, _ := r.s.TypeByID(rel.TargetID())
	if rel.IsMany() && !target.HasPrimaryKey() {
		refuse(fmt.Sprintf("is under the keyless (many) composition %q, where no child conflicts", relation))
		return
	}
	if slotRefused || checkStoredKey(target, inst.nodeKey(), inst.nodeProperties().Get, r.canon) != nil || !r.addresses[at] || r.refused[at] {
		return
	}
	if _, why := x.composedConflict(at, rel, inst, parent); why != "" && !x.childStates(r, at, rel, target, inst, parent) {
		refuse(why)
	}
}

// childStates reports whether the key properties of a child in the slot of parent
// at at under rel state inst's key. target is rel's target type.
func (x *slotIndex[N]) childStates(r *structureRules, at oneSlot, rel *schema.Relation, target *schema.Type, inst, parent N) bool {
	slot := oneSlot{source: at.source, key: at.key, relation: rel.Name()}
	stated, indexed := x.stated[slot]
	if !indexed {
		stated = make(map[string]bool)
		for _, child := range parent.nodeComposed()[rel.Name()] {
			if key, ok := r.statedKey(target, rel.TargetID(), child.nodeProperties()); ok {
				stated[key] = true
			}
		}
		x.stated[slot] = stated
	}
	return stated[r.canon.key(rel.TargetID(), inst.nodeKey()).String()]
}

// compositionOf returns the composition a composed duplicate of type inst under
// a parent of type parent sits in, or nil and the reason no composition there
// holds one: relation is a composition parent declares, whose target is inst.
// parent must resolve under s.
func compositionOf(s *schema.Schema, inst, parent schema.TypeID, relation string) (*schema.Relation, string) {
	pt, _ := s.TypeByID(parent)
	rel, ok := pt.Relation(relation)
	if !ok || rel.Kind() != schema.RelationComposition {
		return nil, fmt.Sprintf("names %s, which %s does not declare as a composition", diag.QuoteValue(relation), parent)
	}
	if inst != rel.TargetID() {
		return nil, fmt.Sprintf("is of type %s; the composition %q declares %s", inst, relation, rel.TargetID())
	}
	return rel, ""
}

// declaresComposition reports whether the type id resolves to under s
// declares relation as a composition.
func declaresComposition(s *schema.Schema, id schema.TypeID, relation string) bool {
	t, ok := s.TypeByID(id)
	if !ok {
		return false
	}
	rel, ok := t.Relation(relation)
	return ok && rel.Kind() == schema.RelationComposition
}

// rootIneligibility names the package doc's "Root type eligibility" rule that
// keeps id from holding a root under s, with Add's code for it, or returns ""
// when it can hold one or s cannot resolve it.
func rootIneligibility(s *schema.Schema, id schema.TypeID) (diag.Code, string) {
	t, ok := s.TypeByID(id)
	if !ok {
		return diag.Code{}, ""
	}
	// In the order [Graph.Add] checks them, so a type breaking two draws Add's code.
	switch {
	case !schema.Addressable(s, id):
		return diag.E_GRAPH_TYPE_NOT_FOUND,
			"is reachable only through an intermediate import, so the entry schema cannot name it; import its schema directly"
	case !t.HasPrimaryKey():
		return diag.E_GRAPH_MISSING_PK, "declares no primary key"
	case t.IsPart():
		return diag.E_GRAPH_INVALID_COMPOSITION, "is a part type, addressed through its parent composition"
	case t.IsAbstract():
		return diag.E_GRAPH_ABSTRACT_TYPE, "is abstract"
	}
	return diag.Code{}, ""
}

// checkSlot judges one parent's slot under relName by the rules [Graph.Add]
// applies when it attaches a child: the slot is a composition t declares, a
// (one) slot holds one child, and each child is an instance of the declared
// target, matched by identity as Add matches it. A child whose own identity
// does not resolve is the identity rule's to report. It reports whether it
// refused the slot itself: its name, or the children its multiplicity or a
// sibling's key leaves no duplicate's conflict to resolve against.
func checkSlot[N structureNode[N]](r *structureRules, t *schema.Type, parent N, relName string, children []N) bool {
	rel, ok := t.Relation(relName)
	if !ok || rel.Kind() != schema.RelationComposition {
		parentName := schema.TagForm(r.s, parent.nodeType())
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
			fmt.Sprintf("%s: %s[%s] holds composed children under %s, which its type does not declare as a composition",
				r.op, parentName, diag.ValueText(parent.nodeKey().String()), diag.QuoteValue(relName))).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relName).Build())
		return true
	}
	refused := !rel.IsMany() && len(children) > 1
	if refused {
		parentName := schema.TagForm(r.s, parent.nodeType())
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
			fmt.Sprintf("%s: %s[%s] composition %q: (one) cardinality violated, got %d children",
				r.op, parentName, diag.ValueText(parent.nodeKey().String()), relName, len(children))).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relName).
			WithDetail(diag.DetailKeyJSONField, rel.FieldName()).Build())
	}
	want := rel.TargetID()
	for i, child := range children {
		got := child.nodeType()
		if got == want {
			continue
		}
		if _, resolves := r.s.TypeByID(got); !resolves {
			continue
		}
		parentName := schema.TagForm(r.s, parent.nodeType())
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("%s: %s[%s] composition %q child %d is of type %s; the composition declares %s",
				r.op, parentName, diag.ValueText(parent.nodeKey().String()), relName, i, got, want)).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relName).
			WithDetail(diag.DetailKeyExpected, want.String()).
			WithDetail(diag.DetailKeyGot, got.String()).Build())
	}
	return siblingKeys(r, parent, rel, children) || refused
}

// siblingKeys refuses two children of one (many) slot at one canonical key,
// as [Graph.Add] refuses them, and reports whether it refused any. A keyless part
// has no key to repeat, and a child of another type, or whose stored key the key
// rule refuses, has none to compare.
func siblingKeys[N structureNode[N]](r *structureRules, parent N, rel *schema.Relation, children []N) bool {
	if !rel.IsMany() || len(children) < 2 {
		return false
	}
	target, ok := r.s.TypeByID(rel.TargetID())
	if !ok || !target.HasPrimaryKey() {
		return false
	}
	sound := func(child N) bool {
		return checkKey(target, child.nodeKey(), child.nodeProperties().Get, r.canon) == nil
	}
	refused := false
	seen := make(map[string]int, len(children))
	for i, child := range children {
		if child.nodeType() != rel.TargetID() {
			continue
		}
		key := r.canon.key(rel.TargetID(), child.nodeKey()).String()
		if first, dup := seen[key]; dup {
			if !sound(child) {
				continue
			}
			if !sound(children[first]) {
				seen[key] = i
				continue
			}
			parentName := schema.TagForm(r.s, parent.nodeType())
			r.c.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
				fmt.Sprintf("%s: %s[%s] composition %q holds children %d and %d at key %s",
					r.op, parentName, diag.ValueText(parent.nodeKey().String()), rel.Name(), first, i, diag.ValueText(key))).
				WithDetail(diag.DetailKeyTypeName, parentName).
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPrimaryKey, key).Build())
			refused = true
			continue
		}
		seen[key] = i
	}
	return refused
}

// address refuses a second root at one canonical key of its type t: an index
// holds one instance per address, so the second would be dropped or shadowed.
// Every root stands at its address, but only a root of a type that can hold one,
// whose stored key the key rule accepts, holds it, whichever comes first: a
// refused one is no duplicate, and owns no slot there. It states the address its key properties name, and
// refusedSlots, the root's slots [checkSlot] refused, are unheld at both. No rule
// judges the records at an address two roots stand at.
func address[N structureNode[N]](r *structureRules, t *schema.Type, root N, refusedSlots []string) {
	id, key := root.nodeType(), root.nodeKey()
	if r.addresses == nil {
		r.addresses = make(map[oneSlot]bool)
	}
	slot := oneSlot{source: id, key: r.canon.key(id, key).String()}
	sound := !r.refusedTypes[id] && checkKey(t, key, root.nodeProperties().Get, r.canon) == nil
	markSlots := func(set *map[oneSlot]bool, at oneSlot) {
		for _, relName := range refusedSlots {
			if *set == nil {
				*set = make(map[oneSlot]bool)
			}
			(*set)[oneSlot{source: at.source, key: at.key, relation: relName}] = true
		}
	}
	if !sound {
		markSlots(&r.unheldCompositions, slot)
		if stated, ok := r.statedKey(t, id, root.nodeProperties()); ok {
			if r.stated == nil {
				r.stated = make(map[oneSlot]bool)
			}
			at := oneSlot{source: id, key: stated}
			r.stated[at] = true
			markSlots(&r.unheldCompositions, at)
		}
	}
	held, present := r.addresses[slot]
	if !present {
		r.addresses[slot] = sound
		if sound {
			markSlots(&r.refusedCompositions, slot)
		}
		return
	}
	// The parts do not say which of two roots at one address a record there is
	// the record of, so no rule judges the records at it.
	r.refuseAddress(slot)
	switch {
	case !sound:
	case !held:
		r.addresses[slot] = true
		markSlots(&r.refusedCompositions, slot)
	default:
		// Whichever root comes first, a slot either refused is refused.
		markSlots(&r.refusedCompositions, slot)
		r.refuse(diag.E_DUPLICATE_PK, fmt.Sprintf("two instances of type %s at address %s: their keys are one under this schema", id, diag.ValueText(slot.key)))
	}
}

// statedKey returns the canonical key the key properties of an instance of t
// state, or false where one is absent, null or no scalar [ParseKey] reads back.
func (r *structureRules) statedKey(t *schema.Type, id schema.TypeID, props immutable.Properties) (string, bool) {
	if !t.HasPrimaryKey() {
		return "", false
	}
	var components []any
	for pk := range t.PrimaryKeys() {
		val, ok := props.Get(pk.Name())
		if !ok || value.Absent(val.Unwrap()) {
			return "", false
		}
		// [immutable.WrapKey] panics on a value it cannot marshal, as a NaN is.
		if _, ok := value.Scalar(val.Unwrap()); !ok {
			return "", false
		}
		if _, err := json.Marshal(val.Unwrap()); err != nil {
			return "", false
		}
		components = append(components, val.Unwrap())
	}
	key := immutable.WrapKey(components)
	if unreadableComponent(t, key) >= 0 {
		return "", false
	}
	return r.canon.key(id, key).String(), true
}

// resolves reports whether a reference to key of id is judged no further: a root
// stands there, its key sound or not, or a refused root states it.
func (r *structureRules) resolves(id schema.TypeID, key immutable.Key) bool {
	at := oneSlot{source: id, key: r.canon.key(id, key).String()}
	_, stands := r.addresses[at]
	return stands || r.stated[at]
}

// slotRefused reports whether a duplicate parented at at meets a refusal of its
// slot under relation: the holder's, or, where no sound root stands at the
// address, a refused root's.
func (r *structureRules) slotRefused(at oneSlot, relation string) bool {
	slot := oneSlot{source: at.source, key: at.key, relation: relation}
	return r.refusedCompositions[slot] || r.unheldCompositions[slot] && !r.addresses[at]
}

// key holds an instance's stored key to [checkStoredKey], the rule [Graph.Add]
// applies: a keyed instance's key to the key its own properties state, and a
// keyless part's key to scalars [ParseKey] reads back. An address the data
// contradicts is a forged address: an edge to it writes a foreign key that
// reads back addressing another instance, or none.
func (r *structureRules) key(position string, t *schema.Type, id schema.TypeID, key immutable.Key, props immutable.Properties) {
	if err := checkStoredKey(t, key, props.Get, r.canon); err != nil {
		name := schema.TagForm(r.s, id)
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
			fmt.Sprintf("%s: %s of type %s at %s: %s", r.op, position, name, diag.ValueText(key.String()), err)).
			WithDetail(diag.DetailKeyTypeName, name).
			WithDetail(diag.DetailKeyPrimaryKey, key.String()).Build())
	}
}

// names refuses a stored property name t does not declare, own or inherited.
// The validator stores values under declared names alone, and [Graph.Add]
// refuses any other.
func (r *structureRules) names(position string, t *schema.Type, id schema.TypeID, key immutable.Key, props immutable.Properties) {
	for _, name := range undeclaredNames(props, func(n string) bool { _, ok := t.Property(n); return ok }) {
		r.refuse(diag.E_UNKNOWN_FIELD, fmt.Sprintf("%s %s[%s] holds property %s, which it does not declare",
			position, id, diag.ValueText(key.String()), diag.QuoteValue(name)))
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
	// refused reports that a rule has refused the record already.
	refused bool
}

// association holds one record to what [Graph.Add] stages, as the package
// doc's "Structural facts" section lists it, and reports whether a rule has
// refused it. A record whose source or target identity does not resolve is the
// identity rule's to report. A record that names no target, by a reason that
// is not "target_missing", owes no target key: [structureRules.unresolvedReason]
// judges what it carries. A record it reports refused is none
// [structureRules.countRecord] counts.
func (r *structureRules) association(rec associationRecord) bool {
	t, ok := r.s.TypeByID(rec.source)
	if !ok {
		return true
	}
	before := r.c.ErrorCount()
	sourceName := schema.TagForm(r.s, rec.source)
	at := fmt.Sprintf("%s %s[%s] under ", rec.position, sourceName, diag.ValueText(rec.sourceKey.String()))
	rel, ok := t.Relation(rec.relation)
	if !ok || !rel.IsAssociation() {
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
			fmt.Sprintf("%s: %s%s, which its type does not declare as an association", r.op, at, diag.QuoteValue(rec.relation))).
			WithDetail(diag.DetailKeyTypeName, sourceName).
			WithDetail(diag.DetailKeyRelationName, rec.relation).Build())
		return true
	}
	// The schema declares the association, so its name is shown whole.
	at += strconv.Quote(rec.relation)
	for _, name := range undeclaredNames(rec.properties, func(n string) bool { _, ok := rel.Property(n); return ok }) {
		r.refuse(diag.E_UNKNOWN_EDGE_FIELD, fmt.Sprintf("%s holds edge property %s, which the association does not declare", at, diag.QuoteValue(name)))
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
				fmt.Sprintf("%s: %s targets %s; the association declares %s", r.op, at, identityText(r.s, rec.target), rel.TargetID())).
				WithDetail(diag.DetailKeyTypeName, sourceName).
				WithDetail(diag.DetailKeyRelationName, rec.relation).
				WithDetail(diag.DetailKeyExpected, rel.TargetID().String()).
				WithDetail(diag.DetailKeyGot, rec.target.String()).Build())
		} else if namesTarget {
			var msg string
			if declared := keyArity(targetType); rec.targetKey.Len() != declared {
				msg = fmt.Sprintf("carries a %d-part target key; the target declares %d", rec.targetKey.Len(), declared)
			} else if i := unreadableComponent(targetType, rec.targetKey); i >= 0 {
				msg = fmt.Sprintf("carries a target key whose component %d is not a scalar graph.ParseKey reads back", i)
			}
			if msg != "" {
				r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
					fmt.Sprintf("%s: %s %s", r.op, at, msg)).
					WithDetail(diag.DetailKeyTypeName, sourceName).
					WithDetail(diag.DetailKeyRelationName, rec.relation).Build())
			}
		}
	}
	return rec.refused || r.c.ErrorCount() > before
}

// unresolvedReason refuses a reason outside the three [UnresolvedEdge]
// documents, and an "absent" or "empty" record that carries a target key or
// edge properties, which a reference with no target cannot hold.
func (r *structureRules) unresolvedReason(reason string, targetKey immutable.Key, props immutable.Properties) {
	switch reason {
	case "target_missing":
		return
	case "absent", "empty":
		if targetKey.Len() == 0 && props.Len() == 0 {
			return
		}
		r.c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
			fmt.Sprintf("%s: unresolved record with reason %q carries a target key or edge properties", r.op, reason)).Build())
	default:
		r.c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
			fmt.Sprintf("%s: unresolved record states reason %s, which is not one of target_missing, absent or empty", r.op, diag.QuoteValue(reason))).Build())
	}
}

// associationTarget returns the declared target of relation, an association of
// source under s, or the zero identity when s declares no such association.
func associationTarget(s *schema.Schema, source schema.TypeID, relation string) schema.TypeID {
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

// recordFacts holds parts to the facts [Graph.Add] derives its records by, which
// no single record shows: every root holds an edge or a record under each
// required association of its type; an absent or empty record stands alone,
// once, and only under a required association; and a target_missing record
// names a target no root holds, since Add resolves one that is present.
// Each break is a caller's, so it is Fatal E_INTERNAL. Every record counts as
// held where it stands, and a record a rule refused is not judged again. Each
// fact refuses the records it reports, so no (one) count holds them; the
// records beside them stay in it. An edge or a record whose source type s
// cannot resolve could stand under any root at its key, so no root there draws
// the required association's fact for it.
func recordFacts(r *structureRules, parts SnapshotParts) {
	at := func(id schema.TypeID, k immutable.Key, relation string) oneSlot {
		return oneSlot{source: id, key: r.canon.key(id, k).String(), relation: relation}
	}
	internal := func(format string, args ...any) {
		r.c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL, r.op+": "+fmt.Sprintf(format, args...)).Build())
	}
	// The facts that judge one record run first, so a record they refuse is
	// neither in the (one) count nor another record beside an absent one.
	for i, up := range parts.Unresolved {
		if r.refusedRecords[i] {
			continue
		}
		switch up.Reason {
		case "absent", "empty":
			if !requiredUnder(r.s, up.SourceType, up.Relation) {
				r.refuseRecord(i)
				internal("unresolved record %d states reason %q under %q of %s[%s], which is optional, where Graph.Add records none",
					i, up.Reason, up.Relation, up.SourceType, diag.ValueText(r.canon.key(up.SourceType, up.SourceKey).String()))
			}
		case "target_missing":
			target := associationTarget(r.s, up.SourceType, up.Relation)
			// A root whose key the key rule refused is one Graph.Add holds not, so
			// a record naming it is no second break.
			if !target.IsZero() && r.addresses[oneSlot{source: target, key: r.canon.key(target, up.TargetKey).String()}] {
				r.refuseRecord(i)
				internal("unresolved record %d names target %s[%s], which a root holds, where Graph.Add resolves an edge",
					i, target, diag.ValueText(r.canon.key(target, up.TargetKey).String()))
			}
		}
	}
	// held and missing count every record where it stands, for the required
	// association fact; beside counts the records no rule refused, for the
	// fact that an absent or empty record stands alone.
	held := make(map[oneSlot]int, len(parts.Edges)+len(parts.Unresolved))
	beside := make(map[oneSlot]int, len(parts.Edges)+len(parts.Unresolved))
	missing := make(map[oneSlot]int)
	for i, ep := range parts.Edges {
		slot := at(ep.SourceType, ep.SourceKey, ep.Relation)
		held[slot]++
		if !r.refusedEdges[i] {
			beside[slot]++
		}
	}
	for i, up := range parts.Unresolved {
		slot := at(up.SourceType, up.SourceKey, up.Relation)
		if up.Reason == "absent" || up.Reason == "empty" {
			missing[slot]++
		} else {
			held[slot]++
		}
		if !r.refusedRecords[i] {
			beside[slot]++
		}
	}
	type untypedRecord struct {
		relation string
		key      immutable.Key
	}
	var untyped []untypedRecord
	for _, ep := range parts.Edges {
		if _, ok := r.s.TypeByID(ep.SourceType); !ok {
			untyped = append(untyped, untypedRecord{relation: ep.Relation, key: ep.SourceKey})
		}
	}
	for _, up := range parts.Unresolved {
		if _, ok := r.s.TypeByID(up.SourceType); !ok {
			untyped = append(untyped, untypedRecord{relation: up.Relation, key: up.SourceKey})
		}
	}
	// Each type and relation reads the records once, however many roots ask.
	names := make(map[oneSlot]map[string]bool)
	mayName := func(slot oneSlot) bool {
		at := oneSlot{source: slot.source, relation: slot.relation}
		keys, read := names[at]
		if !read {
			keys = make(map[string]bool)
			for _, u := range untyped {
				if u.relation == slot.relation {
					keys[r.canon.key(slot.source, u.key).String()] = true
				}
			}
			names[at] = keys
		}
		return keys[slot.key]
	}
	reported := make(map[oneSlot]bool)
	for i, up := range parts.Unresolved {
		if r.refusedRecords[i] || (up.Reason != "absent" && up.Reason != "empty") {
			continue
		}
		slot := at(up.SourceType, up.SourceKey, up.Relation)
		if r.unjudged(slot) || !r.addresses[oneSlot{source: slot.source, key: slot.key}] || beside[slot] < 2 {
			continue
		}
		// Every absent and empty record of the slot is the one break, reported
		// once.
		r.refuseRecord(i)
		if !reported[slot] {
			reported[slot] = true
			internal("%q of %s[%s] holds an %s record beside another record, where Graph.Add records it alone",
				up.Relation, up.SourceType, diag.ValueText(slot.key), up.Reason)
		}
	}
	for _, ip := range parts.Instances {
		t, ok := r.s.TypeByID(ip.TypeID)
		// A root whose key the key rule refused is judged by no record fact. An
		// address two roots share holds no record for either when it holds none.
		if !ok || !r.addresses[at(ip.TypeID, ip.PrimaryKey, "")] {
			continue
		}
		for rel := range t.AllAssociations() {
			// Two roots at one address are one slot, met twice here.
			if slot := at(ip.TypeID, ip.PrimaryKey, rel.Name()); !rel.IsOptional() && held[slot]+missing[slot] == 0 && !reported[slot] &&
				!mayName(slot) {
				reported[slot] = true
				internal("root %s[%s] holds no edge and no record under the required association %q, where Graph.Add records an absent one",
					ip.TypeID, diag.ValueText(slot.key), rel.Name())
			}
		}
	}
}

// requiredUnder reports whether relation is a required association of source
// under s, as [Graph.Add] derives an unresolved record's Required.
func requiredUnder(s *schema.Schema, source schema.TypeID, relation string) bool {
	t, ok := s.TypeByID(source)
	if !ok {
		return false
	}
	rel, ok := t.Relation(relation)
	return ok && rel.IsAssociation() && !rel.IsOptional()
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

func (r *structureRules) refuseRecord(i int) {
	if r.refusedRecords == nil {
		r.refusedRecords = make(map[int]bool)
	}
	r.refusedRecords[i] = true
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
					r.op, slot.relation, name, diag.ValueText(slot.key), n)).
				WithDetail(diag.DetailKeyTypeName, name).
				WithDetail(diag.DetailKeyRelationName, slot.relation).Build())
		}
	}
}

// keyArity returns the number of primary-key components t declares.
func keyArity(t *schema.Type) int {
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
