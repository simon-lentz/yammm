package neo4j

import "github.com/simon-lentz/yammm/schema"

// propertyConstraint returns a declared property's constraint, or nil for a
// name the type does not declare or a zero type.
func propertyConstraint(t schema.Type, name string) schema.Constraint {
	if t.IsZero() {
		return nil
	}
	p, ok := t.Property(name)
	if !ok {
		return nil
	}
	return p.Constraint()
}

// elementConstraint returns the constraint each element of a collection is
// coerced through: a List's element constraint, and Float for a Vector, whose
// elements the validator coerces as Floats. Any other constraint has none.
func elementConstraint(c schema.Constraint) schema.Constraint {
	if c == nil {
		return nil
	}
	switch rc := schema.ResolveAlias(c).(type) {
	case schema.ListConstraint:
		return rc.Element()
	case schema.VectorConstraint:
		return schema.NewFloatConstraint()
	}
	return nil
}
