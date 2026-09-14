package docs_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// TestModuleNamesCarryNoProcessReference points the name gate at the module:
// every tracked path, testdata included, and every test function. A review
// round, a fix-pass group or a row identifier in a name outlives the plan that
// gave it meaning, and nothing else refuses one.
func TestModuleNamesCarryNoProcessReference(t *testing.T) {
	t.Parallel()
	paths, tests := doclint.AssertProcessFreeNames(t, "..")
	// Each floor sits just under the count at the tree that set it, so a walk
	// that stops reaching the tree fails instead of passing over nothing.
	if paths < pathFloor {
		t.Errorf("read only %d paths; the walk is not reaching the tracked tree", paths)
	}
	if tests < testFloor {
		t.Errorf("read only %d test functions; the walk is not reaching the test files", tests)
	}
	t.Logf("read %d paths and %d test functions", paths, tests)
}

const (
	pathFloor = 1600
	testFloor = 3990
)
