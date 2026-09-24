package graph

import (
	"fmt"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// structureRules holds a snapshot to the structural rules [Graph.Add] applies
// as it builds, which the package doc's "Structural facts" section lists.
// [RebuildSnapshot] and [NewFromSnapshot] both apply this one definition, so no
// constructor installs what another refuses.
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
	// addresses holds each root's canonical address, relation left empty.
	addresses map[oneSlot]bool
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

// checkTree applies every per-instance rule to n and to each composed child at
// every depth: identity, then — for an identity that resolves — each slot, the
// stored key and the declared names.
func checkTree[N structureNode[N]](r *structureRules, position string, n N) {
	id := n.nodeType()
	if !r.identity(id, positionAt(position, n.nodeKey().String())) {
		for _, children := range n.nodeComposed() {
			for _, child := range children {
				checkTree(r, "composed child", child)
			}
		}
		return
	}
	t, _ := r.s.TypeByID(id)
	for relName, children := range n.nodeComposed() {
		checkSlot(r, t, n, relName, children)
		for _, child := range children {
			checkTree(r, "composed child", child)
		}
	}
	r.key(position, t, id, n.nodeKey(), n.nodeProperties())
	r.names(position, t, id, n.nodeKey(), n.nodeProperties())
}

// identity refuses the zero identity and one s cannot resolve, and reports
// whether id names a type of s. at names where id stands.
func (r *structureRules) identity(id schema.TypeID, at string) bool {
	if id.IsZero() {
		r.refuse(diag.E_GRAPH_TYPE_NOT_FOUND, "zero type identity at "+at)
		return false
	}
	if _, ok := r.s.TypeByID(id); !ok {
		r.refuse(diag.E_GRAPH_TYPE_NOT_FOUND, fmt.Sprintf("unresolvable type identity %s at %s", id, at))
		return false
	}
	return true
}

// positionAt names an instance position and the key standing there.
func positionAt(position, key string) string {
	return position + " position, key " + key
}

// typesEntry refuses a types entry that is zero, that s cannot resolve, or
// that cannot hold a root instance, whether or not the snapshot holds one:
// adapter/json and adapter/csv key their output by a denoted type's name.
func (r *structureRules) typesEntry(i int, id schema.TypeID) {
	if !r.identity(id, fmt.Sprintf("types entry %d", i)) {
		return
	}
	if code, rule := rootIneligibility(r.s, id); rule != "" {
		r.refuse(code, fmt.Sprintf("types entry %d denotes %s, which %s", i, id, rule))
	}
}

// rootType refuses n root instances of a type that cannot hold one.
func (r *structureRules) rootType(id schema.TypeID, n int) {
	if code, rule := rootIneligibility(r.s, id); rule != "" {
		r.refuse(code, fmt.Sprintf("%d root instance(s) of type %s, which %s", n, id, rule))
	}
}

// derivedConflict returns the instance a duplicate of inst collided with, as
// [Graph.Add] and [Graph.AddComposed] record it, or nil and the reason no
// instance at its position is one: the root at inst's own type and key for a
// root duplicate, and for a composed duplicate of parent under relation the
// sole occupant of a (one) slot, or the child of a keyed (many) slot at inst's
// key. Keys compare in canonical form under s, so it judges an imported
// snapshot's records under the importing schema. root is read only for a root
// duplicate, and a composed duplicate's parent type must resolve under s.
func derivedConflict(s *schema.Schema, canon *canonicalizer, root func(schema.TypeID, string) *Instance,
	inst, parent *Instance, relation string,
) (*Instance, string) {
	if relation == "" {
		if c := root(inst.typeID, inst.primaryKey.String()); c != nil {
			return c, ""
		}
		return nil, "is a root duplicate, and no root is at its own type and key"
	}
	pt, _ := s.TypeByID(parent.typeID)
	rel, ok := pt.Relation(relation)
	if !ok || rel.Kind() != schema.RelationComposition {
		return nil, fmt.Sprintf("names %q, which %s does not declare as a composition", relation, parent.typeID)
	}
	if inst.typeID != rel.TargetID() {
		return nil, fmt.Sprintf("is of type %s; the composition %q declares %s", inst.typeID, relation, rel.TargetID())
	}
	occupants := parent.composed[relation]
	if !rel.IsMany() {
		if len(occupants) == 1 {
			return occupants[0], ""
		}
		return nil, fmt.Sprintf("is under the (one) composition %q, which holds %d children", relation, len(occupants))
	}
	if target, _ := s.TypeByID(rel.TargetID()); !target.HasPrimaryKey() {
		return nil, fmt.Sprintf("is under the keyless (many) composition %q, where no child conflicts", relation)
	}
	want := canon.key(rel.TargetID(), inst.primaryKey).String()
	for _, o := range occupants {
		if canon.key(rel.TargetID(), o.primaryKey).String() == want {
			return o, ""
		}
	}
	return nil, fmt.Sprintf("holds no child of the composition %q at its key", relation)
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
// does not resolve is the identity rule's to report.
func checkSlot[N structureNode[N]](r *structureRules, t *schema.Type, parent N, relName string, children []N) {
	rel, ok := t.Relation(relName)
	if ok && rel.Kind() == schema.RelationComposition && (rel.IsMany() || len(children) <= 1) && allOfType(children, rel.TargetID()) {
		siblingKeys(r, parent, rel, children)
		return
	}
	parentName := schema.TagForm(r.s, parent.nodeType())
	if !ok || rel.Kind() != schema.RelationComposition {
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
			fmt.Sprintf("%s: %s[%s] holds composed children under %q, which its type does not declare as a composition",
				r.op, parentName, parent.nodeKey().String(), relName)).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relName).Build())
		return
	}
	if !rel.IsMany() && len(children) > 1 {
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
			fmt.Sprintf("%s: composition %q: (one) cardinality violated, got %d children", r.op, relName, len(children))).
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
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_COMPOSITION,
			fmt.Sprintf("%s: %s[%s] composition %q child %d is of type %s; the composition declares %s",
				r.op, parentName, parent.nodeKey().String(), relName, i, got, want)).
			WithDetail(diag.DetailKeyTypeName, parentName).
			WithDetail(diag.DetailKeyRelationName, relName).
			WithDetail(diag.DetailKeyExpected, want.String()).
			WithDetail(diag.DetailKeyGot, got.String()).Build())
	}
}

