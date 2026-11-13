package value

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/expr"
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

// textOf reads val as the text of a kind held as a string, as [GetString]
// reads it; want names what the kind accepts. A string
// value is valid UTF-8, so a string that is not is refused as a value that is
// no string is: a type mismatch, before any rule of the kind reads its bytes.
// The message quotes the string by [diag.QuoteValue], so the diagnostic is
// valid UTF-8 itself and its length does not follow the string's.
func textOf(val any, want string) (string, *CheckError) {
	s, ok := GetString(val)
	if !ok {
		return "", typeMismatch("expected %s, got %T", want, val)
	}
	if !utf8.ValidString(s) {
		return "", typeMismatch("expected %s, got a string that is not valid UTF-8: %s", want, diag.QuoteValue(s))
	}
	return s, nil
}

// floatText renders a refused float for a diagnostic with a float indicator,
// so a whole float reads as the float it is: 5.0, never 5. The digits are
// encoding/json's, as the writers emit them: a float32 held in val, a named one
// included, at its 32-bit width, and any other float (a json.Number's, read
// into norm by [Classify]) at 64 bits. A value encoding/json cannot
// encode (NaN, ±Inf) is spelled as strconv spells it.
func floatText(val, norm any) string {
	f, _ := Scalar(val)
	switch f.(type) {
	case float32, float64:
	default:
		f, _ = GetFloat64(norm)
	}
	b, err := json.Marshal(f)
	if err != nil {
		g, _ := GetFloat64(f)
		return strconv.FormatFloat(g, 'g', -1, 64)
	}
	if !bytes.ContainsAny(b, ".eE") {
		b = append(b, '.', '0')
	}
	return string(b)
}

// Check reports whether val conforms to c: nil if it does, or an error that
// describes the violation. The validator, the snapshot reader and the Neo4j
// adapter's parameter coercion all judge a value by it.
func Check(val any, c schema.Constraint) error {
	if Absent(val) {
		// No value is valid for an optional property; the required check is
		// done elsewhere.
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
		return checkTimestamp(val)
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
		return Check(val, resolved)
	default:
		return fmt.Errorf("unknown constraint kind: %s", c.Kind())
	}
}

// Coerce returns val in the form the validator stores at c. Call it after
// [Check] passes.
//
// Canonical types:
//   - Integer → int64
//   - Float → float64
//   - Boolean → bool, a named carrier reduced to its base
//   - String, Enum, Pattern → string, a named carrier reduced to its base
//   - Timestamp, Date, UUID → string, in the one spelling [Canonical] gives
//   - Vector → []float64
//
// Returns the coerced value and nil error on success. Returns (nil, error)
// when the value is not of the kind: every arm refuses a wrong-typed value,
// so a caller that coerces without checking gets an error, never the input
// reported as its own stored form.
func Coerce(val any, c schema.Constraint) (any, error) {
	if Absent(val) {
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
		return Coerce(val, resolved)
	case schema.KindTimestamp, schema.KindDate, schema.KindUUID:
		// These three accept two Go representations and store one string. The
		// rule lives in [Canonical] so the wire and the snapshot rebuild
		// render a value the same way this does.
		// Canonical returns its input beside an error; every arm here
		// returns nil beside one, so a caller never mistakes the input for a
		// stored form.
		if err := invalidText(val); err != nil {
			return nil, err
		}
		out, err := Canonical(val, c)
		if err != nil {
			return nil, err
		}
		return out, nil
	case schema.KindString, schema.KindEnum, schema.KindPattern:
		// A named type over string reaches here from a caller holding
		// schema/gogen's output. Store the base value: accepting the carrier
		// and then persisting it would put a type the wire and the writers
		// never see into the validated instance.
		s, ok := GetString(val)
		if !ok {
			return nil, fmt.Errorf("cannot coerce %T to string", val)
		}
		if err := invalidText(val); err != nil {
			return nil, err
		}
		return s, nil
	case schema.KindBoolean:
		if b, ok := GetBool(val); ok {
			return b, nil
		}
		return nil, fmt.Errorf("cannot coerce %T to bool", val)
	default:
		// Defensive: schema.Constraint is sealed, so every kind is handled above.
		return val, nil
	}
}

// invalidText refuses a string that is not valid UTF-8, as [textOf] refuses it
// on the check side, so a caller that coerces without checking stores no such
// string. The coercion judges no length, membership or pattern. Any other
// value passes.
func invalidText(val any) error {
	if s, ok := GetString(val); ok && !utf8.ValidString(s) {
		return fmt.Errorf("cannot coerce a string that is not valid UTF-8: %s", diag.QuoteValue(s))
	}
	return nil
}

