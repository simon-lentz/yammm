package doclint_test

import (
	"strings"
	"testing"
)

// TestAssertDocCommentsRender_ReportsAStackedDoc pins that a doc whose leading
// identifier names ANOTHER declaration of the same file is reported. Nothing is
// detached — the block abuts a declaration — so the detached rule cannot see a
// paragraph moved onto its neighbour, which then documents the wrong symbol.
// Every declaration is read, exported or not, since go doc -u renders both.
func TestAssertDocCommentsRender_ReportsAStackedDoc(t *testing.T) {
	t.Parallel()
	r, _ := runCommentGate(t)
	if !r.reports("naming Stacked, not Carrier") {
		t.Errorf("a doc stacked onto its neighbour was not reported: %v", r.msgs)
	}
	if !r.reports("naming unexportedStacked, not unexportedCarrier") {
		t.Errorf("an unexported stacked doc was not reported: %v", r.msgs)
	}
	// A doc that opens with its own declaration's name is the ordinary case,
	// and a doc naming a symbol from another file is a reference, not a stack.
	for _, m := range r.msgs {
		if strings.Contains(m, "naming Carrier, not") || strings.Contains(m, "naming Elsewhere") {
			t.Errorf("an ordinary doc was reported as stacked: %s", m)
		}
	}
}
