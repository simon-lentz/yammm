package schema

import (
	"fmt"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// detectCollisions checks for naming collisions in all types.
func (c *completer) detectCollisions() {
	for _, t := range c.schema.types {
		c.detectTypeCollisions(t)
	}
}

// detectTypeCollisions checks for collisions within a single type. Findings
// are collected; there is no failure signal — the completer's final error
// gate decides whether a schema is produced.
func (c *completer) detectTypeCollisions(t Type) {
	c.checkPropertyCaseCollisions(t)
	c.checkEdgePropertyCaseCollisions(t)
	c.checkPropertyRelationCollisions(t)
	c.checkRelationCollisions(t)
}

// checkEdgePropertyCaseCollisions reports two edge properties on one of t's
// own associations whose names differ only by case: the case-folded lookup
// the validator reads them through is sound only when the schema guarantees
// uniqueness under fold, as it does for node properties.
func (c *completer) checkEdgePropertyCaseCollisions(t Type) {
	for r := range t.Associations() {
		first := make(map[string]Property)
		for _, p := range r.properties {
			lower := strings.ToLower(p.Name())
			if prior, seen := first[lower]; seen {
				c.errorfRelated(p.Span(), diag.E_CASE_COLLISION,
					[]location.RelatedInfo{{Span: prior.Span(), Message: "collides with this declaration"}},
					"edge property %q on relation %q in type %q collides with %q (case-insensitive)", p.Name(), r.Name(), t.Name(), prior.Name())
				continue
			}
			first[lower] = p
		}
	}
}

// checkPropertyCaseCollisions detects case-insensitive property name collisions.
// checkPropertyCaseCollisions reports two merged properties whose names differ
// only by case. A collision an own declaration introduces is reported there;
// one two ancestors introduce together is reported once, on the type that
// combined them.
func (c *completer) checkPropertyCaseCollisions(t Type) {
	own := make(map[Property]bool)
	for p := range t.Properties() {
		own[p] = true
	}
	byLower := make(map[string][]Property)
	var order []string
	for p := range t.AllProperties() {
		lower := strings.ToLower(p.Name())
		if _, seen := byLower[lower]; !seen {
			order = append(order, lower)
		}
		byLower[lower] = append(byLower[lower], p)
	}
	for _, lower := range order {
		group := byLower[lower]
		if len(group) < 2 {
			continue
		}
		first := group[0]
		for _, p := range group[1:] {
			if p.Name() == first.Name() {
				continue // the same declaration reached through two ancestors
			}
			switch {
			case own[p.Origin()]:
				c.errorfRelated(p.Span(), diag.E_CASE_COLLISION,
					[]location.RelatedInfo{{Span: first.Span(), Message: "collides with this declaration"}},
					"property %q in type %q collides with %q (case-insensitive)", p.Name(), t.Name(), first.Name())
			case own[first.Origin()]:
				c.errorfRelated(first.Span(), diag.E_CASE_COLLISION,
					[]location.RelatedInfo{{Span: p.Span(), Message: "collides with this declaration"}},
					"property %q in type %q collides with %q (case-insensitive)", first.Name(), t.Name(), p.Name())
			case c.aSuperCarries(t, func(s Type) bool {
				_, a := s.Property(first.Name())
				_, b := s.Property(p.Name())
				return a && b
			}):
				// An ancestor already holds both, so the collision was reported there.
			default:
				c.errorfRelated(t.Span(), diag.E_CASE_COLLISION,
					[]location.RelatedInfo{
						{Span: first.Span(), Message: "declared here"},
						{Span: p.Span(), Message: "and here"},
					},
					"type %q inherits properties %q and %q that collide (case-insensitive)", t.Name(), first.Name(), p.Name())
			}
		}
	}
}

// checkPropertyRelationCollisions detects collisions between property names and relation field names.
// checkPropertyRelationCollisions reports a property and a relation whose
// field name share one lower-case spelling. The own declaration is where the
// diagnostic anchors; two inherited members anchor once on the combining type.
func (c *completer) checkPropertyRelationCollisions(t Type) {
	ownProps := make(map[Property]bool)
	for p := range t.Properties() {
		ownProps[p] = true
	}
	ownRels := make(map[Relation]bool)
	for r := range t.Associations() {
		ownRels[r] = true
	}
	for r := range t.Compositions() {
		ownRels[r] = true
	}
	props := make(map[string]Property)
	for p := range t.AllProperties() {
		lower := strings.ToLower(p.Name())
		if _, seen := props[lower]; !seen {
			props[lower] = p
		}
	}
	check := func(r Relation) {
		prop, found := props[r.FieldName()]
		if !found {
			return
		}
		msg := "relation %q (field: %q) in type %q collides with property %q"
		switch {
		case ownRels[r]:
			c.errorfRelated(r.Span(), diag.E_PROPERTY_RELATION_COLLISION,
				[]location.RelatedInfo{{Span: prop.Span(), Message: "property declared here"}},
				msg, r.Name(), r.FieldName(), t.Name(), prop.Name())
		case ownProps[prop.Origin()]:
			c.errorfRelated(prop.Span(), diag.E_PROPERTY_RELATION_COLLISION,
				[]location.RelatedInfo{{Span: r.Span(), Message: "relation declared here"}},
				msg, r.Name(), r.FieldName(), t.Name(), prop.Name())
		case c.aSuperCarries(t, func(s Type) bool {
			_, a := s.Relation(r.Name())
			_, b := s.Property(prop.Name())
			return a && b
		}):
			// An ancestor already holds both, so the collision was reported there.
		default:
			c.errorfRelated(t.Span(), diag.E_PROPERTY_RELATION_COLLISION,
				[]location.RelatedInfo{
					{Span: r.Span(), Message: "relation declared here"},
					{Span: prop.Span(), Message: "property declared here"},
				},
				msg, r.Name(), r.FieldName(), t.Name(), prop.Name())
		}
	}
	for r := range t.AllAssociations() {
		check(r)
	}
	for r := range t.AllCompositions() {
		check(r)
	}
}

// checkRelationCollisions detects relation name collisions after normalization.
// checkRelationCollisions reports an association and a composition that reach
// the merged view under one name. Within one kind the merge already settled a
// shared name (keep-first, with E_RELATION_COLLISION when definitions that
// are known differ), and two own relations under one name are E_DUPLICATE_RELATION, so
// the only clash left to find is across the two kinds — inherited from
// different ancestors, or one own and one inherited.
func (c *completer) checkRelationCollisions(t Type) {
	ownAssocs := make(map[Relation]bool)
	for r := range t.Associations() {
		ownAssocs[r] = true
	}
	ownComps := make(map[Relation]bool)
	for r := range t.Compositions() {
		ownComps[r] = true
	}
	assocs := make(map[string]Relation, len(t.allAssociations))
	for r := range t.AllAssociations() {
		assocs[r.FieldName()] = r
	}
	for comp := range t.AllCompositions() {
		assoc, found := assocs[comp.FieldName()]
		if !found {
			continue
		}
		related := []location.RelatedInfo{
			{Span: assoc.Span(), Message: "association declared here"},
			{Span: comp.Span(), Message: "composition declared here"},
		}
		anchor := t.Span()
		switch {
		case ownComps[comp]:
			anchor = comp.Span()
		case ownAssocs[assoc]:
			anchor = assoc.Span()
		case c.aSuperCarries(t, func(s Type) bool {
			var hasAssoc, hasComp bool
			for r := range s.AllAssociations() {
				hasAssoc = hasAssoc || r == assoc
			}
			for r := range s.AllCompositions() {
				hasComp = hasComp || r == comp
			}
			return hasAssoc && hasComp
		}):
			continue // an ancestor already holds both, so the collision was reported there
		}
		c.errorfRelated(anchor, diag.E_RELATION_COLLISION, related,
			"type %q carries an association and a composition both named %q",
			t.Name(), comp.Name())
	}
}

// validateRelationTargets checks that all relation targets exist.
func (c *completer) validateRelationTargets() {
	for _, t := range c.schema.types {
		for r := range t.Compositions() {
			c.validateCompositionTarget(t, r)
		}
	}
	c.detectRequiredCompositionCycles()
	c.validateAssociationTargets()
}

// detectRequiredCompositionCycles refuses each group of types that require one
// another through compositions alone: every instance in it must hold a child of
// another kind in it, so none is finite. An optional link ends the chain, so a
// cycle that holds one stays legal. A group is a strongly connected component
// of the required links, so its one issue does not depend on declaration order.
// A cycle stays within one schema, since an import never reaches back to its
// importer.
func (c *completer) detectRequiredCompositionCycles() {
	types := c.schema.types
	order := make(map[Type]int, len(types))
	for i, t := range types {
		order[t] = i
	}
	links := make(map[Type][]Relation, len(types))
	for _, t := range types {
		for r := range t.AllCompositions() {
			if r.IsOptional() {
				continue
			}
			if target := c.resolveTypeID(r.TargetID()); !target.IsZero() && c.typeIndex[target.Name()] == target {
				links[t] = append(links[t], r)
			}
		}
	}
	for _, group := range c.stronglyConnected(types, links) {
		c.reportCompositionCycle(group, links, order)
	}
}

// stronglyConnected returns the strongly connected components of the required
// composition links that hold a cycle (more than one type, or a type that
// requires itself), by Tarjan's algorithm.
func (c *completer) stronglyConnected(types []Type, links map[Type][]Relation) [][]Type {
	index := make(map[Type]int, len(types))
	low := make(map[Type]int, len(types))
	onStack := make(map[Type]bool, len(types))
	var stack []Type
	var groups [][]Type
	next := 0

	var visit func(t Type)
	visit = func(t Type) {
		index[t], low[t] = next, next
		next++
		stack = append(stack, t)
		onStack[t] = true
		for _, r := range links[t] {
			w := c.resolveTypeID(r.TargetID())
			if _, seen := index[w]; !seen {
				visit(w)
				low[t] = min(low[t], low[w])
			} else if onStack[w] {
				low[t] = min(low[t], index[w])
			}
		}
		if low[t] != index[t] {
			return
		}
		var group []Type
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			group = append(group, w)
			if w == t {
				break
			}
		}
		if len(group) > 1 || slices.ContainsFunc(links[t], func(r Relation) bool { return c.resolveTypeID(r.TargetID()) == t }) {
			groups = append(groups, group)
		}
	}
	for _, t := range types {
		if _, seen := index[t]; !seen {
			visit(t)
		}
	}
	return groups
}

