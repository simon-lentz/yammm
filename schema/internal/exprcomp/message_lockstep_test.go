package exprcomp_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema/internal/exprcomp"
)

// invalidNumberWording is the malformed-numeric text this package emits. The
// root internal/parse package holds an identical copy that neither package can
// share, because neither can import the other, so the same literal is pinned
// on both sides and an edit to one wording fails there.
const invalidNumberWording = `malformed numeric literal "0x10": numeric literals are decimal ` +
	`(hexadecimal and suffixed forms are not supported)`

func TestInvalidNumberMessage_WordingIsPinned(t *testing.T) {
	if got := exprcomp.InvalidNumberMessage("0x10"); got != invalidNumberWording {
		t.Errorf("InvalidNumberMessage =\n %q\nwant\n %q", got, invalidNumberWording)
	}
}
