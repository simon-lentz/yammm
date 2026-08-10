package snapshot

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// wireFloat marks a value under a float-bearing constraint so it emits with a
// float indicator (".", "e", or "E") — the set [immutable.NormalizeNumber]
// classifies by on decode. Without it, a whole float emits int-shaped and
// narrows to int64 across a marshal/load round trip.
type wireFloat float64

// MarshalJSON emits the value exactly as encoding/json's float encoder would,
// then appends ".0" when the output carries no float indicator (only the 'f'
// branch can lack one). Non-finite values error, as under encoding/json.
func (f wireFloat) MarshalJSON() ([]byte, error) {
	v := float64(f)
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return nil, fmt.Errorf("wire float: unsupported value: %s", strconv.FormatFloat(v, 'g', -1, 64))
	}
	abs := math.Abs(v)
	format := byte('f')
	if abs != 0 && (abs < 1e-6 || abs >= 1e21) {
		format = 'e'
	}
	b := strconv.AppendFloat(nil, v, format, -1, 64)
	if format == 'e' {
		// Trim a zero-padded exponent (e-09 → e-9), as encoding/json does.
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
		return b, nil
	}
	if !bytes.ContainsAny(b, ".eE") {
		b = append(b, '.', '0')
	}
	return b, nil
}

// resolveWireType resolves a snapshot tag-form type name (alias-qualified for
// imported types) against the schema, falling back to the TypeID for names
// the tag form cannot resolve. A miss returns (nil, false) and callers pass
// values through unwrapped — Marshal never rejects a snapshot Load accepts.
func resolveWireType(s *schema.Schema, tagName string, id schema.TypeID) (*schema.Type, bool) {
	if alias, name, qualified := strings.Cut(tagName, "."); qualified {
		if imp, ok := s.ImportByAlias(alias); ok {
			if imported := imp.Schema(); imported != nil {
				if t, ok := imported.Type(name); ok {
					return t, true
				}
			}
		}
	} else if t, ok := s.Type(tagName); ok {
		return t, true
	}
	if !id.IsZero() {
		return s.TypeByID(id)
	}
	return nil, false
}

// wireProps clones props and rewrites each value under its schema constraint
// so float-bearing values emit with a float indicator. A nil clone stays nil
// (the wire's "properties":null shape); a nil type and undeclared properties
// pass through untouched.
func wireProps(props immutable.Properties, t *schema.Type) map[string]any {
	m := props.Clone()
	if len(m) == 0 || t == nil {
		return m
	}
	for name, v := range m {
		if prop, ok := t.Property(name); ok {
			m[name] = wireValue(v, prop.Constraint())
		}
	}
	return m
}

// wireEdgeProps is wireProps for edge properties, whose constraints hang off
// the source type's relation rather than any target type. Resolved and
// unresolved edges share this one helper so the two paths cannot diverge.
func wireEdgeProps(props immutable.Properties, rel *schema.Relation) map[string]any {
	m := props.Clone()
	if len(m) == 0 || rel == nil {
		return m
	}
	for name, v := range m {
		if p, ok := rel.Property(name); ok {
			m[name] = wireValue(v, p.Constraint())
		}
	}
	return m
}

// wireValue rewrites one cloned value under its resolved constraint kind.
// Only float-bearing paths change; every other kind, an unresolved alias, and
// any unexpected shape pass through untouched.
func wireValue(v any, c schema.Constraint) any {
	if v == nil || c == nil {
		return v
	}
	resolved := schema.ResolveAlias(c)
	//exhaustive:enforce
	switch resolved.Kind() {
	case schema.KindFloat:
		return wireNumeric(v)
	case schema.KindVector:
		// Vector elements are floats by definition; the constraint carries
		// only the dimension.
		elems, ok := v.([]any)
		if !ok {
			return v
		}
		for i, e := range elems {
			elems[i] = wireNumeric(e)
		}
		return elems
	case schema.KindList:
		lc, ok := resolved.(schema.ListConstraint)
		if !ok {
			return v
		}
		elems, ok := v.([]any)
		if !ok {
			return v
		}
		elem := lc.Element()
		for i, e := range elems {
			elems[i] = wireValue(e, elem)
		}
		return elems
	case schema.KindString, schema.KindInteger, schema.KindBoolean,
		schema.KindTimestamp, schema.KindDate, schema.KindUUID,
		schema.KindEnum, schema.KindPattern, schema.KindAlias:
		return v
	}
	return v
}

// maxExactWireInt bounds the healing rule: every int64 in [-2^53, 2^53]
// converts to float64 exactly, so a narrowed whole float always heals inside
// it. Beyond it the conversion itself can corrupt, so values pass through and
// simply re-narrow on the next load.
const maxExactWireInt = int64(1) << 53

// wireNumeric wraps a float64 — or an int64 a prior round trip narrowed — as
// wireFloat. Non-numeric shapes pass through untouched: Load never
// re-validates, so a hand-crafted document can put anything here.
func wireNumeric(v any) any {
	switch n := v.(type) {
	case float64:
		return wireFloat(n)
	case int64:
		if n >= -maxExactWireInt && n <= maxExactWireInt {
			return wireFloat(float64(n))
		}
	}
	return v
}