// reportCompositionCycle reports one group at its first-declared type. The
// message names the shortest cycle through that type, and each required link
// inside the group is related information, so the author sees every link one
// optional multiplicity can break.
func (c *completer) reportCompositionCycle(group []Type, links map[Type][]Relation, order map[Type]int) {
	slices.SortFunc(group, func(a, b Type) int { return order[a] - order[b] })
	in := make(map[Type]bool, len(group))
	for _, t := range group {
		in[t] = true
	}
	anchor := group[0]

	// Breadth-first from the anchor back to it, in declaration order, so the
	// named cycle is the shortest and the same on every load.
	parent := map[Type]Type{}
	var closing Type
	queue := []Type{anchor}
	for len(queue) > 0 && closing.IsZero() {
		t := queue[0]
		queue = queue[1:]
		for _, r := range links[t] {
			w := c.resolveTypeID(r.TargetID())
			if w == anchor {
				closing = t
				break
			}
			if _, seen := parent[w]; !seen && in[w] {
				parent[w] = t
				queue = append(queue, w)
			}
		}
	}
	names := []string{anchor.Name()}
	for t := closing; t != anchor; t = parent[t] {
		names = append(names, t.Name())
	}
	slices.Reverse(names[1:])
	names = append(names, anchor.Name())

	var related []location.RelatedInfo
	for _, t := range group {
		for _, r := range links[t] {
			if w := c.resolveTypeID(r.TargetID()); in[w] {
				related = append(related, location.RelatedInfo{
					Span:    r.Span(),
					Message: fmt.Sprintf("%s requires %s through %s", t.Name(), w.Name(), r.Name()),
				})
			}
		}
	}
	c.errorfRelated(anchor.Span(), diag.E_COMPOSITION_CYCLE, related,
		"composition cycle detected: %s; every composition on it is required, so no instance of %q is finite",
		strings.Join(names, " -> "), anchor.Name())
}