// coerceInteger converts an integer value to int64. A float is refused whole or
// not, as [checkInteger] refuses it: the lexical rule makes 5.0 a float.
func coerceInteger(val any) (any, error) {
	if i, ok := GetInt64(val); ok {
		return i, nil
	}
	kind, norm := Classify(val)
	switch kind {
	case IntKind:
		// GetInt64 reads every integer an int64 holds. An integer literal no
		// int64 holds is refused rather than rounded: its nearest float64 can
		// be a different integer.
		if n, ok := norm.(json.Number); ok {
			return nil, fmt.Errorf("cannot coerce integer %s outside the int64 range to int64", diag.CutValue(string(n)))
		}
		if u, ok := GetUint64(norm); ok {
			return nil, fmt.Errorf("cannot coerce integer %d outside the int64 range to int64", u)
		}
	case FloatKind:
		return nil, fmt.Errorf("cannot coerce float %s to int64", floatText(val, norm))
	}
	if n, ok := noFloat64Holds(val); ok {
		return nil, fmt.Errorf("cannot coerce %s to int64: no finite float64 holds it", diag.CutValue(string(n)))
	}
	if n, ok := UnreadNumber(val); ok {
		return nil, fmt.Errorf("cannot coerce json.Number %s to int64: no JSON number literal", diag.QuoteValue(string(n)))
	}
	return nil, fmt.Errorf("cannot coerce %T to int64", val)
}

// coerceFloat converts a float or an integer to float64. A number literal is
// read to its nearest float64, which keeps a negative zero's sign; one no finite
// float64 holds, a json.Number that is no number literal, and a NaN or infinity
// a caller spells, are refused, as [checkFloat] refuses them.
func coerceFloat(val any) (any, error) {
	// A number literal is read as its own text: [Classify] reads the integer
	// literal -0 as the int64 0, which has no sign.
	if n, ok := NumberLiteral(val); ok {
		// A JSON number literal fails to parse only when no finite float64 holds it.
		f, err := n.Float64()
		if err != nil {
			return nil, fmt.Errorf("cannot coerce %s to float64: no finite float64 holds it", diag.CutValue(string(n)))
		}
		return f, nil
	}
	if n, ok := UnreadNumber(val); ok {
		return nil, fmt.Errorf("cannot coerce json.Number %s to float64: no JSON number literal", diag.QuoteValue(string(n)))
	}
	kind, norm := Classify(val)
	if kind != FloatKind && kind != IntKind {
		return nil, fmt.Errorf("cannot coerce %T to float64", val)
	}
	if f, ok := GetFloat64(norm); ok {
		// Defense in depth: reject NaN/Inf even if check was bypassed
		if !IsFinite(f) {
			return nil, errors.New("cannot coerce non-finite float (NaN or Inf)")
		}
		return f, nil
	}
	if i, ok := GetInt64(norm); ok {
		return float64(i), nil
	}
	if u, ok := GetUint64(norm); ok {
		return float64(u), nil
	}
	return nil, fmt.Errorf("cannot coerce %T to float64", val)
}

// noFloat64Holds returns val as a number literal when it is one that spells a
// number no finite float64 holds: the literal [checkFloat] and [coerceFloat]
// refuse in those words. An Integer refuses it
// in the same words, where it would otherwise name the literal's Go type.
func noFloat64Holds(val any) (json.Number, bool) {
	n, ok := NumberLiteral(val)
	if !ok {
		return "", false
	}
	_, err := n.Float64()
	return n, errors.Is(err, strconv.ErrRange)
}

