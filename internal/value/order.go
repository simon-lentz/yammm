package value

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/immutable"
)

// sliceAccessor returns a SliceStrata value's length and an element reader,
// accepting both a Go slice and an [immutable.Slice], which reflect cannot
// index. Elements unwrap one level, so a nested collection stays wrapped for
// the recursive [Order] call rather than being deep-copied.
func sliceAccessor(v any) (int, func(int) any) {
	if s, ok := v.(immutable.Slice); ok {
		return s.Len(), func(i int) any {
			elem, ok := s.GetOK(i)
			if !ok {
				return nil
			}
			return elem.Unwrap()
		}
	}
	rv := reflect.ValueOf(v)
	return rv.Len(), func(i int) any { return rv.Index(i).Interface() }
}

// toStringComparable converts strings and regexps to comparable string values.
func toStringComparable(v any) (string, error) {
	switch s := v.(type) {
	case string:
		return s, nil
	case *regexp.Regexp:
		if s == nil {
			return "", errors.New("value: nil regexp")
		}
		return s.String(), nil
	}
	if s, ok := GetString(v); ok {
		return s, nil
	}
	return "", fmt.Errorf("value: expected string or regexp values, got %T", v)
}

// TypeOrder orders types canonically and returns 1 if left has a type greater than right, 0 if
// types are of the same strata, and -1 if left is less than right.
// The stratas are nil < bool < numbers < strings < slices. Unsupported types return an error.
func TypeOrder(left, right any) (int, error) {
	leftStrata := TypeStrata(left)
	rightStrata := TypeStrata(right)

	if leftStrata == InvalidStrata || rightStrata == InvalidStrata {
		return 0, fmt.Errorf("value: unsupported type comparison between %T and %T", left, right)
	}

	switch {
	case leftStrata > rightStrata:
		return 1, nil
	case leftStrata == rightStrata:
		return 0, nil
	default:
		return -1, nil
	}
}

type floatClass int

const (
	// Ordered low-to-high to keep Float64Compare deterministic for special values.
	floatClassNegInf floatClass = iota
	floatClassFinite
	floatClassPosInf
	floatClassNaN // sorts after all other float classes
)

func classifyFloat64(v float64) floatClass {
	switch {
	case math.IsNaN(v):
		return floatClassNaN
	case math.IsInf(v, -1):
		return floatClassNegInf
	case math.IsInf(v, 1):
		return floatClassPosInf
	default:
		return floatClassFinite
	}
}

