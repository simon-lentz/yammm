package location

import (
	"errors"
	"strconv"
	"testing"
)

// absoluteStart is what the refusal of an identifier shaped like an absolute
// path says after the identifier: the three starts [looksLikeAbsolute] reads.
const absoluteStart = "; it starts as a file-backed source's path does: with a slash, two backslashes, or a drive letter, a colon and a separator"

// The refusal states which starts make an identifier look absolute and gives
// no advice, so it reads true for a source key and for a source
// identifier alike.
func TestNewSourceID_TheAbsoluteRefusalStatesTheStarts(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"/abs.yammm", "//unc/x", `\\srv\share`, "C:/x.yammm", `c:\x.yammm`} {
		_, err := NewSourceID(id)
		if !errors.Is(err, ErrAbsolutePathSourceID) {
			t.Errorf("NewSourceID(%q) = %v, want ErrAbsolutePathSourceID", id, err)
			continue
		}
		want := ErrAbsolutePathSourceID.Error() + ": " + strconv.Quote(id) + absoluteStart
		if err.Error() != want {
			t.Errorf("NewSourceID(%q):\n got %s\nwant %s", id, err, want)
		}
	}
}
