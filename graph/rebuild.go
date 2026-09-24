package graph

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// SnapshotParts holds the pre-resolved data needed to construct a Snapshot
// without running the graph construction pipeline. Edges and records address
// instances by (type identity, key), never by pointer; [RebuildSnapshot]
// resolves them through the instance index.
// Every type reference below is a [schema.TypeID]. A name cannot carry a
// transitively imported type or separate two same-named types, so no position
// takes one.
//
// Instances holds every root instance, and each is filed under its own
// [InstanceParts.TypeID]: a root carries one identity, so no group key beside
// it can disagree with it. Types lists the types the snapshot denotes; every
// root's type joins it, so an instance cannot be filed under a type the
// snapshot does not report.
type SnapshotParts struct {
	Types      []schema.TypeID
	Instances  []InstanceParts
	Edges      []EdgeParts
	Duplicates []DuplicateParts
	Unresolved []UnresolvedParts

	// Attestation carries the loaded header's validity claim verbatim, and is
	// nil for a document that carries none. RebuildSnapshot stores it without
	// derivation: a rebuilt instance cannot prove validity, only the writing
	// library's claim survives — and a document that claimed nothing must not
	// come back claiming both dimensions are false.
	Attestation *Attestation
}

// InstanceParts holds the data for a single instance. Composed children
// are nested recursively. The instance's name is rendered from TypeID under
// the schema, as [Graph.Add] renders it.
//
// Composed children keep the order the caller gives: RebuildSnapshot does not
// re-sort them, and the writers emit them in that order.
type InstanceParts struct {
	TypeID     schema.TypeID
	PrimaryKey immutable.Key
	Properties immutable.Properties
	Composed   map[string][]InstanceParts
	Provenance *location.Provenance
}

// EdgeParts holds the data for a single resolved edge. Source and target
// are identified by (type identity, primary key) rather than pointer;
// RebuildSnapshot resolves them to *Instance pointers via the instance index.
type EdgeParts struct {
	Relation   string
	SourceType schema.TypeID
	SourceKey  immutable.Key
	TargetType schema.TypeID
	TargetKey  immutable.Key
	Properties immutable.Properties
}

// DuplicateParts holds the data for a single duplicate record. Type and Key
// identify the rejected instance; ConflictType and ConflictKey address the
// instance it collided with. A root conflict resolves through the instance
// index; a composed conflict resolves through the parent slot named by
// ParentType, ParentKey and Relation — set only for a composed-child
// duplicate — where the stated key selects among siblings and an empty key
// addresses the slot's sole occupant.
type DuplicateParts struct {
	Type         schema.TypeID
	Key          immutable.Key
	Instance     InstanceParts
	ConflictType schema.TypeID
	ConflictKey  immutable.Key
	ParentType   schema.TypeID
	ParentKey    immutable.Key
	Relation     string
}

// UnresolvedParts holds the data for a single unresolved edge record.
//
// Properties carries the edge property values declared on the forward
// reference. Populated only when Reason is "target_missing"; empty
// otherwise. Loaded from the "properties" field on unresolved-edge
// entries, which every readable wire format carries.
//
// TargetType is an identity even though no instance of it is present — it is
// what a later [Graph.Add] resolves an arriving target against, so a name
// there would have to be re-resolved and could bind to the wrong type.
//
// No field states whether the association is required: RebuildSnapshot reads
// it from the schema, as [Graph.Add] does, so a record cannot disagree with
// the relation it names.
type UnresolvedParts struct {
	SourceType schema.TypeID
	SourceKey  immutable.Key
	Relation   string
	TargetType schema.TypeID
	TargetKey  immutable.Key
	Reason     string
	Properties immutable.Properties
}

