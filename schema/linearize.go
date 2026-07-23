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

// mergeProperties merges own properties with inherited properties.
// Own properties come first, then inherited (left-to-right supertype order).
// Identical properties from different ancestors are deduplicated (keep-first).
// When a child re-declares a parent property, constraint narrowing is attempted:
// the child's version is accepted if it narrows the parent's (via CanNarrowFrom).
//
// Whenever two ancestors contribute the same property — equal, or one narrowing
// the other — the merged property carries the union of both ancestors'
// annotations, so a semantic marker (e.g. @writeOnce) is never dropped by
// extends order. Only a child's own re-declaration drops inherited annotations,
// and that draws W_ANNOTATION_SHADOWED.
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
	// linearized ancestor, and an ancestor that merely INHERITS the annotated
	// property yields the same *Property its own ancestor declared, so the same
	// annotation conflict is re-detected once per ancestor in the chain.
	reportedConflicts := make(map[string]bool)

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

			if p.Equal(existing) {
				// An own declaration that re-declares an inherited property
				// identically wins keep-first; warn if it drops the inherited
				// annotations.
				if ownProps[p.Name()] {
					c.warnShadowedAnnotations(existing, p)
					continue
				}
				// Two Equal ancestors: union their annotation sets so a semantic
				// marker (e.g. @writeOnce) is not silently dropped based on which
				// ancestor is linearized first.
				c.foldAnnotationsIntoSurvivor(t, result, seen, reportedConflicts, existing, p)
				continue
			}

			// Check if existing (child's own or earlier ancestor) narrows the inherited
			if existing.CanNarrowFrom(p) {
				if ownProps[p.Name()] {
					// A narrowing own declaration wins; warn if it drops the
					// inherited annotations, same as the identical-re-declaration case.
					c.warnShadowedAnnotations(existing, p)
					continue
				}
				// An earlier ancestor's narrower property survives over a later
				// ancestor's wider copy; union the dropped copy's annotations into
				// it, same as the two-Equal-ancestors case.
				c.foldAnnotationsIntoSurvivor(t, result, seen, reportedConflicts, existing, p)
				continue
			}

			// Check if inherited narrows the existing (from another ancestor).
			// This branch only applies when the existing property was inherited
			// from a different ancestor, NOT when it was declared by the child type
			// itself. A child's explicit declaration that widens must be rejected.
			if !ownProps[p.Name()] && p.CanNarrowFrom(existing) {
				// The later ancestor's narrower property replaces the earlier,
				// wider one; union the earlier one's annotations into the survivor
				// so they are not lost by extends order.
				survivor := p
				if merged, changed := c.unionInheritedAnnotations(t, reportedConflicts, p, existing); changed {
					survivor = p.cloneWithAnnotations(merged)
				}
				replaceMerged(result, seen, survivor)
				continue
			}

			// Incompatible
			c.errorf(t.Span(), diag.E_PROPERTY_CONFLICT,
				"type %q inherits conflicting definitions of property %q from %s and %s",
				t.Name(), p.Name(), existing.DeclaringScope(), p.DeclaringScope())
		}
	}

	return result
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

// foldAnnotationsIntoSurvivor unions dropped's annotations into survivor — the
// property kept in the merged view, already present in result — and replaces it
// with a synthesized copy only when the set grows. Used in the two merge
// outcomes where an inherited property is kept and an equal-or-wider sibling
// from another ancestor is dropped, so the dropped sibling's annotations survive
// regardless of extends order.
func (c *completer) foldAnnotationsIntoSurvivor(t *Type, result []*Property, seen map[string]*Property, reportedConflicts map[string]bool, survivor, dropped *Property) {
	if merged, changed := c.unionInheritedAnnotations(t, reportedConflicts, survivor, dropped); changed {
		replaceMerged(result, seen, survivor.cloneWithAnnotations(merged))
	}
}

