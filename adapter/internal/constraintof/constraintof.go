package constraintof

import "github.com/simon-lentz/yammm/schema"

// Property returns a declared property's constraint, or nil for a name the
// type does not declare or a nil type.
func Property(t *schema.Type, name string) schema.Constraint {
	if t == nil {
		return nil
	}
	p, ok := t.Property(name)
	if !ok {
		return nil
	}
	return p.Constraint()
}

// Element returns the constraint each element of a collection renders
// through: a List's element constraint, and Float for a Vector, whose elements
// the validator coerces as Floats. Any other constraint has none.
func Element(c schema.Constraint) schema.Constraint {
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