// RebuildSnapshot constructs a Snapshot from pre-resolved parts.
//
// This is the deserialization entry point for the snapshot package.
// It accepts fully-resolved data (instances, edges, diagnostics) and
// assembles them into an immutable Snapshot without re-running validation
// or edge resolution.
//
// Most users should construct snapshots via [Graph.Add] + [Graph.Snapshot].
// RebuildSnapshot exists for [snapshot.Load] and testing.
//
// RebuildSnapshot does not accept a context.Context. Its work is
// proportional to the already-decoded Parts data and completes in bounded
// time. Context cancellation is checked during the streaming decode phase
// that precedes RebuildSnapshot; see snapshot.Load.
//
// Property values are rewritten into the representation their schema
// constraint stores — a Timestamp, Date or UUID reaches the same text a
// validated value would. A value the constraint cannot render is kept as it
// arrived, because this entry point reconstructs a document rather than
// validating one, and a document written before that rule existed must still
// load. Primary keys are rewritten the same way, before the instance index is
// built, and every position that addresses an instance moves with them, so an
// edge or a duplicate record spelled another way still resolves.
//
// Parts are held to the package doc's "Structural facts", under the codes it
// names: Fatal E_INTERNAL for identity, root and denoted types, declared names,
// root addresses, reasons and duplicate records, and Add's codes for slots,
// keys and associations. An edge or record addressing an instance the parts do
// not hold is Fatal E_INTERNAL too. snapshot.Load holds a document to the same facts
// first, so a failure it passes here is a bug in the reader.
//
// Panics if s is nil (programmer error).
func RebuildSnapshot(s *schema.Schema, parts SnapshotParts) (*Snapshot, diag.Result) {
	if s == nil {
		panic("graph.RebuildSnapshot: nil Schema")
	}
	collector := diag.NewCollector(0)

	// Values arrive in whatever representation the caller assembled them in.
	// Rewriting them here is what keeps a graph rebuilt from parts equal to
	// one built through validation, so a snapshot round trip reaches a
	// fixpoint on the first pass.
	canon := newCanonicalizer(s)

	validateParts(&structureRules{s: s, canon: canon, c: collector, op: "RebuildSnapshot"}, parts)
	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Step 1: Create Instance objects, each filed under its own identity.
	perType := make(map[schema.TypeID]int)
	for _, ip := range parts.Instances {
		perType[ip.TypeID]++
	}
	instances := make(map[schema.TypeID][]*Instance, len(perType))
	instanceIndex := make(map[schema.TypeID]map[string]*Instance, len(perType))
	types := slices.Clone(parts.Types)
	for typeID, n := range perType {
		instances[typeID] = make([]*Instance, 0, n)
		instanceIndex[typeID] = make(map[string]*Instance, n)
		types = append(types, typeID)
	}
	name := tagForms(s)

	for _, ip := range parts.Instances {
		typeID := ip.TypeID
		idx := instanceIndex[typeID]
		inst := rebuildInstance(name, canon.instance(ip))
		// Indexed by the key the instance CARRIES, never the caller's
		// spelling: every lookup below canonicalizes first, and the
		// snapshot's own accessors read this same index. The address rule
		// has refused two parts at one address.
		keyStr := inst.PrimaryKey().String()
		instances[typeID] = append(instances[typeID], inst)
		idx[keyStr] = inst
	}

	// Step 2: Create Edge objects, resolving pointers.
	edges := make([]*Edge, 0, len(parts.Edges))
	for _, ep := range parts.Edges {
		// Canonicalized BEFORE the lookup: step 1 rewrote the instance keys the
		// index is built from, so an endpoint resolved from the caller's
		// spelling would miss an instance that is present.
		ep = canon.edge(ep)
		source := lookupInstance(instanceIndex, ep.SourceType, ep.SourceKey.String())
		target := lookupInstance(instanceIndex, ep.TargetType, ep.TargetKey.String())

		if source == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: edge source %s[%s] not found in instance index",
					ep.SourceType, ep.SourceKey.String())).Build())
			continue
		}
		if target == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: edge target %s[%s] not found in instance index",
					ep.TargetType, ep.TargetKey.String())).Build())
			continue
		}

		edges = append(edges, newEdge(ep.Relation, source, target, ep.Properties))
	}

	// Step 3: Create Duplicate records.
	duplicates := make([]*Duplicate, 0, len(parts.Duplicates))
	for _, dp := range parts.Duplicates {
		// Every address in the record moves with the instances step 1 rewrote.
		dp = canon.duplicate(dp)
		// Defense-in-depth: duplicate instances must not have composed children.
		if len(dp.Instance.Composed) > 0 {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: duplicate instance %s[%s] has composed children",
					dp.Type, dp.Key.String())).Build())
			continue
		}

		// Resolve the conflict pointer.
		conflict := resolveDuplicateConflict(instanceIndex, dp)
		if conflict == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: duplicate conflict %s not found",
					describeDuplicateConflict(dp))).Build())
			continue
		}

		var parent *Instance
		if dp.Relation != "" {
			parent = lookupInstance(instanceIndex, dp.ParentType, dp.ParentKey.String())
		}

		dupInst := rebuildInstance(name, dp.Instance)
		duplicates = append(duplicates, newDuplicate(dupInst, conflict, parent, dp.Relation, diag.Issue{}))
	}

	// Step 4: Create UnresolvedEdge records.
	unresolvedEdges := make([]*UnresolvedEdge, 0, len(parts.Unresolved))
	for _, up := range parts.Unresolved {
		// Both addresses move: the source with the instances step 1 rewrote,
		// the target under the relation's declared target type, as the Add
		// path rendered it.
		up = canon.unresolved(up)
		source := lookupInstance(instanceIndex, up.SourceType, up.SourceKey.String())
		if source == nil {
			collector.Collect(diag.NewIssue(diag.Fatal, diag.E_INTERNAL,
				fmt.Sprintf("RebuildSnapshot: unresolved source %s[%s] not found in instance index",
					up.SourceType, up.SourceKey.String())).Build())
			continue
		}

		// Key.String() renders a keyless key as "[]", but UnresolvedEdge.TargetKey
		// is empty for the "absent" and "empty" reasons, which carry no target.
		targetKey := ""
		if up.TargetKey.Len() > 0 {
			targetKey = up.TargetKey.String()
		}

		unresolvedEdges = append(unresolvedEdges,
			newUnresolvedEdge(source, up.Relation, up.TargetType, targetKey, requiredUnder(s, up.SourceType, up.Relation), up.Reason, up.Properties))
	}

	if collector.HasErrors() {
		return nil, collector.Result()
	}

	// Step 5: Assemble the Snapshot. newSnapshot establishes every ordering
	// and drops the repeats step 1 appended.
	if types == nil {
		types = []schema.TypeID{}
	}

	snap := newSnapshot(s, canon, types, instances, instanceIndex, edges, duplicates, unresolvedEdges, diag.OK(), parts.Attestation)
	return snap, diag.OK()
}

