package gogen

import (
	"errors"
	"fmt"

	"github.com/simon-lentz/yammm/schema"
)

// goBaseType maps a resolved constraint to its Go type, a Date or custom-layout
// Timestamp to the generated type registerTemporalTypes assigned. In collect
// mode it records that temporal type and returns an empty name for it.
func (g *generator) goBaseType(c schema.Constraint) (string, error) {
	c = schema.ResolveAlias(c)
	//exhaustive:enforce
	switch c.Kind() {
	case schema.KindString, schema.KindUUID, schema.KindPattern, schema.KindEnum:
		return "string", nil
	case schema.KindInteger:
		return "int64", nil
	case schema.KindFloat:
		return "float64", nil
	case schema.KindBoolean:
		return "bool", nil
	case schema.KindTimestamp:
		tc, ok := c.(schema.TimestampConstraint)
		if !ok || tc.Format() == "" {
			// The import is decided where time.Time is written, so no
			// emitter can name it without importing it.
			if g.collect == nil {
				g.needsTime = true
			}
			return "time.Time", nil
		}
		if g.collect != nil {
			g.collect.layouts[tc.Format()] = true
			return "", nil
		}
		name, ok := g.temporal.layouts[tc.Format()]
		if !ok {
			return "", fmt.Errorf("gogen: timestamp layout %q reached emission without a registered type", tc.Format())
		}
		return name, nil
	case schema.KindDate:
		if g.collect != nil {
			g.collect.date = true
			return "", nil
		}
		if g.temporal.date == "" {
			return "", errors.New("gogen: a Date position reached emission without the Date type registered")
		}
		return g.temporal.date, nil
	case schema.KindVector:
		return "[]float64", nil
	case schema.KindList:
		lc, ok := c.(schema.ListConstraint)
		if !ok {
			return "", errors.New("gogen: List kind without ListConstraint")
		}
		elem, err := g.goBaseType(lc.Element())
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	case schema.KindAlias:
		// Reachable only for an unresolved or cyclic alias, which ResolveAlias
		// returns unchanged; a completed schema never gets here.
		return "", errors.New("gogen: unresolved alias constraint")
	default:
		return "", fmt.Errorf("gogen: unhandled constraint kind %v", c.Kind())
	}
}

// isSliceKind reports whether a resolved kind renders as a Go slice, which an
// optional field keeps unpointered since nil already encodes absence.
func isSliceKind(k schema.ConstraintKind) bool {
	return k == schema.KindList || k == schema.KindVector
}
