package eval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// CheckErrorKind distinguishes type errors from constraint violations.
type CheckErrorKind uint8

const (
	// KindTypeMismatch indicates a wrong Go type (e.g., string when int expected).
	KindTypeMismatch CheckErrorKind = iota
	// KindConstraintFail indicates correct type but constraint violated (e.g., bounds).
	KindConstraintFail
)

// CheckError carries classification for the validator to emit the correct diagnostic code.
type CheckError struct {
	Kind CheckErrorKind
	Msg  string
}

func (e *CheckError) Error() string { return e.Msg }

func typeMismatch(format string, args ...any) *CheckError {
	return &CheckError{Kind: KindTypeMismatch, Msg: fmt.Sprintf(format, args...)}
}

func constraintFail(format string, args ...any) *CheckError {
	return &CheckError{Kind: KindConstraintFail, Msg: fmt.Sprintf(format, args...)}
}

// floatText renders a refused float for a diagnostic with a float indicator,
// so a whole float reads as the float it is: 5.0, never 5. The digits are
// encoding/json's, as the writers emit them: a float32 held in val, a named one
// included, at its 32-bit width, and any other float (a json.Number's, read
// into norm by [value.Classify]) at 64 bits. A value encoding/json cannot
// encode (NaN, ±Inf) is spelled as strconv spells it.
func floatText(val, norm any) string {
	rv := reflect.ValueOf(val)
	for rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	var f any
	switch rv.Kind() {
	case reflect.Float32:
		f = float32(rv.Float())
	case reflect.Float64:
		f = rv.Float()
	default:
		f, _ = value.GetFloat64(norm)
	}
	b, err := json.Marshal(f)
	if err != nil {
		g, _ := value.GetFloat64(f)
		return strconv.FormatFloat(g, 'g', -1, 64)
	}
	if !bytes.ContainsAny(b, ".eE") {
		b = append(b, '.', '0')
	}
	return string(b)
}

// maxNumberText bounds how much of a number literal a diagnostic repeats: a
// literal can run to megabytes, and the message is written once per value.
const maxNumberText = 32

// numberText renders a number literal for a diagnostic, cut to its first
// maxNumberText bytes with its length beside it when it is longer.
func numberText(n json.Number) string {
	if len(n) <= maxNumberText {
		return string(n)
	}
	return fmt.Sprintf("%s… (%d characters)", n[:maxNumberText], len(n))
}

// CheckValue validates that val conforms to the given constraint, by the one
// value rule [github.com/simon-lentz/yammm/internal/value] defines.
// Returns nil if valid, or an error describing the violation.
func CheckValue(val any, c schema.Constraint) error {
	if val == nil {
		// nil is valid for optional properties; required check is done elsewhere
		return nil
	}

	//exhaustive:enforce
	switch c.Kind() {
	case schema.KindString:
		return checkString(val, c)
	case schema.KindInteger:
		return checkInteger(val, c)
	case schema.KindFloat:
		return checkFloat(val, c)
	case schema.KindBoolean:
		return checkBoolean(val)
	case schema.KindTimestamp:
		return checkTimestamp(val, c)
	case schema.KindDate:
		return checkDate(val)
	case schema.KindUUID:
		return checkUUID(val)
	case schema.KindEnum:
		return checkEnum(val, c)
	case schema.KindPattern:
		return checkPattern(val, c)
	case schema.KindVector:
		return checkVector(val, c)
	case schema.KindList:
		return checkList(val, c)
	case schema.KindAlias:
		alias, ok := c.(schema.AliasConstraint)
		if !ok {
			return errors.New("invalid alias constraint type")
		}
		resolved := alias.Resolved()
		if resolved == nil {
			return fmt.Errorf("unresolved alias constraint: %s", alias.DataTypeName())
		}
		return CheckValue(val, resolved)
	default:
		return fmt.Errorf("unknown constraint kind: %s", c.Kind())
	}
}

