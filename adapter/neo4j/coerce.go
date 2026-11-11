package neo4j

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// coerceScalar returns raw, a value in the form the validator stores, as the
// driver's value at constraint. It judges the form, and an instant's offset by
// the server's bound: bounds, members and patterns are the validator's.
// Every path reaches it with a constraint a loaded or judged schema holds.
func coerceScalar(constraint schema.Constraint, raw any) (any, error) {
	// The driver packs a nil slice as an empty list, so it must reach the driver as nil.
	if value.Absent(raw) {
		//nolint:nilnil // a value that holds none coerces to nil with no error: it is an absent property, not a failure
		return nil, nil
	}
	if constraint == nil {
		// No constraint to coerce against: pass through, matching coerceValue's
		// nil-constraint handling. A typed coercion needs a type.
		return raw, nil
	}
	resolved := schema.ResolveAlias(constraint)
	// A property type Neo4j cannot store is refused first, whatever the value.
	if err := nestedCollection(resolved); err != nil {
		return raw, err
	}
	return scalarForm(resolved, raw)
}

// scalarForm is [coerceScalar] at an alias-resolved constraint that is no nested
// collection. A nil has no form there, so [coerceSlice] refuses a list element
// that holds none by it.
func scalarForm(resolved schema.Constraint, raw any) (any, error) {
	kind := resolved.Kind()
	if reflect.ValueOf(raw).Kind() == reflect.Map {
		// Neo4j stores no map as a property value, so no constraint admits one.
		return raw, fmt.Errorf("coerce %s: cannot store %T, a map, as a property value", kind, raw)
	}
	//exhaustive:enforce
	switch kind {
	case schema.KindFloat:
		if _, ok := raw.(float64); !ok {
			return raw, fmt.Errorf("coerce %s: cannot use %T (want float64)", kind, raw)
		}
		return raw, nil
	case schema.KindTimestamp:
		v, ok := raw.(string)
		if !ok {
			return raw, fmt.Errorf("coerce %s: cannot use %T (want string)", kind, raw)
		}
		if t, ok := value.ParseTimestamp(v, resolved); ok {
			_, offset := t.Zone()
			if offset < -maxOffsetSeconds || offset > maxOffsetSeconds {
				return raw, fmt.Errorf("coerce %s: cannot store %s: its UTC offset %s is past the 18 hours the server holds", kind, diag.QuoteValue(v), t.Format("-07:00:00"))
			}
			return offsetForm(t, offset), nil
		}
		if tc, ok := resolved.(schema.TimestampConstraint); ok && tc.Format() != "" {
			return raw, fmt.Errorf("coerce %s: cannot parse %s against format %q", kind, diag.QuoteValue(v), tc.Format())
		}
		return raw, fmt.Errorf("coerce %s: cannot parse %s as an RFC3339 timestamp", kind, diag.QuoteValue(v))
	case schema.KindDate:
		switch v := raw.(type) {
		case dbtype.Date:
			// The driver's own date, which [CoerceParams] passes at a Date.
			return dbtype.Date(calendarDate(time.Time(v))), nil
		case string:
			if t, err := time.Parse(time.DateOnly, v); err == nil {
				return dbtype.Date(t), nil
			}
			return raw, fmt.Errorf("coerce %s: cannot parse %s as a YYYY-MM-DD date", kind, diag.QuoteValue(v))
		default:
			return raw, fmt.Errorf("coerce %s: cannot use %T (want a YYYY-MM-DD string or dbtype.Date)", kind, raw)
		}
	case schema.KindInteger:
		if _, ok := raw.(int64); !ok {
			return raw, fmt.Errorf("coerce %s: cannot use %T (want int64)", kind, raw)
		}
		return raw, nil
	case schema.KindString, schema.KindEnum, schema.KindPattern:
		if _, ok := raw.(string); !ok {
			return raw, fmt.Errorf("coerce %s: cannot use %T (want string)", kind, raw)
		}
		return raw, nil
	case schema.KindUUID:
		s, ok := raw.(string)
		if !ok {
			return raw, fmt.Errorf("coerce %s: cannot use %T (want string)", kind, raw)
		}
		id, ok := value.ParseUUID(s)
		if !ok {
			return raw, fmt.Errorf("coerce %s: cannot parse %s as a UUID", kind, diag.QuoteValue(s))
		}
		// The canonical text goes as it is held, so a stored UUID costs no copy.
		if len(s) == 36 && strings.ToLower(s) == s {
			return raw, nil
		}
		return id.String(), nil
	case schema.KindBoolean:
		if _, ok := raw.(bool); !ok {
			return raw, fmt.Errorf("coerce %s: cannot use %T (want bool)", kind, raw)
		}
		return raw, nil
	case schema.KindVector:
		if _, ok := raw.([]float64); !ok {
			return raw, fmt.Errorf("coerce %s: cannot use %T (want a list of floats)", kind, raw)
		}
		return raw, nil
	case schema.KindList:
		return raw, fmt.Errorf("coerce %s: cannot use %T (want a list)", kind, raw)
	case schema.KindAlias:
		return raw, nil
	default:
		return raw, fmt.Errorf("coerce: unhandled schema.ConstraintKind %v", kind)
	}
}

