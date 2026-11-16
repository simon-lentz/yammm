package schema

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
)

// visitState tracks DFS progress for cycle detection.
type visitState int

const (
	unvisited visitState = iota
	visiting
	visited
)

// detectCycles detects inheritance cycles using DFS.
// Reports E_INHERIT_CYCLE with the full cycle path.
func (c *completer) detectCycles() bool {
	state := make(map[string]visitState)
	stack := make([]string, 0, 16)
	ok := true

	var dfs func(name string)
	dfs = func(name string) {
		if state[name] == visited {
			return
		}
		if state[name] == visiting {
			// Cycle detected - build the path
			cycle := buildCyclePath(stack, name)
			t := c.typeIndex[name]
			span := t.Span()
			c.errorf(span, diag.E_INHERIT_CYCLE,
				"inheritance cycle detected: %s", strings.Join(cycle, " -> "))
			ok = false
			return
		}

		state[name] = visiting
		stack = append(stack, name)

		t := c.typeIndex[name]
		if !t.IsZero() {
			for ref := range t.Inherits() {
				superName := c.resolveTypeRefName(ref)
				if superName != "" {
					dfs(superName)
				}
			}
		}

		state[name] = visited
		stack = stack[:len(stack)-1]
	}

	// Declaration order, so a cycle's message and span do not move between loads.
	for _, t := range c.schema.types {
		if state[t.Name()] == unvisited {
			dfs(t.Name())
		}
	}

	return ok
}

// buildCyclePath creates the cycle path from the DFS stack.
func buildCyclePath(stack []string, target string) []string {
	// Find where the cycle starts
	idx := -1
	for i, name := range stack {
		if name == target {
			idx = i
			break
		}
	}
	if idx == -1 {
		// Target not in stack - shouldn't happen
		return append(append([]string(nil), stack...), target)
	}
	// Return the cycle portion plus the target again to show the loop
	return append(append([]string(nil), stack[idx:]...), target)
}

// resolveTypeRefName resolves a TypeRef to a type name.
// For local refs, returns the name directly.
// For qualified refs, looks up the imported schema.
func (c *completer) resolveTypeRefName(ref TypeRef) string {
	if ref.Name() == "" {
		return ""
	}

	if ref.Qualifier() == "" {
		// Local type
		if _, ok := c.typeIndex[ref.Name()]; ok {
			return ref.Name()
		}
		return ""
	}

	// Qualified type
	importedSchema, ok := c.resolveImportedSchema(ref.Qualifier())
	if !ok {
		return ""
	}

	if _, ok := importedSchema.Type(ref.Name()); ok {
		return ref.String() // Return qualified name for cross-schema types
	}

	return ""
}

