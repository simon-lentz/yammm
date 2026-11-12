package gogen

import (
	"errors"
	"fmt"

	"github.com/simon-lentz/yammm/schema"
)

// goBaseType maps a resolved constraint to its Go type, a Date to the generated
// Date type.
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
		// The import is decided where time.Time is written, so no
		// emitter can name it without importing it.
		g.needsTime = true
		return "time.Time", nil
	case schema.KindDate:
		// The Date type is declared where its name is written, as the import is.
		g.needsDate = true
		return dateGoName, nil
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
