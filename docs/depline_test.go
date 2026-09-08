package docs_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// TestModuleDependencyLines points the dependency-line checker at the module.
// It lives here rather than in internal/doclint because it reads the whole
// module; that package's own tests prove the checker reports what it should.
// A dependency line is a claim no other gate reads.
func TestModuleDependencyLines(t *testing.T) {
	t.Parallel()
	checked, headings := doclint.AssertDependencyLines(t, "..")
	// Every heading was read, which is stronger than a floor: a floor measures
	// the walk, and this measures the claims. A heading the gate cannot read is
	// an error inside the gate, so reaching this with headings > checked means
	// the walk lost a file rather than that a claim went unstated.
	if headings == 0 {
		t.Error("no # Dependencies heading was found; the walk is not reaching the package docs")
	}
	if checked < headings {
		t.Errorf("read %d rows under %d headings; every heading owes at least one row", checked, headings)
	}
	t.Logf("checked %d dependency rows under %d headings", checked, headings)
}
