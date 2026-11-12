package value

import (
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Canonical returns val in the single stored representation c defines.
//
// Three kinds accept more than one Go representation and store one: a
// Timestamp renders through [time.RFC3339Nano], a UUID through
// [uuid.UUID.String], and a Date through [time.DateOnly] in the value's own
// location. A string form is parsed
// and re-rendered, so one value reaches one spelling whichever way a caller
// wrote it. A List canonicalizes at its element, so a List over one of those
// three kinds is rebuilt as a fresh []any. Every other kind, an unresolved
// alias and a value that holds none ([Absent]) pass through untouched, so a nil
// slice at a List stays nil and never becomes an empty list.
//
// The returned value is always usable: on error it is val unchanged, so a
// caller that heals what it can and passes through what it cannot may ignore
// the error and use the value directly.
//
// A Timestamp's stored text drops an offset's seconds. A Timestamp is stored
// only as text [time.Time.AppendText] writes, so one at a UTC offset of 24
// hours or more is refused, and a time.Time is stored as its rendered text is.
func Canonical(val any, c schema.Constraint) (any, error) {
	if c == nil || Absent(val) {
		return val, nil
	}
	resolved := schema.ResolveAlias(c)
	//exhaustive:enforce
	switch resolved.Kind() {
	case schema.KindTimestamp:
		return canonicalTimestamp(val)
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

// canonicalTimestamp renders val as RFC 3339 with fractional seconds.
func canonicalTimestamp(val any) (any, error) {
	// A time.Time is stored as its rendered text is, so both forms of one
	// instant reach one spelling.
	t, isTime := val.(time.Time)
	// The string form is whatever GetString reads, as the check side accepts
	// it: a carrier the check passes is one this side renders.
	s, isText := GetString(val)
	switch {
	case isTime:
		s = t.Format(timestampLayout)
	case !isText:
		return val, fmt.Errorf("value: cannot canonicalize %T as a Timestamp (want a string or time.Time)", val)
	}
	var buf [timestampTextSize]byte
	stored, ok := storedTimestamp(buf[:0], s)
	switch {
	case ok && s == string(stored):
		// Text in its stored form is returned as given, which saves the copy.
		return s, nil
	case ok:
		return string(stored), nil
	case isTime:
		return val, fmt.Errorf("value: a time.Time renders as %s, which is no RFC 3339 timestamp", diag.QuoteValue(s))
	}
	return val, fmt.Errorf("value: %s is not an RFC 3339 timestamp", diag.QuoteValue(s))
}

// timestampTextSize holds a Timestamp's stored text on the stack: no stored
// text is longer than its layout.
const timestampTextSize = len(timestampLayout)

// storedTimestamp appends to buf the text a Timestamp stores for s, and
// reports whether s is stored. It refuses text [ParseTimestamp] does not read,
// and an offset hour of 24, which [time.Time.AppendText] does not write.
func storedTimestamp(buf []byte, s string) ([]byte, bool) {
	t, ok := ParseTimestamp(s)
	if !ok {
		return buf, false
	}
	stored, err := t.AppendText(buf)
	return stored, err == nil
}

// DateText returns t in the text a Date stores, and reports whether that text
// reads back as a date. A year outside 0 to 9999 renders as text that does not.
func DateText(t time.Time) (string, bool) {
	s := t.Format(time.DateOnly)
	_, err := time.Parse(time.DateOnly, s)
	return s, err == nil
}

// timestampLayout is the layout of a Timestamp's stored text, RFC 3339 with
// fractional seconds, which is the text [time.Time.AppendText] and
// encoding/json write for a time.Time. A time.Time is rendered in it and then
// stored as that text is. RFC3339 carries no fraction: it would truncate a
// sub-second value and move that instance's primary key.
const timestampLayout = time.RFC3339Nano

// ParseTimestamp reads s as RFC 3339 text, as the [time.RFC3339] layout reads
// it, with or without fractional seconds. That reading takes a comma before
// the fraction, a one-digit hour, an offset minute of 60 and an offset hour of
// 24, and no lower-case t or z and no leap second. A Timestamp holds no text
// at an offset hour of 24: [Canonical] refuses it. It reads in UTC, where
// [time.Parse] reads an offset the host's zone holds into the host's location,
// so one text gives one instant in one location on every host. The checker,
// the canonical form and the Neo4j adapter all read by it.
func ParseTimestamp(s string) (time.Time, bool) {
	// RFC3339 parses fractional seconds too, so no RFC3339Nano fallback is
	// needed: nothing that layout accepts, this one refuses.
	t, err := time.ParseInLocation(time.RFC3339, s, time.UTC)
	return t, err == nil
}

// ParseUUID reads s as a UUID's text: the hyphenated form, that form between
// braces or after "urn:uuid:", or 32 hex digits, in either case. [uuid.Parse]
// alone reads any 38 bytes that hold a UUID between two bytes it does not
// check. The checker, the canonical form and the Neo4j adapter all read by it.
func ParseUUID(s string) (uuid.UUID, bool) {
	if uuid.Validate(s) != nil {
		return uuid.Nil, false
	}
	u, err := uuid.Parse(s)
	return u, err == nil
}

// canonicalUUID renders val through [uuid.UUID.String]. Parsing the string form
// first is what collapses the brace, urn, uppercase and bare-hex spellings
// [ParseUUID] accepts onto one key. The string form is whatever [GetString]
// reads, so a carrier the check side accepts is one this side renders.
func canonicalUUID(val any) (any, error) {
	if u, ok := val.(uuid.UUID); ok {
		return u.String(), nil
	}
	s, ok := GetString(val)
	if !ok {
		return val, fmt.Errorf("value: cannot canonicalize %T as a UUID (want a string or uuid.UUID)", val)
	}
	u, ok := ParseUUID(s)
	if !ok {
		return val, fmt.Errorf("value: %s is not a UUID", diag.QuoteValue(s))
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
		s, readsBack := DateText(t)
		if !readsBack {
			return val, fmt.Errorf("value: a time.Time renders as %s, which is no YYYY-MM-DD date", diag.QuoteValue(s))
		}
		return s, nil
	}
	s, ok := GetString(val)
	if !ok {
		return val, fmt.Errorf("value: cannot canonicalize %T as a Date (want a string or time.Time)", val)
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return val, fmt.Errorf("value: %s is not a YYYY-MM-DD date", diag.QuoteValue(s))
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

// ListElems reads val as a list: a non-nil []any as it is, an [immutable.Slice] as
// its unwrapped elements, and any other non-nil slice elementwise. Anything
// else — a value that holds none ([Absent]), a nil slice included, and a
// fixed-size array — is not a list: no decoder in the
// module produces an array for a list position, and an array is how a scalar
// carrier is spelled (uuid.UUID is [16]byte). This is the one reader every
// list position in the module goes through, so the check, coerce, canonical
// and builtin paths agree on what a list is.
func ListElems(val any) ([]any, bool) {
	switch v := val.(type) {
	case []any:
		return v, v != nil
	case immutable.Slice:
		if immutable.IsNilContainer(val) {
			return nil, false
		}
		elems := make([]any, v.Len())
		for i, e := range v.Iter2() {
			elems[i] = e.Unwrap()
		}
		return elems, true
	}
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice || rv.IsNil() {
		return nil, false
	}
	elems := make([]any, rv.Len())
	for i := range elems {
		elems[i] = rv.Index(i).Interface()
	}
	return elems, true
}
