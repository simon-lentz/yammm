package immutable

import "testing"

// TestWrapMap_FoldedViewOnlyForStringKeys holds WrapMap to allocating the
// folded view only where it can be read: PropertiesOf, its one reader, takes a
// Map[string], so a view on any other key type is never read.
func TestWrapMap_FoldedViewOnlyForStringKeys(t *testing.T) {
	t.Parallel()

	// allocatedForOtherKeys records today's behaviour: WrapMap allocates the view
	// for every key type. The change that stops it must set this to false.
	const allocatedForOtherKeys = true

	if WrapMap(map[string]any{"a": 1}).folded == nil {
		t.Error("a Map[string] carries no folded view, so PropertiesOf cannot memoise its index")
	}

	type label string
	for name, allocated := range map[string]bool{
		"int keys":          WrapMap(map[int]any{1: "a"}).folded != nil,
		"named string keys": WrapMap(map[label]any{"a": 1}).folded != nil,
	} {
		if allocated != allocatedForOtherKeys {
			t.Errorf("%s: folded view allocated = %v; allocatedForOtherKeys says %v, so update it", name, allocated, allocatedForOtherKeys)
		}
	}
}
