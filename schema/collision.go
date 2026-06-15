package schema

import (
	"strings"

	"github.com/simon-lentz/yammm/diag"
)

// reservedPrefix is rejected for property and relation names.
const reservedPrefix = "_target_"

// detectCollisions checks for naming collisions in all types.
func (c *completer) detectCollisions() {
	for _, t := range c.schema.TypesSlice() {
		c.detectTypeCollisions(t)
	}
}

// detectTypeCollisions checks for collisions within a single type. Findings
// are collected; there is no failure signal — the completer's final error
// gate decides whether a schema is produced.
func (c *completer) detectTypeCollisions(t *Type) {
	// Check reserved prefixes on own properties.
	// We intentionally check PropertiesSlice() (own) not AllPropertiesSlice() (all)
	// because inherited properties are validated when their declaring type is checked,
	// avoiding duplicate errors when parent and child are in the same schema.
	for _, p := range t.PropertiesSlice() {
		if strings.HasPrefix(strings.ToLower(p.Name()), reservedPrefix) {
			c.errorf(p.Span(), diag.E_RESERVED_PREFIX,
				"property %q uses reserved prefix %q", p.Name(), reservedPrefix)
		}
	}

	// Check reserved prefixes on own relations
	for _, r := range t.AssociationsSlice() {
		if strings.HasPrefix(strings.ToLower(r.FieldName()), reservedPrefix) {
			c.errorf(r.Span(), diag.E_RESERVED_PREFIX,
				"relation %q uses reserved prefix %q", r.Name(), reservedPrefix)
		}
	}
	for _, r := range t.CompositionsSlice() {
		if strings.HasPrefix(strings.ToLower(r.FieldName()), reservedPrefix) {
			c.errorf(r.Span(), diag.E_RESERVED_PREFIX,
				"relation %q uses reserved prefix %q", r.Name(), reservedPrefix)
		}
	}

	c.checkPropertyCaseCollisions(t)
	c.checkPropertyRelationCollisions(t)
	c.checkRelationCollisions(t)
}

// checkPropertyCaseCollisions detects case-insensitive property name collisions.
func (c *completer) checkPropertyCaseCollisions(t *Type) {
	seen := make(map[string]*Property) // lowercase -> first property

	for _, p := range t.AllPropertiesSlice() {
		lower := strings.ToLower(p.Name())
		if existing, found := seen[lower]; found {
			if existing.Name() != p.Name() {
				// Case collision - same lowercase but different actual names
				c.errorf(p.Span(), diag.E_CASE_COLLISION,
					"property %q in type %q collides with %q (case-insensitive)",
					p.Name(), t.Name(), existing.Name())
			}
			// Same exact name is OK (might be inherited from same ancestor)
		} else {
			seen[lower] = p
		}
	}
}

// checkPropertyRelationCollisions detects collisions between property names and relation field names.
func (c *completer) checkPropertyRelationCollisions(t *Type) {
	// Build property name set (lowercase for case-insensitive check)
	propNames := make(map[string]*Property)
	for _, p := range t.AllPropertiesSlice() {
		propNames[strings.ToLower(p.Name())] = p
	}

	// Check associations
	for _, r := range t.AllAssociationsSlice() {
		lower := strings.ToLower(r.FieldName())
		if prop, found := propNames[lower]; found {
			c.errorf(r.Span(), diag.E_PROPERTY_RELATION_COLLISION,
				"relation %q (field: %q) in type %q collides with property %q",
				r.Name(), r.FieldName(), t.Name(), prop.Name())
		}
	}

	// Check compositions
	for _, r := range t.AllCompositionsSlice() {
		lower := strings.ToLower(r.FieldName())
		if prop, found := propNames[lower]; found {
			c.errorf(r.Span(), diag.E_PROPERTY_RELATION_COLLISION,
				"relation %q (field: %q) in type %q collides with property %q",
				r.Name(), r.FieldName(), t.Name(), prop.Name())
		}
	}
}

// checkRelationCollisions detects relation name collisions after normalization.
func (c *completer) checkRelationCollisions(t *Type) {
	seen := make(map[string]*Relation) // fieldName -> first relation

	// Check associations
	for _, r := range t.AllAssociationsSlice() {
		if existing, found := seen[r.FieldName()]; found {
			// Collision - check if they're from the same declaration
			if !r.Equal(existing) {
				// E_RELATION_NORMALIZATION_COLLISION: different raw names normalize to same field name
				c.errorf(r.Span(), diag.E_RELATION_NORMALIZATION_COLLISION,
					"association %q in type %q collides with existing relation %q (both normalize to %q)",
					r.Name(), t.Name(), existing.Name(), r.FieldName())
			}
		} else {
			seen[r.FieldName()] = r
		}
	}

	// Check compositions
	for _, r := range t.AllCompositionsSlice() {
		if existing, found := seen[r.FieldName()]; found {
			// Collision between composition and association, or composition and composition
			if !r.Equal(existing) {
				// E_RELATION_NORMALIZATION_COLLISION: different raw names normalize to same field name
				c.errorf(r.Span(), diag.E_RELATION_NORMALIZATION_COLLISION,
					"composition %q in type %q collides with existing relation %q (both normalize to %q)",
					r.Name(), t.Name(), existing.Name(), r.FieldName())
			}
		} else {
			seen[r.FieldName()] = r
		}
	}
}

