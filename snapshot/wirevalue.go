package snapshot

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
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
//
// Delegating to json.Marshal instead would be shorter but costs 2.18× the
// time and eight more allocations per call, on the path every float in a
// document takes. [TestWireFloat_MatchesEncodingJSON] holds the two in
// lockstep so this copy cannot drift from the encoder it mirrors.
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

// wireTagForm renders a TypeID in tag form — the bare name for a locally
// declared type, alias-qualified for an imported one. It inverts
// [resolveWireType] and matches the TypeName a graph instance carries.
func wireTagForm(s *schema.Schema, id schema.TypeID) string {
	if s == nil || id.IsZero() || id.SchemaPath() == s.SourceID() {
		return id.Name()
	}
	if alias := s.FindImportAlias(id.SchemaPath()); alias != "" {
		return alias + "." + id.Name()
	}
	return id.Name()
}

// resolveWireType resolves a snapshot tag-form type name (alias-qualified for
// imported types) against the schema, falling back to the TypeID for names
// the tag form cannot resolve. A miss returns (nil, false) and callers pass
// values through unwrapped — Marshal never rejects a snapshot Load accepts.
func resolveWireType(s *schema.Schema, tagName string, id schema.TypeID) (*schema.Type, bool) {
	if s == nil {
		return nil, false
	}
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

// typeByWireID resolves a persisted type_id — schema path and name together —
// against the entry schema's import closure. Matching the path as well as the
// name is what keeps two same-named types in different schemas apart; a
// name-only lookup silently rebinds one to the other.
func typeByWireID(s *schema.Schema, w *typeIDWire) (*schema.Type, bool) {
	if s == nil || w == nil {
		return nil, false
	}
	for _, cs := range s.Closure() {
		if cs.SourceID().String() != w.SchemaPath {
			continue
		}
		if t, ok := cs.Type(w.Name); ok {
			return t, true
		}
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
		elems, ok := wireElems(v)
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
		elems, ok := wireElems(v)
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

// twoPow63 and twoPow64 are the rounding ceilings of int64 and uint64 in
// float64: a conversion landing on one has rounded past the integer type's
// range, and converting it back is undefined.
const (
	twoPow63 = 1 << 63
	twoPow64 = 1 << 64
)

// exactWireInt reports n's float64 form when the conversion is exact. Only an
// exactly convertible integer can have come from a narrowed whole float, so
// healing an inexact one would invent precision the document never carried.
func exactWireInt(n int64) (float64, bool) {
	f := float64(n)
	if f == twoPow63 {
		return 0, false
	}
	return f, int64(f) == n
}

// exactWireUint is [exactWireInt] for unsigned values, which reach a float
// position only from a caller-assembled snapshot.
func exactWireUint(n uint64) (float64, bool) {
	f := float64(n)
	if f == twoPow64 {
		return 0, false
	}
	return f, uint64(f) == n
}

// wireNumeric wraps any numeric value as wireFloat so it emits with a float
// indicator; the type switch fast-paths the two types validation produces, and
// reflection reaches the rest, which arrive only from a caller-assembled
// snapshot. Non-numeric shapes pass through — Load never re-validates.
func wireNumeric(v any) any {
	switch n := v.(type) {
	case float64:
		return wireFloat(n)
	case int64:
		if f, ok := exactWireInt(n); ok {
			return wireFloat(f)
		}
		return v
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return wireFloat(rv.Float())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if f, ok := exactWireInt(rv.Int()); ok {
			return wireFloat(f)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if f, ok := exactWireUint(rv.Uint()); ok {
			return wireFloat(f)
		}
	}
	return v
}

// wireElems returns v's elements as []any for any slice or array, so a vector
// or list position is reached whatever concrete container the caller built.
func wireElems(v any) ([]any, bool) {
	if elems, ok := v.([]any); ok {
		return elems, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	elems := make([]any, rv.Len())
	for i := range elems {
		elems[i] = rv.Index(i).Interface()
	}
	return elems, true
}
