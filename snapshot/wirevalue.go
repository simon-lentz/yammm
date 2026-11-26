package snapshot

import (
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// wireFloat marks a float so it emits with a float indicator (".", "e", or "E"),
// the set [value.IsIntegerLiteral] reads. Without it, a whole float emits
// int-shaped and reads back as an int64 at every position but a Float or a
// Vector element.
type wireFloat float64

// MarshalJSON emits the value as [value.AppendWrittenFloat] writes it, the text
// the stored-value order ranks. A held float is finite: the validator refuses
// another, and a document spells none. [TestWireFloat_MatchesEncodingJSON]
// holds the text to encoding/json's float encoder.
func (f wireFloat) MarshalJSON() ([]byte, error) {
	return value.AppendWrittenFloat(nil, float64(f)), nil
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

// wireProps clones props and rewrites each value under its schema constraint
// ([wireValue]). A nil clone stays nil (the wire's "properties":null shape).
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

// wireValue rewrites one cloned value under its resolved constraint kind.
// Timestamp, Date and UUID render to their canonical text, and a List rewrites
// each element under its element constraint. Anywhere else, a value that is not
// the kind's shape included, every float in it keeps its indicator at any depth
// of its lists and string-keyed maps ([wireUnconstrained]), and every other value
// passes through untouched: an integer or a json.Number at a Float is written as
// held, and the reader reads every number literal there as its nearest float64.
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
	case schema.KindString, schema.KindInteger, schema.KindFloat, schema.KindBoolean,
		schema.KindEnum, schema.KindPattern, schema.KindVector, schema.KindAlias:
		return wireUnconstrained(v)
	}
	return wireUnconstrained(v)
}

// wireCanonical renders a temporal or UUID value in the one form the kind
// stores. A shape the constraint cannot render, which only a document can hold,
// passes through, its floats keeping their indicator: Unmarshal re-validates
// only when the caller asks, and what it reads stays writable.
func wireCanonical(v any, c schema.Constraint) any {
	canonical, err := value.Canonical(v, c)
	if err != nil {
		return wireUnconstrained(v)
	}
	return canonical
}

// wireUnconstrained marks every float in v, at any depth of its lists and
// string-keyed maps, so the document states the Go type the snapshot holds: a whole float under an Integer is written 5.0 and read
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
