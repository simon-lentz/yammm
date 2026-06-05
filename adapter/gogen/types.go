package gogen

import (
	"errors"
	"fmt"

	"github.com/simon-lentz/yammm/schema"
)

// goBaseType maps a constraint to its primitive Go type. It is the single
// ConstraintKind dispatch site in the generator and is guarded so a newly-added
// kind fails the build rather than silently emitting a wrong or empty type.
// Enum maps to its string underlying; named Enum/DataType types are applied by
// the field emitter, not here.
func goBaseType(c schema.Constraint) (string, error) {
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
	case schema.KindTimestamp, schema.KindDate:
		return "time.Time", nil
	case schema.KindVector:
		return "[]float64", nil
	case schema.KindList:
		lc, ok := c.(schema.ListConstraint)
		if !ok {
			return "", errors.New("gogen: List kind without ListConstraint")
		}
		elem, err := goBaseType(lc.Element())
		if err != nil {
			return "", err
		}
		return "[]" + elem, nil
	case schema.KindAlias:
		// Listed for the exhaustiveness guard. Reachable only for an unresolved or
		// cyclic alias (ResolveAlias returns the alias unchanged in those cases);
		// a completed schema never reaches here.
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