// siblingKeys refuses two children of one (many) slot at one canonical key,
// as [Graph.Add] refuses them. A keyless part has no key to repeat.
func siblingKeys[N structureNode[N]](r *structureRules, parent N, rel *schema.Relation, children []N) {
	if !rel.IsMany() || len(children) < 2 {
		return
	}
	target, ok := r.s.TypeByID(rel.TargetID())
	if !ok || !target.HasPrimaryKey() {
		return
	}
	seen := make(map[string]int, len(children))
	for i, child := range children {
		key := r.canon.key(rel.TargetID(), child.nodeKey()).String()
		if first, dup := seen[key]; dup {
			parentName := schema.TagForm(r.s, parent.nodeType())
			r.c.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_COMPOSED_PK,
				fmt.Sprintf("%s: %s[%s] composition %q holds children %d and %d at key %s",
					r.op, parentName, parent.nodeKey().String(), rel.Name(), first, i, key)).
				WithDetail(diag.DetailKeyTypeName, parentName).
				WithDetail(diag.DetailKeyRelationName, rel.Name()).
				WithDetail(diag.DetailKeyPrimaryKey, key).Build())
			continue
		}
		seen[key] = i
	}
}

// address refuses a second root of id at one canonical key: an index holds one
// instance per address, so the second would be dropped or shadowed.
func (r *structureRules) address(id schema.TypeID, key immutable.Key) {
	if r.addresses == nil {
		r.addresses = make(map[oneSlot]bool)
	}
	slot := oneSlot{source: id, key: r.canon.key(id, key).String()}
	if r.addresses[slot] {
		r.refuse(diag.E_DUPLICATE_PK, fmt.Sprintf("two instances of type %s at address %s: their keys are one under this schema", id, slot.key))
		return
	}
	r.addresses[slot] = true
}

