package markdown

import (
	"strings"
	"testing"
)

const grandparentInvariantSource = `schema "p"

abstract type A {
	id String primary
	! "from a" id != "x"
}

abstract type B extends A {
}

abstract type D {
	d String
}

type E extends B, D {
}
`

func TestMarshal_AnInvariantNamesTheAncestorThatDeclaresIt(t *testing.T) {
	t.Parallel()
	doc := marshalString(t, grandparentInvariantSource)
	var e string
	if _, after, ok := strings.Cut(doc, "### E"); ok {
		e = after
	}
	if !strings.Contains(e, "— from A") || strings.Contains(e, "\"from a\"** — from B") {
		t.Errorf("E's section:\n%s", e)
	}
}
