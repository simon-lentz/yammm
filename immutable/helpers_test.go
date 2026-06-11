package immutable

import "testing"

// wantString asserts v holds a string equal to want.
func wantString(t *testing.T, v Value, want string) {
	t.Helper()
	s, ok := v.String()
	if !ok || s != want {
		t.Errorf("String() = %q, %v; want %q, true", s, ok, want)
	}
}

// wantInt asserts v holds an integer equal to want.
func wantInt(t *testing.T, v Value, want int64) {
	t.Helper()
	n, ok := v.Int()
	if !ok || n != want {
		t.Errorf("Int() = %d, %v; want %d, true", n, ok, want)
	}
}

// wantBool asserts v holds a bool equal to want.
func wantBool(t *testing.T, v Value, want bool) {
	t.Helper()
	b, ok := v.Bool()
	if !ok || b != want {
		t.Errorf("Bool() = %v, %v; want %v, true", b, ok, want)
	}
}

// wantNil asserts v is nil-valued.
func wantNil(t *testing.T, v Value) {
	t.Helper()
	if !v.IsNil() {
		t.Errorf("IsNil() = false; want true (got %v)", v.Unwrap())
	}
}
