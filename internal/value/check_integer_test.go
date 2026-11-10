package value_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// checkFloat accepts every unsigned value coerceFloat converts: a uint64
// above math.MaxInt64 is a finite float, not a type mismatch, and the largest
// uintptr is accepted whatever its width.
func TestCheckFloat_AcceptsUnsignedAboveInt64(t *testing.T) {
	t.Parallel()
	c := schema.NewFloatConstraint()
	for name, tc := range map[string]struct {
		val  any
		want float64
	}{
		"uint64":  {uint64(1<<63 + 5), float64(1<<63 + 5)},
		"uintptr": {^uintptr(0), float64(^uintptr(0))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := value.Check(tc.val, c); err != nil {
				t.Fatalf("Check(%T) = %v, want nil", tc.val, err)
			}
			got, err := value.Coerce(tc.val, c)
			if err != nil || got != tc.want {
				t.Errorf("Coerce(%T) = %v, %v; want %v", tc.val, got, err, tc.want)
			}
		})
	}
}

// A float at an Integer is a type mismatch whatever it holds: whole, outside
// the int64 range or not finite. The message names the float.
func TestCheckInteger_AFloatIsATypeMismatch(t *testing.T) {
	t.Parallel()
	for _, f := range []float64{1e19, 3, math.Inf(1)} {
		err := value.Check(f, schema.NewIntegerConstraint())
		var ce *value.CheckError
		if !errors.As(err, &ce) {
			t.Fatalf("Check(%v) err = %v, want a *CheckError", f, err)
		}
		if ce.Kind != value.KindTypeMismatch || !strings.Contains(ce.Msg, "got float") {
			t.Errorf("Check(%v): kind=%v msg=%q; want KindTypeMismatch naming the float", f, ce.Kind, ce.Msg)
		}
	}
}

// An integer literal no int64 holds is a type mismatch at an Integer, with
// the check and the coercion agreeing. Its nearest float64 is not a stand-in:
// for -9223372036854775809 that float is exactly -2^63, a different integer.
func TestIntegerLiteralOutsideInt64_IsATypeMismatchAtAnInteger(t *testing.T) {
	t.Parallel()
	for _, n := range []json.Number{"9223372036854775808", "-9223372036854775809", "-9223372036854776832"} {
		t.Run(string(n), func(t *testing.T) {
			t.Parallel()
			err := value.Check(n, schema.NewIntegerConstraint())
			var ce *value.CheckError
			if !errors.As(err, &ce) {
				t.Fatalf("Check err = %v, want a *CheckError", err)
			}
			if ce.Kind != value.KindTypeMismatch || !strings.Contains(ce.Msg, "outside the int64 range") {
				t.Errorf("kind=%v msg=%q; want KindTypeMismatch naming the range", ce.Kind, ce.Msg)
			}
			if got, err := value.Coerce(n, schema.NewIntegerConstraint()); err == nil {
				t.Errorf("Coerce(%s) = %#v, want an error", n, got)
			}
		})
	}
}

// An integer literal no float64 holds is refused at a Vector element by the
// check, as the coercion refuses it, and the message says why. One a float64
// holds is read as its nearest float64.
func TestIntegerLiteralNoFloatHolds_IsRefusedAtAVectorElement(t *testing.T) {
	t.Parallel()
	huge := json.Number("1" + strings.Repeat("0", 400))
	c := schema.NewVectorConstraint(2)
	err := value.Check([]any{huge, int64(1)}, c)
	var ce *value.CheckError
	if !errors.As(err, &ce) {
		t.Fatalf("Check err = %v, want a *CheckError", err)
	}
	const want = "vector element [0]: expected float, got a number no float64 holds: 10000000000000000000000000000000… (401 bytes)"
	if ce.Kind != value.KindTypeMismatch || ce.Msg != want {
		t.Errorf("Check: kind=%v msg=%q; want KindTypeMismatch %q", ce.Kind, ce.Msg, want)
	}
	if _, err := value.Coerce([]any{huge, int64(1)}, c); err == nil {
		t.Error("Coerce accepted a vector element no float64 holds")
	}
	big := json.Number("99999999999999999999")
	if err := value.Check([]any{big, int64(1)}, c); err != nil {
		t.Errorf("Check refused an integer a float64 holds: %v", err)
	}
	if _, err := value.Coerce([]any{big, int64(1)}, c); err != nil {
		t.Errorf("Coerce refused an integer a float64 holds: %v", err)
	}
}

