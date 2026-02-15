package eval

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"slices"
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

// Checker performs constraint checking with an optional custom type registry.
// The registry enables recognition of custom Go types (e.g., `type MyInt int`)
// during value classification.
type Checker struct {
	registry value.Registry
}

// NewChecker creates a Checker with the given value registry.
// A zero-value Registry falls back to built-in type detection.
func NewChecker(reg value.Registry) *Checker {
	return &Checker{registry: reg}
}

// DefaultChecker returns a Checker using built-in type detection only.
// This is equivalent to NewChecker(value.Registry{}).
func DefaultChecker() *Checker {
	return &Checker{}
}

// CheckValue validates that val conforms to the given constraint.
// Returns nil if valid, or an error describing the violation.
//
// This package-level function uses built-in type detection. For custom type
// recognition, use Checker.CheckValue with a configured registry.
func CheckValue(val any, c schema.Constraint) error {
	return DefaultChecker().CheckValue(val, c)
}

// CheckValue validates that val conforms to the given constraint.
// Returns nil if valid, or an error describing the violation.
//
// The Checker's registry is used for custom type recognition during
// value classification.
func (ch *Checker) CheckValue(val any, c schema.Constraint) error {
	if val == nil {
		// nil is valid for optional properties; required check is done elsewhere
		return nil
	}

	switch c.Kind() {
	case schema.KindString:
		return checkString(val, c)
	case schema.KindInteger:
		return ch.checkInteger(val, c)
	case schema.KindFloat:
		return ch.checkFloat(val, c)
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
		return ch.checkVector(val, c)
	case schema.KindAlias:
		// Resolve alias and check against resolved constraint
		alias, ok := c.(schema.AliasConstraint)
		if !ok {
			return errors.New("invalid alias constraint type")
		}
		resolved := alias.Resolved()
		if resolved == nil {
			return fmt.Errorf("unresolved alias constraint: %s", alias.DataTypeName())
		}
		return ch.CheckValue(val, resolved)
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
//   - Boolean → bool (unchanged)
//   - String types (String, Timestamp, Date, UUID, Enum, Pattern) → string (unchanged)
//   - Vector → []float64
//
// Returns the coerced value and nil error on success.
// Returns (nil, error) if coercion fails (should not happen after CheckValue).
//
// This package-level function uses built-in type detection. For custom type
// recognition, use Checker.CoerceValue with a configured registry.
func CoerceValue(val any, c schema.Constraint) (any, error) {
	return DefaultChecker().CoerceValue(val, c)
}

// CoerceValue coerces a validated value to its canonical Go type.
// This should be called after CheckValue succeeds to ensure the stored
// value uses the canonical representation.
//
// The Checker's registry is used for custom type recognition, enabling
// coercion of custom Go types (e.g., `type MyInt int64`) to canonical types.
func (ch *Checker) CoerceValue(val any, c schema.Constraint) (any, error) {
	if val == nil {
		return nil, nil //nolint:nilnil // This is the expected behavior
	}

	switch c.Kind() {
	case schema.KindInteger:
		return ch.coerceInteger(val)
	case schema.KindFloat:
		return ch.coerceFloat(val)
	case schema.KindVector:
		return ch.coerceVector(val)
	case schema.KindAlias:
		// Resolve alias and coerce against resolved constraint
		alias, ok := c.(schema.AliasConstraint)
		if !ok {
			return nil, errors.New("invalid alias constraint type")
		}
		resolved := alias.Resolved()
		if resolved == nil {
			return nil, fmt.Errorf("unresolved alias constraint: %s", alias.DataTypeName())
		}
		return ch.CoerceValue(val, resolved)
	default:
		// String, Boolean, Timestamp, Date, UUID, Enum, Pattern - already canonical
		return val, nil
	}
}

// coerceInteger converts any integer-compatible value to int64.
// Accepts integer types and float64 whole numbers per spec.
// Uses registry for custom type recognition, with reflection fallback.
func (ch *Checker) coerceInteger(val any) (any, error) {
	// Try direct integer extraction first (fast path)
	if i, ok := value.GetInt64(val); ok {
		return i, nil
	}
	// Try float64 whole number extraction
	if f, ok := value.GetFloat64(val); ok {
		if i, ok := value.GetInt64FromFloat(f); ok {
			return i, nil
		}
		if !value.IsFinite(f) {
			return nil, errors.New("cannot coerce non-finite float (NaN or Inf) to int64")
		}
		return nil, fmt.Errorf("cannot coerce float with fractional part %v to int64", f)
	}

	// Reflection fallback for custom integer types recognized by registry
	kind, _ := value.ClassifyWithRegistry(ch.registry, val)
	if kind == value.IntKind {
		rv := reflect.ValueOf(val)
		// Dereference pointers
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil, errors.New("cannot coerce nil pointer to int64")
			}
			rv = rv.Elem()
		}
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return rv.Int(), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			u := rv.Uint()
			if u > uint64(math.MaxInt64) {
				return nil, fmt.Errorf("uint64 value %d exceeds int64 max", u)
			}
			return int64(u), nil
		}
	}

	return nil, fmt.Errorf("cannot coerce %T to int64", val)
}

