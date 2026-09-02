package gogen

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// TestSourceKey_SyntheticRoot pins the key derivation for a schema loaded
// under a synthetic root, which [schema.Schema.ModuleRoot] now reports.
//
// A synthetic root is not a filesystem path, so filepath.Rel is the wrong
// instrument: it works on these inputs only because filepath.Clean collapses
// the scheme's double slash identically on both arguments, which is an
// accident of Clean rather than a rule. The explicit branch makes the
// derivation a rule, and these cases are what it must hold for.
func TestSourceKey_SyntheticRoot(t *testing.T) {
	t.Parallel()

	entry := location.NewSourceID("embedded://app/assets/main.yammm")

	for name, tc := range map[string]struct {
		root string
		id   string
		want string
	}{
		"single level":    {"embedded://app", "embedded://app/main.yammm", "main.yammm"},
		"two levels":      {"embedded://app", "embedded://app/assets/main.yammm", "assets/main.yammm"},
		"three levels":    {"embedded://app", "embedded://app/a/b/c.yammm", "a/b/c.yammm"},
		"escapes root":    {"embedded://app", "embedded://app/../outside.yammm", "../outside.yammm"},
		"no scheme slash": {"embedded:/app", "embedded:/app/x.yammm", "x.yammm"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := sourceKey(tc.root, entry, location.NewSourceID(tc.id))
			if got != tc.want {
				t.Errorf("sourceKey(%q, %q) = %q, want %q", tc.root, tc.id, got, tc.want)
			}
		})
	}
}