// IsFinite reports whether f is a finite number (not NaN, +Inf, or -Inf).
func IsFinite(f float64) bool {
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// Order returns the canonical order of the two values, using TypeOrder to first
// determine the order of the type of the values. If values have the same type order they
// are compared taking data type into account. Unsupported types yield an error instead of
// panicking. For supported types, comparisons are antisymmetric and transitive (i.e., a total
// order over the supported set). Floats are ordered as -Inf < finite < +Inf < NaN (with
// NaN considered equal to NaN) to keep ordering deterministic. Maps, structs, and other
// complex shapes are intentionally out of scope; normalize them before ordering if you need
// stable comparisons.
func Order(left, right any) (int, error) {
	// If type order is different, no need to compare values.
	if to, err := TypeOrder(left, right); err != nil {
		return 0, err
	} else if to != 0 {
		return to, nil
	}
	switch TypeStrata(left) {
	case NilStrata:
		return 0, nil // the other value must also be nil
	case BoolStrata:
		lb, lbok := GetBool(left)
		rb, rbok := GetBool(right)
		if !lbok || !rbok {
			return 0, fmt.Errorf("value: expected boolean values (left %T, right %T)", left, right)
		}
		if lb == rb {
			return 0, nil
		}
		if lb {
			return 1, nil
		}
		return -1, nil
	case NumericStrata:
		if isWideInteger(left) || isWideInteger(right) {
			return compareExact(left, right)
		}
		li, liok := GetInt64(left)
		lu, luok := GetUint64(left)
		lf, lfok := GetFloat64(left)
		ri, riok := GetInt64(right)
		ru, ruok := GetUint64(right)
		rf, rfok := GetFloat64(right)

		switch {
		// Both signed integers
		case liok && riok:
			return Int64Compare(li, ri), nil

		// Both unsigned integers (handles values > MaxInt64)
		case luok && ruok:
			return Uint64Compare(lu, ru), nil

		// Mixed signed/unsigned: negative signed is always less than unsigned
		case liok && ruok:
			if li < 0 {
				return -1, nil
			}
			// Non-negative signed can safely compare as uint64
			return Uint64Compare(uint64(li), ru), nil
		case luok && riok:
			if ri < 0 {
				return 1, nil
			}
			return Uint64Compare(lu, uint64(ri)), nil

		// Float vs signed integer: exact comparison (preserves transitivity for values > 2^53)
		case lfok && riok:
			return -CompareInt64Float64(ri, lf), nil
		case liok && rfok:
			return CompareInt64Float64(li, rf), nil

		// Float vs unsigned integer: exact comparison (preserves transitivity for values > 2^53)
		case lfok && ruok:
			return -CompareUint64Float64(ru, lf), nil
		case luok && rfok:
			return CompareUint64Float64(lu, rf), nil

		// Both floats. Last, because a json.Number holding an integer answers
		// GetFloat64 too: taking this arm for it rounds 2^64-1 up to 2^64 and
		// the order lies at every power of two above 2^53.
		case lfok && rfok:
			return Float64Compare(lf, rf), nil
		}
		return 0, fmt.Errorf("value: expected numeric values (left %T, right %T)", left, right)

	case StringStrata:
		ls, err := toStringComparable(left)
		if err != nil {
			return 0, err
		}
		rs, err := toStringComparable(right)
		if err != nil {
			return 0, err
		}
		return strings.Compare(ls, rs), nil

	case SliceStrata:
		leftLen, leftAt := sliceAccessor(left)
		rightLen, rightAt := sliceAccessor(right)
		minLen := min(leftLen, rightLen)
		for i := range minLen {
			leftVal := leftAt(i)
			rightVal := rightAt(i)
			which, err := Order(leftVal, rightVal)
			if err != nil {
				return 0, err
			}
			if which == 0 {
				continue
			}
			return which, nil
		}
		// Equal up to same length, the shorter is smaller
		if leftLen == rightLen {
			return 0, nil
		}
		if leftLen > rightLen {
			return 1, nil
		}
		return -1, nil
	}
	return 0, fmt.Errorf("value: unknown strata for comparison between %T and %T", left, right)
}

// isWideInteger reports whether v is a json.Number integer literal, read as
// [Scalar] reads it, that neither int64 nor uint64 holds. Its nearest float64
// can be a different integer, so [Order] compares it exactly rather than
// through that float.
func isWideInteger(v any) bool {
	_, ok := wideInteger(v)
	return ok
}

// wideInteger returns the literal [isWideInteger] reports.
func wideInteger(v any) (json.Number, bool) {
	n, ok := NumberLiteral(v)
	if !ok || !immutable.IsIntegerLiteral(n) {
		return "", false
	}
	_, err := strconv.ParseInt(string(n), 10, 64)
	if !errors.Is(err, strconv.ErrRange) {
		return "", false
	}
	_, err = strconv.ParseUint(string(n), 10, 64)
	return n, err != nil
}

// compareExact orders two numbers of which one is a wide integer, as rationals.
// A float operand keeps its place in the float order: -Inf below every number,
// +Inf above, and NaN above +Inf.
func compareExact(left, right any) (int, error) {
	l, lrank, err := exactOf(left)
	if err != nil {
		return 0, err
	}
	r, rrank, err := exactOf(right)
	if err != nil {
		return 0, err
	}
	// One operand is wide and ranks 0, so equal ranks mean two rationals.
	if lrank != rrank {
		return cmp.Compare(lrank, rrank), nil
	}
	return l.Cmp(r), nil
}

// exactOf returns v as a rational, or nil with a rank for a non-finite float:
// -1 for -Inf, 1 for +Inf, 2 for NaN; a finite number ranks 0.
func exactOf(v any) (*big.Rat, int, error) {
	if n, ok := wideInteger(v); ok {
		// A JSON integer literal is decimal digits after an optional '-', which
		// SetString reads.
		r, _ := new(big.Rat).SetString(string(n))
		return r, 0, nil
	}
	if i, ok := GetInt64(v); ok {
		return new(big.Rat).SetInt64(i), 0, nil
	}
	if u, ok := GetUint64(v); ok {
		return new(big.Rat).SetUint64(u), 0, nil
	}
	if f, ok := GetFloat64(v); ok {
		switch {
		case math.IsNaN(f):
			return nil, 2, nil
		case math.IsInf(f, -1):
			return nil, -1, nil
		case math.IsInf(f, 1):
			return nil, 1, nil
		}
		return new(big.Rat).SetFloat64(f), 0, nil
	}
	return nil, 0, fmt.Errorf("value: expected numeric value, got %T", v)
}

// GetInt64 extracts an int64 from any integer type.
// Returns (value, true) if the input is any signed or unsigned integer type.
// Returns (0, false) if the input is not an integer type or if unsigned values
// exceed math.MaxInt64 (to prevent silent overflow).
//
// Supported types: int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, uintptr.
func GetInt64(val any) (int64, bool) {
	switch x := val.(type) {
	// Signed integers
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	// Unsigned integers with overflow checking
	case uint:
		// Use uint64 comparison for 32-bit architecture portability.
		n64 := uint64(x)
		if n64 > uint64(math.MaxInt64) {
			return 0, false
		}
		return int64(n64), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		if x > math.MaxInt64 {
			return 0, false
		}
		return int64(x), true
	case uintptr:
		if uint64(x) > uint64(math.MaxInt64) {
			return 0, false
		}
		return int64(x), true
	}
	// Floats stay rejected here: the caller distinguishes an integer from a
	// float by which getter answers.
	rv, ok := walk(val)
	if !ok || !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if u := rv.Uint(); u <= math.MaxInt64 {
			return int64(u), true
		}
		return 0, false
	}
	if n, ok := literal(rv); ok {
		i, err := n.Int64()
		return i, err == nil
	}
	return 0, false
}