// completeTypes linearizes inheritance and merges members for each type.
// Per-type findings (unknown supertypes, rejected primary-key types,
// inheritance conflicts) are collected; every type is completed so each
// carries its merged member view for the downstream phases.
func (c *completer) completeTypes() {
	completed := make(map[string]bool)

	// Set schema name on each type for cross-schema display.
	schemaName := c.schema.Name()
	for _, t := range c.schema.types {
		t.setSchemaName(schemaName)
	}

	var completeType func(t Type)
	completeType = func(t Type) {
		if completed[t.Name()] {
			return
		}

		// Mark early to handle re-entry during recursion
		completed[t.Name()] = true

		// Complete supertypes first
		supers := make([]ResolvedTypeRef, 0)
		seenSupers := make(map[TypeID]bool)

		// A cross-schema inheritance cycle cannot form: it would need mutual
		// import, which the loader refuses (E_IMPORT_CYCLE). Marking the
		// type itself keeps a local walk from adding a type to its own supers.
		seenSupers[t.ID()] = true

		// DFS traversal of inheritance, left-to-right, keep-first
		var linearize func(ref TypeRef) //nolint:staticcheck // recursive closure requires separate declaration
		linearize = func(ref TypeRef) {
			resolved := c.resolveTypeRef(ref)
			if resolved.IsZero() {
				// Defer a displaced local name, and a qualified ref when its
				// qualifier names an import the loader saw but could not resolve:
				// the duplicate or the import failure already carries the
				// root-cause diagnostic, and blaming every reference to it would
				// bury it in noise. With a resolved
				// import, a qualified supertype that still does not resolve is a genuine error (an undefined alias, or a type absent
				// from the imported schema), mirroring validateRelationTarget;
				// silently dropping it would strip the child of every inherited
				// member with no diagnostic.
				if c.referenceDeferred(ref) {
					return
				}
				c.errorf(t.Span(), diag.E_UNKNOWN_TYPE,
					"unknown type %q in extends clause of type %q", ref.String(), t.Name())
				return
			}
			if !resolved.IsAbstract() && (resolved.IsPart() != t.IsPart() || t.IsAbstract()) {
				c.errorf(ref.Span(), diag.E_INVALID_SUPERTYPE,
					"%s %q extends %s %q: a supertype is abstract or of the subtype's own kind",
					typeKindName(t), t.Name(), typeKindName(resolved), ref.String())
			}

			id := resolved.ID()
			if seenSupers[id] {
				return // Keep-first deduplication
			}
			seenSupers[id] = true

			// Complete local supertypes FIRST (before reading their ancestors).
			// This ensures SuperTypes() is populated even when derived types
			// are declared before their base types in the source file.
			if ref.Qualifier() == "" {
				if st, exists := c.typeIndex[ref.Name()]; exists {
					completeType(st)
				}
			}

			// Now read supertype's ancestors (guaranteed to be populated)
			for super := range resolved.SuperTypes() {
				superID := super.ID()
				if !seenSupers[superID] {
					seenSupers[superID] = true
					supers = append(supers, super)
				}
			}

			supers = append(supers, NewResolvedTypeRef(ref, id))
		}

		// Process declared inherits
		for ref := range t.Inherits() {
			linearize(ref)
		}

		t.setSuperTypes(supers)

		// Merge properties from ancestors, then settle their annotations.
		// The order is load-bearing: resolveInheritedAnnotations replaces
		// entries of allProps with annotation-merged clones, and
		// setAllProperties snapshots those records into the by-name index, so
		// indexing first would leave Type.Property returning the structural
		// survivor with one ancestor's annotations instead of the union.
		direct := c.directSuperTypes(t)
		allProps := c.mergeProperties(t, supers, direct)
		c.resolveInheritedAnnotations(t, direct, allProps)
		t.setAllProperties(allProps)

		// Extract primary keys. mergeProperties emits t's own properties first
		// (unmodified) and inherited ones after, so allProps[:ownPropCount] are
		// exactly t's own — an O(1), record-exact "declared on t?" test for the
		// rejection branch below, without a clone or a per-type set.
		ownPropCount := len(t.properties)
		pks := make([]Property, 0)
		for i, p := range allProps {
			if p.IsPrimaryKey() {
				// A primary whose kind is not known is a key all the same, and
				// the cause of its type's failure is already reported.
				if constraintKindUnknown(p.Constraint()) {
					pks = append(pks, p)
					continue
				}
				if !isPrimaryKeyAllowed(p.Constraint()) {
					// Report only at the declaring type. An inherited rejected
					// primary was already reported when its ancestor completed
					// (supertypes complete before their subtypes); re-checking the
					// same inherited Property at every descendant would duplicate
					// the identical diagnostic at the identical span. i < ownPropCount
					// holds exactly for t's own properties (see above).
					if i < ownPropCount {
						c.errorf(p.Span(), diag.E_INVALID_PRIMARY_KEY_TYPE,
							"property %q: %s cannot be used as a primary key (allowed: String, UUID, Date, Timestamp)",
							p.Name(), ResolveAlias(p.Constraint()).Kind())
					}
					continue
				}
				pks = append(pks, p)
			}
		}
		t.setPrimaryKeys(pks)

		// Merge associations from ancestors
		allAssocs := c.mergeRelations(t, slices.Collect(t.Associations()), supers, RelationAssociation)
		t.setAllAssociations(allAssocs)

		// Merge compositions from ancestors
		allComps := c.mergeRelations(t, slices.Collect(t.Compositions()), supers, RelationComposition)
		t.setAllCompositions(allComps)

		// Merge invariants from ancestors
		allInvs := c.mergeInvariants(t)
		t.setAllInvariants(allInvs)

		// Merge type-level annotations from ancestors
		allAnns := c.mergeAnnotations(t, supers)
		t.setAllAnnotations(allAnns)
	}

	for _, t := range c.schema.types {
		completeType(t)
	}

	// Each local supertype's list is set once: a rebuild per subtype costs the
	// cube of a chain's depth. An imported supertype is sealed and takes none.
	subs := make(map[string][]ResolvedTypeRef)
	var order []string
	for _, t := range c.schema.types {
		for super := range t.SuperTypes() {
			superID := super.ID()
			if superID.SchemaPath() != c.sourceID {
				continue
			}
			if _, ok := subs[superID.Name()]; !ok {
				order = append(order, superID.Name())
			}
			subs[superID.Name()] = append(subs[superID.Name()], ResolvedTypeRefFromType(t, superID.SchemaPath().String()))
		}
	}
	for _, name := range order {
		c.typeIndex[name].setSubTypes(subs[name])
	}
}

// resolveTypeRef resolves a TypeRef to a Type.
func (c *completer) resolveTypeRef(ref TypeRef) Type {
	if ref.Name() == "" {
		return Type{}
	}

	if ref.Qualifier() == "" {
		// Local type
		return c.typeIndex[ref.Name()]
	}

	// Qualified type
	importedSchema, ok := c.resolveImportedSchema(ref.Qualifier())
	if !ok {
		return Type{}
	}

	t, _ := importedSchema.Type(ref.Name())
	return t
}

// resolveImportedSchema resolves a non-empty import qualifier to the imported
// schema through the resolution map — read by [completer.classifyQualifier], as
// every qualifier-resolving site reads it — returning false when the qualifier
// is absent or deferred. It is the completion-time counterpart to the runtime [Schema.ResolveType] /
// [Schema.ResolveDataType], which read the wired Import objects instead; routing
// completion through classifyQualifier keeps a qualifier's deferred/resolved
// state read in one place during a load, not two.
func (c *completer) resolveImportedSchema(qualifier string) (Schema, bool) {
	state, sourceID := c.classifyQualifier(qualifier)
	if state != aliasResolved {
		return Schema{}, false
	}
	s, ok := c.loaded[sourceID]
	return s, ok
}

// resolveTypeID resolves a TypeID to a Type.
func (c *completer) resolveTypeID(id TypeID) Type {
	if id.SchemaPath() == c.sourceID {
		return c.typeIndex[id.Name()]
	}

	importedSchema, ok := c.loaded[id.SchemaPath()]
	if !ok {
		return Type{}
	}

	t, _ := importedSchema.Type(id.Name())
	return t
}

// annotationSource pairs an inherited annotation with the direct supertype whose
// merged view supplied it, so a conflict diagnostic can name where each side
// reached the type from rather than reading a declaring scope off a merged
// clone, which can name the same ancestor twice.
type annotationSource struct {
	from Type
	ann  Annotation
}

