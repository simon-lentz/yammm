package jschema

import (
	"errors"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/schema"
)

// refTo returns a $ref fragment pointing at a $defs entry. The key is
// escaped as a JSON Pointer reference token (RFC 6901: "~" -> "~0", "/" ->
// "~1") — reachable because qualified keys embed schema names, which are
// unconstrained string literals.
func refTo(defName string) val {
	escaped := strings.ReplaceAll(strings.ReplaceAll(defName, "~", "~0"), "/", "~1")
	return object(kv{"$ref", scalar("#/$defs/" + escaped)})
}

// schemaForProperty returns the JSON Schema fragment for a property's
// constraint, rendering named-DataType references faithfully as $refs. dtRef
// supplies the $defs key for a property whose constraint — or whose list
// element — is a named DataType reference; the table is keyed by property
// pointer and populated per declaring schema, so a property inherited from a
// cross-schema parent resolves to the correct def. A DataType-referencing
// property with no registered key is a generator wiring bug, surfaced as an
// error. Properties with no DataType reference route to [schemaForConstraint].
func schemaForProperty(p *schema.Property, dtRef func(*schema.Property) (string, bool)) (val, error) {
	c := p.Constraint()

	if ac, ok := c.(schema.AliasConstraint); ok {
		name, ok := dtRef(p)
		if !ok {
			return val{}, fmt.Errorf("jschema: no registered $defs key for datatype property %q (%s)", p.Name(), ac.DataTypeName())
		}
		return refTo(name), nil
	}

	// A list of a named DataType keeps the named element as a $ref
	// (List<FipsCode> -> items: #/$defs/FipsCode). The parser records the
	// element's DataTypeRef as the property's DataTypeRef, so the same
	// per-property registration covers this case.
	if lc, ok := c.(schema.ListConstraint); ok {
		if ec, elemIsAlias := lc.Element().(schema.AliasConstraint); elemIsAlias {
			name, ok := dtRef(p)
			if !ok {
				return val{}, fmt.Errorf("jschema: no registered $defs key for list-element datatype on property %q (%s)", p.Name(), ec.DataTypeName())
			}
			return listSchema(lc, refTo(name)), nil
		}
	}

	return schemaForConstraint(schema.ResolveAlias(c))
}

