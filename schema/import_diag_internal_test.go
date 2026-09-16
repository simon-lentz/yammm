package schema

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestModuleRootClause_DiscoveredNamesTheMarkerUnderTheRoot holds the
// discovered clause to the marker joined under the root's identity by one "/",
// the separator every identity uses, at a root that already ends in one too.
func TestModuleRootClause_DiscoveredNamesTheMarkerUnderTheRoot(t *testing.T) {
	t.Parallel()

	for _, row := range []struct{ root, want string }{
		{"/proj", "module root /proj, discovered from /proj/yammm.mod"},
		{"/", "module root /, discovered from /yammm.mod"},
		{"C:/proj", "module root C:/proj, discovered from C:/proj/yammm.mod"},
		{"C:/", "module root C:/, discovered from C:/yammm.mod"},
		{"//server/share/", "module root //server/share/, discovered from //server/share/yammm.mod"},
		{"//server/share/proj", "module root //server/share/proj, discovered from //server/share/proj/yammm.mod"},
	} {
		if got := moduleRootClause(row.root, diag.ModuleRootDiscovered); got != row.want {
			t.Errorf("moduleRootClause(%q) = %q; want %q", row.root, got, row.want)
		}
	}
}
