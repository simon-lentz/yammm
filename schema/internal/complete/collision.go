package complete

import (
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// reservedPrefix is rejected for property and relation names.
const reservedPrefix = "_target_"

// detectCollisions checks for naming collisions in all types.
func (c *completer) detectCollisions() bool {
	ok := true

	for _, t := range c.schema.TypesSlice() {
		if !c.detectTypeCollisions(t) {
			ok = false
		}
	}

	return ok
}

// detectTypeCollisions checks for collisions within a single type.
func (c *completer) detectTypeCollisions(t *schema.Type) bool {
	ok := true

	// Check reserved prefixes on own properties.
	// We intentionally check PropertiesSlice() (own) not AllPropertiesSlice() (all)
	// because inherited properties are validated when their declaring type is checked,
	// avoiding duplicate errors when parent and child are in the same schema.
	for _, p := range t.PropertiesSlice() {
		if strings.HasPrefix(strings.ToLower(p.Name()), reservedPrefix) {
			c.errorf(p.Span(), diag.E_RESERVED_PREFIX,
				"property %q uses reserved prefix %q", p.Name(), reservedPrefix)
			ok = false
		}
	}

	// Check reserved prefixes on own relations
	for _, r := range t.AssociationsSlice() {
		if strings.HasPrefix(strings.ToLower(r.FieldName()), reservedPrefix) {
			c.errorf(r.Span(), diag.E_RESERVED_PREFIX,
				"relation %q uses reserved prefix %q", r.Name(), reservedPrefix)
			ok = false
		}
	}
	for _, r := range t.CompositionsSlice() {
		if strings.HasPrefix(strings.ToLower(r.FieldName()), reservedPrefix) {
			c.errorf(r.Span(), diag.E_RESERVED_PREFIX,
				"relation %q uses reserved prefix %q", r.Name(), reservedPrefix)
			ok = false
		}
	}

	// Check case-insensitive property collisions
	if !c.checkPropertyCaseCollisions(t) {
		ok = false
	}

	// Check property-relation collisions
	if !c.checkPropertyRelationCollisions(t) {
		ok = false
	}

	// Check relation name collisions (after normalization)
	if !c.checkRelationCollisions(t) {
		ok = false
	}

	return ok
}

// checkPropertyCaseCollisions detects case-insensitive property name collisions.
func (c *completer) checkPropertyCaseCollisions(t *schema.Type) bool {
	ok := true
	seen := make(map[string]*schema.Property) // lowercase -> first property

	for _, p := range t.AllPropertiesSlice() {
		lower := strings.ToLower(p.Name())
		if existing, found := seen[lower]; found {
			if existing.Name() != p.Name() {
				// Case collision - same lowercase but different actual names
				c.errorf(p.Span(), diag.E_CASE_COLLISION,
					"property %q in type %q collides with %q (case-insensitive)",
					p.Name(), t.Name(), existing.Name())
				ok = false
			}
			// Same exact name is OK (might be inherited from same ancestor)
		} else {
			seen[lower] = p
		}
	}

	return ok
}

// checkPropertyRelationCollisions detects collisions between property names and relation field names.
func (c *completer) checkPropertyRelationCollisions(t *schema.Type) bool {
	ok := true

	// Build property name set (lowercase for case-insensitive check)
	propNames := make(map[string]*schema.Property)
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
			ok = false
		}
	}

	// Check compositions
	for _, r := range t.AllCompositionsSlice() {
		lower := strings.ToLower(r.FieldName())
		if prop, found := propNames[lower]; found {
			c.errorf(r.Span(), diag.E_PROPERTY_RELATION_COLLISION,
				"relation %q (field: %q) in type %q collides with property %q",
				r.Name(), r.FieldName(), t.Name(), prop.Name())
			ok = false
		}
	}

	return ok
}