// validateAssociationTargets checks that part types don't declare associations
// and that associations target a concrete type (not a part or abstract type).
func (c *completer) validateAssociationTargets() {
	for _, t := range c.schema.types {
		// A part type carries no association, declared or inherited: an
		// inherited one is a member of the holder without being declared
		// there, so the merged view is what the rule reads.
		if t.IsPart() {
			own := make(map[Relation]bool)
			for r := range t.Associations() {
				own[r] = true
				c.errorf(r.Span(), diag.E_INVALID_ASSOCIATION_TARGET,
					"part type %q cannot declare association %q", t.Name(), r.Name())
			}
			for r := range t.AllAssociations() {
				if own[r] {
					continue
				}
				// Reported once, on the first part type in the chain that
				// inherits it. The whole linearized ancestry is read, so a
				// non-part type between two part types does not hide the one
				// that already reported.
				if c.aPartAncestorCarries(t, r) {
					continue
				}
				c.errorfRelated(t.Span(), diag.E_INVALID_ASSOCIATION_TARGET,
					[]location.RelatedInfo{{Span: r.Span(), Message: "association declared here"}},
					"part type %q inherits association %q from %s; a part type cannot carry an association",
					t.Name(), r.Name(), r.Owner())
			}
		}

		// Associations must target a concrete type. An edge resolves against the
		// declared target's exact TypeID, and neither a part (composition-only) nor
		// an abstract (non-instantiable) type ever has an instance under that
		// TypeID, so such an edge could never resolve in a graph.
		for r := range t.Associations() {
			target := c.resolveTypeID(r.TargetID())
			if target.IsZero() {
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
// resolveRelationTargets binds every own relation to its target's identity
// (phase 3c). A target that does not exist is reported here once; a target
// behind an import the loader could not resolve, or one whose type declaration
// is displaced, is deferred, since the import failure or the duplicate already
// carries the root cause.
func (c *completer) resolveRelationTargets() {
	for _, t := range c.schema.types {
		for r := range t.Associations() {
			c.resolveRelationTarget(r, "association")
		}
		for r := range t.Compositions() {
			c.resolveRelationTarget(r, "composition")
		}
	}
}

func (c *completer) resolveRelationTarget(r Relation, kind string) {
	target := c.resolveTypeRef(r.Target())
	if target.IsZero() {
		if c.referenceDeferred(r.Target()) {
			return
		}
		c.errorf(r.Span(), diag.E_UNKNOWN_TYPE,
			"type %q referenced in %s %q does not exist",
			r.Target().String(), kind, r.Name())
		return
	}
	r.setTargetID(target.ID())
}

// validateCompositionTarget checks that a composition target is a concrete part type.
func (c *completer) validateCompositionTarget(t Type, r Relation) {
	// An unresolved target was reported or deferred when targets resolved.
	target := c.resolveTypeID(r.TargetID())
	if target.IsZero() {
		return
	}

	if !target.IsPart() {
		c.errorf(r.Span(), diag.E_INVALID_COMPOSITION_TARGET,
			"composition %q in type %q must reference a part type, but %q is not a part",
			r.Name(), t.Name(), target.Name())
	}
}

// aPartAncestorCarries reports whether a part type anywhere in t's linearized
// ancestry already carries r, and so has already drawn the refusal.
func (c *completer) aPartAncestorCarries(t Type, r Relation) bool {
	for ref := range t.SuperTypes() {
		s := c.resolveTypeID(ref.ID())
		if !s.IsZero() && s.IsPart() && slices.Contains(slices.Collect(s.AllAssociations()), r) {
			return true
		}
	}
	return false
}

// aSuperCarries reports whether some direct supertype of t satisfies has.
// A collision among inherited members that one ancestor already carries was
// reported on that ancestor, so the types below it do not repeat it.
func (c *completer) aSuperCarries(t Type, has func(Type) bool) bool {
	return slices.ContainsFunc(c.directSuperTypes(t), has)
}