// Only an integer literal outside int64 is named as out of range. A json.Number
// that is no number literal is named as one, and a decimal or exponent
// literal is a float however long its digit run, although strconv reports
// ErrRange for the run before it reads the '.' or 'e'.
func TestCoerceInteger_OnlyAnIntegerLiteralIsCalledOutOfRange(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		n    json.Number
		want string
	}{
		{"abc", `cannot coerce json.Number "abc" to int64: no JSON number literal`},
		{"20000000000000000000e-10", "cannot coerce float 2000000000.0 to int64"},
		{"100000000000000000000.5", "cannot coerce float 100000000000000000000.0 to int64"},
		{"18446744073709551616e-18", "cannot coerce float 18.446744073709553 to int64"},
	} {
		_, err := value.Coerce(c.n, schema.NewIntegerConstraint())
		if err == nil || err.Error() != c.want {
			t.Errorf("Coerce(%q) = %v, want %q", c.n, err, c.want)
		}
		if cerr := value.Check(c.n, schema.NewIntegerConstraint()); cerr == nil || strings.Contains(cerr.Error(), "outside the int64 range") {
			t.Errorf("Check(%q) = %v, want a refusal that names no range", c.n, cerr)
		}
	}
}

// An integer literal's message quotes a bounded prefix of it, so a literal of a
// megabyte does not write a megabyte into every diagnostic.
func TestIntegerLiteralOutsideInt64_MessageIsBounded(t *testing.T) {
	t.Parallel()
	n := json.Number("9" + strings.Repeat("0", 100000))
	err := value.Check(n, schema.NewIntegerConstraint())
	const want = "expected integer, got an integer outside the int64 range: 90000000000000000000000000000000… (100001 bytes)"
	if err == nil || err.Error() != want {
		t.Errorf("Check = %v, want %q", err, want)
	}
	if _, cerr := value.Coerce(n, schema.NewIntegerConstraint()); cerr == nil || len(cerr.Error()) > 200 {
		t.Errorf("Coerce error is %d bytes, want a bounded message", len(fmt.Sprint(cerr)))
	}
}

// At a Float the same literal is its nearest float64, as the check and the
// coercion both read it.
func TestIntegerLiteralOutsideInt64_IsItsNearestFloatAtAFloat(t *testing.T) {
	t.Parallel()
	n := json.Number("99999999999999999999")
	if err := value.Check(n, schema.NewFloatConstraint()); err != nil {
		t.Fatalf("Check: %v", err)
	}
	got, err := value.Coerce(n, schema.NewFloatConstraint())
	if err != nil {
		t.Fatalf("Coerce: %v", err)
	}
	if got != float64(1e20) {
		t.Errorf("Coerce = %#v, want float64(1e20)", got)
	}
}

// The check and the coercion agree on a number literal: an integer literal at
// an Integer is the int64 it spells, the literal -0 at a Float keeps its sign,
// and a fraction at an Integer is named a float by both.
func TestNumberLiteral_CheckAndCoerceAgree(t *testing.T) {
	t.Parallel()
	five := json.Number("5")
	if err := value.Check(five, schema.NewIntegerConstraint()); err != nil {
		t.Errorf("Check(5) = %v, want nil", err)
	}
	if got, err := value.Coerce(five, schema.NewIntegerConstraint()); err != nil || got != int64(5) {
		t.Errorf("Coerce(5) = %#v, %v; want int64(5)", got, err)
	}
	got, err := value.Coerce(json.Number("-0"), schema.NewFloatConstraint())
	if f, ok := got.(float64); err != nil || !ok || !math.Signbit(f) {
		t.Errorf("Coerce(-0) = %#v, %v; want the float64 -0", got, err)
	}
	half := json.Number("2.5")
	if err := value.Check(half, schema.NewIntegerConstraint()); err == nil || err.Error() != "expected integer, got float 2.5" {
		t.Errorf("Check(2.5) = %v", err)
	}
	if _, err := value.Coerce(half, schema.NewIntegerConstraint()); err == nil || err.Error() != "cannot coerce float 2.5 to int64" {
		t.Errorf("Coerce(2.5) = %v", err)
	}
}