// validateParts holds parts to the structural rules ([structureRules]) at
// every position: the types entries, each root and its composed children at
// every depth, each duplicate's instance, and each edge and unresolved record.
func validateParts(r *structureRules, parts SnapshotParts) {
	for i, id := range parts.Types {
		r.typesEntry(i, id)
	}

	counts := make(map[schema.TypeID]int)
	var order []schema.TypeID
	for _, ip := range parts.Instances {
		if counts[ip.TypeID] == 0 {
			order = append(order, ip.TypeID)
		}
		counts[ip.TypeID]++
	}
	for _, typeID := range order {
		r.rootType(typeID, counts[typeID])
	}
	for _, ip := range parts.Instances {
		checkTree(r, "instance", ip)
		if _, ok := r.s.TypeByID(ip.TypeID); ok {
			r.address(ip.TypeID, ip.PrimaryKey)
		}
	}

	for _, ep := range parts.Edges {
		r.identity(ep.SourceType, positionAt("edge source", ep.SourceKey.String()))
		r.identity(ep.TargetType, positionAt("edge target", ep.TargetKey.String()))
		r.association(associationRecord{
			position: "edge from", source: ep.SourceType, sourceKey: ep.SourceKey, relation: ep.Relation,
			target: ep.TargetType, targetKey: ep.TargetKey, properties: ep.Properties,
		})
	}

	for i, dp := range parts.Duplicates {
		r.identity(dp.Type, positionAt("duplicate", dp.Key.String()))
		r.duplicateRecord(i, dp)
		r.identity(dp.ConflictType, positionAt("duplicate conflict", dp.ConflictKey.String()))
		// A root duplicate names no parent, so its ParentType is zero by right.
		if dp.Relation != "" {
			r.identity(dp.ParentType, positionAt("duplicate parent", dp.ParentKey.String()))
		} else {
			r.rootDuplicate(i, dp.Type)
		}
		checkTree(r, "duplicate instance", dp.Instance)
	}

	for _, up := range parts.Unresolved {
		r.identity(up.SourceType, positionAt("unresolved source", up.SourceKey.String()))
		r.identity(up.TargetType, positionAt("unresolved target", up.TargetKey.String()))
		r.unresolvedReason(up.Reason, up.TargetKey, up.Properties)
		r.association(associationRecord{
			position: "unresolved record of", source: up.SourceType, sourceKey: up.SourceKey, relation: up.Relation,
			target: up.TargetType, targetKey: up.TargetKey, reason: up.Reason, properties: up.Properties,
		})
	}
	r.oneOverflow()
}

