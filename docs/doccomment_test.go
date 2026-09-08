package docs_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// TestModuleDocCommentsRender points the doc-comment checker at the module.
// Every shape it reads is silent in source: a detached comment still looks
// attached, a stacked one still looks like documentation, and markup go/doc has
// no syntax for still reads as markup.
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
