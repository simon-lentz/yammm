package complete

import (
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
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
func (c *completer) resolveTypeRefName(ref schema.TypeRef) string {
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

	imp, ok := c.schema.ImportByAlias(ref.Qualifier())
	if !ok {
		return ""
	}

	importedSchema, ok := c.registry.LookupBySourceID(imp.ResolvedSourceID())
	if !ok {
		return ""
	}

	if _, ok := importedSchema.Type(ref.Name()); ok {
		return ref.String() // Return qualified name for cross-schema types
	}

	return ""
}

// completeTypes linearizes inheritance and merges members for each type.
func (c *completer) completeTypes() bool {
	ok := true
	completed := make(map[string]bool)

	// Set schema name on each type for cross-schema display.
	schemaName := c.schema.Name()
	for _, t := range c.schema.TypesSlice() {
		t.SetSchemaName(schemaName)
	}

	var completeType func(t *schema.Type) bool
	completeType = func(t *schema.Type) bool {
		if completed[t.Name()] {
			return true
		}

		// Mark early to handle re-entry during recursion
		completed[t.Name()] = true

		// Complete supertypes first
		supers := make([]schema.ResolvedTypeRef, 0)
		seenSupers := make(map[schema.TypeID]bool)

		// Initialize with current type to prevent self-inclusion via cross-schema cycles.
		// If A extends B (cross-schema), and B's SuperTypes includes A, the seenSupers
		// check will correctly skip adding A to its own supers.
		// NOTE: This local seenSupers check prevents infinite loops during single-schema
		// completion. Full cross-schema cycle detection (A → B → A spanning schemas)
		// is performed separately via DetectCrossSchemaInheritanceCycles after all
		// schemas are loaded.
		seenSupers[t.ID()] = true

		// DFS traversal of inheritance, left-to-right, keep-first
		var linearize func(ref schema.TypeRef) //nolint:staticcheck // recursive closure requires separate declaration
		linearize = func(ref schema.TypeRef) {
			resolved := c.resolveTypeRef(ref)
			if resolved == nil {
				// Only emit error for local refs; cross-schema refs are deferred
				// when registry is nil (they will be validated when registry is available).
				if ref.Qualifier() == "" {
					c.errorf(t.Span(), diag.E_UNKNOWN_TYPE,
						"unknown type %q in extends clause of type %q", ref.Name(), t.Name())
					ok = false
				}
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
					if !completeType(st) {
						ok = false
					}
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

			supers = append(supers, schema.NewResolvedTypeRef(ref, id))
		}

		// Process declared inherits
		for ref := range t.Inherits() {
			linearize(ref)
		}

		t.SetSuperTypes(supers)

		// Merge properties from ancestors
		allProps := c.mergeProperties(t, supers)
		t.SetAllProperties(allProps)

		// Extract primary keys
		pks := make([]*schema.Property, 0)
		for _, p := range allProps {
			if p.IsPrimaryKey() {
				if !isPrimaryKeyAllowed(p.Constraint()) {
					c.errorf(p.Span(), diag.E_INVALID_PRIMARY_KEY_TYPE,
						"property %q: %s cannot be used as a primary key (allowed: String, UUID, Date, Timestamp)",
						p.Name(), p.Constraint().Kind())
					ok = false
					continue
				}
				pks = append(pks, p)
			}
		}
		t.SetPrimaryKeys(pks)

		// Merge associations from ancestors
		allAssocs := c.mergeRelations(t, t.AssociationsSlice(), supers, schema.RelationAssociation)
		t.SetAllAssociations(allAssocs)

		// Merge compositions from ancestors
		allComps := c.mergeRelations(t, t.CompositionsSlice(), supers, schema.RelationComposition)
		t.SetAllCompositions(allComps)

		// Merge invariants from ancestors
		allInvs := c.mergeInvariants(t, supers)
		t.SetAllInvariants(allInvs)

		return ok
	}

	for _, t := range c.schema.TypesSlice() {
		if !completeType(t) {
			ok = false
		}
	}

	// Set subtypes after all types are completed.
	// Only update subtypes for types in the current schema; cross-schema
	// types are already sealed and their subtypes are not mutable here.
	for _, t := range c.schema.TypesSlice() {
		for super := range t.SuperTypes() {
			superID := super.ID()
			// Only set subtypes on local types (same schema)
			if superID.SchemaPath() != c.sourceID {
				continue
			}
			if superType := c.resolveTypeID(superID); superType != nil {
				subs := superType.SubTypesSlice()
				subs = append(subs, schema.ResolvedTypeRefFromType(t, superID.SchemaPath().String()))
				superType.SetSubTypes(subs)
			}
		}
	}

	return ok
}

// resolveTypeRef resolves a TypeRef to a Type.
func (c *completer) resolveTypeRef(ref schema.TypeRef) *schema.Type {
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

	imp, ok := c.schema.ImportByAlias(ref.Qualifier())
	if !ok {
		return nil
	}

	importedSchema, ok := c.registry.LookupBySourceID(imp.ResolvedSourceID())
	if !ok {
		return nil
	}

	t, _ := importedSchema.Type(ref.Name())
	return t
}

// resolveTypeID resolves a TypeID to a Type.
func (c *completer) resolveTypeID(id schema.TypeID) *schema.Type {
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
func (c *completer) mergeProperties(t *schema.Type, supers []schema.ResolvedTypeRef) []*schema.Property {
	// Start with own properties
	result := t.PropertiesSlice()
	seen := make(map[string]*schema.Property)
	ownProps := make(map[string]bool)
	for _, p := range result {
		seen[p.Name()] = p
		ownProps[p.Name()] = true
	}

	// Add inherited properties in linearized order
	for _, superRef := range supers {
		superType := c.resolveTypeID(superRef.ID())
		if superType == nil {
			continue
		}

		for _, p := range superType.AllPropertiesSlice() {
			existing, ok := seen[p.Name()]
			if !ok {
				seen[p.Name()] = p
				result = append(result, p)
				continue
			}

			if p.Equal(existing) {
				continue
			}

			// Check if existing (child's own or earlier ancestor) narrows the inherited
			if existing.CanNarrowFrom(p) {
				continue // Existing narrower version is already in result
			}

			// Check if inherited narrows the existing (from another ancestor).
			// This branch only applies when the existing property was inherited
			// from a different ancestor, NOT when it was declared by the child type
			// itself. A child's explicit declaration that widens must be rejected.
			if !ownProps[p.Name()] && p.CanNarrowFrom(existing) {
				seen[p.Name()] = p
				for i, r := range result {
					if r.Name() == p.Name() {
						result[i] = p
						break
					}
				}
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

// mergeInvariants merges own invariants with inherited invariants.
// Own invariants come first, then inherited (left-to-right supertype order).
// Deduplication by name: keep-first (child can override parent's invariant by name).
func (c *completer) mergeInvariants(t *schema.Type, supers []schema.ResolvedTypeRef) []*schema.Invariant {
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

		for _, inv := range superType.AllInvariantsSlice() {
			if seen[inv.Name()] {
				continue
			}
			seen[inv.Name()] = true
			result = append(result, inv)
		}
	}

	return result
}

// mergeRelations merges own relations with inherited relations.
// Similar to mergeProperties but for relations of a specific kind.
// Reports E_RELATION_COLLISION when an inherited relation conflicts
// with an existing relation (own or from another ancestor).
func (c *completer) mergeRelations(t *schema.Type, own []*schema.Relation, supers []schema.ResolvedTypeRef, kind schema.RelationKind) []*schema.Relation {
	result := own
	seen := make(map[string]*schema.Relation)
	for _, r := range result {
		seen[r.FieldName()] = r
	}

	for _, superRef := range supers {
		superType := c.resolveTypeID(superRef.ID())
		if superType == nil {
			continue
		}

		var inherited []*schema.Relation
		if kind == schema.RelationAssociation {
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