// unionInheritedAnnotations merges the annotations of the dropped inherited
// property p into those of the surviving sibling property existing (both
// inherited from different ancestors, whether Equal or one narrowing the other),
// returning the merged set and whether it grew. A same-name annotation with
// identical arguments is idempotent; a same-name annotation with different
// arguments — only @vector similarities differ in practice, and only between
// Equal Vector ancestors, since VectorConstraint.Equal ignores the similarity
// keyword — is an unsatisfiable inheritance conflict, reported as
// E_INVALID_ANNOTATION with existing's value kept. existing.annotations is never
// mutated: it is cloned lazily on first growth, since it is shared with the
// ancestor type.
//
// reportedConflicts holds the (property, annotation) pairs already blamed for
// this type, so one conflict draws one diagnostic no matter how many ancestors
// carry the property forward.
func (c *completer) unionInheritedAnnotations(t *Type, reportedConflicts map[string]bool, existing, p *Property) ([]*Annotation, bool) {
	if len(p.annotations) == 0 {
		return existing.annotations, false
	}
	byName := make(map[string]*Annotation, len(existing.annotations)+len(p.annotations))
	for _, a := range existing.annotations {
		byName[a.name] = a
	}
	merged := existing.annotations
	changed := false
	for _, a := range p.annotations {
		switch prior, ok := byName[a.name]; {
		case !ok:
			if !changed {
				merged = slices.Clone(existing.annotations)
				changed = true
			}
			merged = append(merged, a)
			byName[a.name] = a
		case prior.identity() != a.identity():
			key := p.Name() + "\x00" + a.name
			if reportedConflicts[key] {
				continue
			}
			reportedConflicts[key] = true
			c.errorf(t.Span(), diag.E_INVALID_ANNOTATION,
				"type %q inherits conflicting @%s annotations for property %q from %s and %s",
				t.Name(), a.name, p.Name(), existing.DeclaringScope(), p.DeclaringScope())
		}
	}
	return merged, changed
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
// — so validateAnnotations can detect and report it against the raw own set
// before any adapter reads the merged view. Collapsing own duplicates here
// would make that diagnostic vanish silently.
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

		for _, a := range superType.AllAnnotationsSlice() {
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

// warnShadowedAnnotations emits a Warning when a type's own property
// declaration (surviving) drops annotations that the inherited property
// (shadowed) carried. It is called from the two mergeProperties branches where
// an own declaration wins over an inherited one — identical re-declaration or
// narrowing — so a child shadowing the same property across N annotated
// ancestors draws N warnings (once per shadowing ancestor). The Issue is built
// directly on the collector with Warning severity and a related span at the
// shadowed declaration; completer.errorf cannot serve here because it hardcodes
// Error severity and exposes no related-info hook.
func (c *completer) warnShadowedAnnotations(surviving, shadowed *Property) {
	dropped := droppedAnnotationNames(surviving, shadowed)
	if len(dropped) == 0 {
		return
	}
	display := make([]string, len(dropped))
	for i, name := range dropped {
		display[i] = "@" + name
	}
	msg := fmt.Sprintf(
		"re-declaration of property %q drops inherited annotation(s) %s; re-state them on this declaration to keep them",
		surviving.Name(), strings.Join(display, ", "),
	)
	c.collector.Collect(
		diag.NewIssue(diag.Warning, diag.W_ANNOTATION_SHADOWED, msg).
			WithSpan(surviving.Span()).
			WithRelated(location.RelatedInfo{
				Span:    shadowed.Span(),
				Message: "inherited annotation declared here",
			}).Build(),
	)
}

// droppedAnnotationNames returns the names of annotations on shadowed that are
// absent by name from surviving's annotation set — the annotations a
// re-declaration drops. Comparison is name-level: re-stating an annotation of
// the same name (any args) suppresses the warning for it.
func droppedAnnotationNames(surviving, shadowed *Property) []string {
	if len(shadowed.annotations) == 0 {
		return nil
	}
	have := make(map[string]bool, len(surviving.annotations))
	for _, a := range surviving.annotations {
		have[a.name] = true
	}
	var dropped []string
	for _, a := range shadowed.annotations {
		if !have[a.name] {
			dropped = append(dropped, a.name)
		}
	}
	return dropped
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
				if !existing.Equal(r) {
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