// maxPointerDepth bounds how many pointers and interfaces [walk] follows, so no
// reader of the value layer follows a pointer that points to itself forever.
const maxPointerDepth = 64

// GetFloat64 extracts a float64 from any float type.
// Returns (value, true) if the input is float32 or float64.
// Returns (0, false) for non-float types.
func GetFloat64(val any) (float64, bool) {
	switch x := val.(type) {
	case float32:
		return float64(x), true
	case float64:
		return x, true
	}
	rv, ok := walk(val)
	if !ok || !rv.IsValid() {
		return 0, false
	}
	if k := rv.Kind(); k == reflect.Float32 || k == reflect.Float64 {
		return rv.Float(), true
	}
	if n, ok := literal(rv); ok {
		f, err := n.Float64()
		return f, err == nil
	}
	return 0, false
}

// GetBool extracts a bool, accepting a pointer, an interface or a named type
// over bool so it agrees with the BoolStrata category TypeStrata assigns them.
func GetBool(val any) (bool, bool) {
	if b, ok := val.(bool); ok {
		return b, true
	}
	rv, ok := walk(val)
	if !ok || !rv.IsValid() || rv.Kind() != reflect.Bool {
		return false, false
	}
	return rv.Bool(), true
}

// GetString extracts a string, accepting a pointer, an interface or a named
// type over string. It rejects *regexp.Regexp, which toStringComparable accepts
// for ordering but which is not a value any string-kinded constraint holds, and
// a json.Number behind any carrier, which is a named type over string but a
// number to every other getter here.
func GetString(val any) (string, bool) {
	if s, ok := val.(string); ok {
		return s, true
	}
	rv, ok := walk(val)
	if !ok || !rv.IsValid() || rv.Kind() != reflect.String || rv.Type() == numberType {
		return "", false
	}
	return rv.String(), true
}

// GetUint64 extracts a uint64 from any unsigned integer type, or from a
// json.Number whose text is a JSON number literal spelling a non-negative
// integer within uint64, each read through its carriers as [Scalar] reads it.
// Returns (0, false) for every other value.
//
// Unlike GetInt64, this function does not reject any valid unsigned values.
// All uint64 values (including those > math.MaxInt64) are supported.
func GetUint64(val any) (uint64, bool) {
	switch x := val.(type) {
	case uint:
		return uint64(x), true
	case uint8:
		return uint64(x), true
	case uint16:
		return uint64(x), true
	case uint32:
		return uint64(x), true
	case uint64:
		return x, true
	case uintptr:
		return uint64(x), true
	}
	rv, ok := walk(val)
	if !ok || !rv.IsValid() {
		return 0, false
	}
	switch rv.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint(), true
	}
	if n, ok := literal(rv); ok {
		u, err := strconv.ParseUint(string(n), 10, 64)
		return u, err == nil
	}
	return 0, false
}

// Int64Compare compares two int64 values and returns -1, 0, or +1.
func Int64Compare(left, right int64) int {
	return cmp.Compare(left, right)
}

