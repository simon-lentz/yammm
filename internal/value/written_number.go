package value

import (
	"bytes"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// AppendWrittenFloat appends f as a snapshot writes a float64: encoding/json's
// form, then ".0" when that form holds no float indicator, so a reader reads a
// float back. f is finite.
func AppendWrittenFloat(b []byte, f float64) []byte {
	start := len(b)
	b = appendJSONFloat(b, f, 64)
	if !bytes.ContainsAny(b[start:], ".eE") {
		b = append(b, '.', '0')
	}
	return b
}

// appendJSONFloat appends f as encoding/json's float encoder writes a float of
// the given bit size: 'f' form, and 'e' form with a one-digit negative exponent
// unpadded below 1e-6 and from 1e21.
func appendJSONFloat(b []byte, f float64, bits int) []byte {
	abs := math.Abs(f)
	format := byte('f')
	if abs != 0 {
		if bits == 64 && (abs < 1e-6 || abs >= 1e21) ||
			bits == 32 && (float32(abs) < 1e-6 || float32(abs) >= 1e21) {
			format = 'e'
		}
	}
	b = strconv.AppendFloat(b, f, format, -1, bits)
	if format == 'e' {
		if n := len(b); n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b
}

// writtenNumber returns the text a snapshot writes for v, a number, and false
// for a float that is not finite. A literal is written as held, a float64 as
// [AppendWrittenFloat] writes it, and any other number as encoding/json does.
func writtenNumber(v any) (string, bool) {
	if n, ok := NumberLiteral(v); ok {
		return string(n), true
	}
	if f, ok := v.(float64); ok {
		if !IsFinite(f) {
			return "", false
		}
		return string(AppendWrittenFloat(nil, f)), true
	}
	rv := reflect.ValueOf(v)
	bits := 64
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(rv.Uint(), 10), true
	case reflect.Float32:
		bits = 32
	case reflect.Float64:
	default:
		return "", false
	}
	if f := rv.Float(); IsFinite(f) {
		return string(appendJSONFloat(nil, f, bits)), true
	}
	return "", false
}

// orderStoredNumbers orders two numbers by the exact value of the text a
// snapshot writes for each, then by that text, so 1 ranks before 1.0 and -0.0
// before 0. A float that is not finite keeps its place in [orderNumbers]' order.
func orderStoredNumbers(left, right any) (int, error) {
	lw, lok := writtenNumber(left)
	rw, rok := writtenNumber(right)
	if !lok || !rok {
		return orderNumbers(left, right)
	}
	if c := compareDecimal(lw, rw); c != 0 {
		return c, nil
	}
	return strings.Compare(lw, rw), nil
}