// coerceVector converts any numeric slice to []float64, each element through
// coerceFloat. A []float64, the stored form, is copied without boxing its
// elements.
func coerceVector(val any) (any, error) {
	if stored, ok := val.([]float64); ok {
		for i, f := range stored {
			if !IsFinite(f) {
				_, err := coerceFloat(f)
				return nil, fmt.Errorf("vector element [%d]: %w", i, err)
			}
		}
		return slices.Clone(stored), nil
	}
	slice, ok := toSlice(val)
	if !ok {
		return nil, fmt.Errorf("cannot coerce %T to []float64", val)
	}

	result := make([]float64, len(slice))
	for i, elem := range slice {
		if Absent(elem) {
			return nil, fmt.Errorf("vector element [%d]: expected float, got null", i)
		}
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

// checkString validates that val is a string of valid UTF-8 with optional
// length bounds. Per SPEC, string length is counted in runes (characters), not
// bytes.
func checkString(val any, c schema.Constraint) error {
	s, refused := textOf(val, "string")
	if refused != nil {
		return refused
	}

	sc, ok := c.(schema.StringConstraint)
	if !ok {
		return errors.New("invalid string constraint type")
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
// is an integer literal or a Go integer, by the rule [Classify] applies.
func checkInteger(val any, c schema.Constraint) error {
	kind, norm := Classify(val)

	var i int64
	switch kind {
	case IntKind:
		var ok bool
		i, ok = GetInt64(norm)
		if !ok {
			if n, isNumber := norm.(json.Number); isNumber {
				return typeMismatch("expected integer, got an integer outside the int64 range: %s", diag.CutValue(string(n)))
			}
			if u, isUnsigned := GetUint64(norm); isUnsigned {
				return typeMismatch("expected integer, got an integer outside the int64 range: %d", u)
			}
			return typeMismatch("cannot convert %T to int64", val)
		}
	case FloatKind:
		return typeMismatch("expected integer, got float %s", floatText(val, norm))
	default:
		if n, ok := noFloat64Holds(val); ok {
			return typeMismatch("expected integer, got a number no float64 holds: %s", diag.CutValue(string(n)))
		}
		if n, ok := UnreadNumber(val); ok {
			return typeMismatch("expected integer, got a json.Number that is no JSON number literal: %s", diag.QuoteValue(string(n)))
		}
		return typeMismatch("expected integer, got %T", val)
	}

	ic, ok := c.(schema.IntegerConstraint)
	if !ok {
		return errors.New("invalid integer constraint type")
	}

	if lo, hasMin := ic.Min(); hasMin && i < lo {
		return constraintFail("integer %d is less than minimum %d", i, lo)
	}
	if hi, hasMax := ic.Max(); hasMax && i > hi {
		return constraintFail("integer %d exceeds maximum %d", i, hi)
	}
	return nil
}

// vectorElementConstraint is the constraint of a Vector's element: a Float with
// no bounds.
var vectorElementConstraint schema.Constraint = schema.NewFloatConstraint()

// checkFloat validates that val is a float or integer with optional bounds.
// An integer widens: an unsigned value above math.MaxInt64 and an integer
// literal no int64 holds are read as their nearest float64, as coerceFloat
// converts them. A literal no finite float64 holds is refused.
func checkFloat(val any, c schema.Constraint) error {
	var f float64
	if n, isNumber := NumberLiteral(val); isNumber {
		// A JSON number literal fails to parse only when no finite float64 holds it.
		fv, err := n.Float64()
		if err != nil {
			return typeMismatch("expected float, got a number no float64 holds: %s", diag.CutValue(string(n)))
		}
		f = fv
	} else {
		kind, norm := Classify(val)
		if kind != FloatKind && kind != IntKind {
			if n, ok := UnreadNumber(val); ok {
				return typeMismatch("expected float, got a json.Number that is no JSON number literal: %s", diag.QuoteValue(string(n)))
			}
			return typeMismatch("expected float, got %T", val)
		}
		if fv, ok := GetFloat64(norm); ok {
			f = fv
		} else if iv, ok := GetInt64(norm); ok {
			f = float64(iv)
		} else if uv, ok := GetUint64(norm); ok {
			f = float64(uv)
		} else {
			return typeMismatch("cannot convert %T to float64", val)
		}
	}

	// Reject NaN and Inf values per spec
	if !IsFinite(f) {
		return constraintFail("float value is not finite (NaN or Inf)")
	}

	fc, ok := c.(schema.FloatConstraint)
	if !ok {
		return errors.New("invalid float constraint type")
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
	if _, ok := GetBool(val); ok {
		return nil
	}
	return typeMismatch("expected boolean, got %T", val)
}

// checkTimestamp validates that val is a valid timestamp.
// Accepts a time.Time, or a string read as RFC 3339, that [Canonical] stores.
func checkTimestamp(val any) error {
	// A time.Time is judged as the text it renders as, so the Go path and the
	// text path refuse the same values.
	var s string
	if t, ok := val.(time.Time); ok {
		s = t.Format(timestampLayout)
	} else {
		var refused *CheckError
		if s, refused = textOf(val, "timestamp string or time.Time"); refused != nil {
			return refused
		}
	}

	// Text is judged as the text it is stored as, so a value this check passes
	// is one [Canonical] stores.
	var buf [timestampTextSize]byte
	if _, ok := storedTimestamp(buf[:0], s); ok {
		return nil
	}
	return constraintFail("invalid timestamp format: %s", diag.QuoteValue(s))
}

// checkDate validates that val is a valid date.
// Accepts a time.Time whose stored text reads back, or a string parsed as
// YYYY-MM-DD.
//
// The time.Time arm has to be here rather than only in the coercer: [Check]
// runs first, so rejecting the type here would make the coercer's Date arm
// unreachable and the generated time.Time field unusable.
func checkDate(val any) error {
	// A time.Time is judged as the text it is stored as, as checkTimestamp does.
	if t, ok := val.(time.Time); ok {
		s, readsBack := DateText(t)
		if readsBack {
			return nil
		}
		val = s
	}

	s, refused := textOf(val, "date string or time.Time")
	if refused != nil {
		return refused
	}
	if _, err := time.Parse(time.DateOnly, s); err != nil {
		return constraintFail("invalid date format: %s (expected YYYY-MM-DD)", diag.QuoteValue(s))
	}
	return nil
}

// checkUUID validates that val is a valid UUID.
// Accepts uuid.UUID (always valid) or string (parsed as UUID).
func checkUUID(val any) error {
	// A uuid.UUID is always valid.
	if _, ok := val.(uuid.UUID); ok {
		return nil
	}

	s, refused := textOf(val, "UUID string or uuid.UUID")
	if refused != nil {
		return refused
	}
	if _, ok := ParseUUID(s); !ok {
		return constraintFail("invalid UUID: %s", diag.QuoteValue(s))
	}
	return nil
}

// checkEnum validates that val is one of the allowed enum values.
func checkEnum(val any, c schema.Constraint) error {
	s, refused := textOf(val, "string for enum")
	if refused != nil {
		return refused
	}

	ec, ok := c.(schema.EnumConstraint)
	if !ok {
		return errors.New("invalid enum constraint type")
	}

	allowed := ec.Values()
	if slices.Contains(allowed, s) {
		return nil
	}
	return constraintFail("value %s not in enum %v", diag.QuoteValue(s), allowed)
}

// checkPattern validates that val matches all constraint patterns.
func checkPattern(val any, c schema.Constraint) error {
	s, refused := textOf(val, "string for pattern")
	if refused != nil {
		return refused
	}

	if _, ok := c.(schema.PatternConstraint); !ok {
		return errors.New("invalid pattern constraint type")
	}

	// A pattern's String is its source text, and the hook takes c as it stands, so a
	// value this path judges allocates nothing.
	for _, pattern := range expr.PatternRegexps(c) {
		if !pattern.MatchString(s) {
			return constraintFail("value %s does not match pattern %s", diag.QuoteValue(s), pattern.String())
		}
	}
	return nil
}

// checkVector validates that val is a float slice with correct dimension. A
// []float64, the stored form, is judged in place: [ListElems] boxes each element.
func checkVector(val any, c schema.Constraint) error {
	stored, isStored := val.([]float64)
	var slice []any
	if !isStored {
		var ok bool
		if slice, ok = toSlice(val); !ok {
			return typeMismatch("expected array for vector, got %T", val)
		}
	}

	vc, ok := c.(schema.VectorConstraint)
	if !ok {
		return errors.New("invalid vector constraint type")
	}

	// Check dimension
	expected := vc.Dimension()
	if n := len(stored) + len(slice); n != expected {
		return constraintFail("vector has %d elements, expected %d", n, expected)
	}

	for i, f := range stored {
		if !IsFinite(f) {
			return vectorElement(i, checkFloat(f, vectorElementConstraint))
		}
	}
	// Each element is judged by the Float rule, and coerceVector converts each
	// through coerceFloat, which refuses what checkFloat refuses.
	for i, elem := range slice {
		if Absent(elem) {
			return typeMismatch("vector element [%d]: expected float, got null", i)
		}
		if err := checkFloat(elem, vectorElementConstraint); err != nil {
			return vectorElement(i, err)
		}
	}
	return nil
}

// vectorElement returns err, the Float rule's refusal of a Vector's element i,
// naming the element.
func vectorElement(i int, err error) error {
	if ce, ok := errors.AsType[*CheckError](err); ok {
		return &CheckError{Kind: ce.Kind, Msg: fmt.Sprintf("vector element [%d]: %s", i, ce.Msg)}
	}
	return fmt.Errorf("vector element [%d]: %w", i, err)
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
		// Check short-circuits a value that is absent for the
		// optional-property slot. No element position is optional, so a null
		// element is rejected here.
		if Absent(elem) {
			return typeMismatch("element [%d]: expected %s, got null", i, elementKindName(elemConstraint))
		}
		if err := Check(elem, elemConstraint); err != nil {
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
		if Absent(elem) {
			return nil, fmt.Errorf("element [%d]: expected %s, got null", i, elementKindName(elemConstraint))
		}
		coerced, err := Coerce(elem, elemConstraint)
		if err != nil {
			return nil, fmt.Errorf("element [%d]: %w", i, err)
		}
		result[i] = coerced
	}

	return result, nil
}

// toSlice converts val to []any if it's a slice.
func toSlice(val any) ([]any, bool) {
	return ListElems(val)
}