// CoerceValue coerces a validated value to its canonical Go type.
// This should be called after CheckValue succeeds to ensure the stored
// value uses the canonical representation (e.g., int64 for Integer, float64 for Float).
//
// Canonical types:
//   - Integer → int64
//   - Float → float64
//   - Boolean → bool, a named carrier reduced to its base
//   - String, Enum, Pattern → string, a named carrier reduced to its base
//   - Timestamp, Date, UUID → string, rendered through the constraint
//   - Vector → []float64
//
// Returns the coerced value and nil error on success. Returns (nil, error)
// when the value is not of the kind: every arm refuses a wrong-typed value,
// so a caller that coerces without checking gets an error, never the input
// reported as its own stored form.
func CoerceValue(val any, c schema.Constraint) (any, error) {
	if val == nil {
		return nil, nil //nolint:nilnil // This is the expected behavior
	}

	//exhaustive:enforce
	switch c.Kind() {
	case schema.KindInteger:
		return coerceInteger(val)
	case schema.KindFloat:
		return coerceFloat(val)
	case schema.KindVector:
		return coerceVector(val)
	case schema.KindList:
		return coerceList(val, c)
	case schema.KindAlias:
		alias, ok := c.(schema.AliasConstraint)
		if !ok {
			return nil, errors.New("invalid alias constraint type")
		}
		resolved := alias.Resolved()
		if resolved == nil {
			return nil, fmt.Errorf("unresolved alias constraint: %s", alias.DataTypeName())
		}
		return CoerceValue(val, resolved)
	case schema.KindTimestamp, schema.KindDate, schema.KindUUID:
		// These three accept two Go representations and store one string. The
		// rule lives in [value.Canonical] so the wire, both writers and the
		// snapshot rebuild render a value the same way this does.
		// value.Canonical returns its input beside an error; every arm here
		// returns nil beside one, so a caller never mistakes the input for a
		// stored form.
		out, err := value.Canonical(val, c)
		if err != nil {
			return nil, err //nolint:wrapcheck // the canonicalizer's errors name the value and the layout; a wrapper here adds nothing
		}
		return out, nil
	case schema.KindString, schema.KindEnum, schema.KindPattern:
		// A named type over string reaches here from a caller holding
		// adapter/gogen's output. Store the base value: accepting the carrier
		// and then persisting it would put a type the wire and the writers
		// never see into the validated instance.
		if s, ok := value.GetString(val); ok {
			return s, nil
		}
		return nil, fmt.Errorf("cannot coerce %T to string", val)
	case schema.KindBoolean:
		if b, ok := value.GetBool(val); ok {
			return b, nil
		}
		return nil, fmt.Errorf("cannot coerce %T to bool", val)
	default:
		// Defensive: schema.Constraint is sealed, so every kind is handled above.
		return val, nil
	}
}

// coerceInteger converts an integer value to int64. A float is refused whole or
// not, as [checkInteger] refuses it: the lexical rule makes 5.0 a float.
func coerceInteger(val any) (any, error) {
	if i, ok := value.GetInt64(val); ok {
		return i, nil
	}
	kind, norm := value.Classify(val)
	switch kind {
	case value.IntKind:
		// Classify reads through a pointer to a json.Number, which GetInt64
		// does not, so an integer literal that fits is read from norm.
		if i, ok := value.GetInt64(norm); ok {
			return i, nil
		}
		// An integer literal no int64 holds: its nearest float64 can be a
		// different integer, so it is refused rather than rounded.
		if n, ok := norm.(json.Number); ok {
			return nil, fmt.Errorf("cannot coerce integer %s outside the int64 range to int64", numberText(n))
		}
		if u, ok := value.GetUint64(norm); ok {
			return nil, fmt.Errorf("uint64 value %d exceeds int64 max", u)
		}
	case value.FloatKind:
		return nil, fmt.Errorf("cannot coerce float %s to int64", floatText(val, norm))
	}
	return nil, fmt.Errorf("cannot coerce %T to int64", val)
}