// A NaN or an infinity a caller spells as a json.Number is refused by the
// coercion as the check refuses it, at a Float and at a Vector element.
func TestNonFiniteNumberLiteral_IsRefusedByCheckAndCoerce(t *testing.T) {
	t.Parallel()
	for _, n := range []json.Number{"NaN", "Inf", "-Infinity"} {
		if err := value.Check(n, schema.NewFloatConstraint()); err == nil {
			t.Errorf("Check(%q) accepted", n)
		}
		if got, err := value.Coerce(n, schema.NewFloatConstraint()); err == nil {
			t.Errorf("Coerce(%q) = %v, want an error", n, got)
		}
		if got, err := value.Coerce([]any{n, json.Number("1")}, schema.NewVectorConstraint(2)); err == nil {
			t.Errorf("Coerce([%q 1], Vector[2]) = %v, want an error", n, got)
		}
	}
	if _, err := value.Coerce(json.Number("0x1q"), schema.NewFloatConstraint()); err == nil || strings.Contains(err.Error(), "no finite float64") {
		t.Errorf("a malformed literal drew %v, want a refusal that names no range", err)
	}
}

// Each message names the fact in the form it is written: an uppercase exponent
// after a long digit run is a float, an exponent form takes no ".0", a float32
// keeps its 32-bit digits, a literal of exactly the bound is quoted whole, an
// unsigned value above MaxInt64 names the bound, named or not, and a wide
// literal is named out of range by the coercion as by the check.
func TestIntegerMessages_NameTheFactAsWritten(t *testing.T) {
	t.Parallel()
	ic := schema.NewIntegerConstraint()
	msg := func(err error) string {
		if err == nil {
			return "<nil>"
		}
		return err.Error()
	}
	if _, err := value.Coerce(json.Number("20000000000000000000E-10"), ic); msg(err) != "cannot coerce float 2000000000.0 to int64" {
		t.Errorf("uppercase exponent: %v", err)
	}
	if err := value.Check(1e21, ic); msg(err) != "expected integer, got float 1e+21" {
		t.Errorf("exponent form: %v", err)
	}
	if err := value.Check(float32(0.1), ic); msg(err) != "expected integer, got float 0.1" {
		t.Errorf("float32: %v", err)
	}
	if err := value.Check(namedFloat32(1.1), ic); msg(err) != "expected integer, got float 1.1" {
		t.Errorf("named float32: %v", err)
	}
	for _, v := range []any{uint64(1 << 63), namedUint(1 << 63)} {
		if err := value.Check(v, ic); msg(err) != "expected integer, got an integer outside the int64 range: 9223372036854775808" {
			t.Errorf("%T above MaxInt64: %v", v, err)
		}
	}
	n32 := json.Number("1" + strings.Repeat("0", 31))
	if err := value.Check(n32, ic); msg(err) != "expected integer, got an integer outside the int64 range: "+string(n32) {
		t.Errorf("32-byte literal: %v", err)
	}
	if _, err := value.Coerce(uint64(1<<63), ic); msg(err) != "cannot coerce integer 9223372036854775808 outside the int64 range to int64" {
		t.Errorf("uint64: %v", err)
	}
	if _, err := value.Coerce(json.Number("1"+strings.Repeat("0", 400)), ic); !strings.Contains(msg(err), "outside the int64 range") {
		t.Errorf("wide literal: %v", err)
	}
}
