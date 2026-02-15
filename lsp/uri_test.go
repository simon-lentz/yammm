package lsp

import (
	"testing"

	"github.com/simon-lentz/yammm/lsp/testutil"
)

// TestPathToURIEquivalence verifies that testutil.PathToURI produces the same
// output as lsp.PathToURI for all test cases. This catches any divergence
// between the copy in testutil and the main implementation.
func TestPathToURIEquivalence(t *testing.T) {
	// Use absolute paths to avoid cwd-relative differences
	cases := []string{
		"/simple/path.yammm",
		"/path with spaces/file.yammm",
		"/path/with/nested/dirs/schema.yammm",
		"/path/with-dashes/file_underscores.yammm",
		"/tmp/test/schema.yammm",
		"/Users/test/project/models/user.yammm",
	}

	for _, path := range cases {
		got := testutil.PathToURI(path)
		want := PathToURI(path)
		if got != want {
			t.Errorf("PathToURI(%q):\n  testutil = %q\n  lsp      = %q", path, got, want)
		}
	}
}