// rebuildInstance creates an Instance from InstanceParts, recursing into
// composed children. Rebuilt instances never report Validated: the wire
// carries the header attestation as a claim, not a per-instance proof.
func rebuildInstance(name func(schema.TypeID) string, ip InstanceParts) *Instance {
	inst := newInstance(name(ip.TypeID), ip.TypeID, ip.PrimaryKey, ip.Properties, ip.Provenance, false)

	for relName, children := range ip.Composed {
		for _, childParts := range children {
			child := rebuildInstance(name, childParts)
			inst.addComposed(relName, child)
		}
	}

	return inst
}

// tagForms returns [schema.TagForm] under s, rendered once per identity: a
// snapshot holds few types and many instances.
func tagForms(s *schema.Schema) func(schema.TypeID) string {
	names := make(map[schema.TypeID]string)
	return func(id schema.TypeID) string {
		n, ok := names[id]
		if !ok {
			n = schema.TagForm(s, id)
			names[id] = n
		}
		return n
	}
}

// resolveDuplicateConflict finds the instance a duplicate collided with, at
// the stated conflict address. Composed children never enter the instance
// index, so a composed conflict resolves through the parent's relation slot;
// slot-alone addressing needs exactly one occupant.
func resolveDuplicateConflict(index map[schema.TypeID]map[string]*Instance, dp DuplicateParts) *Instance {
	if dp.Relation == "" {
		return lookupInstance(index, dp.ConflictType, dp.ConflictKey.String())
	}
	parent := lookupInstance(index, dp.ParentType, dp.ParentKey.String())
	if parent == nil {
		return nil
	}
	occupants := parent.Composed(dp.Relation)
	if dp.ConflictKey.Len() > 0 {
		keyStr := dp.ConflictKey.String()
		for _, child := range occupants {
			if child.TypeID() == dp.ConflictType && child.PrimaryKey().String() == keyStr {
				return child
			}
		}
		return nil
	}
	if len(occupants) == 1 && occupants[0].TypeID() == dp.ConflictType {
		return occupants[0]
	}
	return nil
}

// describeDuplicateConflict renders a duplicate's stated conflict address
// for a diagnostic message.
func describeDuplicateConflict(dp DuplicateParts) string {
	if dp.Relation == "" {
		return fmt.Sprintf("%s[%s] in the instance index", dp.ConflictType, dp.ConflictKey.String())
	}
	return fmt.Sprintf("%s[%s] under %s[%s].%s",
		dp.ConflictType, dp.ConflictKey.String(), dp.ParentType, dp.ParentKey.String(), dp.Relation)
}

// lookupInstance finds an instance in the index by type identity and key string.
func lookupInstance(index map[schema.TypeID]map[string]*Instance, id schema.TypeID, keyStr string) *Instance {
	typeIdx := index[id]
	if typeIdx == nil {
		return nil
	}
	return typeIdx[keyStr]
}

// compareEdges provides the sorting comparator for edges.
// Sorts by (sourceType, sourceKey, relation, targetType, targetKey), comparing
// types by identity so two types rendering one name do not interleave.
func compareEdges(a, b *Edge) int {
	if c := cmp.Compare(a.source.typeID.String(), b.source.typeID.String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.source.primaryKey.String(), b.source.primaryKey.String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.relation, b.relation); c != 0 {
		return c
	}
	if c := cmp.Compare(a.target.typeID.String(), b.target.typeID.String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.target.primaryKey.String(), b.target.primaryKey.String()); c != 0 {
		return c
	}
	return compareProps(a.properties, b.properties)
}