// coerceFloat converts a float or an integer to float64. A number literal is
// read to its nearest float64, which keeps a negative zero's sign; one no finite
// float64 holds, and a NaN or infinity a caller spells, is refused, as
// [checkFloat] refuses it.
func coerceFloat(val any) (any, error) {
	if n, ok := asNumber(val); ok {
		f, err := n.Float64()
		switch {
		case errors.Is(err, strconv.ErrRange):
			return nil, fmt.Errorf("cannot coerce %s to float64: no finite float64 holds it", numberText(n))
		case err != nil:
			return nil, fmt.Errorf("cannot coerce json.Number %q to float64", numberText(n))
		case !value.IsFinite(f):
			return nil, errors.New("cannot coerce non-finite float (NaN or Inf)")
		}
		return f, nil
	}
	kind, norm := value.Classify(val)
	if kind != value.FloatKind && kind != value.IntKind {
		return nil, fmt.Errorf("cannot coerce %T to float64", val)
	}
	if f, ok := value.GetFloat64(norm); ok {
		// Defense in depth: reject NaN/Inf even if check was bypassed
		if !value.IsFinite(f) {
			return nil, errors.New("cannot coerce non-finite float (NaN or Inf)")
		}
		return f, nil
	}
	if i, ok := value.GetInt64(norm); ok {
		return float64(i), nil
	}
	if u, ok := value.GetUint64(norm); ok {
		return float64(u), nil
	}
	return nil, fmt.Errorf("cannot coerce %T to float64", val)
}

// asNumber returns val as a json.Number, through any pointers to one. A number
// literal is read as its own text at a Float: [value.Classify] reads the
// integer literal -0 as the int64 0, which has no sign.
func asNumber(val any) (json.Number, bool) {
	rv := reflect.ValueOf(val)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "", false
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Type() != reflect.TypeFor[json.Number]() {
		return "", false
	}
	return json.Number(rv.String()), true
}

// coerceVector converts any numeric slice to []float64, each element through
// coerceFloat.
func coerceVector(val any) (any, error) {
	slice, ok := toSlice(val)
	if !ok {
		return nil, fmt.Errorf("cannot coerce %T to []float64", val)
	}

	result := make([]float64, len(slice))
	for i, elem := range slice {
		coerced, err := coerceFloat(elem)
		if err != nil {
			return nil, fmt.Errorf("vector element [%d]: %w", i, err)
		}
		// coerceFloat always returns float64 on success
		f, ok := coerced.(float64)
		if !ok {
			return nil, fmt.Errorf("vector element [%d]: internal error: coerceFloat returned %T", i, coerced)
		}
		result[i] = f
	}
	return result, nil
}

// checkString validates that val is a string with optional length bounds.
// Per SPEC, string length is counted in runes (characters), not bytes.
func checkString(val any, c schema.Constraint) error {
	s, ok := value.GetString(val)
	if !ok {
		return typeMismatch("expected string, got %T", val)
	}

	// Check length bounds if available
	sc, ok := c.(schema.StringConstraint)
	if !ok {
		return nil // No bounds to check
	}

	// Use rune count per SPEC: string length is counted in runes, not bytes
	runes := int64(utf8.RuneCountInString(s))
	if minLen, hasMin := sc.MinLen(); hasMin && runes < minLen {
		return constraintFail("string length %d is less than minimum %d", runes, minLen)
	}
	if maxLen, hasMax := sc.MaxLen(); hasMax && runes > maxLen {
		return constraintFail("string length %d exceeds maximum %d", runes, maxLen)
	}
	return nil
}

