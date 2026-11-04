package snapshot

import (
	"encoding/json"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// A string, int64 or bool pair is decided without rendering either side, so the
// check every keyed instance runs allocates nothing for the common key kinds.
func TestWireComponentAgrees_DecidesAStringIntOrBoolPairWithoutAllocating(t *testing.T) {
	str := schema.NewStringConstraint()
	for _, c := range []struct {
		name      string
		x, y      any
		c         schema.Constraint
		wantAgree bool
	}{
		{"one string", "a", "a", str, true},
		{"two strings", "a", "b", str, false},
		{"one integer", json.Number("7"), json.Number("7"), nil, true},
		{"two integers", json.Number("7"), json.Number("8"), nil, false},
		{"one bool", true, true, nil, true},
		{"two bools", true, false, nil, false},
	} {
		if got := wireComponentAgrees(c.x, c.y, c.c); got != c.wantAgree {
			t.Errorf("%s: wireComponentAgrees = %v, want %v", c.name, got, c.wantAgree)
		}
		if n := testing.AllocsPerRun(100, func() { wireComponentAgrees(c.x, c.y, c.c) }); n != 0 {
			t.Errorf("%s: %v allocations, want 0", c.name, n)
		}
	}
}
