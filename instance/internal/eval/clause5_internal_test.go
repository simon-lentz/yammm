package eval

import (
	"reflect"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
)

// The three readers of a list position read every list shape alike, through
// value.ListElems: asSlice keeps the builtins' nil-is-empty rule in front of
// it, toSlice refuses nil, and both read an immutable.Slice and an array.
func TestListReaders_AgreeOnEveryShape(t *testing.T) {
	t.Parallel()
	s := immutable.Wrap([]any{int64(1), "a"}).Unwrap()
	for _, tc := range []struct {
		name string
		in   any
		want []any
		ok   bool
	}{
		{"immutable.Slice", s, []any{int64(1), "a"}, true},
		{"array", [2]int64{1, 2}, []any{int64(1), int64(2)}, true},
		{"typed slice", []string{"x"}, []any{"x"}, true},
		{"scalar", "x", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := toSlice(tc.in)
			if ok != tc.ok || (ok && !reflect.DeepEqual(got, tc.want)) {
				t.Errorf("toSlice(%T) = %#v, %v; want %#v, %v", tc.in, got, ok, tc.want, tc.ok)
			}
			got2, err := asSlice("T", tc.in)
			if (err == nil) != tc.ok || (tc.ok && !reflect.DeepEqual(got2, tc.want)) {
				t.Errorf("asSlice(%T) = %#v, %v; want %#v, ok=%v", tc.in, got2, err, tc.want, tc.ok)
			}
		})
	}
	if got, err := asSlice("T", nil); err != nil || len(got) != 0 {
		t.Errorf("asSlice(nil) = %#v, %v; want an empty list", got, err)
	}
	if _, ok := toSlice(nil); ok {
		t.Error("toSlice(nil) = ok, want not a list")
	}
}

// Every datatype check expr.IsDatatypeCheck admits has a checker, and a name
// it does not admit is an error: no name falls through to another kind's
// checker by default.
func TestDatatypeChecker_CoversTheSetAndRefusesTheRest(t *testing.T) {
	t.Parallel()
	e := NewEvaluator()
	for _, name := range []string{"String", "Integer", "Float", "Boolean", "UUID", "Timestamp", "Date", "date", "DATE"} {
		if c, err := e.datatypeChecker(name); err != nil || c == nil {
			t.Errorf("datatypeChecker(%q) = %v, %v; want a checker", name, c, err)
		}
	}
	for _, name := range []string{"Vector", "List", "Enum", "Pattern", "number", ""} {
		if _, err := e.datatypeChecker(name); err == nil {
			t.Errorf("datatypeChecker(%q) = nil error, want unknown datatype", name)
		}
	}
	// A String check must not answer as a Date check would.
	c, _ := e.datatypeChecker("String")
	if ok, _ := c(int64(5)); ok {
		t.Error("the String checker accepted an integer")
	}
}