// checkInteger validates that val is an integer with optional bounds. A float
// is refused whatever it holds, a whole 5.0 or 1e2 included: an Integer value
// is an integer literal or a Go integer, by the rule [value.Classify] applies.
func checkInteger(val any, c schema.Constraint) error {
	kind, norm := value.Classify(val)

	var i int64
	switch kind {
	case value.IntKind:
		var ok bool
		i, ok = value.GetInt64(norm)
		if !ok {
			if n, isNumber := norm.(json.Number); isNumber {
				return typeMismatch("expected integer, got an integer outside the int64 range: %s", numberText(n))
			}
			return typeMismatch("cannot convert %T to int64", val)
		}
	case value.FloatKind:
		return typeMismatch("expected integer, got float %s", floatText(val, norm))
	default:
		return typeMismatch("expected integer, got %T", val)
	}

	// Check bounds if available
	ic, ok := c.(schema.IntegerConstraint)
	if !ok {
		return nil // No bounds to check
	}

	if lo, hasMin := ic.Min(); hasMin && i < lo {
		return constraintFail("integer %d is less than minimum %d", i, lo)
	}
	if hi, hasMax := ic.Max(); hasMax && i > hi {
		return constraintFail("integer %d exceeds maximum %d", i, hi)
	}
	return nil
}

// checkFloat validates that val is a float or integer with optional bounds.
// An integer widens: an unsigned value above math.MaxInt64 and an integer
// literal no int64 holds are read as their nearest float64, as coerceFloat
// converts them. A literal no finite float64 holds is refused.
func checkFloat(val any, c schema.Constraint) error {
	var f float64
	if n, isNumber := asNumber(val); isNumber {
		fv, err := n.Float64()
		switch {
		case err == nil:
			f = fv
		case errors.Is(err, strconv.ErrRange):
			return typeMismatch("expected float, got a number no float64 holds: %s", numberText(n))
		default:
			return typeMismatch("expected float, got %T", val)
		}
	} else {
		kind, norm := value.Classify(val)
		if kind != value.FloatKind && kind != value.IntKind {
			return typeMismatch("expected float, got %T", val)
		}
		if fv, ok := value.GetFloat64(norm); ok {
			f = fv
		} else if iv, ok := value.GetInt64(norm); ok {
			f = float64(iv)
		} else if uv, ok := value.GetUint64(norm); ok {
			f = float64(uv)
		} else {
			return typeMismatch("cannot convert %T to float64", val)
		}
	}

	// Reject NaN and Inf values per spec
	if !value.IsFinite(f) {
		return constraintFail("float value is not finite (NaN or Inf)")
	}

	// Check bounds if available
	fc, ok := c.(schema.FloatConstraint)
	if !ok {
		return nil // No bounds to check
	}

	if lo, hasMin := fc.Min(); hasMin && f < lo {
		return constraintFail("float %v is less than minimum %v", f, lo)
	}
	if hi, hasMax := fc.Max(); hasMax && f > hi {
		return constraintFail("float %v exceeds maximum %v", f, hi)
	}
	return nil
}

// checkBoolean validates that val is a boolean.
func checkBoolean(val any) error {
	if _, ok := value.GetBool(val); ok {
		return nil
	}
	return typeMismatch("expected boolean, got %T", val)
}

// checkTimestamp validates that val is a valid timestamp.
// Accepts time.Time (always valid) or string (parsed against format).
func checkTimestamp(val any, c schema.Constraint) error {
	// Accept time.Time directly - always valid
	if _, ok := val.(time.Time); ok {
		return nil
	}

	s, ok := value.GetString(val)
	if !ok {
		return typeMismatch("expected timestamp string or time.Time, got %T", val)
	}

	// Check for custom format
	tc, ok := c.(schema.TimestampConstraint)
	if ok && tc.Format() != "" {
		if _, err := time.Parse(tc.Format(), s); err != nil {
			return constraintFail("invalid timestamp format: %s (expected %s)", s, tc.Format())
		}
		return nil
	}

	// RFC 3339 parses fractional seconds too, so no RFC3339Nano fallback is
	// needed: nothing that layout accepts, this one refuses.
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		return constraintFail("invalid timestamp format: %s", s)
	}
	return nil
}

