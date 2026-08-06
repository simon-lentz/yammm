package parse

import "testing"

// invalidNumberWording is the malformed-numeric text this package emits. It is
// the canonical copy: schema/internal/exprcomp delegates here rather than
// keeping its own, and pins the same literal so a wording edit fails on both
// sides rather than silently re-pointing one of them.
const invalidNumberWording = `malformed numeric literal "0x10": numeric literals are decimal ` +
	`(hexadecimal and suffixed forms are not supported)`

func TestInvalidNumberMessage_WordingIsPinned(t *testing.T) {
	if got := InvalidNumberMessage("0x10"); got != invalidNumberWording {
		t.Errorf("InvalidNumberMessage =\n %q\nwant\n %q", got, invalidNumberWording)
	}
}
