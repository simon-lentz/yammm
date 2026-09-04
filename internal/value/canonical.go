package value

import (
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Canonical returns val in the single stored representation c defines.
//
// Three kinds accept more than one Go representation and store one: a
// Timestamp renders through its declared layout when it has one and through
// [time.RFC3339Nano] otherwise, a UUID through [uuid.UUID.String], and a Date
// through [time.DateOnly] in the value's own location. A string form is parsed
// and re-rendered, so one value reaches one spelling whichever way a caller
// wrote it. A List canonicalizes at its element, so a List over one of those
// three kinds is rebuilt as a fresh []any. Every other kind, an unresolved
// alias and a nil value pass through untouched.
//
// The returned value is always usable: on error it is val unchanged, so a
// caller that heals what it can and passes through what it cannot may ignore
// the error and use the value directly.
//
// Rendering a Timestamp through a declared layout drops whatever that layout
// cannot express. A schema declaring such a layout draws
// [github.com/simon-lentz/yammm/diag.W_TIMESTAMP_LOSSY_FORMAT] at load.
func Canonical(val any, c schema.Constraint) (any, error) {
	if val == nil || c == nil {
		return val, nil
	}
	resolved := schema.ResolveAlias(c)
	//exhaustive:enforce
	switch resolved.Kind() {
	case schema.KindTimestamp:
		return canonicalTimestamp(val, resolved)
	case schema.KindUUID:
		return canonicalUUID(val)
	case schema.KindDate:
		return canonicalDate(val)
	case schema.KindList:
		lc, ok := resolved.(schema.ListConstraint)
		if !ok {
			return val, nil
		}
		return canonicalList(val, lc.Element())
	case schema.KindString, schema.KindInteger, schema.KindFloat,
		schema.KindBoolean, schema.KindEnum, schema.KindPattern,
		schema.KindVector, schema.KindAlias:
		return val, nil
	}
	return val, nil
}

// canonicalTimestamp renders val through the constraint's layout. The layout
// is the declared one when the constraint carries it, so a custom-format
// timestamp is stored in text that satisfies its own format.
func canonicalTimestamp(val any, c schema.Constraint) (any, error) {
	layout := timestampLayout(c)
	if t, ok := val.(time.Time); ok {
		return t.Format(layout), nil
	}
	// The string form is whatever GetString reads, as the check side accepts
	// it: a carrier the check passes is one this side renders.
	s, ok := GetString(val)
	if !ok {
		return val, fmt.Errorf("value: cannot canonicalize %T as a Timestamp (want a string or time.Time)", val)
	}
	t, err := parseTimestamp(s, c)
	if err != nil {
		return val, err
	}
	return t.Format(layout), nil
}

// timestampLayout returns the declared Go layout, or RFC 3339 with fractional
// seconds. RFC3339Nano rather than RFC3339 because RFC3339 carries no fraction:
// it would truncate a sub-second value and move that instance's primary key,
// where RFC3339Nano is the text encoding/json already produces for a time.Time.
func timestampLayout(c schema.Constraint) string {
	if tc, ok := c.(schema.TimestampConstraint); ok && tc.Format() != "" {
		return tc.Format()
	}
	return time.RFC3339Nano
}

// parseTimestamp reads s under the same rule the checker accepts it by: the
// declared layout exclusively when the constraint carries one, and RFC 3339
// with or without fractional seconds otherwise.
func parseTimestamp(s string, c schema.Constraint) (time.Time, error) {
	if tc, ok := c.(schema.TimestampConstraint); ok && tc.Format() != "" {
		t, err := time.Parse(tc.Format(), s)
		if err != nil {
			return time.Time{}, fmt.Errorf("value: %q does not satisfy the declared timestamp format %q", s, tc.Format())
		}
		return t, nil
	}
	// RFC3339 parses fractional seconds too, so no RFC3339Nano fallback is
	// needed: nothing that layout accepts, this one refuses.
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("value: %q is not an RFC 3339 timestamp", s)
	}
	return t, nil
}