// checkDate validates that val is a valid date.
// Accepts time.Time (always valid) or string (parsed as YYYY-MM-DD).
//
// The time.Time arm has to be here rather than only in the coercer: CheckValue
// runs first, so rejecting the type here would make the coercer's Date arm
// unreachable and the generated time.Time field unusable.
func checkDate(val any) error {
	// Accept time.Time directly - always valid
	if _, ok := val.(time.Time); ok {
		return nil
	}

	s, ok := value.GetString(val)
	if !ok {
		return typeMismatch("expected date string or time.Time, got %T", val)
	}
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return constraintFail("invalid date format: %s (expected YYYY-MM-DD)", s)
	}
	return nil
}

// checkUUID validates that val is a valid UUID.
// Accepts uuid.UUID (always valid) or string (parsed as UUID).
func checkUUID(val any) error {
	// Accept uuid.UUID directly - always valid
	if _, ok := val.(uuid.UUID); ok {
		return nil
	}

	s, ok := value.GetString(val)
	if !ok {
		return typeMismatch("expected UUID string or uuid.UUID, got %T", val)
	}
	if _, err := uuid.Parse(s); err != nil {
		return constraintFail("invalid UUID: %s", s)
	}
	return nil
}

// checkEnum validates that val is one of the allowed enum values.
func checkEnum(val any, c schema.Constraint) error {
	s, ok := value.GetString(val)
	if !ok {
		return typeMismatch("expected string for enum, got %T", val)
	}

	ec, ok := c.(schema.EnumConstraint)
	if !ok {
		return errors.New("invalid enum constraint type")
	}

	allowed := ec.Values()
	if slices.Contains(allowed, s) {
		return nil
	}
	return constraintFail("value %q not in enum %v", s, allowed)
}

// checkPattern validates that val matches all constraint patterns.
func checkPattern(val any, c schema.Constraint) error {
	s, ok := value.GetString(val)
	if !ok {
		return typeMismatch("expected string for pattern, got %T", val)
	}

	pc, ok := c.(schema.PatternConstraint)
	if !ok {
		return errors.New("invalid pattern constraint type")
	}

	compiled := pc.CompiledPatterns()
	patterns := pc.Patterns()
	for i, pattern := range compiled {
		if !pattern.MatchString(s) {
			return constraintFail("value %q does not match pattern %s", s, patterns[i])
		}
	}
	return nil
}

// checkVector validates that val is a float slice with correct dimension.
func checkVector(val any, c schema.Constraint) error {
	// Get the slice as []any
	slice, ok := toSlice(val)
	if !ok {
		return typeMismatch("expected array for vector, got %T", val)
	}

	vc, ok := c.(schema.VectorConstraint)
	if !ok {
		return errors.New("invalid vector constraint type")
	}

	// Check dimension
	expected := vc.Dimension()
	if len(slice) != expected {
		return constraintFail("vector has %d elements, expected %d", len(slice), expected)
	}

	// Each element is judged by the Float rule, and coerceVector converts each
	// through coerceFloat, which refuses what checkFloat refuses.
	for i, elem := range slice {
		if err := checkFloat(elem, nil); err != nil {
			if ce, ok := errors.AsType[*CheckError](err); ok {
				return &CheckError{Kind: ce.Kind, Msg: fmt.Sprintf("vector element [%d]: %s", i, ce.Msg)}
			}
			return fmt.Errorf("vector element [%d]: %w", i, err)
		}
	}
	return nil
}

// elementKindName renders an element constraint's kind for a diagnostic,
// resolving an alias so the reader sees the underlying kind rather than the
// DataType name.
func elementKindName(c schema.Constraint) string {
	return strings.ToLower(schema.ResolveAlias(c).Kind().String())
}