// validateRelationTargets checks that all relation targets exist.
func (c *completer) validateRelationTargets() {
	for _, t := range c.schema.TypesSlice() {
		// Check associations
		for _, r := range t.AssociationsSlice() {
			c.validateRelationTarget(t, r, "association")
		}

		// Check compositions - must target a concrete part type
		for _, r := range t.CompositionsSlice() {
			c.validateCompositionTarget(t, r)
		}
	}

	// Validate association constraints on part types
	c.validateAssociationTargets()
}

// validateAssociationTargets checks that part types don't declare associations
// and that associations target a concrete type (not a part or abstract type).
func (c *completer) validateAssociationTargets() {
	for _, t := range c.schema.TypesSlice() {
		// Part types cannot declare associations
		if t.IsPart() {
			for _, r := range t.AssociationsSlice() {
				c.errorf(r.Span(), diag.E_INVALID_ASSOCIATION_TARGET,
					"part type %q cannot declare association %q", t.Name(), r.Name())
			}
		}

		// Associations must target a concrete type. An edge resolves against the
		// declared target's exact TypeID, and neither a part (composition-only) nor
		// an abstract (non-instantiable) type ever has an instance under that
		// TypeID, so such an edge could never resolve in a graph.
		for _, r := range t.AssociationsSlice() {
			target := c.resolveTypeRef(r.Target())
			if target == nil {
				// Deferred cross-schema ref (registry absent); re-validated on a
				// full registry-backed load, as in validateCompositionTarget.
				continue
			}
			switch {
			case target.IsPart():
				c.errorf(r.Span(), diag.E_INVALID_ASSOCIATION_TARGET,
					"association %q in type %q cannot target part type %q",
					r.Name(), t.Name(), target.Name())
			case target.IsAbstract():
				c.errorf(r.Span(), diag.E_INVALID_ASSOCIATION_TARGET,
					"association %q in type %q cannot target abstract type %q (associations must reference a concrete type)",
					r.Name(), t.Name(), target.Name())
			}
		}
	}
}

// validateRelationTarget checks that a relation target exists and, when it
// resolves, records the target's semantic identity on the relation.
func (c *completer) validateRelationTarget(_ *Type, r *Relation, kind string) {
	target := c.resolveTypeRef(r.Target())
	if target == nil {
		// Deferred: either no registry is available to resolve qualified
		// refs (checked when one is), or the qualifier names an import the
		// loader saw but could not resolve — the import failure already
		// carries the root-cause diagnostic, so the reference is skipped
		// rather than re-blamed.
		if r.Target().Qualifier() != "" && (c.registry == nil || c.qualifierDeferred(r.Target().Qualifier())) {
			return
		}

		// Target not found
		c.errorf(r.Span(), diag.E_UNKNOWN_TYPE,
			"type %q referenced in %s %q does not exist",
			r.Target().String(), kind, r.Name())
		return
	}

	// Resolve the semantic identity
	r.setTargetID(target.ID())
}

// validateCompositionTarget checks that a composition target is a concrete part type.
// NOTE: When the target is a cross-schema ref and registry is nil, the IsPart and
// IsAbstract checks are deferred. These constraints should be re-validated when
// the schema is linked with a registry that can resolve cross-schema references.
func (c *completer) validateCompositionTarget(t *Type, r *Relation) {
	target := c.resolveTypeRef(r.Target())
	if target == nil {
		// Deferred: no registry to resolve cross-schema refs (validated at
		// linking), or the qualifier names an import the loader saw but
		// could not resolve — the import failure is the root-cause
		// diagnostic, so the reference is skipped rather than re-blamed.
		if r.Target().Qualifier() != "" && (c.registry == nil || c.qualifierDeferred(r.Target().Qualifier())) {
			return
		}

		c.errorf(r.Span(), diag.E_UNKNOWN_TYPE,
			"type %q referenced in composition %q does not exist",
			r.Target().String(), r.Name())
		return
	}

	// Composition targets must be part types
	if !target.IsPart() {
		c.errorf(r.Span(), diag.E_INVALID_COMPOSITION_TARGET,
			"composition %q in type %q must reference a part type, but %q is not a part",
			r.Name(), t.Name(), target.Name())
		return
	}

	// Composition targets cannot be abstract
	if target.IsAbstract() {
		c.errorf(r.Span(), diag.E_INVALID_COMPOSITION_TARGET,
			"composition %q in type %q must reference a concrete type, but %q is abstract",
			r.Name(), t.Name(), target.Name())
		return
	}

	// Resolve the semantic identity
	r.setTargetID(target.ID())
}
