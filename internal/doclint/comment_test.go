package doclint_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/doclint"
)

// commentFixture is a module of its own so the gate runs its real entry point,
// go.mod read included, rather than a path that only tests exercise.
const commentFixture = "testdata/commentfixture"

func runCommentGate(t *testing.T) (*recorder, int) {
	t.Helper()
	r := &recorder{}
	return r, doclint.AssertDocCommentsRender(r, commentFixture)
}

func TestAssertDocCommentsRender_WellFormedDocsAreSilent(t *testing.T) {
	t.Parallel()
	r, checked := runCommentGate(t)
	for _, quiet := range []string{"/clean/", "/attached/"} {
		for _, m := range r.msgs {
			if strings.Contains(m, quiet) {
				t.Errorf("well-formed doc reported: %s", m)
			}
		}
	}
	if checked == 0 {
		t.Error("the gate read no doc comment, so it asserts nothing")
	}
}

// The defect this shape names is silent in source: the block still sits above
// the declaration and still looks like its documentation.
func TestAssertDocCommentsRender_ReportsADetachedBlock(t *testing.T) {
	t.Parallel()
	r, _ := runCommentGate(t)
	// Every position a doc can occupy: a top-level declaration, a spec inside a
	// parenthesised block, and an exported struct field. The scan reached only
	// the first, so the other two lost their documentation silently.
	for _, name := range []string{"from Orphan", "from OrphanConst", "from Holder.OrphanField"} {
		if !r.reports("separates this comment block " + name) {
			t.Errorf("%s was not reported: %v", name, r.msgs)
		}
	}
	// A gap comment that names no declaration is a note, not lost
	// documentation, and reporting it is what made the rule unusable.
	for _, m := range r.msgs {
		if strings.Contains(m, "from Noted") || strings.Contains(m, "from KeptConst") || strings.Contains(m, "KeptField") {
			t.Errorf("a note or an attached doc was reported: %s", m)
		}
	}
}

func TestAssertDocCommentsRender_ReportsAsteriskPairEmphasis(t *testing.T) {
	t.Parallel()
	r, _ := runCommentGate(t)
	if !r.reports("emphasis/emphasis.go") {
		t.Errorf("the emphasis was not reported: %v", r.msgs)
	}
}

// An unexported declaration publishes nothing, so a block detached from it
// costs no documentation.
func TestAssertDocCommentsRender_IgnoresUnexportedDeclarations(t *testing.T) {
	t.Parallel()
	r, _ := runCommentGate(t)
	if r.reports("/unexported/") {
		t.Errorf("an unexported declaration was reported: %v", r.msgs)
	}
}

// Nothing in a test file reaches go doc, and a block above a test function is
// ordinary style. Reading them buried the one real defect under seventy-six.
func TestAssertDocCommentsRender_SkipsTestFiles(t *testing.T) {
	t.Parallel()
	r, _ := runCommentGate(t)
	if r.reports("_test.go") {
		t.Errorf("a test file was read: %v", r.msgs)
	}
}

func TestAssertDocCommentsRender_MissingRootIsReported(t *testing.T) {
	t.Parallel()
	r := &recorder{}
	if checked := doclint.AssertDocCommentsRender(r, "testdata/does-not-exist"); checked != 0 {
		t.Errorf("read %d doc comments under a root that does not exist", checked)
	}
	if len(r.msgs) == 0 {
		t.Error("a missing root was not reported")
	}
}