// compareProps orders two property sets by walking both in sorted key order.
//
// It compares field by field rather than rendering each set to one string:
// a "name=value;" rendering collides whenever a value holds the separators, so
// an ordinary String edge property containing ';' or '=' made two distinct sets
// compare equal and put map-iteration order back into the document. Values are
// discriminated by type before value, so an int64 1 and the string "1" do not
// tie.
//
// What this does not promise: two values of one type whose %v forms are equal
// still tie. For the scalar types an edge property can hold, %#v is injective.
// A composite renders by its content (see renderValue), so allocation never
// decides, and quoting keeps one string apart from two.
func compareProps(a, b immutable.Properties) int {
	// The key order is precomputed at construction, so collecting it costs one
	// allocation and no sort. An earlier rewrite walked two iter.Pull2
	// coroutines to save that allocation and paid a coroutine pair per call
	// instead — inside three comparators a sort runs O(n log n) times.
	an := slices.Collect(a.SortedKeys())
	bn := slices.Collect(b.SortedKeys())
	for i := range min(len(an), len(bn)) {
		if c := cmp.Compare(an[i], bn[i]); c != 0 {
			return c
		}
		av, _ := a.Get(an[i])
		bv, _ := b.Get(bn[i])
		if c := cmp.Compare(renderValue(av), renderValue(bv)); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(an), len(bn))
}

// renderValue renders one property value with its stored type and its content,
// so values of different types never compare equal. A Go type name holds no '|',
// so the split is unambiguous. The content is the value's clone written with
// %#v, which quotes strings and writes a nil container apart from an empty one; a
// container's own %v would print its address.
func renderValue(v immutable.Value) string {
	return fmt.Sprintf("%T|%#v", v.Unwrap(), v.Clone())
}

// compareUnresolved orders unresolved records. Graph.pending is a map,
// so the slice these are collected into arrives in map-iteration order; an
// ordering that leaves any two distinct records equal hands their positions to
// that order, and Marshal's documented determinism fails across process runs.
// Reproduced before this comparator was completed: sixty tied pairs spread
// across sixty pending buckets produced ten distinct documents in ten runs.
func compareUnresolved(a, b *UnresolvedEdge) int {
	if c := cmp.Compare(a.Source.TypeID().String(), b.Source.TypeID().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Source.PrimaryKey().String(), b.Source.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Relation, b.Relation); c != 0 {
		return c
	}
	if c := cmp.Compare(a.TargetType.String(), b.TargetType.String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.TargetKey, b.TargetKey); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Reason, b.Reason); c != 0 {
		return c
	}
	if a.Required != b.Required {
		if a.Required {
			return 1
		}
		return -1
	}
	return compareProps(a.properties, b.properties)
}

// compareDuplicates orders duplicate records. Two rejections of one
// key against one conflict differ only in the instance they carry, so the
// rejected instance's properties are the last discriminator.
func compareDuplicates(a, b *Duplicate) int {
	if c := cmp.Compare(a.Instance.TypeID().String(), b.Instance.TypeID().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Instance.PrimaryKey().String(), b.Instance.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Relation, b.Relation); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Conflict.TypeID().String(), b.Conflict.TypeID().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Conflict.PrimaryKey().String(), b.Conflict.PrimaryKey().String()); c != 0 {
		return c
	}
	if c := cmp.Compare(parentKeyOf(a), parentKeyOf(b)); c != 0 {
		return c
	}
	if c := compareProps(a.Instance.Properties(), b.Instance.Properties()); c != 0 {
		return c
	}
	// Two rows of one file can collide with one instance carrying identical
	// properties, and differ only in where they came from. Without this arm
	// they tie, and slices.SortFunc is not stable, so a consumer pairing the
	// Nth record with the Nth input row pairs the wrong span.
	return cmp.Compare(provenanceKeyOf(a.Instance), provenanceKeyOf(b.Instance))
}

// provenanceKeyOf renders an instance's source position for ordering.
//
// A loaded instance carries a provenance only when the document's instance did:
// the decoder guards on a present provenance and the writer emits none for an
// instance without one. Where it does, the span is zero because the decoder
// builds it that way, so within one source name every loaded record ties at 0:0
// and across source names the arm orders by name — a stable order, kept. An
// instance with no provenance renders empty and ties.
func provenanceKeyOf(i *Instance) string {
	prov := i.Provenance()
	if prov == nil {
		return ""
	}
	span := prov.Span()
	return fmt.Sprintf("%s\x00%d:%d", prov.SourceName(), span.Start.Line, span.Start.Column)
}

// parentKeyOf renders a duplicate's parent slot, or the empty string for a root
// duplicate. The wire carries the parent coordinates, so two composed
// duplicates rejected from different slots are different records and an
// ordering that ignores the parent leaves their positions to the input order.
func parentKeyOf(d *Duplicate) string {
	if d.Parent == nil {
		return ""
	}
	return d.Parent.TypeID().String() + "\x00" + d.Parent.PrimaryKey().String()
}
