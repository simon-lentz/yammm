package neo4j

import (
	"fmt"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/schema"
)

// Coerce converts a single raw value to the Neo4j-driver-native type the given
// schema kind requires, so the value satisfies Neo4j TYPE constraints:
//
//   - KindFloat: int/int32/int64 -> float64. Repairs the JSON round-trip where
//     a whole-number Float decodes as int64 and Neo4j rejects it against an
//     IS :: FLOAT constraint.
//   - KindTimestamp: an RFC3339 / RFC3339Nano string -> time.Time (Neo4j ZONED
//     DATETIME). A non-empty string that parses as neither is a coercion
//     failure and returns an error.
//   - KindDate: a "2006-01-02" string OR a time.Time -> dbtype.Date (Neo4j DATE).
//     A non-empty string that does not parse is a coercion failure and returns
//     an error. Mapping a time.Time to dbtype.Date (rather than passing it
//     through) keeps a Date-constrained value from reaching Neo4j as ZONED
//     DATETIME and unifies this scalar path with the list path ([coerceSlice])
//     on one rule.
//   - Every other kind is already driver-native and passes through unchanged.
//
// A nil value always passes through. An unhandled kind returns an error so a
// schema kind added after this switch was written surfaces in tests/CI rather
// than as a silent driver-side PROPERTY_TYPE rejection in production; the
// //exhaustive:enforce directive turns that omission into a build failure.
func Coerce(kind schema.ConstraintKind, raw any) (any, error) {
	if raw == nil {
		//nolint:nilnil // a nil value coerces to nil with no error: nil is a valid absent property, not a failure
		return nil, nil
	}
	//exhaustive:enforce
	switch kind {
	case schema.KindFloat:
		switch v := raw.(type) {
		case int64:
			return float64(v), nil
		case int:
			return float64(v), nil
		case int32:
			return float64(v), nil
		default:
			return raw, nil
		}
	case schema.KindTimestamp:
		s, ok := raw.(string)
		if !ok {
			return raw, nil // already time.Time or otherwise driver-native
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, nil
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t, nil
		}
		return raw, fmt.Errorf("coerce %s: cannot parse %q as an RFC3339 timestamp", kind, s)
	case schema.KindDate:
		if t, ok := raw.(time.Time); ok {
			return dbtype.Date(t), nil
		}
		s, ok := raw.(string)
		if !ok {
			return raw, nil
		}
		if t, err := time.Parse(time.DateOnly, s); err == nil {
			return dbtype.Date(t), nil
		}
		return raw, fmt.Errorf("coerce %s: cannot parse %q as a YYYY-MM-DD date", kind, s)
	case schema.KindString, schema.KindInteger, schema.KindBoolean,
		schema.KindUUID, schema.KindEnum, schema.KindPattern,
		schema.KindVector, schema.KindList, schema.KindAlias:
		// Driver-native scalars; collection kinds are coerced elementwise by
		// the property paths ([coerceSlice]), not at this scalar boundary.
		return raw, nil
	default:
		return raw, fmt.Errorf("coerce: unhandled schema.ConstraintKind %v", kind)
	}
}

// ParamTypes maps a Cypher parameter name to the schema kind its value must
// inhabit. Nested params are addressed with "outer.inner" dot-notation
// (e.g. ParamTypes{"rows.principal_amount": schema.KindFloat} tags
// principal_amount inside each row of a $rows []map[string]any). Unknown keys
// are a no-op.
type ParamTypes map[string]schema.ConstraintKind

// CoerceParams returns a copy of params with each value coerced via [Coerce]
// against the kind declared for its key in types. Keys absent from types pass
// through. Returns params unchanged when types or params is empty so zero-cost
// calls stay free. Walks one level of nested map[string]any and
// []map[string]any using the "outer.inner" convention. Returns the first
// coercion error encountered, naming the offending key.
//
// Call this at any direct-Cypher parameter boundary that writes schema-typed
// Timestamp / Date / Float properties but does not pass through the snapshot
// write path's coercion (e.g. an enrichment MERGE built by hand).
func CoerceParams(params map[string]any, types ParamTypes) (map[string]any, error) {
	if len(types) == 0 || len(params) == 0 {
		return params, nil
	}
	out := make(map[string]any, len(params))
	for k, v := range params {
		cv, err := coerceParam(k, v, types)
		if err != nil {
			return nil, err
		}
		out[k] = cv
	}
	return out, nil
}

// coerceParam coerces a single top-level (key, value) pair. Nested maps and
// row slices are walked one level via the "outer.inner" convention; a plain
// []any list passes through (its elements are coerced on the snapshot write
// path, not here).
func coerceParam(key string, value any, types ParamTypes) (any, error) {
	switch v := value.(type) {
	case nil:
		//nolint:nilnil // a nil param value coerces to nil with no error: nil is a valid absent value, not a failure
		return nil, nil
	case map[string]any:
		return coerceNested(key, v, types)
	case []map[string]any:
		out := make([]map[string]any, len(v))
		for i, row := range v {
			cr, err := coerceNested(key, row, types)
			if err != nil {
				return nil, err
			}
			out[i] = cr
		}
		return out, nil
	case []any:
		return v, nil
	default:
		if kind, ok := types[key]; ok {
			cv, err := Coerce(kind, value)
			if err != nil {
				return nil, fmt.Errorf("param %q: %w", key, err)
			}
			return cv, nil
		}
		return value, nil
	}
}

// coerceNested walks one level of a nested param map, coercing each field whose
// "outer.inner" key is declared in types. Fields absent from types, and nil
// values, pass through unchanged.
func coerceNested(outer string, m map[string]any, types ParamTypes) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for k, v := range m {
		dotKey := outer + "." + k
		if kind, ok := types[dotKey]; ok && v != nil {
			cv, err := Coerce(kind, v)
			if err != nil {
				return nil, fmt.Errorf("param %q: %w", dotKey, err)
			}
			out[k] = cv
			continue
		}
		out[k] = v
	}
	return out, nil
}

// ParamTypesForType derives a ParamTypes from a schema type's properties.
// For top-level params pass prefix ""; keys are bare property names. For a
// nested param map pass the outer param name (e.g. "rows" / "updates"); each
// key is joined to its property name with the same "outer.inner" dot-notation
// CoerceParams uses, so ParamTypesForType(t, "rows") yields keys like
// "rows.principal_amount" that coerceNested looks up. Lets callers avoid
// hand-listing each property's kind.
func ParamTypesForType(t *schema.Type, prefix string) ParamTypes {
	pt := make(ParamTypes)
	for p := range t.Properties() {
		key := p.Name()
		if prefix != "" {
			key = prefix + "." + p.Name()
		}
		pt[key] = schema.ResolveAlias(p.Constraint()).Kind()
	}
	return pt
}