// allOfType reports whether every child is an instance of want.
func allOfType[N structureNode[N]](children []N, want schema.TypeID) bool {
	for _, child := range children {
		if child.nodeType() != want {
			return false
		}
	}
	return true
}

// key holds a keyed instance's stored key to the key its own properties state,
// by [checkKey], the rule [Graph.Add] applies. An address the data contradicts
// is a forged address: an edge to it writes a foreign key that reads back
// addressing another instance, or none.
func (r *structureRules) key(position string, t *schema.Type, id schema.TypeID, key immutable.Key, props immutable.Properties) {
	if !t.HasPrimaryKey() {
		return
	}
	if err := checkKey(t, key, props.Get, r.canon); err != nil {
		name := schema.TagForm(r.s, id)
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_INVALID_PK,
			fmt.Sprintf("%s: %s of type %s at %s: %s", r.op, position, name, key.String(), err)).
			WithDetail(diag.DetailKeyTypeName, name).
			WithDetail(diag.DetailKeyPrimaryKey, key.String()).Build())
	}
}

// names refuses a stored property name t does not declare, own or inherited.
// The validator stores values under declared names alone, and [Graph.Add]
// refuses any other.
func (r *structureRules) names(position string, t *schema.Type, id schema.TypeID, key immutable.Key, props immutable.Properties) {
	for _, name := range undeclaredNames(props, func(n string) bool { _, ok := t.Property(n); return ok }) {
		r.refuse(diag.E_UNKNOWN_FIELD, fmt.Sprintf("%s %s[%s] holds property %q, which it does not declare",
			position, id, key.String(), name))
	}
}

// associationRecord is one record under an association as the rules read it:
// a resolved edge, whose reason is "", or an unresolved record.
type associationRecord struct {
	// position names the record kind in every message.
	position  string
	source    schema.TypeID
	sourceKey immutable.Key
	relation  string
	// target is the type of the instance an imported edge points at, and zero
	// where the record's target is the association's own.
	target     schema.TypeID
	targetKey  immutable.Key
	reason     string
	properties immutable.Properties
}

