package jschema

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/schema"
)

// defsRefPrefix begins every emitted "$ref": a URI fragment holding a JSON
// Pointer into the document's own $defs.
const defsRefPrefix = "#/$defs/"

// refTo returns a $ref fragment pointing at a $defs entry. The key is
// escaped as a JSON Pointer reference token (RFC 6901: "~" -> "~0", "/" ->
// "~1"), then percent-encoded where RFC 3986 admits no such character in a
// fragment — reachable because qualified keys embed schema names, which are
// unconstrained string literals.
func refTo(defName string) val {
	token := strings.ReplaceAll(strings.ReplaceAll(defName, "~", "~0"), "/", "~1")
	return object(kv{"$ref", scalar(defsRefPrefix + fragmentEscape(token))})
}

// refKey inverts [refTo]: it returns the $defs key a "$ref" value names, read
// as a resolver reads it — the fragment percent-decoded, then split on "/"
// into RFC 6901 tokens, then each token unescaped. A pointer below a $defs
// entry names no entry, so a second token is refused, and so is a "~" that is
// not "~0" or "~1", which RFC 6901 does not define.
func refKey(ref string) (string, error) {
	token, ok := strings.CutPrefix(ref, defsRefPrefix)
	if !ok {
		return "", fmt.Errorf("$ref %q is not a %s pointer", ref, defsRefPrefix)
	}
	token, err := url.PathUnescape(token)
	if err != nil {
		return "", fmt.Errorf("$ref %q is not a valid URI fragment: %w", ref, err)
	}
	if strings.Contains(token, "/") {
		return "", fmt.Errorf("$ref %q points below a %s entry", ref, defsRefPrefix)
	}
	for i := 0; i < len(token); i++ {
		if token[i] != '~' {
			continue
		}
		if i+1 == len(token) || (token[i+1] != '0' && token[i+1] != '1') {
			return "", fmt.Errorf("$ref %q holds a \"~\" RFC 6901 does not define", ref)
		}
		i++
	}
	return strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~"), nil
}

// fragmentEscape percent-encodes every byte RFC 3986's fragment production
// does not admit literally, the "%" of an existing escape among them.
func fragmentEscape(s string) string {
	var sb strings.Builder
	for i := range len(s) {
		if b := s[i]; fragmentByte(b) {
			sb.WriteByte(b)
		} else {
			fmt.Fprintf(&sb, "%%%02X", b)
		}
	}
	return sb.String()
}

// fragmentByte reports whether RFC 3986 admits b literally in a fragment:
// unreserved, sub-delims, ":", "@", "/" and "?".
func fragmentByte(b byte) bool {
	switch {
	case 'a' <= b && b <= 'z', 'A' <= b && b <= 'Z', '0' <= b && b <= '9':
		return true
	}
	return strings.IndexByte("-._~!$&'()*+,;=:@/?", b) >= 0
}

// schemaForProperty returns the JSON Schema fragment for a property's
// constraint, rendering a named-DataType reference as a $ref at any List
// depth. dtRef supplies the $defs key for a property whose constraint — or
// whose innermost list element — is a named DataType reference; the table is
// keyed by property pointer and populated per declaring schema, so a property
// inherited from a cross-schema parent resolves to the correct def. A
// DataType-referencing property with no registered key is a generator wiring
// bug, surfaced as an error. Properties with no DataType reference route to
// [schemaForConstraint].
func schemaForProperty(p *schema.Property, dtRef func(*schema.Property) (string, bool)) (val, error) {
	c := p.Constraint()
	lists, ac, ok := aliasInLists(c)
	if !ok {
		return schemaForConstraint(c)
	}
	name, ok := dtRef(p)
	if !ok {
		return val{}, fmt.Errorf("jschema: no registered $defs key for datatype property %q (%s)", p.Name(), ac.DataTypeName())
	}
	return wrapInLists(lists, refTo(name)), nil
}

// aliasInLists unwraps c's List layers, outermost first. It reports the
// named-DataType reference they wrap, or false when the innermost element is
// not one.
func aliasInLists(c schema.Constraint) ([]schema.ListConstraint, schema.AliasConstraint, bool) {
	var lists []schema.ListConstraint
	for {
		switch x := c.(type) {
		case schema.ListConstraint:
			lists = append(lists, x)
			c = x.Element()
		case schema.AliasConstraint:
			return lists, x, true
		default:
			return nil, schema.AliasConstraint{}, false
		}
	}
}

// wrapInLists builds the array fragments of lists, outermost first, around
// the innermost element's fragment.
func wrapInLists(lists []schema.ListConstraint, inner val) val {
	for _, lc := range slices.Backward(lists) {
		inner = listSchema(lc, inner)
	}
	return inner
}

// schemaForConstraint maps an alias-free constraint to its JSON Schema
// fragment. It is the single ConstraintKind dispatch site in the generator and
// is guarded so a newly-added kind fails the build rather than silently
// emitting a wrong or empty fragment. Named DataType references are the
// callers' concern ([schemaForProperty], [dataTypeDef]); an alias reaching
// this switch errors.
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
		return patternSchema(pc.Patterns()), nil

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
		items, err := schemaForConstraint(lc.Element())
		if err != nil {
			return val{}, err
		}
		return listSchema(lc, items), nil

	case schema.KindAlias:
		// Every caller routes a datatype reference, at any List depth, to a
		// $ref first ([schemaForProperty], [dataTypeDef]), so an alias here
		// is a generator bug.
		return val{}, errors.New("jschema: alias constraint reached the constraint mapper")

	default:
		return val{}, fmt.Errorf("jschema: unhandled constraint kind %v", c.Kind())
	}
}

// patternSchema emits the string fragment for a Pattern constraint. Each
// pattern is rewritten by [sharedPattern]; yammm requires a value to match
// every pattern, so two compose as allOf, not anyOf. A pattern with no shared
// form is not asserted: its source form is carried as the description, as a
// custom Timestamp layout is.
func patternSchema(patterns []string) val {
	var asserted []string
	var unstated []string
	for _, p := range patterns {
		if shared, ok := sharedPattern(p); ok {
			asserted = append(asserted, shared)
		} else {
			unstated = append(unstated, fmt.Sprintf("%q", p))
		}
	}
	pairs := []kv{{"type", scalar("string")}}
	switch len(asserted) {
	case 0:
	case 1:
		pairs = append(pairs, kv{"pattern", scalar(asserted[0])})
	default:
		all := make([]val, len(asserted))
		for i, p := range asserted {
			all[i] = object(kv{"pattern", scalar(p)})
		}
		pairs = append(pairs, kv{"allOf", array(all...)})
	}
	if len(unstated) > 0 {
		pairs = append(pairs, kv{"description", scalar("Pattern[" + strings.Join(unstated, ", ") + "]")})
	}
	return object(pairs...)
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
