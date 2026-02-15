package value

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
)

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
	default:
		return "", fmt.Errorf("value: expected string or regexp values, got %T", v)
	}
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

// ValueOrder returns the canonical order of the two values, using TypeOrder to first
// determine the order of the type of the values. If values have the same type order they
// are compared taking data type into account. Unsupported types yield an error instead of
// panicking. For supported types, comparisons are antisymmetric and transitive (i.e., a total
// order over the supported set). Floats are ordered as -Inf < finite < +Inf < NaN (with
// NaN considered equal to NaN) to keep ordering deterministic. Maps, structs, and other
// complex shapes are intentionally out of scope; normalize them before ordering if you need
// stable comparisons.
func ValueOrder(left, right any) (int, error) {
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
		lb, lbok := left.(bool)
		rb, rbok := right.(bool)
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

		// Both floats
		case lfok && rfok:
			return Float64Compare(lf, rf), nil

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
		leftVo := reflect.ValueOf(left)
		leftLen := leftVo.Len()
		rightVo := reflect.ValueOf(right)
		rightLen := rightVo.Len()
		minLen := Min(leftLen, rightLen)
		for i := range minLen {
			leftVal := leftVo.Index(i).Interface()
			rightVal := rightVo.Index(i).Interface()
			which, err := ValueOrder(leftVal, rightVal)
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

// Less returns true when left is strictly less than right according to ValueOrder. This is a
// convenience for wiring canonical ordering into sort helpers; callers must handle the returned
// error for unsupported inputs.
func Less(left, right any) (bool, error) {
	cmp, err := ValueOrder(left, right)
	if err != nil {
		return false, err
	}
	return cmp < 0, nil
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
	return 0, false
}

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
	return 0, false
}

// IsWholeNumber reports whether f is a finite float64 that represents
// a whole number within int64 range. Used for Integer constraint coercion.
//
// Returns true if:
//   - f is finite (not NaN, not Inf)
//   - math.Trunc(f) == f (no fractional part)
//   - f is within [MinInt64, MaxInt64) range
func IsWholeNumber(f float64) bool {
	if !IsFinite(f) {
		return false
	}
	if math.Trunc(f) != f {
		return false
	}
	// Check int64 bounds.
	// Note: float64(MaxInt64) rounds UP to 2^63, so we need f < 2^63.
	// Note: float64(MinInt64) is exactly -2^63.
	const maxInt64AsFloat = float64(1 << 63)  // 2^63
	const minInt64AsFloat = -float64(1 << 63) // -2^63
	return f >= minInt64AsFloat && f < maxInt64AsFloat
}

// GetInt64FromFloat extracts an int64 from a float64 that represents a whole number.
// Returns (value, true) if f is a finite whole number within int64 range.
// Returns (0, false) otherwise (fractional, NaN, Inf, or out of range).
func GetInt64FromFloat(f float64) (int64, bool) {
	if !IsWholeNumber(f) {
		return 0, false
	}
	return int64(f), true
}

// GetUint64 extracts a uint64 from any unsigned integer type.
// Returns (value, true) if the input is any unsigned integer type.
// Returns (0, false) for non-unsigned types.
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
	return 0, false
}

// Int64Compare compares two int64 values and returns 1 if left > right,
// 0 if equal, and -1 if left < right.
func Int64Compare(left, right int64) int {
	if left == right {
		return 0
	}
	if left > right {
		return 1
	}
	return -1
}

// Uint64Compare compares two uint64 values and returns 1 if left > right,
// 0 if equal, and -1 if left < right.
func Uint64Compare(left, right uint64) int {
	if left == right {
		return 0
	}
	if left > right {
		return 1
	}
	return -1
}

// Float64Compare compares two float64 values and returns 1 if left > right,
// 0 if equal, and -1 if left < right. Special values are ordered as:
// -Inf < finite < +Inf < NaN (NaN equals NaN) to keep comparisons antisymmetric.
func Float64Compare(left, right float64) int {
	leftClass := classifyFloat64(left)
	rightClass := classifyFloat64(right)

	if leftClass != floatClassFinite || rightClass != floatClassFinite {
		if leftClass == rightClass {
			return 0
		}
		if leftClass < rightClass {
			return -1
		}
		return 1
	}

	if left == right {
		return 0
	}
	if left > right {
		return 1
	}
	return -1
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
		// f is not a whole number - compare i with the truncated value
		// Range check for trunc
		if trunc > float64(math.MaxInt64) {
			return -1 // trunc > MaxInt64, so f > any int64
		}
		if trunc < float64(math.MinInt64) {
			return 1 // trunc < MinInt64, so f < any int64
		}

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