// mergeProperties merges a type's own properties with those it inherits, one
// rule per property name over every declaration of that name on its linearized
// ancestors:
//
//   - The type's own declaration must narrow (equal, or [Property.CanNarrowFrom])
//     every inherited declaration of its name, and it is the property.
//   - With no own declaration, the property is the inherited declaration that
//     narrows every other. With none, the load fails.
//
// The verdict does not depend on the order of an extends clause. A failure
// draws one E_PROPERTY_CONFLICT per type and property, unless a declaration of
// that name has a type that is not known ([typeUnknown]): its cause is already
// reported, and a known type there could make the declarations agree.
//
// Declarations that narrow each other are equal, and the merged view holds the
// one the first of direct supertypes holds, so a re-declaration stands in place
// of the declaration it re-declares. The result holds own properties in
// declaration order, then each inherited name at the place of its first
// declaration in linearization order; key order, generated field order and
// documentation order read it. Where no declaration is the property, the place
// holds the first of unknown type, so the rules that read the merged view defer
// to its reported cause, or else the first. The rule decides STRUCTURE only:
// [completer.resolveInheritedAnnotations] settles annotations.
func (c *completer) mergeProperties(t Type, supers []ResolvedTypeRef, direct []Type) []Property {
	result := slices.Collect(t.Properties())
	at := make(map[string]int, len(result))
	for i, p := range result {
		at[p.Name()] = i
	}
	ownCount := len(result)

	// The linearized ancestors are transitively closed, so reading each one's
	// own declarations reads every inherited declaration once.
	inherited := make(map[string][]Property)
	for _, superRef := range supers {
		for p := range c.resolveTypeID(superRef.ID()).Properties() {
			inherited[p.Name()] = append(inherited[p.Name()], p)
			if _, ok := at[p.Name()]; !ok {
				at[p.Name()] = len(result)
				result = append(result, p)
			}
		}
	}

	picked := make(map[string]Property)
	for i, p := range result {
		decls := inherited[p.Name()]
		if len(decls) == 0 {
			continue
		}
		unknown := slices.ContainsFunc(decls, typeUnknown)
		if i < ownCount {
			unknown = unknown || typeUnknown(p)
			var wider []Property
			for _, d := range decls {
				if !narrows(p, d) {
					wider = append(wider, d)
				}
			}
			if len(wider) == 0 {
				continue
			}
			c.conflictedProperties[p] = true
			if unknown {
				continue
			}
			related := []location.RelatedInfo{{Span: p.Span(), Message: "the type's own definition"}}
			for _, d := range wider {
				related = append(related, location.RelatedInfo{Span: d.Span(), Message: "inherited definition it does not narrow"})
			}
			c.errorfRelated(t.Span(), diag.E_PROPERTY_CONFLICT, related,
				"type %q declares property %q, which does not narrow its definition from %s",
				t.Name(), p.Name(), wider[0].DeclaringScope())
			continue
		}
		d := minimal(decls)
		apart := slices.IndexFunc(decls, func(e Property) bool { return !narrows(d, e) })
		if apart < 0 {
			result[i] = d
			picked[p.Name()] = d
			continue
		}
		if unknown {
			result[i] = decls[slices.IndexFunc(decls, typeUnknown)]
			continue
		}
		related := make([]location.RelatedInfo, len(decls))
		for j, e := range decls {
			related[j] = location.RelatedInfo{Span: e.Span(), Message: "definition declared here"}
		}
		// d is minimal, so a declaration it does not narrow cannot narrow it.
		c.errorfRelated(t.Span(), diag.E_PROPERTY_CONFLICT, related,
			"type %q inherits definitions of property %q from %s and %s, and neither narrows the other",
			t.Name(), p.Name(), d.DeclaringScope(), decls[apart].DeclaringScope())
	}

	for _, s := range direct {
		for q := range s.AllProperties() {
			if d, ok := picked[q.Name()]; ok && narrows(q, d) && narrows(d, q) {
				result[at[q.Name()]] = q
				delete(picked, q.Name())
			}
		}
	}
	return result
}

// narrows reports whether declaration a narrows declaration b: the two are
// equal, or every value a accepts b accepts.
func narrows(a, b Property) bool {
	return a.Equal(b) || a.CanNarrowFrom(b)
}

// minimal returns a declaration of decls that no other narrows strictly, and
// the first that narrows every other when one does; narrowing is a preorder,
// so a candidate that moves only to a strictly narrower one ends there.
func minimal(decls []Property) Property {
	cand := decls[0]
	for _, d := range decls[1:] {
		if narrows(d, cand) && !narrows(cand, d) {
			cand = d
		}
	}
	return cand
}

