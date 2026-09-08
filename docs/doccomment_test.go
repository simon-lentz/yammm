package docs_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// TestModuleDocCommentsRender points the doc-comment checker at the module.
// Both shapes it reads are silent: a detached comment still looks attached in
// source, and asterisk-pair emphasis still reads as emphasis. One option's whole
// documentation was invisible to go doc for three trees on the first.
func TestModuleDocCommentsRender(t *testing.T) {
	t.Parallel()
	checked := doclint.AssertDocCommentsRender(t, "..")
	// The walk is the only thing standing between "every doc renders" and "no
	// doc was read", so a shrunken walk is an error rather than a silent pass.
	// The floor sits just under the count at the tree that raised it, not at a
	// round number: a build-constraint filter once dropped ten tracked files
	// and both trees stayed green because the floor was six times lower.
	if checked < 3200 {
		t.Errorf("read only %d doc comments across the module; the walk is not reaching the source", checked)
	}
	t.Logf("read %d doc comments", checked)
}
