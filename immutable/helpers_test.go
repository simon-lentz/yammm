package immutable

import "testing"

// wantString asserts v holds a string equal to want.
func wantString(t *testing.T, v Value, want string) {
	t.Helper()
	s, ok := v.AsString()
	if !ok || s != want {
		t.Errorf("AsString() = %q, %v; want %q, true", s, ok, want)
	}
}

// wantInt asserts v holds an int64 equal to want.
func wantInt(t *testing.T, v Value, want int64) {
	t.Helper()
	n, ok := v.AsInt()
	if !ok || n != want {
		t.Errorf("AsInt() = %d, %v; want %d, true", n, ok, want)
	}
}

// wantBool asserts v holds a bool equal to want.
func wantBool(t *testing.T, v Value, want bool) {
	t.Helper()
	b, ok := v.AsBool()
	if !ok || b != want {
		t.Errorf("AsBool() = %v, %v; want %v, true", b, ok, want)
	}
}

// wantNil asserts v holds literal nil.
func wantNil(t *testing.T, v Value) {
	t.Helper()
	if v.Any() != nil {
		t.Errorf("Any() = %#v; want nil", v.Any())
	}
}