// association holds one record to what [Graph.Add] stages, as the package
// doc's "Structural facts" section lists it. A record whose source or target
// identity does not resolve is the identity rule's to report.
func (r *structureRules) association(rec associationRecord) {
	t, ok := r.s.TypeByID(rec.source)
	if !ok {
		return
	}
	sourceName := schema.TagForm(r.s, rec.source)
	at := fmt.Sprintf("%s %s[%s] under %q", rec.position, sourceName, rec.sourceKey.String(), rec.relation)
	rel, ok := t.Relation(rec.relation)
	if !ok || !rel.IsAssociation() {
		r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
			fmt.Sprintf("%s: %s, which its type does not declare as an association", r.op, at)).
			WithDetail(diag.DetailKeyTypeName, sourceName).
			WithDetail(diag.DetailKeyRelationName, rec.relation).Build())
		return
	}
	for _, name := range undeclaredNames(rec.properties, func(n string) bool { _, ok := rel.Property(n); return ok }) {
		r.refuse(diag.E_UNKNOWN_EDGE_FIELD, fmt.Sprintf("%s holds edge property %q, which the association does not declare", at, name))
	}
	missing := rec.reason == "absent" || rec.reason == "empty"
	if targetType, resolves := r.s.TypeByID(rel.TargetID()); resolves {
		if !rec.target.IsZero() && rec.target != rel.TargetID() {
			r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_UNKNOWN_RELATION,
				fmt.Sprintf("%s: %s targets %s; the association declares %s", r.op, at, rec.target, rel.TargetID())).
				WithDetail(diag.DetailKeyTypeName, sourceName).
				WithDetail(diag.DetailKeyRelationName, rec.relation).
				WithDetail(diag.DetailKeyExpected, rel.TargetID().String()).
				WithDetail(diag.DetailKeyGot, rec.target.String()).Build())
		} else if !missing {
			var msg string
			if declared := keyArity(targetType); rec.targetKey.Len() != declared {
				msg = fmt.Sprintf("carries a %d-part target key; the target declares %d", rec.targetKey.Len(), declared)
			} else if i := unreadableComponent(rec.targetKey); i >= 0 {
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
	if !rel.IsMany() {
		r.countOne(oneSlot{source: rec.source, key: r.canon.key(rec.source, rec.sourceKey).String(), relation: rec.relation})
	}
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
			fmt.Sprintf("%s: unresolved record states reason %q, which is not one of target_missing, absent or empty", r.op, reason)).Build())
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
// Each break is a caller's, so it is Fatal E_INTERNAL.
func recordFacts(r *structureRules, parts SnapshotParts) {
	at := func(id schema.TypeID, k immutable.Key, relation string) oneSlot {
		return oneSlot{source: id, key: r.canon.key(id, k).String(), relation: relation}
	}
	internal := func(format string, args ...any) {
		r.c.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL, r.op+": "+fmt.Sprintf(format, args...)).Build())
	}
	held := make(map[oneSlot]int, len(parts.Edges)+len(parts.Unresolved))
	missing := make(map[oneSlot]int)
	for _, ep := range parts.Edges {
		held[at(ep.SourceType, ep.SourceKey, ep.Relation)]++
	}
	for _, up := range parts.Unresolved {
		if up.Reason == "absent" || up.Reason == "empty" {
			missing[at(up.SourceType, up.SourceKey, up.Relation)]++
		} else {
			held[at(up.SourceType, up.SourceKey, up.Relation)]++
		}
	}
	reported := make(map[oneSlot]bool)
	for i, up := range parts.Unresolved {
		slot := at(up.SourceType, up.SourceKey, up.Relation)
		switch up.Reason {
		case "absent", "empty":
			switch {
			case !requiredUnder(r.s, up.SourceType, up.Relation):
				internal("unresolved record %d states reason %q under %q of %s[%s], which is optional, where Graph.Add records none",
					i, up.Reason, up.Relation, up.SourceType, slot.key)
			case !reported[slot] && (missing[slot] > 1 || held[slot] > 0):
				reported[slot] = true
				internal("%q of %s[%s] holds an %s record beside another record, where Graph.Add records it alone",
					up.Relation, up.SourceType, slot.key, up.Reason)
			}
		case "target_missing":
			target := associationTarget(r.s, up.SourceType, up.Relation)
			if !target.IsZero() && r.addresses[oneSlot{source: target, key: r.canon.key(target, up.TargetKey).String()}] {
				internal("unresolved record %d names target %s[%s], which a root holds, where Graph.Add resolves an edge",
					i, target, r.canon.key(target, up.TargetKey).String())
			}
		}
	}
	for _, ip := range parts.Instances {
		t, ok := r.s.TypeByID(ip.TypeID)
		if !ok {
			continue
		}
		for rel := range t.AllAssociations() {
			if slot := at(ip.TypeID, ip.PrimaryKey, rel.Name()); !rel.IsOptional() && held[slot]+missing[slot] == 0 {
				internal("root %s[%s] holds no edge and no record under the required association %q, where Graph.Add records an absent one",
					ip.TypeID, slot.key, rel.Name())
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

// oneOverflow refuses each (one) association holding more than one record,
// edges and unresolved records together: [Graph.Add] stages at most one record
// per (one) association.
func (r *structureRules) oneOverflow() {
	for _, slot := range r.order {
		if n := r.ones[slot]; n > 1 {
			name := schema.TagForm(r.s, slot.source)
			r.c.Collect(diag.NewIssue(diag.Error, diag.E_GRAPH_CARDINALITY,
				fmt.Sprintf("%s: (one) association %q of %s[%s] holds %d records",
					r.op, slot.relation, name, slot.key, n)).
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
