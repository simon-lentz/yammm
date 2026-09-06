package docs_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// TestModuleDependencyLines points the dependency-line checker at the module.
// It lives here rather than in internal/doclint because it reads the whole
// module; that package's own tests prove the checker reports what it should.
// The class had been wrong at three consecutive trees, each error beside a
// paragraph asserting the opposite, because no gate read the claim.
func TestModuleDependencyLines(t *testing.T) {
	t.Parallel()
	checked := doclint.AssertDependencyLines(t, "..")
	// The walk is the only thing standing between "no line disagrees" and "no
	// line was read", so a shrunken walk is an error rather than a silent pass.
	if checked < 8 {
		t.Errorf("checked only %d dependency lines across the module; the walk is not reaching the package docs", checked)
	}
	t.Logf("checked %d dependency lines", checked)
}