// schemaForConstraint maps a resolved (alias-free) constraint to its JSON
// Schema fragment. It is the single ConstraintKind dispatch site in the
// generator and is guarded so a newly-added kind fails the build rather than
// silently emitting a wrong or empty fragment. Named DataType references are
// the caller's concern ([schemaForProperty]); an alias reaching this switch
// is unresolved or cyclic and errors.
func schemaForConstraint(c schema.Constraint) (val, error) {
	//exhaustive:enforce
	switch c.Kind() {
	case schema.KindString:
		sc, ok := c.(schema.StringConstraint)
		if !ok {
			return val{}, errors.New("jschema: String kind without StringConstraint")
		}
		pairs := []kv{{"type", scalar("string")}}
		if lo, has := sc.MinLen(); has {
			pairs = append(pairs, kv{"minLength", scalar(lo)})
		}
		if hi, has := sc.MaxLen(); has {
			pairs = append(pairs, kv{"maxLength", scalar(hi)})
		}
		return object(pairs...), nil

	case schema.KindInteger:
		ic, ok := c.(schema.IntegerConstraint)
		if !ok {
			return val{}, errors.New("jschema: Integer kind without IntegerConstraint")
		}
		pairs := []kv{{"type", scalar("integer")}}
		if lo, has := ic.Min(); has {
			pairs = append(pairs, kv{"minimum", scalar(lo)})
		}
		if hi, has := ic.Max(); has {
			pairs = append(pairs, kv{"maximum", scalar(hi)})
		}
		return object(pairs...), nil

	case schema.KindFloat:
		fc, ok := c.(schema.FloatConstraint)
		if !ok {
			return val{}, errors.New("jschema: Float kind without FloatConstraint")
		}
		pairs := []kv{{"type", scalar("number")}}
		if lo, has := fc.Min(); has {
			pairs = append(pairs, kv{"minimum", scalar(lo)})
		}
		if hi, has := fc.Max(); has {
			pairs = append(pairs, kv{"maximum", scalar(hi)})
		}
		return object(pairs...), nil

	case schema.KindBoolean:
		return object(kv{"type", scalar("boolean")}), nil

	case schema.KindEnum:
		ec, ok := c.(schema.EnumConstraint)
		if !ok {
			return val{}, errors.New("jschema: Enum kind without EnumConstraint")
		}
		values := ec.Values()
		items := make([]val, len(values))
		for i, v := range values {
			items[i] = scalar(v)
		}
		return object(kv{"type", scalar("string")}, kv{"enum", array(items...)}), nil

	case schema.KindPattern:
		pc, ok := c.(schema.PatternConstraint)
		if !ok {
			return val{}, errors.New("jschema: Pattern kind without PatternConstraint")
		}
		patterns := pc.Patterns()
		switch len(patterns) {
		case 0:
			return object(kv{"type", scalar("string")}), nil
		case 1:
			return object(kv{"type", scalar("string")}, kv{"pattern", scalar(patterns[0])}), nil
		default:
			// yammm requires a value to match every pattern, so multiple
			// patterns compose as allOf, not anyOf.
			all := make([]val, len(patterns))
			for i, p := range patterns {
				all[i] = object(kv{"pattern", scalar(p)})
			}
			return object(kv{"type", scalar("string")}, kv{"allOf", array(all...)}), nil
		}

	case schema.KindUUID:
		return object(kv{"type", scalar("string")}, kv{"format", scalar("uuid")}), nil

	case schema.KindDate:
		return object(kv{"type", scalar("string")}, kv{"format", scalar("date")}), nil

	case schema.KindTimestamp:
		tc, ok := c.(schema.TimestampConstraint)
		if !ok {
			return val{}, errors.New("jschema: Timestamp kind without TimestampConstraint")
		}
		if tc.Format() == "" {
			return object(kv{"type", scalar("string")}, kv{"format", scalar("date-time")}), nil
		}
		// A custom Go time layout is not expressible as a JSON Schema
		// format assertion; carry the source form as guidance instead.
		return object(kv{"type", scalar("string")}, kv{"description", scalar(tc.String())}), nil

	case schema.KindVector:
		vc, ok := c.(schema.VectorConstraint)
		if !ok {
			return val{}, errors.New("jschema: Vector kind without VectorConstraint")
		}
		n := int64(vc.Dimension())
		return object(
			kv{"type", scalar("array")},
			kv{"items", object(kv{"type", scalar("number")})},
			kv{"minItems", scalar(n)},
			kv{"maxItems", scalar(n)},
		), nil

	case schema.KindList:
		lc, ok := c.(schema.ListConstraint)
		if !ok {
			return val{}, errors.New("jschema: List kind without ListConstraint")
		}
		items, err := schemaForConstraint(schema.ResolveAlias(lc.Element()))
		if err != nil {
			return val{}, err
		}
		return listSchema(lc, items), nil

	case schema.KindAlias:
		// Reachable only for an unresolved or cyclic alias (ResolveAlias
		// returns the alias unchanged in those cases); a completed schema
		// never reaches here.
		return val{}, errors.New("jschema: unresolved alias constraint")

	default:
		return val{}, fmt.Errorf("jschema: unhandled constraint kind %v", c.Kind())
	}
}

// listSchema assembles the array fragment for a List constraint around an
// already-built items fragment (primitive, inline enum, or $ref).
func listSchema(lc schema.ListConstraint, items val) val {
	pairs := []kv{{"type", scalar("array")}, {"items", items}}
	if lo, has := lc.MinLen(); has {
		pairs = append(pairs, kv{"minItems", scalar(lo)})
	}
	if hi, has := lc.MaxLen(); has {
		pairs = append(pairs, kv{"maxItems", scalar(hi)})
	}
	return object(pairs...)
}