// coerceFloat converts any float-compatible value to float64.
// Uses registry for custom type recognition, with reflection fallback.
func (ch *Checker) coerceFloat(val any) (any, error) {
	// Try direct float extraction first (fast path)
	if f, ok := value.GetFloat64(val); ok {
		// Defense in depth: reject NaN/Inf even if check was bypassed
		if !value.IsFinite(f) {
			return nil, errors.New("cannot coerce non-finite float (NaN or Inf)")
		}
		return f, nil
	}
	if i, ok := value.GetInt64(val); ok {
		return float64(i), nil // integers are always finite
	}

	// Reflection fallback for custom numeric types recognized by registry
	kind, _ := value.ClassifyWithRegistry(ch.registry, val)
	if kind == value.FloatKind || kind == value.IntKind {
		rv := reflect.ValueOf(val)
		// Dereference pointers
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil, errors.New("cannot coerce nil pointer to float64")
			}
			rv = rv.Elem()
		}
		switch rv.Kind() {
		case reflect.Float32, reflect.Float64:
			f := rv.Float()
			if !value.IsFinite(f) {
				return nil, errors.New("cannot coerce non-finite float (NaN or Inf)")
			}
			return f, nil
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return float64(rv.Int()), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			return float64(rv.Uint()), nil
		}
	}

	return nil, fmt.Errorf("cannot coerce %T to float64", val)
}

