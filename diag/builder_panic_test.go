package diag

import (
	"strings"
	"testing"
)

// wantPanic runs call and asserts it panicked with a message naming what.
func wantPanic(t *testing.T, what string, call func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("no panic; %s is a programmer error and fails far from its cause otherwise", what)
			return
		}
		msg, ok := r.(string)
		if !ok {
			t.Errorf("panic value is %T; want a string", r)
			return
		}
		if !strings.Contains(msg, what) {
			t.Errorf("panic message %q does not name %q", msg, what)
		}
	}()
	call()
}

// TestBuild_PanicsOnABuilderNoConstructorMade holds Build to refusing a builder
// the package did not construct. Such a builder yields a zero Issue, which its
// own godoc calls valid and which Collect then panics on — one call further from
// the mistake.
func TestBuild_PanicsOnABuilderNoConstructorMade(t *testing.T) {
	t.Parallel()

	wantPanic(t, "Build", func() {
		var b IssueBuilder
		_ = b.Build()
	})
}

// TestNewCode_PanicsOnAnEmptyValue holds NewCode to refusing an empty code, as
// it refuses a duplicate. An empty code registers, appears in AllCodes, and can
// be carried by no issue: IsZero reports it unset.
func TestNewCode_PanicsOnAnEmptyValue(t *testing.T) {
	t.Parallel()

	wantPanic(t, "empty", func() {
		_ = NewCode("", CategorySentinel)
	})
}