// Uint64Compare compares two uint64 values and returns -1, 0, or +1.
func Uint64Compare(left, right uint64) int {
	return cmp.Compare(left, right)
}

// Float64Compare compares two float64 values and returns -1, 0, or +1.
// Special values are ordered as:
// -Inf < finite < +Inf < NaN (NaN equals NaN) to keep comparisons antisymmetric.
func Float64Compare(left, right float64) int {
	leftClass := classifyFloat64(left)
	rightClass := classifyFloat64(right)

	if leftClass != floatClassFinite || rightClass != floatClassFinite {
		return cmp.Compare(leftClass, rightClass)
	}
	return cmp.Compare(left, right)
}

// CompareInt64Float64 compares an int64 with a float64 exactly, without precision loss.
// This preserves transitivity for values > 2^53 by converting the float to int64
// (not vice versa), since float64 can only represent integers exactly up to 2^53.
//
// Returns -1 if i < f, 0 if i == f, 1 if i > f.
// Special values are ordered: -Inf < finite < +Inf < NaN (i < NaN for any integer i).
func CompareInt64Float64(i int64, f float64) int {
	// Handle special float values per our ordering
	switch classifyFloat64(f) {
	case floatClassNegInf:
		return 1 // i > -Inf
	case floatClassPosInf:
		return -1 // i < +Inf
	case floatClassNaN:
		return -1 // i < NaN
	}

	// f is finite

	// Check if f has a fractional part
	trunc, frac := math.Modf(f)

	if frac != 0 {
		// f is not a whole number: compare i with the truncated value. A
		// float64 with a fractional part has magnitude below 2^52, so trunc
		// is always within int64.
		fi := int64(trunc)
		if i < fi {
			return -1
		}
		if i > fi {
			return 1
		}
		// i == trunc(f)
		// f = trunc + frac, where frac has the same sign as f
		// if f > 0: frac > 0, so f > trunc = i, thus i < f
		// if f < 0: frac < 0, so f < trunc = i, thus i > f
		if frac > 0 {
			return -1
		}
		return 1
	}

	// f is mathematically a whole number (frac == 0)
	// trunc is the exact integer value f represents

	// Range check: is f within int64 bounds?
	// Note: float64(MaxInt64) rounds UP to 2^63, so we need f < 2^63
	// Note: float64(MinInt64) is exactly -2^63
	const maxInt64AsFloat = float64(1 << 63)  // 2^63
	const minInt64AsFloat = -float64(1 << 63) // -2^63

	if f >= maxInt64AsFloat {
		return -1 // f >= 2^63 > MaxInt64
	}
	if f < minInt64AsFloat {
		return 1 // f < -2^63 = MinInt64
	}

	// Safe to convert f to int64 and compare exactly
	fi := int64(f)
	return Int64Compare(i, fi)
}

// CompareUint64Float64 compares a uint64 with a float64 exactly, without precision loss.
// This preserves transitivity for values > 2^53 by converting the float to uint64
// (not vice versa), since float64 can only represent integers exactly up to 2^53.
//
// Returns -1 if u < f, 0 if u == f, 1 if u > f.
// Special values are ordered: -Inf < finite < +Inf < NaN (u < NaN for any unsigned u).
func CompareUint64Float64(u uint64, f float64) int {
	// Handle special float values per our ordering
	switch classifyFloat64(f) {
	case floatClassNegInf:
		return 1 // u > -Inf (u >= 0)
	case floatClassPosInf:
		return -1 // u < +Inf
	case floatClassNaN:
		return -1 // u < NaN
	}

	// f is finite

	// If f is negative, u is greater (u >= 0)
	if f < 0 {
		return 1
	}

	// f >= 0

	// Check if f has a fractional part
	trunc, frac := math.Modf(f)

	// 2^64 as float64 (exactly representable)
	const maxUint64AsFloat = float64(1<<63) * 2

	if frac != 0 {
		// f is not a whole number - compare u with floor(f)
		if trunc >= maxUint64AsFloat {
			return -1 // floor(f) >= 2^64 > any uint64
		}

		fu := uint64(trunc)
		if u < fu {
			return -1
		}
		if u > fu {
			return 1
		}
		// u == floor(f), and frac > 0 (since f >= 0), so u < f
		return -1
	}

	// f is a non-negative whole number
	if f >= maxUint64AsFloat {
		return -1 // f >= 2^64 > any uint64
	}

	// Safe to convert f to uint64 and compare exactly
	fu := uint64(f)
	return Uint64Compare(u, fu)
}