// resolveInheritedAnnotations settles the annotation set of every merged
// property, once mergeProperties has decided which Property survives under each
// name. The rule is one line:
//
//	effective(T, n) = T's own declaration's annotations, when T declares n
//	                = union over T's DIRECT supertypes S of effective(S, n)
//
// It reads each direct supertype's already-merged view through [Type.Property]
// (setAllProperties keys that index by the merged set), and supertypes complete
// before their subtypes, so every view it reads is final.
//
// Reading direct supertypes rather than the linearized ancestor list is what
// makes inheritance transitive in the ordinary sense. A direct supertype's view
// already encodes ITS decisions about the property — including a deliberate
// re-declaration that dropped an inherited annotation — so a grandparent's
// annotation cannot reappear past a parent that shadowed it, while a union
// across incomparable mixins still keeps every marker regardless of extends
// order. The linearized list is transitively closed, so consulting it here would
// re-admit that grandparent as though it were an independent sibling.
//
// Iteration is over result — a slice, in merged order — so every diagnostic this
// emits is deterministically ordered.
func (c *completer) resolveInheritedAnnotations(t Type, supers []Type, result []Property) {
	if len(supers) == 0 {
		return // Nothing to inherit; own annotations stand as declared.
	}

	ownProps := make(map[string]bool, len(t.properties))
	for _, p := range t.properties {
		ownProps[p.Name()] = true
	}

	// One inherited-annotation conflict is one diagnostic, keyed by
	// (property, annotation).
	reported := make(map[string]bool)

	// Annotation names this type's inheritance DROPPED, per property. Carried
	// forward to subtypes so a drop stays a decision rather than decaying into a
	// plain absence — see [completer.inheritedAnnotations].
	var suppressed map[string]map[string]bool
	suppress := func(prop string, names map[string]bool) {
		if len(names) == 0 {
			return
		}
		if suppressed == nil {
			suppressed = make(map[string]map[string]bool)
		}
		suppressed[prop] = names
	}

	for i, p := range result {
		name := p.Name()
		if ownProps[name] {
			// A re-declaration the load REJECTED decides nothing. Letting it
			// record a suppression would carry its opinion to every subtype,
			// where an arm that still carries the annotation reads as
			// disagreeing with it — a second error derived from a declaration
			// that is already an error, pointing the user at a subtype when the
			// only real fix is at the rejected declaration. The same set
			// suppresses its shadow warning in
			// [completer.flushShadowedAnnotations], for the same reason.
			if c.conflictedProperties[p] {
				continue
			}
			// An own declaration's annotation set is authoritative; what it drops
			// relative to its direct supertypes is what gets warned about — after
			// validation, see [completer.recordShadowedAnnotations] — and is
			// suppressed for every subtype.
			suppress(name, c.recordShadowedAnnotations(p, supers))
			continue
		}

		merged, supp := c.inheritedAnnotations(t, supers, name, reported)
		suppress(name, supp)
		if !slices.Equal(p.annotations, merged) {
			result[i] = p.cloneWithAnnotations(merged)
		}
	}

	t.setSuppressedAnnotations(suppressed)
}

// directSuperTypes resolves t's declared extends clause to the MAXIMAL direct
// supertypes, in declaration order: those no other resolved supertype descends
// from. A ref that does not resolve is skipped — the failure carries its own
// diagnostic — and a type named twice is visited once.
//
// Maximality matters because an extends clause may be redundant: `extends Drop,
// Base` where `Drop extends Base` is legal, and there Base is not a peer of Drop
// but its ancestor. Drop's merged view already reflects Base with Drop's
// decisions applied, so keeping Base would make the annotation resolver treat a
// resolved chain as two rivals — a spurious disagreement. The reduction is over
// the declared supertypes only, against the union of their transitively-closed
// [Type.SuperTypes], so a long clause stays linear in those sets; it is NOT the
// closure-wide maximal scan that made an earlier design superlinear.
func (c *completer) directSuperTypes(t Type) []Type {
	var resolved []Type
	var seen map[TypeID]bool
	for ref := range t.Inherits() {
		s := c.resolveTypeRef(ref)
		if s.IsZero() {
			continue
		}
		if seen == nil {
			seen = make(map[TypeID]bool)
		}
		if seen[s.ID()] {
			continue
		}
		seen[s.ID()] = true
		resolved = append(resolved, s)
	}
	if len(resolved) < 2 {
		return resolved
	}

	ancestors := make(map[TypeID]bool)
	for _, s := range resolved {
		for _, st := range s.superTypes {
			ancestors[st.id] = true
		}
	}
	var out []Type
	for _, cand := range resolved {
		if !ancestors[cand.ID()] {
			out = append(out, cand)
		}
	}
	return out
}

// inheritedAnnotations returns the annotations an inherited property carries in
// t's merged view: the union across t's direct supertypes' merged views, in
// direct-supertype declaration order, first occurrence of each name winning.
//
// Within ONE supertype a repeated annotation name is skipped rather than treated
// as a rival: that is a load error reported against the declaration itself
// (validatePropertyAnnotations), and comparing an annotation with its own
// duplicate here would add a second, self-contradictory "inherits conflicting @x
// … from S and S".
//
// Two supertypes DISAGREE about an annotation in two ways, both reported as
// E_INVALID_ANNOTATION and both resolved by re-stating the annotation on the
// subtype:
//
//   - Same name, different arguments — only differing @vector similarity
//     keywords in practice, since Property.Equal excludes annotations and so
//     lets two structurally equal ancestors disagree about them.
//   - One supertype carries the annotation while another SUPPRESSED it: some
//     type on that arm re-declared the property without it, and honouring
//     either arm silently overrides the other's decision.
//
// The second is why a "drop" has to be tracked separately from an absence.
// Never having declared an annotation is not an opinion — that is the ordinary
// mixin case, where one ancestor contributes the marker, the other simply has
// nothing to say, and the union is what stops extends order from deciding.
// Dropping one IS an opinion, and two opinions in conflict have no correct
// merge, so the loader refuses to pick.
//
// Both sides of a conflict are named by the CONTRIBUTING supertype, not by the
// annotation's declaring scope: the conflict is about how the two reach t, and a
// scope read off a merged clone can name the same ancestor twice. Related spans
// point at each annotation's own declaration.
//
// reported holds the (property, annotation) pairs already blamed for this type,
// so one conflict draws one diagnostic no matter how many supertypes carry the
// property forward. It returns the merged set and the names t must itself
// suppress for its own subtypes: the union of every arm's suppressions, not
// filtered by what another arm carries. A name that one arm suppresses and
// another carries is a disagreement, which this same pass reports and which
// fails the load, so no sealed schema is ever observed with such a name in the
// map. If that rule is ever relaxed so the carrying arm wins, this union must
// be filtered here or the annotation would silently read as dropped for the
// whole subtree.
func (c *completer) inheritedAnnotations(t Type, supers []Type, name string, reported map[string]bool) ([]Annotation, map[string]bool) {
	var merged []Annotation
	var byName map[string]annotationSource
	var suppressed map[string]bool

	// A suppression on any arm is a decision the whole subtree inherits.
	for _, s := range supers {
		for ann := range s.suppressedAnnotations(name) {
			if suppressed == nil {
				suppressed = make(map[string]bool)
			}
			suppressed[ann] = true
		}
	}

	for _, s := range supers {
		sp, ok := s.Property(name)
		if !ok || len(sp.annotations) == 0 {
			continue
		}
		var seenHere map[string]bool
		for _, a := range sp.annotations {
			if seenHere[a.name] {
				continue // A duplicate on one declaration; its own diagnostic owns it.
			}
			if seenHere == nil {
				seenHere = make(map[string]bool, len(sp.annotations))
			}
			seenHere[a.name] = true

			if dropper := suppressingSuper(supers, name, a.name); !dropper.IsZero() {
				c.reportAnnotationDisagreement(t, name, a, reported,
					fmt.Sprintf("type %q inherits @%s for property %q from %s, but %s drops it; re-state or omit it on %s to decide",
						t.Name(), a.name, name, s.Name(), dropper.Name(), t.Name()),
					dropper.Span(), "dropped by this type's re-declaration")
				continue
			}

			prior, dup := byName[a.name]
			switch {
			case !dup:
				if byName == nil {
					byName = make(map[string]annotationSource)
				}
				byName[a.name] = annotationSource{from: s, ann: a}
				merged = append(merged, a)
			case prior.ann.identity() == a.identity():
				// The same annotation reaching t by two routes; idempotent.
			default:
				c.reportAnnotationDisagreement(t, name, a, reported,
					fmt.Sprintf("type %q inherits conflicting @%s annotations for property %q from %s and %s",
						t.Name(), a.name, name, prior.from.Name(), s.Name()),
					prior.ann.Span(), "conflicting annotation declared here")
			}
		}
	}
	return merged, suppressed
}