// directForm returns the value the validator stores for raw at c, the form the
// write path reads, or the validator's refusal. c is one [CoerceParams] judged.
// A dbtype.Date at a Date is the driver's own date and passes. It recovers no
// panic.
func directForm(raw any, c schema.Constraint) (any, error) {
	if c == nil {
		return raw, nil
	}
	// A property type Neo4j cannot store is refused before its value is read.
	if err := nestedCollection(c); err != nil {
		return nil, err
	}
	if _, ok := raw.(dbtype.Date); ok && schema.ResolveAlias(c).Kind() == schema.KindDate {
		return raw, nil
	}
	if err := value.Check(raw, c); err != nil {
		return nil, err //nolint:wrapcheck // the validator's message is the refusal, and the caller names the param
	}
	//nolint:wrapcheck // as above
	return value.Coerce(raw, c)
}

// coerceValue returns raw, a value in the form the validator stores, as the
// driver's value at constraint: the one rule the write path ([propsToParamMap],
// [coerceRelProps], [coerceKey]) and [CoerceParams] share. A []any goes to
// [coerceSlice], and is an error under a scalar constraint; every other value
// goes to [coerceScalar]. A nil constraint passes the value through unchanged.
//
// A nil []any reaches it as nil, because [heldForm] and [coerceParam] each answer
// one first. The scalar rule returns any other value that holds none as nil.
func coerceValue(constraint schema.Constraint, raw any) (any, error) {
	if constraint == nil {
		return raw, nil
	}
	var cv any
	var err error
	if slice, ok := raw.([]any); ok {
		cv, err = coerceSlice(slice, constraint)
	} else {
		cv, err = coerceScalar(constraint, raw)
	}
	return cv, markUnrepresentable(err)
}

// ParamTypes maps a Cypher parameter name to the schema constraint its value
// must satisfy. A key no param holds coerces nothing, and [CoerceParams] still
// judges its constraint.
//
// The value is the full [schema.Constraint], not just its
// [schema.ConstraintKind]: element coercion of List properties needs the
// element type, which a bare kind discards. Derive one from a schema type with
// [ParamTypesForType], or hand-list constraints via the schema constructors.
type ParamTypes map[string]schema.Constraint

