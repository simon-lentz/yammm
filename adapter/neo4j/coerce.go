package neo4j

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// coerceScalar returns raw, a value in the form the validator stores, as the
// driver's value at constraint. It judges the form alone: a value of another
// kind's form is refused, and bounds, members and patterns are the validator's.
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
		format := ""
		if tc, ok := resolved.(schema.TimestampConstraint); ok {
			format = tc.Format()
		}
		v, ok := raw.(string)
		if !ok {
			return raw, fmt.Errorf("coerce %s: cannot use %T (want string)", kind, raw)
		}
		if format != "" {
			if t, err := time.Parse(format, v); err == nil {
				return driverZone(t), nil
			}
			return raw, fmt.Errorf("coerce %s: cannot parse %s against format %q", kind, diag.QuoteValue(v), format)
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return driverZone(t), nil
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
		id, err := uuid.Parse(s)
		if err != nil {
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
// must satisfy. Nested params are addressed with "outer.inner" dot-notation
// (e.g. ParamTypes{"rows.unit_price": schema.NewFloatConstraint()} tags
// unit_price inside each row of a $rows []map[string]any). A key no param holds
// coerces nothing, and [CoerceParams] still judges its constraint.
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
// none, a param, a field of a nested param map or a row, is returned as nil,
// since the driver packs a nil slice as an empty list. Any other value under a
// key absent from types passes through as given, save a nested map or rows,
// which are walked. params is never mutated, and for a non-empty params the returned top-level map is a
// new one, so mutating its top-level keys cannot affect the input
// (values that pass through uncoerced may still share backing storage). When no constraint in types is refused, an empty params
// map is returned as-is — there is nothing to copy. Walks one level of nested
// map[string]any and of rows using the "outer.inner" convention: a
// []map[string]any, or a []any whose elements are each a map[string]any or an
// untyped nil, at least one a map. Rows are returned as a []any, so a nil row
// reaches the driver as null. A field or a row's field is typed at
// "outer.inner", and a non-nil constraint at a nested map's or a rows param's
// own key is an error.
//
// Keys are processed in sorted order, and the rows of a nested []map[string]any
// in order, so when several values fail coercion the returned error names the
// same offending key on every run — failures are reproducible rather than dependent on map iteration order.
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

// coerceParam coerces a single top-level (key, value) pair. A value that holds
// none is nil, and nested maps and rows ([isRows]) are walked one level via the
// "outer.inner" convention, or refused under a non-nil constraint at their own
// key; any other value is coerced against its declared constraint via
// [coerceValue], or returned as given under an undeclared key.
func coerceParam(key string, val any, types ParamTypes) (any, error) {
	if value.Absent(val) {
		//nolint:nilnil // a val that holds none coerces to nil with no error: it is a valid absent val, not a failure
		return nil, nil
	}
	switch v := val.(type) {
	case map[string]any:
		// A nested map's fields are typed at "key.field", so a constraint at key
		// would judge nothing: it is refused, as at a rows param's key.
		if types[key] != nil {
			return nil, fmt.Errorf("param %s: a nested map takes no constraint at its own key; declare each field as \"<key>.<field>\"", diag.QuoteValue(key))
		}
		return coerceNested(key, v, types)
	case []map[string]any:
		return coerceRows(key, v, types)
	case []any:
		// The rows CoerceParams returns are a []any, so a second pass reads them as
		// rows too. Rows that are empty or all nil hold no map, and read as a list.
		if isRows(v) {
			return coerceRows(key, v, types)
		}
	}
	if constraint, ok := types[key]; ok {
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
	return val, nil
}

// coerceRows walks each row of a rows param. The driver packs a nil map in a
// []map[string]any as an empty one, so the rows return as a []any, where a row
// that holds none is nil.
func coerceRows[E any](key string, rows []E, types ParamTypes) ([]any, error) {
	// A second pass reads rows that hold no map as a list, which a constraint at
	// key would judge, so a constraint there is refused on every pass.
	if types[key] != nil {
		return nil, fmt.Errorf("param %s: rows take no constraint at their own key; declare each row field as \"<key>.<field>\"", diag.QuoteValue(key))
	}
	out := make([]any, len(rows))
	for i, row := range rows {
		m, ok := any(row).(map[string]any)
		if !ok || m == nil {
			continue
		}
		cr, err := coerceNested(key, m, types)
		if err != nil {
			return nil, err
		}
		out[i] = cr
	}
	return out, nil
}

// isRows reports whether v holds rows in the shape [coerceRows] returns them:
// each element a map[string]any or an untyped nil, at least one of them a map.
// Any other []any is a list, nil elements alone included.
func isRows(v []any) bool {
	rows := false
	for _, e := range v {
		switch e.(type) {
		case map[string]any:
			rows = true
		case nil:
		default:
			return false
		}
	}
	return rows
}

// coerceNested walks one level of a nested param map, coercing each field whose
// "outer.inner" key is declared in types. A field that holds no value is nil,
// and any other field absent from types passes through unchanged.
func coerceNested(outer string, m map[string]any, types ParamTypes) (map[string]any, error) {
	out := make(map[string]any, len(m))
	for _, k := range slices.Sorted(maps.Keys(m)) {
		v := m[k]
		dotKey := outer + "." + k
		if value.Absent(v) {
			out[k] = nil
			continue
		}
		if constraint, ok := types[dotKey]; ok {
			held, err := directForm(v, constraint)
			if err != nil {
				return nil, fmt.Errorf("param %s: %w", diag.QuoteValue(dotKey), markUnrepresentable(err))
			}
			cv, err := coerceValue(constraint, held)
			if err != nil {
				return nil, fmt.Errorf("param %s: %w", diag.QuoteValue(dotKey), err)
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
// name belongs to: in a flat row `key_id` is a property so named, while in the
// batched node-merge row shape `key_id` is the merge key for `id` and the
// properties live nested under row.props. A caller whose own shape carries
// both is responsible for describing the merge keys itself and merging the two
// maps.
func ParamTypesForType(t schema.Type, prefix string) ParamTypes {
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
		pt[paramKey(prefix, p.Name())] = p.Constraint()
	}
	return pt
}

// driverOffsetZoneName is the zone abbreviation the Neo4j driver treats as
// "this instant carries an offset, not a zone identifier". It is the driver's
// own sentinel, not this package's invention: the driver documents it on its
// OffsetTimeOf constructor, and its read path hydrates every offset-bearing
// ZONED DATETIME into a location built with exactly this name.
const driverOffsetZoneName = "Offset"

// THE TEMPORAL RULE, stated here in full; the package doc and driverZone's godoc
// restate its arms.
//
// A temporal value is sent in the form the driver and the server will both read
// back as the instant, zone and date yammm holds. One rule, two arms, and
// everything below implements it:
//
//   - An instant is read from its stored text. It keeps the zone name the text
//     carries only when the host's tz database holds the name AND agrees with
//     the offset the value carries. Otherwise the offset is what yammm holds,
//     and the value goes as a fixed offset.
//   - A date carries no time of day, because the driver derives its day number
//     by an integer division that a time of day can push across a boundary.
//
// resolvedZones memoizes time.LoadLocation by name: the location when the
// host's tz database holds it, nil when it does not. It caches a host fact
// fixed for the process lifetime, not adapter state — LoadLocation opens a
// zoneinfo file on every call, and a batch write hands every row's instants
// through [coerceScalar].
//
//nolint:gochecknoglobals // a process-wide memo of a fact that cannot change at runtime
var resolvedZones sync.Map // name -> *time.Location (nil when unresolvable)

// zoneNamed returns the host tz database's location for name, or nil.
//
// One source reaches this: an abbreviation that a Timestamp layout with a
// zone-abbreviation token read out of stored text. The memo is therefore bounded
// by the distinct abbreviations the data and the parameters hold. Each entry is
// a short string and a pointer, so the growth is small.
func zoneNamed(name string) *time.Location {
	if v, ok := resolvedZones.Load(name); ok {
		loc, isLocation := v.(*time.Location)
		if !isLocation {
			return nil
		}
		return loc
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		loc = nil
	}
	resolvedZones.Store(name, loc)
	return loc
}

// calendarDate strips the time of day, keeping the wall-clock date the value
// itself shows, so every Date arm hands the driver one form.
func calendarDate(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// driverZone re-renders t, an instant time.Parse read from stored text, into a
// location the driver can send.
//
// The driver sends Location().String() as a time-zone identifier the server
// must resolve when the host's tz database holds that name at the offset t
// carries, and sends the offset otherwise. driverZone applies that rule first,
// so a text with a numeric offset and no abbreviation coerces to one value on
// every host. Two rules, in order: time.Local, which time.Parse gives a text
// whose offset or abbreviation the host's zone uses, is always re-expressed in
// its offset; any other location is kept only when the host's tz database
// resolves its name, and is otherwise re-expressed in its offset. An unnamed
// location (what time.Parse produces for a numeric offset) resolves under
// LoadLocation too, so it is refused before the check.
//
// The instant is unchanged — only the location it is expressed in moves — and
// the offset form is the same shape the driver produces when reading such a
// value back, so a write and a subsequent read agree.
func driverZone(t time.Time) time.Time {
	loc := t.Location()
	if loc == time.Local {
		return offsetForm(t)
	}
	if loc == time.UTC {
		return t
	}
	name := loc.String()
	if name == "" {
		return offsetForm(t)
	}
	// A resolvable NAME is not enough. The driver sends a resolvable name as a
	// zone identifier and the server re-derives the offset from it, so a value
	// carrying "MST" at +00:00 — which time.Parse mints from any layout with a
	// zone-abbreviation token — would come back shifted by seven hours. Where
	// the name and the carried offset disagree, the offset is what yammm holds.
	named := zoneNamed(name)
	if named == nil {
		return offsetForm(t)
	}
	_, carried := t.Zone()
	_, resolved := t.In(named).Zone()
	if carried != resolved {
		return offsetForm(t)
	}
	return t
}

// offsetForm expresses t in the driver's offset sentinel location.
func offsetForm(t time.Time) time.Time {
	_, offset := t.Zone()
	return t.In(time.FixedZone(driverOffsetZoneName, offset))
}