// checkList validates that val is a slice with elements matching the element constraint.
func checkList(val any, c schema.Constraint) error {
	slice, ok := toSlice(val)
	if !ok {
		return typeMismatch("expected array for list, got %T", val)
	}

	lc, ok := c.(schema.ListConstraint)
	if !ok {
		return errors.New("invalid list constraint type")
	}

	// Check length bounds
	length := int64(len(slice))
	if minLen, hasMin := lc.MinLen(); hasMin && length < minLen {
		return constraintFail("list length %d is less than minimum %d", length, minLen)
	}
	if maxLen, hasMax := lc.MaxLen(); hasMax && length > maxLen {
		return constraintFail("list length %d exceeds maximum %d", length, maxLen)
	}

	// Check each element
	elemConstraint := lc.Element()
	for i, elem := range slice {
		// CheckValue short-circuits nil for the optional-property slot. No
		// element position is optional, so the element constraint applies here
		// and a null element is rejected — checkVector rejects one inline for
		// the same reason.
		if elem == nil {
			return typeMismatch("element [%d]: expected %s, got null", i, elementKindName(elemConstraint))
		}
		if err := CheckValue(elem, elemConstraint); err != nil {
			if ce, ok := errors.AsType[*CheckError](err); ok {
				return &CheckError{
					Kind: ce.Kind,
					Msg:  fmt.Sprintf("element [%d]: %s", i, ce.Msg),
				}
			}
			return fmt.Errorf("element [%d]: %w", i, err)
		}
	}

	return nil
}

// coerceList coerces each element to its canonical type.
func coerceList(val any, c schema.Constraint) (any, error) {
	slice, ok := toSlice(val)
	if !ok {
		return nil, fmt.Errorf("expected array for list, got %T", val)
	}

	lc, ok := c.(schema.ListConstraint)
	if !ok {
		return nil, errors.New("invalid list constraint type")
	}

	elemConstraint := lc.Element()
	result := make([]any, len(slice))
	for i, elem := range slice {
		// The check side rejects a null element, so reaching one here means a
		// caller coerced without checking. Refusing keeps the invariant that a
		// coerced list holds no nil.
		if elem == nil {
			return nil, fmt.Errorf("element [%d]: expected %s, got null", i, elementKindName(elemConstraint))
		}
		coerced, err := CoerceValue(elem, elemConstraint)
		if err != nil {
			return nil, fmt.Errorf("element [%d]: %w", i, err)
		}
		result[i] = coerced
	}

	return result, nil
}

// toSlice converts val to []any if it's a slice.
func toSlice(val any) ([]any, bool) {
	return value.ListElems(val)
}

// TypeChecker is a function that checks if a value matches a type.
// Returns (true, "") if valid, or (false, message) with an error description.
type TypeChecker func(val any) (bool, string)

// checkerOf adapts a kind's check function to a TypeChecker, so a datatype
// check (`=~ Kind`) applies exactly the rule a property of that kind does.
// A nil constraint carries no bounds, layout or member set.
func checkerOf(check func(val any, c schema.Constraint) error) TypeChecker {
	return func(val any) (bool, string) {
		if err := check(val, nil); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
}

// IsString returns a TypeChecker that validates string values.
func IsString() TypeChecker {
	return checkerOf(checkString)
}

// IsInteger returns a TypeChecker that validates integer values. A float is
// refused, whole or not.
func IsInteger() TypeChecker {
	return checkerOf(checkInteger)
}

// IsFloat returns a TypeChecker that validates finite float and integer values.
func IsFloat() TypeChecker {
	return checkerOf(checkFloat)
}

// IsBoolean returns a TypeChecker that validates boolean values.
func IsBoolean() TypeChecker {
	return checkerOf(func(val any, _ schema.Constraint) error { return checkBoolean(val) })
}

// IsUUID returns a TypeChecker that validates UUID values.
func IsUUID() TypeChecker {
	return checkerOf(func(val any, _ schema.Constraint) error { return checkUUID(val) })
}

// IsTimestamp returns a TypeChecker that validates timestamp values: a
// time.Time, or a string in RFC 3339. A bare datatype names no layout, so a
// value stored under a Timestamp["layout"] property matches only when that
// layout is RFC 3339.
func IsTimestamp() TypeChecker {
	return checkerOf(checkTimestamp)
}

// IsDate returns a TypeChecker that validates date values.
func IsDate() TypeChecker {
	return checkerOf(func(val any, _ schema.Constraint) error { return checkDate(val) })
}
