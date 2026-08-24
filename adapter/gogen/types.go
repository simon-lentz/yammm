package gogen

import (
	"errors"
	"fmt"

	"github.com/simon-lentz/yammm/schema"
)

// goBaseType maps a constraint to its Go type: the primitive for most kinds,
// and for a Date or custom-layout Timestamp the generated type
// registerTemporalTypes assigned. Named Enum/DataType types are applied by
// the field emitter, not here.
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
			return "time.Time", nil
		}
		name, ok := g.temporal.layouts[tc.Format()]
		if !ok {
			return "", fmt.Errorf("gogen: timestamp layout %q reached emission without a registered type", tc.Format())
		}
		return name, nil
	case schema.KindDate:
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

// isSliceKind reports whether a resolved kind renders as a Go slice (so the
// optional-pointer rule does not apply — a nil slice already encodes absence).
func isSliceKind(k schema.ConstraintKind) bool {
	return k == schema.KindList || k == schema.KindVector
}
