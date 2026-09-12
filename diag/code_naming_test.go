package diag

import (
	"strings"
	"testing"
)

// TestPathFallbackCodeIsWarningPrefixed holds the one code raised at Warning
// alone to the W_ prefix every other such code carries. A code read as an
// error by its name, and never raised as one, misleads a consumer matching on
// the string.
func TestPathFallbackCodeIsWarningPrefixed(t *testing.T) {
	t.Parallel()

	got := W_SNAPSHOT_PATH_FALLBACK.String()
	if !strings.HasPrefix(got, "W_") {
		t.Errorf("the path-fallback code is %q; it is raised at Warning alone, so it is W_-prefixed", got)
	}
	if want := "W_SNAPSHOT_PATH_FALLBACK"; got != want {
		t.Errorf("the path-fallback code is %q; want %q", got, want)
	}
}