// suppressingSuper returns the direct supertype that suppressed annotation ann
// on property name — the arm whose decision to drop it contradicts the decision
// of the arm that keeps it. No type both keeps and suppresses one name, so the
// arm found is never the one that keeps it.
func suppressingSuper(supers []Type, name, ann string) Type {
	for _, s := range supers {
		if s.suppressedAnnotations(name)[ann] {
			return s
		}
	}
	return Type{}
}

// reportAnnotationDisagreement reports one inherited-annotation disagreement,
// deduplicated per (property, annotation), with a related span at the other
// side.
//
// It does NOT mark the annotation in diagnosedAnnotations. That set exists to
// stop the shadow warning from advising re-statement of an annotation rejected
// AT ITS DECLARATION; a disagreement is the opposite — the annotation is
// perfectly valid, and re-stating it is the RESOLUTION the message recommends.
// Marking it would also be completer-wide, silencing a legitimate shadow warning
// for the same Annotation at an unrelated type. The disagreeing type re-declares
// nothing (a re-declaration would resolve the disagreement), so it queues no
// shadow warning of its own for the mark to suppress.
func (c *completer) reportAnnotationDisagreement(
	t Type, prop string, a Annotation, reported map[string]bool,
	msg string, otherSpan location.Span, otherLabel string,
) {
	key := prop + "\x00" + a.name
	if reported[key] {
		return
	}
	reported[key] = true
	c.errorfRelated(t.Span(), diag.E_INVALID_ANNOTATION, []location.RelatedInfo{
		{Span: a.Span(), Message: "annotation declared here"},
		{Span: otherSpan, Message: otherLabel},
	}, "%s", msg)
}

// shadowedAnnotation records one annotation that a type's own property
// declaration dropped, together with the direct supertype it came through.
// Queued during linearization and emitted after annotation validation — see
// [completer.flushShadowedAnnotations].
type shadowedAnnotation struct {
	own  Property
	from Type
	ann  Annotation
}

// shadowGroupKey groups queued shadow entries by (own declaration, contributing
// supertype) for [completer.flushShadowedAnnotations].
type shadowGroupKey struct {
	own  Property
	from Type
}

// recordShadowedAnnotations queues a W_ANNOTATION_SHADOWED for every annotation
// an own declaration drops, one entry per (direct supertype, annotation).
//
// Per supertype, not per annotation name: two incomparable mixins that both
// annotate the property are two independent declarations the re-declaration
// dropped, and the user needs to be pointed at both — even when they happen to
// use the SAME annotation name. Deduplication is by declared Annotation
// identity, so one annotation reaching t through both arms of a diamond is one
// entry, while two mixins declaring their own @index are two.
//
// Comparison against the own set is name-level: re-stating an annotation of the
// same name (any args) suppresses the warning for it.
//
// An annotation that any direct supertype SUPPRESSED is CONTESTED, and a
// re-declaration dropping a contested annotation is resolving a disagreement,
// not making a silent slip — that is precisely what the disagreement diagnostic
// told the user to do. The warning exists for the accidental drop of a
// uniformly-inherited annotation, so a contested one draws no warning.
//
// It DOES still draw a suppression entry when this declaration does not
// re-state it: the child's decision is now authoritative and has to bind its
// own subtypes, or they would re-derive the annotation from the carrying arm
// and re-raise the disagreement the child just settled.
//
// It returns the annotation names this declaration suppresses for its own
// subtypes: the uncontested annotations it dropped, plus its supertypes' still-
// live suppressions, minus anything it re-states. A re-declaration that re-states
// an annotation revives it for the whole subtree.
func (c *completer) recordShadowedAnnotations(own Property, supers []Type) map[string]bool {
	var have map[string]bool
	if len(own.annotations) > 0 {
		have = make(map[string]bool, len(own.annotations))
		for _, a := range own.annotations {
			have[a.name] = true
		}
	}

	// Annotation names some direct supertype dropped: contested, so this
	// declaration settles them silently rather than warning.
	var contested map[string]bool
	for _, s := range supers {
		for ann := range s.suppressedAnnotations(own.Name()) {
			if contested == nil {
				contested = make(map[string]bool)
			}
			contested[ann] = true
		}
	}

	var suppressed map[string]bool
	suppress := func(ann string) {
		if suppressed == nil {
			suppressed = make(map[string]bool)
		}
		suppressed[ann] = true
	}
	for ann := range contested {
		if !have[ann] {
			suppress(ann)
		}
	}

	var seen map[Annotation]bool
	for _, s := range supers {
		sp, ok := s.Property(own.Name())
		if !ok {
			continue
		}
		for _, a := range sp.annotations {
			if have[a.name] || contested[a.name] {
				continue // Re-stated, or a resolved disagreement: no warning, no drop.
			}
			suppress(a.name)
			if seen[a] {
				continue
			}
			if seen == nil {
				seen = make(map[Annotation]bool)
			}
			seen[a] = true
			c.pendingShadowed = append(c.pendingShadowed,
				shadowedAnnotation{own: own, from: s, ann: a})
		}
	}
	return suppressed
}