// checkRelationCollisions detects relation name collisions after normalization.
func (c *completer) checkRelationCollisions(t *schema.Type) bool {
	ok := true
	seen := make(map[string]*schema.Relation) // fieldName -> first relation

	// Check associations
	for _, r := range t.AllAssociationsSlice() {
		if existing, found := seen[r.FieldName()]; found {
			// Collision - check if they're from the same declaration
			if !r.Equal(existing) {
				// E_RELATION_NORMALIZATION_COLLISION: different raw names normalize to same field name
				c.errorf(r.Span(), diag.E_RELATION_NORMALIZATION_COLLISION,
					"association %q in type %q collides with existing relation %q (both normalize to %q)",
					r.Name(), t.Name(), existing.Name(), r.FieldName())
				ok = false
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
				ok = false
			}
		} else {
			seen[r.FieldName()] = r
		}
	}

	return ok
}

// validateRelationTargets checks that all relation targets exist.
func (c *completer) validateRelationTargets() bool {
	ok := true

	for _, t := range c.schema.TypesSlice() {
		// Check associations
		for _, r := range t.AssociationsSlice() {
			if !c.validateRelationTarget(t, r, "association") {
				ok = false
			}
		}

		// Check compositions - must target a concrete part type
		for _, r := range t.CompositionsSlice() {
			if !c.validateCompositionTarget(t, r) {
				ok = false
			}
		}
	}

	// Validate association constraints on part types
	if !c.validateAssociationTargets() {
		ok = false
	}

	return ok
}

// validateAssociationTargets checks that part types don't declare associations
// and that associations don't target part types.
func (c *completer) validateAssociationTargets() bool {
	ok := true

	for _, t := range c.schema.TypesSlice() {
		// Part types cannot declare associations
		if t.IsPart() {
			for _, r := range t.AssociationsSlice() {
				c.errorf(r.Span(), diag.E_INVALID_ASSOCIATION_TARGET,
					"part type %q cannot declare association %q", t.Name(), r.Name())
				ok = false
			}
		}

		// Associations cannot target part types
		for _, r := range t.AssociationsSlice() {
			target := c.resolveTypeRef(r.Target())
			if target != nil && target.IsPart() {
				c.errorf(r.Span(), diag.E_INVALID_ASSOCIATION_TARGET,
					"association %q in type %q cannot target part type %q",
					r.Name(), t.Name(), target.Name())
				ok = false
			}
		}
	}

	return ok
}

// validateRelationTarget checks that a relation target exists.
func (c *completer) validateRelationTarget(_ *schema.Type, r *schema.Relation, kind string) bool {
	target := c.resolveTypeRef(r.Target())
	if target == nil {
		// Check if it's a qualified ref that we can't resolve yet
		if r.Target().Qualifier() != "" && c.registry == nil {
			// Deferred - will be checked when registry is available
			return true
		}

		// Target not found
		c.errorf(r.Span(), diag.E_UNKNOWN_TYPE,
			"type %q referenced in %s %q does not exist",
			r.Target().String(), kind, r.Name())
		return false
	}

	// Resolve the semantic identity
	r.SetTargetID(target.ID())

	return true
}

// validateCompositionTarget checks that a composition target is a concrete part type.
// NOTE: When the target is a cross-schema ref and registry is nil, the IsPart and
// IsAbstract checks are deferred. These constraints should be re-validated when
// the schema is linked with a registry that can resolve cross-schema references.
func (c *completer) validateCompositionTarget(t *schema.Type, r *schema.Relation) bool {
	target := c.resolveTypeRef(r.Target())
	if target == nil {
		// Cross-schema ref without registry: defer validation to linking phase.
		if r.Target().Qualifier() != "" && c.registry == nil {
			return true
		}

		c.errorf(r.Span(), diag.E_UNKNOWN_TYPE,
			"type %q referenced in composition %q does not exist",
			r.Target().String(), r.Name())
		return false
	}

	// Composition targets must be part types
	if !target.IsPart() {
		c.errorf(r.Span(), diag.E_INVALID_COMPOSITION_TARGET,
			"composition %q in type %q must reference a part type, but %q is not a part",
			r.Name(), t.Name(), target.Name())
		return false
	}

	// Composition targets cannot be abstract
	if target.IsAbstract() {
		c.errorf(r.Span(), diag.E_INVALID_COMPOSITION_TARGET,
			"composition %q in type %q must reference a concrete type, but %q is abstract",
			r.Name(), t.Name(), target.Name())
		return false
	}

	// Resolve the semantic identity
	r.SetTargetID(target.ID())

	return true
}