// canonicalUUID renders val through [uuid.UUID.String]. Parsing the string form
// first is what collapses the brace, urn, uppercase and bare-hex spellings
// [uuid.Parse] accepts onto one key. The string form is whatever [GetString]
// reads, so a carrier the check side accepts is one this side renders.
func canonicalUUID(val any) (any, error) {
	if u, ok := val.(uuid.UUID); ok {
		return u.String(), nil
	}
	s, ok := GetString(val)
	if !ok {
		return val, fmt.Errorf("value: cannot canonicalize %T as a UUID (want a string or uuid.UUID)", val)
	}
	u, err := uuid.Parse(s)
	if err != nil {
		return val, fmt.Errorf("value: %q is not a UUID", s)
	}
	return u.String(), nil
}

// canonicalDate renders val through [time.DateOnly] in the value's own
// location, so the calendar day is the one the caller built the value in: an
// instant at 00:30 +02:00 is the 19th, and the same instant written 22:30Z is
// the 18th.
//
// The string arm re-renders a value [time.DateOnly] already accepts, so it is
// an identity today — the layout admits exactly one spelling. It exists so the
// three kinds read alike, not because a second spelling is waiting for it.
func canonicalDate(val any) (any, error) {
	if t, ok := val.(time.Time); ok {
		return t.Format(time.DateOnly), nil
	}
	s, ok := GetString(val)
	if !ok {
		return val, fmt.Errorf("value: cannot canonicalize %T as a Date (want a string or time.Time)", val)
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return val, fmt.Errorf("value: %q is not a YYYY-MM-DD date", s)
	}
	return t.Format(time.DateOnly), nil
}

// canonicalList canonicalizes each element under the list's element
// constraint. A list whose element kind has no canonical form is returned as
// it arrived, so a caller's concrete slice type survives untouched. A value
// that is not a list is an error, as it is to the check side's coerceList.
func canonicalList(val any, elem schema.Constraint) (any, error) {
	if !Canonicalizes(elem) {
		return val, nil
	}
	elems, ok := ListElems(val)
	if !ok {
		return val, fmt.Errorf("value: cannot canonicalize %T as a list (want a slice)", val)
	}
	out := make([]any, len(elems))
	for i, e := range elems {
		c, err := Canonical(e, elem)
		if err != nil {
			return val, fmt.Errorf("element [%d]: %w", i, err)
		}
		out[i] = c
	}
	return out, nil
}

// Canonicalizes reports whether c names a kind [Canonical] rewrites. A caller
// walking many values uses it to skip the ones that cannot move, and the list
// arm uses it to leave a list of a pass-through kind as it arrived.
func Canonicalizes(c schema.Constraint) bool {
	if c == nil {
		return false
	}
	resolved := schema.ResolveAlias(c)
	switch resolved.Kind() {
	case schema.KindTimestamp, schema.KindUUID, schema.KindDate:
		return true
	case schema.KindList:
		lc, ok := resolved.(schema.ListConstraint)
		return ok && Canonicalizes(lc.Element())
	default:
		return false
	}
}

// ListElems reads val as a list: a []any as it is, an [immutable.Slice] as
// its unwrapped elements, and any other slice or array elementwise. Anything
// else — nil included — is not a list. This is the one reader every list
// position in the module goes through, so the check, coerce, canonical and
// builtin paths agree on what a list is.
func ListElems(val any) ([]any, bool) {
	switch v := val.(type) {
	case []any:
		return v, true
	case immutable.Slice:
		elems := make([]any, v.Len())
		for i, e := range v.Iter2() {
			elems[i] = e.Unwrap()
		}
		return elems, true
	}
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	elems := make([]any, rv.Len())
	for i := range elems {
		elems[i] = rv.Index(i).Interface()
	}
	return elems, true
}
