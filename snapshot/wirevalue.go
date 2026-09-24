package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
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
// then appends ".0" when the output carries no float indicator.
//
// Delegating to json.Marshal instead would be shorter but costs 2.18× the
// time and eight more allocations per call, on the path every float in a
// document takes. [TestWireFloat_MatchesEncodingJSON] holds the two in
// lockstep so this copy cannot drift from the encoder it mirrors.
func (f wireFloat) MarshalJSON() ([]byte, error) {
	return appendWireFloat(float64(f), 64)
}

// wireFloat32 is [wireFloat] for a value the caller stored in 32 bits.
// Emitting it at bitSize 64 spends 17 digits describing 32 bits of value.
type wireFloat32 float32

// MarshalJSON is [wireFloat.MarshalJSON] at bitSize 32.
// [TestWireFloat32_MatchesEncodingJSON] holds it in lockstep.
func (f wireFloat32) MarshalJSON() ([]byte, error) {
	return appendWireFloat(float64(f), 32)
}

// appendWireFloat formats v as encoding/json's float encoder would at bitSize,
// then appends ".0" when the result carries no float indicator. bitSize sets
// both the shortest-form width and the exponent cutoff, compared in float32
// for a 32-bit value so the boundary lands where encoding/json's does.
func appendWireFloat(v float64, bitSize int) ([]byte, error) {
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return nil, fmt.Errorf("wire float: unsupported value: %s", strconv.FormatFloat(v, 'g', -1, bitSize))
	}
	abs := math.Abs(v)
	format := byte('f')
	if abs != 0 {
		if bitSize == 32 {
			if a := float32(abs); a < 1e-6 || a >= 1e21 {
				format = 'e'
			}
		} else if abs < 1e-6 || abs >= 1e21 {
			format = 'e'
		}
	}
	b := strconv.AppendFloat(nil, v, format, -1, bitSize)
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
func typeByWireID(s *schema.Schema, schemaName, name string) (*schema.Type, bool) {
	if s == nil {
		return nil, false
	}
	for _, cs := range s.Closure() {
		if cs.Name() != schemaName {
			continue
		}
		if t, ok := cs.Type(name); ok {
			return t, true
		}
	}
	return nil, false
}

// wireProps clones props and rewrites each value under its schema constraint
// so float-bearing values emit with a float indicator. A nil clone stays nil
// (the wire's "properties":null shape). An undeclared property, and every
// property under a nil type, has no constraint: its floats keep their
// indicator ([wireValue]) and everything else passes through untouched.
func wireProps(props immutable.Properties, t *schema.Type) map[string]any {
	m := props.Clone()
	if len(m) == 0 {
		return m
	}
	for name, v := range m {
		var c schema.Constraint
		if t != nil {
			if prop, ok := t.Property(name); ok {
				c = prop.Constraint()
			}
		}
		m[name] = wireValue(v, c)
	}
	return m
}

// wireEdgeProps is wireProps for edge properties, whose constraints hang off
// the source type's relation rather than any target type. The resolved and
// unresolved paths agree because both derive rel from the source instance's
// own TypeID — the shared input, not the shared body.
func wireEdgeProps(props immutable.Properties, rel *schema.Relation) map[string]any {
	m := props.Clone()
	if len(m) == 0 {
		return m
	}
	for name, v := range m {
		var c schema.Constraint
		if rel != nil {
			if p, ok := rel.Property(name); ok {
				c = p.Constraint()
			}
		}
		m[name] = wireValue(v, c)
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
		elems, ok := wireElems(v)
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
		elems, ok := wireElems(v)
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

// wireHeldFloat marks one float with a float indicator, so the document states
// the Go type the snapshot holds: a whole float under an Integer is written 5.0
// and read back as the float it is, which the Integer check refuses, rather
// than as the integer 5. [wireUnconstrained] walks a value to each float in its
// lists and string-keyed maps. Every other value passes through.
func wireHeldFloat(v any) any {
	switch rv := reflect.ValueOf(v); rv.Kind() {
	case reflect.Float32:
		return wireFloat32(float32(rv.Float()))
	case reflect.Float64:
		return wireFloat(rv.Float())
	}
	return v
}

// wireCanonical renders a temporal or UUID value in the one form the kind
// stores, catching a value that reached the graph without validation. A shape
// the constraint cannot render passes through, its floats keeping their
// indicator — Load re-validates only when the caller asks, and a document
// written before this rule existed must stay writable.
func wireCanonical(v any, c schema.Constraint) any {
	canonical, err := value.Canonical(v, c)
	if err != nil {
		return wireUnconstrained(v)
	}
	return canonical
}

// wireUnconstrained marks every float in a value no Float position describes,
// at any depth of its lists and string-keyed maps, as [wireHeldFloat] marks
// one. It does not walk a Go array, which is how a scalar carrier such as
// uuid.UUID is spelled, nor a map with other keys, which no reader produces.
func wireUnconstrained(v any) any {
	if t, ok := v.(map[string]any); ok {
		for k, e := range t {
			t[k] = wireUnconstrained(e)
		}
		return t
	}
	if elems, ok := wireElems(v); ok {
		for i, e := range elems {
			elems[i] = wireUnconstrained(e)
		}
		return elems
	}
	return wireHeldFloat(v)
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

// wireNumeric wraps a float, and an integer a float64 holds exactly, as a wire
// float so it emits with a float indicator; the type switch fast-paths what
// validation and the decoder produce, and reflection reaches the rest. A
// json.Number is read as the reader reads it ([wireJSONNumber]), any other
// integer passes through int-shaped, and any other shape goes to
// [wireUnconstrained] — Load re-validates only when the caller asks.
func wireNumeric(v any) any {
	switch n := v.(type) {
	case float64:
		return wireFloat(n)
	case float32:
		return wireFloat32(n)
	case int64:
		if f, ok := exactWireInt(n); ok {
			return wireFloat(f)
		}
		return v
	case json.Number:
		return wireJSONNumber(n)
	}

	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Float32:
		return wireFloat32(float32(rv.Float()))
	case reflect.Float64:
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
		if f, ok := exactWireInt(norm); ok {
			return wireFloat(f)
		}
	}
	return n
}

// wireElems returns v's elements as []any for any slice, so a vector or list
// position is reached whatever concrete slice the caller built. An ARRAY is
// not a list — the rule [value.ListElems] states and every reader follows — so
// one at a list position takes the writer's dropped-value path rather than
// being written as a list.
func wireElems(v any) ([]any, bool) {
	if elems, ok := v.([]any); ok {
		return elems, true
	}
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	elems := make([]any, rv.Len())
	for i := range elems {
		elems[i] = rv.Index(i).Interface()
	}
	return elems, true
}