// flushShadowedAnnotations emits the queued W_ANNOTATION_SHADOWED warnings, one
// per (own declaration, contributing supertype), naming every annotation that
// supertype contributed and dropping a related span on each.
//
// It runs after annotation validation rather than inside linearization because
// the warning's advice — "re-state them on this declaration to keep them" — is
// actively wrong for an annotation the same load rejects: following it produces
// a second copy of the same error. Linearization cannot know that; only [completer.validateAnnotations]
// establishes which annotations are well-formed, known, correctly placed, and
// eligible for their target. Any annotation that drew a diagnostic of its own is
// therefore skipped here — as is any re-declaration the merge refused, whether
// or not E_PROPERTY_CONFLICT reports the refusal, since a rejected declaration cannot usefully be told to re-state
// anything.
//
// Re-statement is matched by NAME, so the advice stays followable even when two
// supertypes contributed the same annotation name with different arguments:
// re-stating either spelling settles it.
//
// Annotations inherited from another schema are always emitted: that schema
// completed cleanly or the load already failed, so its annotations are valid.
//
// The queue is appended in type-completion order and then merged-property order,
// so the emitted warnings are deterministically ordered.
func (c *completer) flushShadowedAnnotations() {
	type group struct {
		own  Property
		from Type
		anns []Annotation
	}
	var groups []*group
	// A typed key rather than [2]any: the pair is (own property, contributing
	// supertype), both comparable handles, so boxing them into interfaces buys
	// nothing and only risks an accidental type-mismatched lookup.
	index := make(map[shadowGroupKey]*group)

	for _, s := range c.pendingShadowed {
		if c.diagnosedAnnotations[s.ann] {
			continue // The annotation is not usable; advising its re-statement would mislead.
		}
		if c.conflictedProperties[s.own] {
			continue // The merge refused the re-declaration; nothing to advise.
		}
		key := shadowGroupKey{own: s.own, from: s.from}
		g, ok := index[key]
		if !ok {
			g = &group{own: s.own, from: s.from}
			index[key] = g
			groups = append(groups, g)
		}
		g.anns = append(g.anns, s.ann)
	}

	for _, g := range groups {
		display := make([]string, len(g.anns))
		related := make([]location.RelatedInfo, len(g.anns))
		for i, a := range g.anns {
			display[i] = "@" + a.name
			related[i] = location.RelatedInfo{
				Span:    a.Span(),
				Message: "inherited annotation declared here",
			}
		}
		// The contributing supertype is named because two incomparable mixins
		// dropping the SAME annotation name otherwise produce two byte-identical
		// warnings at one span, which reads as a duplicate rather than as two
		// declarations the re-declaration shadowed.
		msg := fmt.Sprintf(
			"re-declaration of property %q drops annotation(s) %s inherited from %s; re-state them on this declaration to keep them",
			g.own.Name(), strings.Join(display, ", "), g.from.Name(),
		)
		c.collector.Collect(diag.NewIssue(diag.Warning, diag.W_ANNOTATION_SHADOWED, msg).
			WithSpan(g.own.Span()).WithRelated(related...).Build())
	}
}