// coerceVector converts any numeric slice to []float64.
// Uses registry for custom type recognition via coerceFloat for each element.
func (ch *Checker) coerceVector(val any) (any, error) {
	slice, ok := toSlice(val)
	if !ok {
		return nil, fmt.Errorf("cannot coerce %T to []float64", val)
	}

	result := make([]float64, len(slice))
	for i, elem := range slice {
		// Use coerceFloat for registry-aware coercion of each element
		coerced, err := ch.coerceFloat(elem)
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
	s, ok := val.(string)
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

// checkInteger validates that val is an integer with optional bounds.
// Per spec, accepts integer types and float64 whole numbers (math.Trunc(f) == f).
func (ch *Checker) checkInteger(val any, c schema.Constraint) error {
	kind, _ := value.ClassifyWithRegistry(ch.registry, val)

	var i int64
	var ok bool

	switch kind {
	case value.IntKind:
		i, ok = value.GetInt64(val)
		if !ok {
			return typeMismatch("cannot convert %T to int64", val)
		}
	case value.FloatKind:
		// Accept float64 whole numbers per spec
		f, fok := value.GetFloat64(val)
		if !fok {
			return typeMismatch("cannot convert %T to float64", val)
		}
		i, ok = value.GetInt64FromFloat(f)
		if !ok {
			if !value.IsFinite(f) {
				return constraintFail("expected integer, got non-finite float (NaN or Inf)")
			}
			return typeMismatch("expected integer, got float with fractional part: %v", f)
		}
	default:
		return typeMismatch("expected integer, got %T", val)
	}

	// Check bounds if available
	ic, ok := c.(schema.IntegerConstraint)
	if !ok {
		return nil // No bounds to check
	}

	if min, hasMin := ic.Min(); hasMin && i < min {
		return constraintFail("integer %d is less than minimum %d", i, min)
	}
	if max, hasMax := ic.Max(); hasMax && i > max {
		return constraintFail("integer %d exceeds maximum %d", i, max)
	}
	return nil
}

// checkFloat validates that val is a float or integer with optional bounds.
func (ch *Checker) checkFloat(val any, c schema.Constraint) error {
	kind, _ := value.ClassifyWithRegistry(ch.registry, val)
	if kind != value.FloatKind && kind != value.IntKind {
		return typeMismatch("expected float, got %T", val)
	}

	// Get float64 value - handle both float and integer types
	var f float64
	if fv, ok := value.GetFloat64(val); ok {
		f = fv
	} else if iv, ok := value.GetInt64(val); ok {
		f = float64(iv)
	} else {
		return typeMismatch("cannot convert %T to float64", val)
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

	if min, hasMin := fc.Min(); hasMin && f < min {
		return constraintFail("float %v is less than minimum %v", f, min)
	}
	if max, hasMax := fc.Max(); hasMax && f > max {
		return constraintFail("float %v exceeds maximum %v", f, max)
	}
	return nil
}

// checkBoolean validates that val is a boolean.
func checkBoolean(val any) error {
	if _, ok := val.(bool); ok {
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

	s, ok := val.(string)
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

	// Default: RFC 3339
	if _, err := time.Parse(time.RFC3339, s); err != nil {
		// Also try RFC3339Nano
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			return constraintFail("invalid timestamp format: %s", s)
		}
	}
	return nil
}

// checkDate validates that val is a valid date string (YYYY-MM-DD).
func checkDate(val any) error {
	s, ok := val.(string)
	if !ok {
		return typeMismatch("expected date string, got %T", val)
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
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

	s, ok := val.(string)
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
	s, ok := val.(string)
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
	s, ok := val.(string)
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
		if pattern != nil && !pattern.MatchString(s) {
			return constraintFail("value %q does not match pattern %s", s, patterns[i])
		}
	}
	return nil
}

// checkVector validates that val is a float slice with correct dimension.
func (ch *Checker) checkVector(val any, c schema.Constraint) error {
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

	// Check each element is numeric and finite
	for i, elem := range slice {
		kind, _ := value.ClassifyWithRegistry(ch.registry, elem)
		if kind != value.FloatKind && kind != value.IntKind {
			return typeMismatch("vector element [%d]: expected number, got %T", i, elem)
		}
		// Check for NaN/Inf in float elements (integers are always finite)
		if kind == value.FloatKind {
			if fv, ok := value.GetFloat64(elem); ok && !value.IsFinite(fv) {
				return constraintFail("vector element [%d]: value is not finite (NaN or Inf)", i)
			}
		}
	}
	return nil
}

// toSlice converts val to []any if it's a slice.
func toSlice(val any) ([]any, bool) {
	if val == nil {
		return nil, false
	}
	if slice, ok := val.([]any); ok {
		return slice, true
	}
	// Use reflection for typed slices
	rv := reflect.ValueOf(val)
	if rv.Kind() != reflect.Slice {
		return nil, false
	}
	result := make([]any, rv.Len())
	for i := range rv.Len() {
		result[i] = rv.Index(i).Interface()
	}
	return result, true
}

// TypeChecker is a function that checks if a value matches a type.
// Returns (true, "") if valid, or (false, message) with an error description.
type TypeChecker func(val any) (bool, string)

// CheckerFor returns a TypeChecker for the given constraint.
func CheckerFor(c schema.Constraint) TypeChecker {
	return func(val any) (bool, string) {
		if err := CheckValue(val, c); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
}

// IsString returns a TypeChecker that validates string values.
func IsString() TypeChecker {
	return func(val any) (bool, string) {
		if _, ok := val.(string); ok {
			return true, ""
		}
		return false, fmt.Sprintf("expected string, got %T", val)
	}
}

// IsInteger returns a TypeChecker that validates integer values.
// Accepts integer types and float64 whole numbers per spec.
func IsInteger() TypeChecker {
	return func(val any) (bool, string) {
		kind, _ := value.Classify(val)
		switch kind {
		case value.IntKind:
			return true, ""
		case value.FloatKind:
			if f, ok := value.GetFloat64(val); ok && value.IsWholeNumber(f) {
				return true, ""
			}
		}
		return false, fmt.Sprintf("expected integer, got %T", val)
	}
}

// IsFloat returns a TypeChecker that validates float values.
func IsFloat() TypeChecker {
	return func(val any) (bool, string) {
		kind, _ := value.Classify(val)
		if kind == value.FloatKind || kind == value.IntKind {
			return true, ""
		}
		return false, fmt.Sprintf("expected float, got %T", val)
	}
}

// IsBoolean returns a TypeChecker that validates boolean values.
func IsBoolean() TypeChecker {
	return func(val any) (bool, string) {
		if _, ok := val.(bool); ok {
			return true, ""
		}
		return false, fmt.Sprintf("expected boolean, got %T", val)
	}
}

// IsUUID returns a TypeChecker that validates UUID values.
func IsUUID() TypeChecker {
	return func(val any) (bool, string) {
		if err := checkUUID(val); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
}

// IsTimestamp returns a TypeChecker that validates timestamp values.
// Accepts time.Time (always valid) or string (parsed as RFC3339).
func IsTimestamp() TypeChecker {
	return func(val any) (bool, string) {
		// Accept time.Time directly
		if _, ok := val.(time.Time); ok {
			return true, ""
		}

		s, ok := val.(string)
		if !ok {
			return false, fmt.Sprintf("expected timestamp string or time.Time, got %T", val)
		}
		if _, err := time.Parse(time.RFC3339, s); err != nil {
			if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
				return false, "invalid timestamp format: " + s
			}
		}
		return true, ""
	}
}

// IsDate returns a TypeChecker that validates date values.
func IsDate() TypeChecker {
	return func(val any) (bool, string) {
		if err := checkDate(val); err != nil {
			return false, err.Error()
		}
		return true, ""
	}
}

// MatchesPattern returns a TypeChecker that validates values against a regex pattern.
func MatchesPattern(pattern *regexp.Regexp) TypeChecker {
	return func(val any) (bool, string) {
		s, ok := val.(string)
		if !ok {
			return false, fmt.Sprintf("expected string for pattern, got %T", val)
		}
		if !pattern.MatchString(s) {
			return false, fmt.Sprintf("value %q does not match pattern %s", s, pattern.String())
		}
		return true, ""
	}
}

// InEnum returns a TypeChecker that validates values are in the allowed set.
func InEnum(allowed []string) TypeChecker {
	return func(val any) (bool, string) {
		s, ok := val.(string)
		if !ok {
			return false, fmt.Sprintf("expected string for enum, got %T", val)
		}
		if slices.Contains(allowed, s) {
			return true, ""
		}
		return false, fmt.Sprintf("value %q not in enum %v", s, allowed)
	}
}