// CoerceParams returns a new map with each value read at the constraint
// declared for its key in types, as [github.com/simon-lentz/yammm/instance.Validator]
// reads a property value, and returned as the write path sends the value the
// validator stores. A value the validator refuses is an error, and a
// dbtype.Date at a Date passes as the driver's own date. A value that holds
// none is returned as nil at every key, since the driver packs a nil slice as
// an empty list. Any other value under a key absent from types passes through
// as given.
//
// params is one flat map: a key of types names a key of params, and no value
// is walked. A map or a list of maps at a key that holds a constraint is judged
// as the validator judges that value, so a map is refused; to coerce the fields
// of a nested map or of a row, call CoerceParams on that map. params is never
// mutated, and for a non-empty params the returned map is a new one, so mutating
// its keys cannot affect the input (values that pass through uncoerced may still
// share backing storage). When no constraint in types is refused, an empty
// params map is returned as-is — there is nothing to copy.
//
// Keys are processed in sorted order, so when several values fail coercion the
// returned error names the same offending key on every run — failures are
// reproducible rather than dependent on map iteration order.
//
// The non-nil constraints in types are judged by [schema.CheckConstraint] before
// any value is coerced, so a hand-built constraint it refuses is an error naming
// its key, whether or not params holds that
// key. Of several refused keys the error names the first in sorted order. The
// map returned with that error is nil, for an empty or a nil params too.
//
// The package doc states the rule under Parameter Coercion. A value that holds
// one at a List whose element is a collection is refused, an empty list
// included, and a list reaches the driver as a typed slice (a List<Float> of
// int64s as []float64, a List<Date> of strings as []dbtype.Date). Every refusal
// of a value matches [ErrUnrepresentable].
//
// Call this at any direct-Cypher parameter boundary that writes schema-typed
// properties but does not pass through the adapter write path's coercion
// (e.g. an enrichment MERGE built by hand).
func CoerceParams(params map[string]any, types ParamTypes) (map[string]any, error) {
	if key, err := firstRefusedType(types); err != nil {
		return nil, fmt.Errorf("param type %s: %w", diag.QuoteValue(key), err)
	}
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

// firstRefusedType returns the lexicographically first key of types whose
// non-nil constraint [schema.CheckConstraint] refuses, with that error. It
// judges keys in map order and skips one that sorts after a key already refused.
func firstRefusedType(types ParamTypes) (string, error) {
	var (
		first    string
		firstErr error
	)
	for k, c := range types {
		if c == nil || (firstErr != nil && k >= first) {
			continue
		}
		if err := schema.CheckConstraint(c); err != nil {
			first, firstErr = k, err
		}
	}
	return first, firstErr
}

// coerceParam coerces one (key, value) pair. A value that holds none is nil at
// every key. Any other value is coerced against the key's constraint via
// [coerceValue], and returned as given where types holds none for the key.
func coerceParam(key string, val any, types ParamTypes) (any, error) {
	if value.Absent(val) {
		//nolint:nilnil // a val that holds none coerces to nil with no error: it is a valid absent val, not a failure
		return nil, nil
	}
	constraint := types[key]
	held, err := directForm(val, constraint)
	if err != nil {
		return nil, fmt.Errorf("param %s: %w", diag.QuoteValue(key), markUnrepresentable(err))
	}
	cv, err := coerceValue(constraint, held)
	if err != nil {
		return nil, fmt.Errorf("param %s: %w", diag.QuoteValue(key), err)
	}
	return cv, nil
}

// ParamTypesForType derives a ParamTypes from a schema type's properties, own
// and inherited: each key is a property name, as a flat param map of the
// type's properties spells it. Lets callers avoid hand-listing each property's
// constraint.
//
// A primary key is a property, so its constraint is in the map under the
// property's name and under no other. A row that also carries the key under a
// name of the caller's choosing, as the batched node-merge row carries `key_id`
// for `id`, needs an entry the caller adds under that name.
func ParamTypesForType(t schema.Type) ParamTypes {
	if t.IsZero() {
		// Zero-guarded like [ImmutableKeysFor], its sibling: both take a schema
		// type a caller may not hold, and one panicking where the other returns
		// an empty answer is a difference no caller can predict.
		return ParamTypes{}
	}
	pt := make(ParamTypes)
	// AllProperties, not Properties: an inherited property is as present in a
	// param map as an own one.
	for p := range t.AllProperties() {
		pt[p.Name()] = p.Constraint()
	}
	return pt
}

// driverOffsetZoneName names the location of an instant the driver sends as an
// offset. The driver sends a time.Time under its location's name when that name
// loads as a zone whose offset agrees, and as an offset otherwise; no zone has
// this name. It is the driver's own name for the form: OffsetTimeOf builds a
// location of this name, and so does the read path for every ZONED DATETIME
// that holds an offset.
const driverOffsetZoneName = "Offset"

// calendarDate strips the time of day, keeping the wall-clock date the value
// itself shows, so every Date arm hands the driver one form.
func calendarDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// maxOffsetSeconds is the widest UTC offset the server holds in a ZONED
// DATETIME, 18 hours either way. The server refuses a wider one with an error
// that names no property.
const maxOffsetSeconds = 18 * 60 * 60

// offsetForm expresses t, an instant read from its stored text at offset, in
// the driver's offset sentinel location, so every instant reaches the server as
// its offset and never under a zone name. The server holds one instant under a
// zone name and under that zone's offset as two unequal values, and Cypher's
// own datetime() gives the offset form.
func offsetForm(t time.Time, offset int) time.Time {
	if offset == 0 {
		return t.In(offsetZero)
	}
	return t.In(time.FixedZone(driverOffsetZoneName, offset))
}

// offsetZero is the driver's offset location at UTC, built once: most instants
// are UTC, and a location built for each costs three allocations.
var offsetZero = time.FixedZone(driverOffsetZoneName, 0)
