package gogen

import (
	"strings"
	"testing"
)

// TestFinish_RefusesAnEmbeddedStoreThatDoesNotReload drives the round-trip
// check over the store the generator emitted, with one imported source's key
// withdrawn, so the entry's import misses it on re-load.
func TestFinish_RefusesAnEmbeddedStoreThatDoesNotReload(t *testing.T) {
	t.Parallel()

	g, err := newGenerator(loadFixture(t, "imports/main", ""))
	if err != nil {
		t.Fatal(err)
	}
	data, err := g.generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.finish(data); err != nil {
		t.Fatalf("finish over the emitted store = %v, want nil", err)
	}
	for k := range g.embedded {
		if k != g.entryKey {
			delete(g.embedded, k)
			break
		}
	}
	if _, err := g.finish(data); err == nil || !strings.Contains(err.Error(), "does not re-load") {
		t.Errorf("finish = %v, want the store refused", err)
	}
}
