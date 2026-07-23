package schema

import (
	"fmt"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
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
		if t != nil {
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

	for name := range c.typeIndex {
		if state[name] == unvisited {
			dfs(name)
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
// For qualified refs, looks up the imported schema if registry is available.
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

	// Qualified type - requires registry lookup
	if c.registry == nil {
		return "" // Deferred
	}

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

	var completeType func(t *Type)
	completeType = func(t *Type) {
		if completed[t.Name()] {
			return
		}

		// Mark early to handle re-entry during recursion
		completed[t.Name()] = true

		// Complete supertypes first
		supers := make([]ResolvedTypeRef, 0)
		seenSupers := make(map[TypeID]bool)

		// Initialize with current type to prevent self-inclusion via cross-schema cycles.
		// If A extends B (cross-schema), and B's SuperTypes includes A, the seenSupers
		// check will correctly skip adding A to its own supers.
		// NOTE: This local seenSupers check prevents infinite loops during single-schema
		// completion. Full cross-schema cycle detection (A → B → A spanning schemas)
		// is performed separately via detectCrossSchemaInheritanceCycles after all
		// schemas are loaded.
		seenSupers[t.ID()] = true

		// DFS traversal of inheritance, left-to-right, keep-first
		var linearize func(ref TypeRef) //nolint:staticcheck // recursive closure requires separate declaration
		linearize = func(ref TypeRef) {
			resolved := c.resolveTypeRef(ref)
			if resolved == nil {
				// Defer a qualified ref when no registry is present to resolve it
				// (a schema.Builder schema built without WithRegistry, or a direct
				// completeModel caller), or when its qualifier names an import the
				// loader saw but could not resolve — the import failure already
				// carries the root-cause diagnostic, and blaming every reference
				// through the alias would bury it in noise. With a registry and a
				// resolved import, a qualified supertype that still does not
				// resolve is a genuine error (an undefined alias, or a type absent
				// from the imported schema), mirroring validateRelationTarget;
				// silently dropping it would strip the child of every inherited
				// member with no diagnostic.
				if c.referenceDeferred(ref.Qualifier()) {
					return
				}
				c.errorf(t.Span(), diag.E_UNKNOWN_TYPE,
					"unknown type %q in extends clause of type %q", ref.String(), t.Name())
				return
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

		// Merge properties from ancestors
		allProps := c.mergeProperties(t, supers)
		t.setAllProperties(allProps)

		// Extract primary keys. mergeProperties emits t's own properties first
		// (unmodified) and inherited ones after, so allProps[:ownPropCount] are
		// exactly t's own — an O(1), pointer-exact "declared on t?" test for the
		// rejection branch below, without a clone or a per-type set.
		ownPropCount := len(t.properties)
		pks := make([]*Property, 0)
		for i, p := range allProps {
			if p.IsPrimaryKey() {
				// A primary whose type bottoms out in an already-diagnosed
				// unresolvable alias (deferred import, cyclic local chain)
				// is kept as a key without the type check: the declaration
				// IS a primary key — only its type is unknowable here — and
				// the root cause carries its own diagnostic.
				if c.primaryKeyTypeDeferred(p.Constraint()) {
					pks = append(pks, p)
					continue
				}
				if !isPrimaryKeyAllowed(p.Constraint()) {
					// Report only at the declaring type. An inherited rejected
					// primary was already reported when its ancestor completed
					// (supertypes complete before their subtypes); re-checking the
					// same inherited *Property at every descendant would duplicate
					// the identical diagnostic at the identical span. i < ownPropCount
					// holds exactly for t's own properties (see above).
					if i < ownPropCount {
						// The constraint is nil when parse-error recovery kept a
						// property that never received a type; describe that
						// rather than dereferencing it.
						kind := "missing type"
						if pc := p.Constraint(); pc != nil {
							kind = pc.Kind().String()
						}
						c.errorf(p.Span(), diag.E_INVALID_PRIMARY_KEY_TYPE,
							"property %q: %s cannot be used as a primary key (allowed: String, UUID, Date, Timestamp)",
							p.Name(), kind)
					}
					continue
				}
				pks = append(pks, p)
			}
		}
		t.setPrimaryKeys(pks)

		// Merge associations from ancestors
		allAssocs := c.mergeRelations(t, t.AssociationsSlice(), supers, RelationAssociation)
		t.setAllAssociations(allAssocs)

		// Merge compositions from ancestors
		allComps := c.mergeRelations(t, t.CompositionsSlice(), supers, RelationComposition)
		t.setAllCompositions(allComps)

		// Merge invariants from ancestors
		allInvs := c.mergeInvariants(t, supers)
		t.setAllInvariants(allInvs)

		// Merge type-level annotations from ancestors
		allAnns := c.mergeAnnotations(t, supers)
		t.setAllAnnotations(allAnns)
	}

	for _, t := range c.schema.types {
		completeType(t)
	}

	// Set subtypes after all types are completed.
	// Only update subtypes for types in the current schema; cross-schema
	// types are already sealed and their subtypes are not mutable here.
	for _, t := range c.schema.types {
		for super := range t.SuperTypes() {
			superID := super.ID()
			// Only set subtypes on local types (same schema)
			if superID.SchemaPath() != c.sourceID {
				continue
			}
			if superType := c.resolveTypeID(superID); superType != nil {
				subs := superType.SubTypesSlice()
				subs = append(subs, ResolvedTypeRefFromType(t, superID.SchemaPath().String()))
				superType.setSubTypes(subs)
			}
		}
	}
}

// resolveTypeRef resolves a TypeRef to a Type.
func (c *completer) resolveTypeRef(ref TypeRef) *Type {
	if ref.Name() == "" {
		return nil
	}

	if ref.Qualifier() == "" {
		// Local type
		return c.typeIndex[ref.Name()]
	}

	// Qualified type - requires registry lookup
	if c.registry == nil {
		return nil
	}

	importedSchema, ok := c.resolveImportedSchema(ref.Qualifier())
	if !ok {
		return nil
	}

	t, _ := importedSchema.Type(ref.Name())
	return t
}

// resolveImportedSchema resolves a non-empty import qualifier to the imported
// schema through the resolution map — [completer.classifyQualifier], the single
// interpreter of that map — returning false when the qualifier is absent or
// deferred, or when its resolved schema is not registered. It is the
// completion-time counterpart to the runtime [Schema.ResolveType] /
// [Schema.ResolveDataType], which read the wired Import objects instead; routing
// completion through classifyQualifier keeps a qualifier's deferred/resolved
// state read in one place during a load, not two. The caller must have already
// handled the registry-less case (no registry means the reference defers).
func (c *completer) resolveImportedSchema(qualifier string) (*Schema, bool) {
	state, sourceID := c.classifyQualifier(qualifier)
	if state != aliasResolved {
		return nil, false
	}
	return c.registry.LookupBySourceID(sourceID)
}

// resolveTypeID resolves a TypeID to a Type.
func (c *completer) resolveTypeID(id TypeID) *Type {
	if id.SchemaPath() == c.sourceID {
		return c.typeIndex[id.Name()]
	}

	if c.registry == nil {
		return nil
	}

	importedSchema, ok := c.registry.LookupBySourceID(id.SchemaPath())
	if !ok {
		return nil
	}

	t, _ := importedSchema.Type(id.Name())
	return t
}

// annotationSource pairs an inherited annotation with the direct supertype whose
// merged view supplied it, so a conflict diagnostic can name where each side
// reached the type from rather than reading a declaring scope off a merged
// clone, which can name the same ancestor twice.
type annotationSource struct {
	from *Type
	ann  *Annotation
}

// mergeProperties merges own properties with inherited properties.
// Own properties come first, then inherited (left-to-right supertype order).
// Identical properties from different ancestors are deduplicated (keep-first).
// When a child re-declares a parent property, constraint narrowing is attempted:
// the child's version is accepted if it narrows the parent's (via CanNarrowFrom).
//
// The walk decides STRUCTURE only — which *Property survives under each name.
// Annotations are settled afterwards by [completer.resolveInheritedAnnotations],
// which reads the DIRECT supertypes' already-merged views instead of re-deriving
// anything from this transitively-closed list.
func (c *completer) mergeProperties(t *Type, supers []ResolvedTypeRef) []*Property {
	// Start with own properties
	result := t.PropertiesSlice()
	seen := make(map[string]*Property)
	ownProps := make(map[string]bool)
	for _, p := range result {
		seen[p.Name()] = p
		ownProps[p.Name()] = true
	}

	// One inheritance conflict is one diagnostic. The loop below visits every
	// linearized ancestor, and an ancestor that merely INHERITS the conflicting
	// property carries the same declared *Property forward, so the same clash is
	// re-detected once per ancestor in the chain.
	//
	// A conflict is between two narrowing LINEAGES of a property — sets of
	// declarations that are mutually compatible (equal, or one narrowing another)
	// within each set and incompatible across. EITHER side may have several
	// members, so no single *Property, its Origin, or its kind identifies the
	// conflict: a scalar key double-reports when the many-member side varies
	// (survivor swap, or a narrowing chain on the conflicting side) or collapses
	// two independent clashes that happen to share it. This is why keying by name,
	// by the *Property pair, by the incomer Origin, and by the kind pair each
	// failed in turn.
	//
	// The lineage-level identity is exact: two conflicts are the same iff their
	// surviving sides are compatible AND their conflicting sides are compatible.
	// reportedConflicts holds one (survivor, incomer) representative per distinct
	// clash; a new clash is deduped against it by that pairwise compatibility. The
	// list is per property name and holds at most one entry per genuinely
	// independent clash, so the scan is negligible.
	var reportedConflicts []conflictPair

	// Add inherited properties in linearized order
	for _, superRef := range supers {
		superType := c.resolveTypeID(superRef.ID())
		if superType == nil {
			continue
		}

		for p := range superType.AllProperties() {
			existing, ok := seen[p.Name()]
			if !ok {
				seen[p.Name()] = p
				result = append(result, p)
				continue
			}

			switch {
			case p.Equal(existing), existing.CanNarrowFrom(p):
				// The surviving copy wins keep-first: the child's own declaration,
				// or an earlier ancestor's equal-or-narrower one.

			case !ownProps[p.Name()] && p.CanNarrowFrom(existing):
				// A later ancestor's narrower copy replaces the earlier, wider one.
				// Restricted to inherited survivors: a child's own declaration that
				// widens what it inherits must be rejected, not silently narrowed
				// back by the parent.
				replaceMerged(result, seen, p)

			default:
				if conflictAlreadyReported(reportedConflicts, existing, p) {
					continue
				}
				reportedConflicts = append(reportedConflicts, conflictPair{survivor: existing, incomer: p})
				if ownProps[p.Name()] {
					c.conflictedProperties[existing] = true
				}
				c.errorf(t.Span(), diag.E_PROPERTY_CONFLICT,
					"type %q inherits conflicting definitions of property %q from %s and %s",
					t.Name(), p.Name(), existing.DeclaringScope(), p.DeclaringScope())
			}
		}
	}

	c.resolveInheritedAnnotations(t, result, ownProps)

	return result
}

// resolveInheritedAnnotations settles the annotation set of every merged
// property, once mergeProperties has decided which *Property survives under each
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
func (c *completer) resolveInheritedAnnotations(t *Type, result []*Property, ownProps map[string]bool) {
	supers := c.directSuperTypes(t)
	if len(supers) == 0 {
		return // Nothing to inherit; own annotations stand as declared.
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
// the declared supertypes only (a handful), via [Type.IsSubTypeOf] against the
// transitively-closed [Type.SuperTypes], so it is cheap; it is NOT the
// closure-wide maximal scan that made an earlier design superlinear.
func (c *completer) directSuperTypes(t *Type) []*Type {
	var resolved []*Type
	var seen map[TypeID]bool
	for ref := range t.Inherits() {
		s := c.resolveTypeRef(ref)
		if s == nil {
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

	var out []*Type
	for _, cand := range resolved {
		subsumed := false
		for _, other := range resolved {
			if other != cand && other.IsSubTypeOf(cand.ID()) {
				subsumed = true
				break
			}
		}
		if !subsumed {
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
// suppress for its own subtypes — every arm's suppression that no arm carries.
func (c *completer) inheritedAnnotations(t *Type, supers []*Type, name string, reported map[string]bool) ([]*Annotation, map[string]bool) {
	var merged []*Annotation
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

			if dropper := suppressingSuper(supers, s, name, a.name); dropper != nil {
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

// suppressingSuper returns the direct supertype, other than carrier, that
// suppressed annotation ann on property name — the arm whose decision to drop it
// contradicts carrier's decision to keep it.
func suppressingSuper(supers []*Type, carrier *Type, name, ann string) *Type {
	for _, s := range supers {
		if s != carrier && s.suppressedAnnotations(name)[ann] {
			return s
		}
	}
	return nil
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
// for the same *Annotation at an unrelated type. The disagreeing type re-declares
// nothing (a re-declaration would resolve the disagreement), so it queues no
// shadow warning of its own for the mark to suppress.
func (c *completer) reportAnnotationDisagreement(
	t *Type, prop string, a *Annotation, reported map[string]bool,
	msg string, otherSpan location.Span, otherLabel string,
) {
	key := prop + "\x00" + a.name
	if reported[key] {
		return
	}
	reported[key] = true
	c.collector.Collect(
		diag.NewIssue(diag.Error, diag.E_INVALID_ANNOTATION, msg).
			WithSpan(t.Span()).
			WithRelated(
				location.RelatedInfo{Span: a.Span(), Message: "annotation declared here"},
				location.RelatedInfo{Span: otherSpan, Message: otherLabel},
			).Build(),
	)
}

// conflictPair is one reported property-inheritance conflict, kept as a
// (survivor, incomer) representative so a later clash can be deduped against it
// by lineage compatibility. See [conflictAlreadyReported].
type conflictPair struct {
	survivor *Property
	incomer  *Property
}

// conflictAlreadyReported reports whether the clash between survivor and incomer
// is the same one as an already-reported pair: same iff both sides are pairwise
// compatible with that pair's corresponding sides, in either role assignment.
// Compatibility (equal, or one narrowing the other) means "same narrowing
// lineage", so this collapses a clash re-detected because a narrowing chain
// (on the surviving OR conflicting side) contributed several members, while two
// genuinely independent clashes — whose members narrow neither each other — stay
// distinct.
func conflictAlreadyReported(reported []conflictPair, survivor, incomer *Property) bool {
	for _, r := range reported {
		if propertiesCompatible(r.survivor, survivor) && propertiesCompatible(r.incomer, incomer) {
			return true
		}
		if propertiesCompatible(r.survivor, incomer) && propertiesCompatible(r.incomer, survivor) {
			return true
		}
	}
	return false
}

// propertiesCompatible reports whether two same-named properties belong to one
// narrowing lineage: equal, or one a valid narrowing of the other. Members of a
// lineage do not conflict with each other; members of different lineages do.
func propertiesCompatible(a, b *Property) bool {
	return a.Equal(b) || a.CanNarrowFrom(b) || b.CanNarrowFrom(a)
}

// replaceMerged swaps the property named replacement.Name() in result and seen
// for replacement. Both hold exactly one property of that name by construction,
// so it updates the slice element in place (no append) and the index entry.
func replaceMerged(result []*Property, seen map[string]*Property, replacement *Property) {
	seen[replacement.Name()] = replacement
	for i, r := range result {
		if r.Name() == replacement.Name() {
			result[i] = replacement
			break
		}
	}
}

// shadowedAnnotation records one annotation that a type's own property
// declaration dropped, together with the direct supertype it came through.
// Queued during linearization and emitted after annotation validation — see
// [completer.flushShadowedAnnotations].
type shadowedAnnotation struct {
	own  *Property
	from *Type
	ann  *Annotation
}

// shadowGroupKey groups queued shadow entries by (own declaration, contributing
// supertype) for [completer.flushShadowedAnnotations].
type shadowGroupKey struct {
	own  *Property
	from *Type
}

// recordShadowedAnnotations queues a W_ANNOTATION_SHADOWED for every annotation
// an own declaration drops, one entry per (direct supertype, annotation).
//
// Per supertype, not per annotation name: two incomparable mixins that both
// annotate the property are two independent declarations the re-declaration
// dropped, and the user needs to be pointed at both — even when they happen to
// use the SAME annotation name. Deduplication is by declared *Annotation
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
// uniformly-inherited annotation, so a contested one draws neither the warning
// nor a suppression entry (the child's decision is now authoritative and
// settled).
//
// It returns the annotation names this declaration suppresses for its own
// subtypes: the uncontested annotations it dropped, plus its supertypes' still-
// live suppressions, minus anything it re-states. A re-declaration that re-states
// an annotation revives it for the whole subtree.
func (c *completer) recordShadowedAnnotations(own *Property, supers []*Type) map[string]bool {
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

	var seen map[*Annotation]bool
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
				seen = make(map[*Annotation]bool)
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
// a second copy of the same error. Linearization cannot know that; only phase 7c
// establishes which annotations are well-formed, known, correctly placed, and
// eligible for their target. Any annotation that drew a diagnostic of its own,
// or whose argument list failed to parse, is therefore skipped here — as is any
// re-declaration that itself drew E_PROPERTY_CONFLICT, since a rejected
// declaration cannot usefully be told to re-state anything.
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
		own  *Property
		from *Type
		anns []*Annotation
	}
	var groups []*group
	// A typed key rather than [2]any: the pair is (own property, contributing
	// supertype), both comparable pointers, so boxing them into interfaces buys
	// nothing and only risks an accidental type-mismatched lookup.
	index := make(map[shadowGroupKey]*group)

	for _, s := range c.pendingShadowed {
		if s.ann.argsMalformed || c.diagnosedAnnotations[s.ann] {
			continue // The annotation is not usable; advising its re-statement would mislead.
		}
		if c.conflictedProperties[s.own] {
			continue // The re-declaration itself drew E_PROPERTY_CONFLICT; nothing to advise.
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
		c.collector.Collect(
			diag.NewIssue(diag.Warning, diag.W_ANNOTATION_SHADOWED, msg).
				WithSpan(g.own.Span()).
				WithRelated(related...).Build(),
		)
	}
}

// mergeInvariants merges own invariants with inherited invariants.
// Own invariants come first, then inherited (left-to-right supertype order).
// Deduplication by name: keep-first (child can override parent's invariant by name).
func (c *completer) mergeInvariants(t *Type, supers []ResolvedTypeRef) []*Invariant {
	result := t.InvariantsSlice()
	seen := make(map[string]bool)
	for _, inv := range result {
		seen[inv.Name()] = true
	}

	for _, superRef := range supers {
		superType := c.resolveTypeID(superRef.ID())
		if superType == nil {
			continue
		}

		for inv := range superType.AllInvariants() {
			if seen[inv.Name()] {
				continue
			}
			seen[inv.Name()] = true
			result = append(result, inv)
		}
	}

	return result
}

// mergeAnnotations merges own type-level annotations with inherited ones.
// Own annotations come first, then inherited (left-to-right supertype order),
// deduplicated keep-first by exact identity: name plus the ordered argument
// texts. Two type-level annotations differing only in argument list (two
// distinct @@index composites) therefore both survive.
//
// It deduplicates inherited-against-existing ONLY: an own-vs-own duplicate is
// deliberately left in place — exactly as mergeInvariants leaves own duplicates
// — so the merged view an adapter reads reflects what the type body actually
// declares. validateTypeAnnotations reports the duplicate independently, off the
// raw own slice, so the two never interact.
func (c *completer) mergeAnnotations(t *Type, supers []ResolvedTypeRef) []*Annotation {
	result := t.AnnotationsSlice()
	seen := make(map[string]bool)
	for _, a := range result {
		seen[a.identity()] = true
	}

	for _, superRef := range supers {
		superType := c.resolveTypeID(superRef.ID())
		if superType == nil {
			continue
		}

		// AllAnnotations, not AllAnnotationsSlice: the sibling merge helpers
		// iterate their ancestors' members rather than cloning a slice per
		// ancestor only to discard it after the range.
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

// mergeRelations merges own relations with inherited relations.
// Similar to mergeProperties but for relations of a specific kind.
// Reports E_RELATION_COLLISION when an inherited relation conflicts
// with an existing relation (own or from another ancestor).
func (c *completer) mergeRelations(t *Type, own []*Relation, supers []ResolvedTypeRef, kind RelationKind) []*Relation {
	result := own
	seen := make(map[string]*Relation)
	for _, r := range result {
		seen[r.FieldName()] = r
	}

	// One collision is one diagnostic, keyed by the PAIR of clashing relations —
	// the same rule mergeProperties applies to E_PROPERTY_CONFLICT, for the same
	// reason: an ancestor that merely INHERITS the relation carries the same
	// *Relation forward, so the identical clash is re-detected once per ancestor
	// in the chain, while two genuinely different rivals stay two diagnostics.
	reportedCollisions := make(map[[2]*Relation]bool)

	for _, superRef := range supers {
		superType := c.resolveTypeID(superRef.ID())
		if superType == nil {
			continue
		}

		var inherited []*Relation
		if kind == RelationAssociation {
			inherited = superType.AllAssociationsSlice()
		} else {
			inherited = superType.AllCompositionsSlice()
		}

		for _, r := range inherited {
			if existing, ok := seen[r.FieldName()]; ok {
				// Check if they're compatible (same relation)
				if key := [2]*Relation{existing, r}; !existing.Equal(r) && !reportedCollisions[key] {
					reportedCollisions[key] = true
					c.errorf(t.Span(), diag.E_RELATION_COLLISION,
						"type %q inherits conflicting definitions of relation %q from %s and %s",
						t.Name(), r.FieldName(), existing.Owner(), r.Owner())
				}
				continue
			}
			seen[r.FieldName()] = r
			result = append(result, r)
		}
	}

	return result
}