// mergeInvariants merges own invariants with those each direct supertype has
// already merged. An invariant's message is its identity: a subtype's
// declaration overrides an inherited one of the same name, one definition
// reached through two ancestors merges, and two inherited definitions of one
// name that differ are a conflict, reported once on the combining type.
//
// It reads the DIRECT supertypes' already-merged views, the rule
// [completer.resolveInheritedAnnotations] documents at length: a direct
// supertype's view already encodes ITS override, so a grandparent's invariant
// cannot reappear past a parent that replaced it. The linearized list would
// admit the grandparent as a peer. Properties judge that list, since an override
// must narrow what it overrides, and take their record from the direct
// supertypes; no such relation orders invariant expressions.
//
// A definition holding a literal with no value differs from another only when
// no value would make the two one, so the definitions of a name are unified,
// not compared with the first alone: a later definition conflicts when it
// cannot be made one with every definition merged before it.
func (c *completer) mergeInvariants(t Type) []Invariant {
	result := slices.Collect(t.Invariants())
	own := make(map[string]bool, len(result))
	for _, inv := range result {
		own[inv.Name()] = true
	}

	// One conflict is one diagnostic naming every rival, as mergeRelations
	// reports a relation clash: the walk collects, then reports once per name.
	inherited := make(map[string]Invariant)
	merged := make(map[string]Invariant)
	at := make(map[string]int)
	rivals := make(map[string][]Invariant)
	var conflicted []string
	for _, superType := range c.directSuperTypes(t) {
		for inv := range superType.AllInvariants() {
			if own[inv.Name()] {
				continue
			}
			if first, seen := inherited[inv.Name()]; seen {
				if first == inv || slices.Contains(rivals[inv.Name()], inv) {
					continue
				}
				if one, ok := c.unifyInvariants(merged[inv.Name()], inv); ok {
					merged[inv.Name()] = one
					continue
				}
				if rivals[inv.Name()] == nil {
					conflicted = append(conflicted, inv.Name())
				}
				rivals[inv.Name()] = append(rivals[inv.Name()], inv)
				continue
			}
			inherited[inv.Name()] = inv
			merged[inv.Name()] = inv
			at[inv.Name()] = len(result)
			result = append(result, inv)
		}
	}
	// A unification that gave a literal with no value the value another
	// definition writes is what a subtype inherits, so a later merge is held
	// to it too.
	for name, one := range merged {
		result[at[name]] = one
	}

	for _, name := range conflicted {
		kept := inherited[name]
		related := []location.RelatedInfo{{Span: kept.Span(), Message: "kept definition declared here"}}
		for _, rival := range rivals[name] {
			related = append(related, location.RelatedInfo{Span: rival.Span(), Message: "conflicting definition declared here"})
		}
		c.errorfRelated(t.Span(), diag.E_INVARIANT_CONFLICT, related,
			"type %q inherits definitions of invariant %q with different expressions "+
				"(expressions are compared structurally: 1 and 1.0 are different literals)",
			t.Name(), name)
	}

	return result
}

// sameExpression reports whether two expressions serialize to the same bytes
// under the structural hash's encoding, which is the one canonical form the
// package already defines for an expression.
func sameExpression(a, b expr.Expression) bool {
	var ba, bb bytes.Buffer
	hashExpression(&ba, a)
	hashExpression(&bb, b)
	return bytes.Equal(ba.Bytes(), bb.Bytes())
}

// unifyInvariants returns the one definition a and b both are once each
// literal with no value in either is given a value, and whether there is one.
// A literal with no value is one with a literal of the kind the source wrote,
// which it then becomes, or with another such literal of that kind. With no
// such literal in either, a and b are one when their expressions are the same,
// and the result is a.
func (c *completer) unifyInvariants(a, b Invariant) (Invariant, bool) {
	if len(c.noValue) == 0 {
		return a, sameExpression(a.expr, b.expr)
	}
	e, ok := unifyNode(a.expr, b.expr, c.noValue)
	if !ok {
		return Invariant{}, false
	}
	return newInvariant(a.Name(), e, a.Span(), a.Documentation()), true
}

func unifyNode(a, b expr.Expression, noValue map[*expr.Literal]parse.LiteralKind) (expr.Expression, bool) {
	litA, isLitA := a.(*expr.Literal)
	litB, isLitB := b.(*expr.Literal)
	if isLitA && isLitB {
		kindA, noValueA := noValue[litA]
		kindB, noValueB := noValue[litB]
		switch {
		case noValueA && noValueB:
			return a, kindA == kindB
		case noValueA:
			return b, literalKindOf(litB.Val) == kindA
		case noValueB:
			return a, literalKindOf(litA.Val) == kindB
		}
		if argsA, isList := litA.Val.([]expr.Expression); isList {
			argsB, isList := litB.Val.([]expr.Expression)
			if !isList || len(argsA) != len(argsB) {
				return nil, false
			}
			args := make([]expr.Expression, len(argsA))
			for i := range argsA {
				arg, ok := unifyNode(argsA[i], argsB[i], noValue)
				if !ok {
					return nil, false
				}
				args[i] = arg
			}
			return expr.NewLiteral(args), true
		}
		return a, sameExpression(litA, litB)
	}
	sexprA, isSExprA := a.(expr.SExpr)
	sexprB, isSExprB := b.(expr.SExpr)
	if isSExprA && isSExprB {
		if len(sexprA) != len(sexprB) {
			return nil, false
		}
		out := make(expr.SExpr, len(sexprA))
		for i := range sexprA {
			node, ok := unifyNode(sexprA[i], sexprB[i], noValue)
			if !ok {
				return nil, false
			}
			out[i] = node
		}
		return out, true
	}
	return a, sameExpression(a, b)
}

// literalKindOf is the kind of literal token that writes a value of v's Go
// type, or zero for a value no string, integer, float or regex literal writes.
func literalKindOf(v any) parse.LiteralKind {
	switch v.(type) {
	case string:
		return parse.StringLiteral
	case int64:
		return parse.IntegerLiteral
	case float64:
		return parse.FloatLiteral
	case *regexp.Regexp:
		return parse.RegexLiteral
	}
	return 0
}

// mergeAnnotations merges own type-level annotations with inherited ones.
// Own annotations come first, then inherited (left-to-right supertype order),
// deduplicated keep-first by exact identity: name plus the ordered argument
// texts. Two type-level annotations differing only in argument list (two
// distinct @@index composites) therefore both survive.
//
// It deduplicates inherited-against-existing ONLY: an own-vs-own duplicate is
// deliberately left in place — exactly as mergeInvariants leaves own duplicates
// — so the merged view the generators and the write path read reflects what the
// type body actually declares. validateTypeAnnotations reports the duplicate independently, off the
// raw own slice, so the two never interact.
func (c *completer) mergeAnnotations(t Type, supers []ResolvedTypeRef) []Annotation {
	result := slices.Collect(t.Annotations())
	seen := make(map[string]bool)
	for _, a := range result {
		seen[a.identity()] = true
	}

	for _, superRef := range supers {
		superType := c.resolveTypeID(superRef.ID())

		for a := range superType.AllAnnotations() {
			id := a.identity()
			if seen[id] {
				continue
			}
			seen[id] = true
			result = append(result, a)
		}
	}

	return result
}

