package gogen

import (
	"path/filepath"
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
			got, err := keyRoot{synthetic: tc.root}.key(location.NewSourceID(tc.id))
			if err != nil || got != tc.want {
				t.Errorf("keyRoot{%q}.key(%q) = %q, want %q", tc.root, tc.id, got, tc.want)
			}
		})
	}
}

// TestKeyRoot_NoRelativeFormIsAnError pins that a key is never a
// generation-machine path: where a source has no form relative to the root,
// key refuses rather than writing its identity.
func TestKeyRoot_NoRelativeFormIsAnError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for name, tc := range map[string]struct {
		root keyRoot
		id   location.SourceID
	}{
		"a synthetic source outside a synthetic root": {keyRoot{synthetic: "embedded://app"}, location.NewSourceID("embedded://other/a.yammm")},
		"a file source under a synthetic root":        {keyRoot{synthetic: "embedded://app"}, location.MustSourceIDFromPath(filepath.Join(t.TempDir(), "a.yammm"))},
		"a synthetic source under a file root":        {keyRoot{dir: location.MustCanonicalPath(t.TempDir())}, location.NewSourceID("string://a.yammm")},
		"the root directory itself":                   {keyRoot{dir: location.MustCanonicalPath(dir)}, location.MustSourceIDFromPath(dir)},
		"a zero source":                               {keyRoot{dir: location.MustCanonicalPath(dir)}, location.SourceID{}},
		"a file source with no root":                  {keyRoot{}, location.MustSourceIDFromPath(filepath.Join(dir, "a.yammm"))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, err := tc.root.key(tc.id); err == nil {
				t.Errorf("key(%s) = %q, want an error", tc.id, got)
			}
		})
	}
}
