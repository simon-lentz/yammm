package snapshot

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// wireFloat marks a float, or a number under a float-bearing constraint, so it
// emits with a float indicator (".", "e", or "E") — the set
// [immutable.NormalizeNumber] classifies by on decode. Without it, a whole
// float emits int-shaped and narrows to int64 across a marshal/load round trip.
type wireFloat float64

// MarshalJSON emits the value exactly as encoding/json's float encoder would,
// then appends ".0" when the output carries no float indicator. A held float is
// finite: the validator refuses another, and a document spells none.
//
// Delegating to json.Marshal instead would be shorter but slower, and
// allocates more for a whole value, on the path every float in a document
// takes. [TestWireFloat_MatchesEncodingJSON] holds the two in
// lockstep so this copy cannot drift from the encoder it mirrors.
func (f wireFloat) MarshalJSON() ([]byte, error) {
	v := float64(f)
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

// typeByWireID resolves a persisted identity — the declaring schema's name and
// the type's name together — against the entry schema's import closure.
// Matching the schema as well as the type is what keeps two same-named types
// in different schemas apart; a type-name-only lookup silently rebinds one to
// the other.
//
// The schema NAME is the match key rather than its source path, because the
// name is what travels: one schema text loaded from two directories declares
// one name and two paths, and a path-keyed document written under the first
// does not load under the second.
//
// s is not zero: a schema-less read binds no row.
func typeByWireID(s schema.Schema, schemaName, name string) (schema.Type, bool) {
	for cs := range s.Closure() {
		if cs.Name() != schemaName {
			continue
		}
		if t, ok := cs.Type(name); ok {
			return t, true
		}
	}
	return schema.Type{}, false
}

// wireProps clones props and rewrites each value under its schema constraint so
// float-bearing values emit with a float indicator. A nil clone stays nil (the
// wire's "properties":null shape).
func wireProps(props immutable.Properties, t schema.Type) map[string]any {
	m := props.Clone()
	for name, v := range m {
		if prop, found := t.Property(name); found {
			m[name] = wireValue(v, prop.Constraint())
		}
	}
	return m
}

// wireEdgeProps is wireProps for edge properties, whose constraints hang off
// the source type's relation rather than any target type. The resolved and
// unresolved paths agree because both derive rel from the source instance's
// own TypeID — the shared input, not the shared body.
func wireEdgeProps(props immutable.Properties, rel schema.Relation) map[string]any {
	m := props.Clone()
	for name, v := range m {
		if p, found := rel.Property(name); found {
			m[name] = wireValue(v, p.Constraint())
		}
	}
	return m
}

// wireValue rewrites one cloned value under its resolved constraint kind. A
// number at a Float or Vector-element position emits with a float indicator
// when it is a float or an integer a float64 holds exactly ([wireNumeric]), and
// Timestamp, Date and UUID render to their canonical text. Anywhere else, a
// value that is not the kind's shape included, every float in it keeps its
// indicator at any depth of its lists and string-keyed maps
// ([wireUnconstrained]), and every other value passes through untouched.
func wireValue(v any, c schema.Constraint) any {
	if v == nil {
		return v
	}
	if c == nil {
		return wireUnconstrained(v)
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
			return wireUnconstrained(v)
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
			return wireUnconstrained(v)
		}
		elem := lc.Element()
		for i, e := range elems {
			elems[i] = wireValue(e, elem)
		}
		return elems
	case schema.KindTimestamp, schema.KindDate, schema.KindUUID:
		return wireCanonical(v, resolved)
	case schema.KindString, schema.KindInteger, schema.KindBoolean,
		schema.KindEnum, schema.KindPattern, schema.KindAlias:
		return wireUnconstrained(v)
	}
	return wireUnconstrained(v)
}

// wireCanonical renders a temporal or UUID value in the one form the kind
// stores. A shape the constraint cannot render, which only a document can hold,
// passes through, its floats keeping their indicator: Load re-validates only when
// the caller asks, and what it loads stays writable.
func wireCanonical(v any, c schema.Constraint) any {
	canonical, err := value.Canonical(v, c)
	if err != nil {
		return wireUnconstrained(v)
	}
	return canonical
}

// wireUnconstrained marks every float in a value no Float position describes,
// at any depth of its lists and string-keyed maps, so the document states the Go
// type the snapshot holds: a whole float under an Integer is written 5.0 and read
// back as the float it is, which the Integer check refuses, rather than as the
// integer 5.
func wireUnconstrained(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, e := range t {
			t[k] = wireUnconstrained(e)
		}
		return t
	case []any:
		for i, e := range t {
			t[i] = wireUnconstrained(e)
		}
		return t
	case float64:
		return wireFloat(t)
	}
	return v
}

// twoPow63 is the rounding ceiling of int64 in float64: a conversion landing on
// it has rounded past the integer type's range, and converting it back is
// undefined.
const twoPow63 = 1 << 63

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

// wireNumeric wraps a float, and an integer a float64 holds exactly, as a wire
// float so it emits with a float indicator. A json.Number is read as the reader
// reads it ([wireJSONNumber]), and any other shape goes to [wireUnconstrained]:
// Load re-validates only when the caller asks.
func wireNumeric(v any) any {
	switch n := v.(type) {
	case float64:
		return wireFloat(n)
	case int64:
		if f, ok := exactWireInt(n); ok {
			return wireFloat(f)
		}
		return v
	case json.Number:
		return wireJSONNumber(n)
	}
	return wireUnconstrained(v)
}

// wireJSONNumber classifies n with [immutable.NormalizeNumber], the rule the
// reader applies, so one literal cannot mean a float out and an int back in.
// A literal neither parser accepts passes through unwrapped.
func wireJSONNumber(n json.Number) any {
	switch norm := immutable.NormalizeNumber(n).(type) {
	case float64:
		return wireFloat(norm)
	case int64:
		// The integer literal -0 is the integer 0, but a Float stores the
		// negative zero it spells.
		if norm == 0 && n[0] == '-' {
			return wireFloat(math.Copysign(0, -1))
		}
		if f, ok := exactWireInt(norm); ok {
			return wireFloat(f)
		}
	}
	return n
}
