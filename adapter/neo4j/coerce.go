package neo4j

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/schema"
)

// Coerce converts a single raw value to the Neo4j-driver-native type the given
// schema constraint requires, so the value satisfies Neo4j TYPE constraints:
//
//   - Float: any Go signed/unsigned integer width or float32 -> float64; a
//     float64 passes through. Repairs the JSON round-trip where a whole-number
//     Float decodes as int64 and Neo4j rejects it against an IS :: FLOAT
//     constraint. A non-numeric value is a coercion failure and returns an error.
//   - Timestamp: a string -> time.Time (Neo4j ZONED DATETIME), parsed against the
//     constraint's custom Go layout when it declares one (Timestamp["…"]) and
//     against RFC3339 / RFC3339Nano otherwise; a time.Time passes through. A
//     string that does not parse, or any non-string non-time.Time value, is a
//     coercion failure and returns an error.
//   - Date: a "2006-01-02" string OR a time.Time -> dbtype.Date (Neo4j DATE); a
//     dbtype.Date passes through. A string that does not parse, or any other
//     non-temporal value, is a coercion failure and returns an error. Mapping a
//     time.Time to dbtype.Date keeps a Date-constrained value from reaching Neo4j
//     as ZONED DATETIME and unifies this scalar path with the list path
//     ([coerceSlice]) on one rule.
//   - Every other (non-transforming) kind is already driver-native and passes
//     through unchanged.
//
// The three transforming kinds above are strict: they error on a value they can
// neither pass through as already-native nor repair, rather than handing a
// wrong-typed value to the driver. This matches the list path ([coerceSlice]),
// which must build a homogeneous typed slice and errors on a wrong-typed element.
// The non-transforming kinds stay lenient — a correct value of those kinds is
// already driver-native, so there is nothing to repair or reject, and instance
// validation is their type authority. On the validated node path a transforming
// kind only ever receives the types its strict set accepts (Float: float64, or
// int64 after a snapshot round-trip; Timestamp: string or time.Time; Date:
// string), so strictness can only newly fire on a hand-built direct-Cypher param.
//
// Coerce takes the full [schema.Constraint] (not just its kind) so a Timestamp's
// custom layout is honored; the constraint's alias chain is resolved internally.
// It operates on scalar values — a List/collection value is element-coerced by
// [CoerceParams] or the adapter write path, which route each element's constraint
// back through Coerce. Calling Coerce with a collection constraint returns the
// value unchanged.
//
// A nil value, or a nil constraint ("no type to coerce against"), passes through
// unchanged. The default arm is unreachable in practice — schema.Constraint is
// sealed, so every value carries one of the kinds above — but a kind added to the
// enum after this switch was written surfaces as a build failure via the
// //exhaustive:enforce directive, rather than as a silent driver-side
// PROPERTY_TYPE rejection in production.
func Coerce(constraint schema.Constraint, raw any) (any, error) {
	if raw == nil {
		//nolint:nilnil // a nil value coerces to nil with no error: nil is a valid absent property, not a failure
		return nil, nil
	}
	if constraint == nil {
		// No constraint to coerce against: pass through, matching coerceValue's
		// nil-constraint handling. A typed coercion needs a type.
		return raw, nil
	}
	resolved := schema.ResolveAlias(constraint)
	kind := resolved.Kind()
	//exhaustive:enforce
	switch kind {
	case schema.KindFloat:
		switch v := raw.(type) {
		case int:
			return float64(v), nil
		case int8:
			return float64(v), nil
		case int16:
			return float64(v), nil
		case int32:
			return float64(v), nil
		case int64:
			return float64(v), nil
		case uint:
			return float64(v), nil
		case uint8:
			return float64(v), nil
		case uint16:
			return float64(v), nil
		case uint32:
			return float64(v), nil
		case uint64:
			return float64(v), nil
		case float32:
			return float64(v), nil
		case float64:
			return v, nil
		default:
			return raw, fmt.Errorf("coerce %s: cannot convert %T to float64", kind, raw)
		}
	case schema.KindTimestamp:
		format := ""
		if tc, ok := resolved.(schema.TimestampConstraint); ok {
			format = tc.Format()
		}
		switch v := raw.(type) {
		case time.Time:
			return v, nil // already Neo4j ZONED DATETIME native
		case string:
			if format != "" {
				// A Timestamp["layout"] constraint: the declared Go layout is
				// exclusive, matching schema validation (checkTimestamp).
				if t, err := time.Parse(format, v); err == nil {
					return t, nil
				}
				return raw, fmt.Errorf("coerce %s: cannot parse %q against format %q", kind, v, format)
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t, nil
			}
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t, nil
			}
			return raw, fmt.Errorf("coerce %s: cannot parse %q as an RFC3339 timestamp", kind, v)
		default:
			return raw, fmt.Errorf("coerce %s: cannot convert %T to a timestamp (want a string or time.Time)", kind, raw)
		}
	case schema.KindDate:
		switch v := raw.(type) {
		case dbtype.Date:
			return v, nil // already Neo4j DATE native
		case time.Time:
			return dbtype.Date(v), nil
		case string:
			if t, err := time.Parse(time.DateOnly, v); err == nil {
				return dbtype.Date(t), nil
			}
			return raw, fmt.Errorf("coerce %s: cannot parse %q as a YYYY-MM-DD date", kind, v)
		default:
			return raw, fmt.Errorf("coerce %s: cannot convert %T to a date (want a YYYY-MM-DD string, time.Time, or dbtype.Date)", kind, raw)
		}
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

// coerceValue coerces a single value against a schema constraint — the one
// per-value rule shared by every coercion path. A []any is element-coerced via
// [coerceSlice] against the constraint's List/Vector element type (and is an
// error under a scalar constraint — a scalar cannot hold a list). Every other
// value routes through the scalar [Coerce] chokepoint. A nil constraint passes
// the value through unchanged.
//
// Shape note: a non-[]any value under a List or Vector constraint passes through
// [Coerce] unchanged (Coerce treats collection kinds as scalar-passthrough). This
// is deliberate — an already-typed collection value, such as an in-memory Vector
// carried as []float64, must survive untouched rather than be rejected — and it
// means the inverse shape mismatch (a genuine scalar mistakenly declared under a
// collection constraint) is not caught here. Scalar-vs-collection shape is the
// schema validator's authority; on the direct-Cypher path it is the caller's.
//
// Routing both the adapter write path ([propsToParamMap], [coerceRelProps]) and
// the direct-Cypher param path ([CoerceParams]) through this function keeps
// list-element coercion from being silently skipped on any one path.
func coerceValue(constraint schema.Constraint, raw any) (any, error) {
	if constraint == nil {
		return raw, nil
	}
	if slice, ok := raw.([]any); ok {
		return coerceSlice(slice, constraint)
	}
	return Coerce(constraint, raw)
}

// ParamTypes maps a Cypher parameter name to the schema constraint its value
// must satisfy. Nested params are addressed with "outer.inner" dot-notation
// (e.g. ParamTypes{"rows.unit_price": schema.NewFloatConstraint()} tags
// unit_price inside each row of a $rows []map[string]any). Unknown keys
// are a no-op.
//
// The value is the full [schema.Constraint], not just its
// [schema.ConstraintKind]: element coercion of List properties needs the
// element type, which a bare kind discards. Derive one from a schema type with
// [ParamTypesForType], or hand-list constraints via the schema constructors.
type ParamTypes map[string]schema.Constraint

// CoerceParams returns a new map with each value coerced against the constraint
// declared for its key in types. Keys absent from types pass through. The
// returned top-level map is always independent of params: params is never
// mutated, and mutating the result's top-level keys cannot affect the input
// (values that pass through uncoerced may still share backing storage, as on the
// adapter write path). An empty params map is returned as-is — there is nothing
// to copy. Walks one level of nested map[string]any and []map[string]any using
// the "outer.inner" convention.
//
// Keys are processed in sorted order, so when several values fail coercion the
// returned error names the same (lexicographically first) offending key on every
// run — failures are reproducible rather than dependent on map iteration order.
//
// Scalar values route through [Coerce]; []any list values are element-coerced
// against the List element type (a List<Float> of int64s reaches the driver as
// []float64, a List<Date> of strings as []dbtype.Date) — the same rule the
// adapter write path applies, so a direct-Cypher boundary cannot silently skip
// list coercion.
//
// Call this at any direct-Cypher parameter boundary that writes schema-typed
// properties but does not pass through the adapter write path's coercion
// (e.g. an enrichment MERGE built by hand).
func CoerceParams(params map[string]any, types ParamTypes) (map[string]any, error) {
	if len(params) == 0 {
		return params, nil
	}
	out := make(map[string]any, len(params))
	for _, k := range slices.Sorted(maps.Keys(params)) {
		cv, err := coerceParam(k, params[k], types)
		if err != nil {
			return nil, err
		}
		out[k] = cv
	}
	return out, nil
}

// coerceParam coerces a single top-level (key, value) pair. Nested maps and
// row slices are walked one level via the "outer.inner" convention; every other
// value — scalar or []any list — is coerced against its declared constraint via
// [coerceValue].
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
	default:
		if constraint, ok := types[key]; ok {
			cv, err := coerceValue(constraint, value)
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
	for _, k := range slices.Sorted(maps.Keys(m)) {
		v := m[k]
		dotKey := outer + "." + k
		if constraint, ok := types[dotKey]; ok && v != nil {
			cv, err := coerceValue(constraint, v)
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

// paramKey joins a param prefix to a name using the "outer.inner" dot-notation
// [CoerceParams] walks. An empty prefix addresses the top-level param map.
func paramKey(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// ParamTypesForType derives a ParamTypes from a schema type's properties, own
// and inherited. It describes ONE shape: a map whose keys are property names.
// For top-level params pass prefix ""; for a nested param map pass the outer
// param name (e.g. "props" / "update_props" / "rows"), and each key is joined
// with the "outer.inner" dot-notation [CoerceParams] uses — so
// ParamTypesForType(t, "rows") yields keys like "rows.unit_price". Lets callers
// avoid hand-listing each property's constraint.
//
// [CoerceParams] walks one level of nesting, so a two-level parameter map needs
// one call per nested map, merged into a single ParamTypes:
//
//	pt := ParamTypesForType(t, "props")
//	maps.Copy(pt, ParamTypesForType(t, "update_props"))
//
// MERGE keys are NOT included, because they do not sit where the properties sit
// and describing both in one map means guessing which of the two a colliding
// name belongs to: in a flat row `key_id` is a property so named, while in
// [buildBatchNodeMergeQuery]'s row shape `key_id` is the merge key for `id` and
// the properties live nested under row.props. Take them from
// [ParamTypesForMergeKeys], which describes that shape, and merge the two when
// the caller's own shape is the one that carries both.
func ParamTypesForType(t *schema.Type, prefix string) ParamTypes {
	pt := make(ParamTypes)
	// AllProperties, not Properties: an inherited property is as present in a
	// param map as an own one.
	for p := range t.AllProperties() {
		pt[paramKey(prefix, p.Name())] = p.Constraint()
	}
	return pt
}
