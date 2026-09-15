//go:build !windows

package yammmtest

import "testing"

// shortPathName returns long: only Windows has 8.3 short names.
func shortPathName(t *testing.T, long string) string {
	t.Helper()
	return long
}
