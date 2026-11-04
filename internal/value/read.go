package value

import (
	"encoding/json"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Read returns v with each json.Number in it read as [Classify] reads it, at any
// depth of its lists and string-keyed maps: an integer literal an int64 holds is
// that int64, a float literal a finite float64 holds is that float64, and any
// other number stays the json.Number. It rewrites each list and map in place, so
// a caller must own what it passes.
func Read(v any) any {
	switch t := v.(type) {
	case json.Number:
		if !immutable.IsNumberLiteral(t) {
			return v
		}
		_, n := classifyNumber(t)
		return n
	case map[string]any:
		for k, e := range t {
			t[k] = Read(e)
		}
		return t
	case []any:
		for i, e := range t {
			t[i] = Read(e)
		}
		return t
	}
	return v
}

// ReadAt is [Read] at a position c describes. At a Float, and at each element of
// a Vector, a number literal or an int64 is its nearest float64, as the
// validator stores it: an integer past 2^53 rounds, and -0 keeps its sign. A
// List reads each element at its element constraint. A literal no finite
// float64 holds stays the json.Number.
func ReadAt(v any, c schema.Constraint) any {
	switch rc := schema.ResolveAlias(c).(type) {
	case schema.FloatConstraint:
		return readFloat(v)
	case schema.VectorConstraint:
		if elems, ok := v.([]any); ok {
			for i, e := range elems {
				elems[i] = readFloat(e)
			}
			return elems
		}
	case schema.ListConstraint:
		if elems, ok := v.([]any); ok {
			for i, e := range elems {
				elems[i] = ReadAt(e, rc.Element())
			}
			return elems
		}
	}
	return Read(v)
}

// readFloat reads v at a Float.
func readFloat(v any) any {
	switch t := v.(type) {
	case int64:
		return float64(t)
	case json.Number:
		if immutable.IsNumberLiteral(t) {
			if f, err := t.Float64(); err == nil {
				return f
			}
		}
	}
	return Read(v)
}

// ReadsAsFloat reports whether c describes a position at which [ReadAt] reads
// a number as a float64: a Float, a Vector, or a List whose element is one.
func ReadsAsFloat(c schema.Constraint) bool {
	switch rc := schema.ResolveAlias(c).(type) {
	case schema.FloatConstraint, schema.VectorConstraint:
		return true
	case schema.ListConstraint:
		return ReadsAsFloat(rc.Element())
	}
	return false
}
