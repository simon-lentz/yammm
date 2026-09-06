//go:build integration

package tagged

// TagOnly exists only under the integration tag. Its link to
// [NeverAnywhere] dangles, but this file is outside the default build and
// outside the gate's reach, as it is outside go doc's.
func TagOnly() {}
