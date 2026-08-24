package neo4j

import "testing"

// TestZoneNameResolves_CachesEitherAnswer pins that both outcomes are
// memoized: a name the host cannot resolve must not cost a zoneinfo lookup
// on every row of a batch write.
func TestZoneNameResolves_CachesEitherAnswer(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]bool{
		"Nowhere/Nope": false,
		"EST":          true,
	} {
		if got := zoneNameResolves(name); got != want {
			t.Fatalf("zoneNameResolves(%q) = %v, want %v", name, got, want)
		}
		cached, ok := resolvableZones.Load(name)
		if !ok {
			t.Errorf("%q was not cached", name)
		} else if cached != want {
			t.Errorf("%q cached as %v, want %v", name, cached, want)
		}
	}
}
