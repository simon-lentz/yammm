package parse

import "testing"

// invalidNumberWording is the malformed-numeric text this package emits, and
// this package is its only source. A wording edit must change the literal here
// too, which is what makes the edit deliberate.
const invalidNumberWording = `malformed numeric literal "0x10": numeric literals are decimal ` +
	`(hexadecimal and suffixed forms are not supported)`

func TestInvalidNumberMessage_WordingIsPinned(t *testing.T) {
	if got := InvalidNumberMessage("0x10"); got != invalidNumberWording {
		t.Errorf("InvalidNumberMessage =\n %q\nwant\n %q", got, invalidNumberWording)
	}
}
