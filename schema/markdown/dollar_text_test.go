package markdown

import (
	"strings"
	"testing"
)

// GitHub reads a pair of dollars outside code as math, and a backslash does not
// stop it there: its documentation ("Writing mathematical expressions") gives
// span tags round a dollar as the one escape outside a math expression. The
// inputs are the four GitHub's rendering of a committed file read as math (a
// schema name in the title, and three invariant messages), with the two it read
// as text beside them. No dollar the generator writes outside code may stand
// bare.
func TestMarshal_WritesNoBareDollarOutsideCode(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, `schema "a $x$ b"

type T {
	id String primary
	n Integer
	! "cost $a$ ok" n > 0
	! "blk $$a$$ ok" n > 1
	! "tick $`+"`a`"+`$ ok" n > 2
	! "pay $5" n > 3
	! "and $6 more" n > 4
}
`)
	out, err := Marshal(s)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	inFence := false
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(strings.TrimLeft(line, " "), "```") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if bare := strings.ReplaceAll(line, documentedDollar, ""); strings.Contains(bare, "$") {
			t.Errorf("a dollar outside code stands bare, which GitHub may read as math: %q", line)
		}
	}
	if want := "# Schema a " + documentedDollar + "x" + documentedDollar + " b\n"; !strings.HasPrefix(string(out), want) {
		t.Errorf("the title is not %q:\n%s", want, out)
	}
}

// documentedDollar is the span GitHub's documentation writes round a dollar outside
// a math expression.
const documentedDollar = "<span>$</span>"