// typeUnknown reports whether p's type is not known: the schema parser could
// not build it, or it reads a name that did not resolve to a constraint, itself
// or in a List element. The cause of each is already reported.
func typeUnknown(p Property) bool {
	constraint := p.Constraint()
	return constraint == nil || !constraint.IsResolved()
}

// relationUnknown reports whether r's target did not resolve or r holds an edge
// property of unknown type. The cause of each is already reported.
func relationUnknown(r Relation) bool {
	if r.TargetID().IsZero() {
		return true
	}
	for p := range r.Properties() {
		if typeUnknown(p) {
			return true
		}
	}
	return false
}

// mergeRelations merges own relations with inherited relations.
// Similar to mergeProperties but for relations of a specific kind.
// An inherited relation that conflicts with an existing relation (own or
// from another ancestor) draws E_RELATION_COLLISION, unless a definition of
// the relation is not known ([relationUnknown]).
func (c *completer) mergeRelations(t Type, own []Relation, supers []ResolvedTypeRef, kind RelationKind) []Relation {
	result := own
	seen := make(map[string]Relation)
	for _, r := range result {
		seen[r.FieldName()] = r
	}

	// One collision is one diagnostic per affected type. Ancestors that merely
	// INHERIT a relation carry the same Relation forward, and two ancestors
	// may independently declare rivals that are structurally identical; both
	// are re-detections of one collision, and one edit to the surviving
	// declaration clears them together.
	//
	// Rivals group by [Relation.Equal], a true equivalence: structural
	// field-by-field equality, so it is transitive and testing a group's first
	// member decides the whole group. Relations merge keep-first with no
	// narrowing, so the survivor under a field name never changes mid-walk.
	var collisions []*relationClash

	// unjudged names each relation that reached t with a definition that is not
	// known. No collision is reported for it on t, in whatever order its
	// definitions arrive.
	var unjudged map[string]bool

	for _, superRef := range supers {
		superType := c.resolveTypeID(superRef.ID())

		var inherited []Relation
		if kind == RelationAssociation {
			inherited = slices.Collect(superType.AllAssociations())
		} else {
			inherited = slices.Collect(superType.AllCompositions())
		}

		for _, r := range inherited {
			if existing, ok := seen[r.FieldName()]; ok {
				// The cause of a relation that is not known is already reported,
				// and no definition can be judged against it.
				if relationUnknown(existing) || relationUnknown(r) {
					if unjudged == nil {
						unjudged = make(map[string]bool)
					}
					unjudged[r.FieldName()] = true
					continue
				}
				if !existing.Equal(r) {
					matched := false
					for _, cl := range collisions {
						if cl.matches(r) {
							cl.rivals = appendUniqueRelation(cl.rivals, r)
							matched = true
							break
						}
					}
					if !matched {
						collisions = append(collisions, &relationClash{
							survivor: existing,
							rivals:   []Relation{r},
						})
					}
				}
				continue
			}
			seen[r.FieldName()] = r
			result = append(result, r)
		}
	}

	// Emit once the walk has seen every carrier, so a diagnostic can name every
	// rival declaration — including ones that arrive after the collision is
	// first detected. Collapsing identical rivals into one report is only safe
	// because the report points at all of them; naming just the first would
	// leave the user to fix what they were shown and meet the rest on reload.
	for _, cl := range collisions {
		if unjudged[cl.survivor.FieldName()] {
			continue
		}
		related := make([]location.RelatedInfo, 0, len(cl.rivals)+1)
		related = append(related, location.RelatedInfo{
			Span:    cl.survivor.Span(),
			Message: "surviving definition declared here",
		})
		for _, rival := range cl.rivals {
			related = append(related, location.RelatedInfo{
				Span:    rival.Span(),
				Message: "conflicting definition declared here",
			})
		}
		c.errorfRelated(t.Span(), diag.E_RELATION_COLLISION, related,
			"type %q inherits conflicting definitions of relation %q from %s and %s",
			t.Name(), cl.survivor.Name(), cl.survivor.Owner(), cl.rivals[0].Owner())
	}

	return result
}

// relationClash is one recorded relation collision: the relation that survived
// keep-first under a field name, and the rival declarations that could not
// merge with it. Rivals are structurally identical to each other — one edit to
// the survivor resolves all of them — and unique by declaration, since the
// linearized closure re-presents each rival once per carrying ancestor.
type relationClash struct {
	survivor Relation
	rivals   []Relation
}

// matches reports whether a rival re-detects this collision: same field, and
// structurally equal to the rivals already recorded. Testing only the first
// recorded rival is sound because [Relation.Equal] is a true equivalence
// relation — structural equality, hence transitive — so every recorded rival
// answers identically.
func (cl *relationClash) matches(rival Relation) bool {
	return cl.survivor.FieldName() == rival.FieldName() && cl.rivals[0].Equal(rival)
}

// appendUniqueRelation appends r to rivals unless the same declaration is
// already recorded.
func appendUniqueRelation(rivals []Relation, r Relation) []Relation {
	if slices.Contains(rivals, r) {
		return rivals
	}
	return append(rivals, r)
}

// typeKindName names t's kind as a message writes it.
func typeKindName(t Type) string {
	switch {
	case t.IsAbstract():
		return "abstract type"
	case t.IsPart():
		return "part type"
	default:
		return "concrete type"
	}
}
