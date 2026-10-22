package location

import (
	"errors"
	"strconv"
	"strings"
	"testing"
)

// absoluteStart is what the refusal of an identifier shaped like an absolute
// path says after the identifier: the three starts [looksLikeAbsolute] reads.
const absoluteStart = "; it starts as a file-backed source's path does: with a slash, two backslashes, or a drive letter, a colon and a separator"

// The refusal states which starts make an identifier look absolute and gives
// no advice, so it reads true for a source key and for a source
// identifier alike.
func TestValidateSyntheticSourceID_TheAbsoluteRefusalStatesTheStarts(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"/abs.yammm", "//unc/x", `\\srv\share`, "C:/x.yammm", `c:\x.yammm`} {
		err := ValidateSyntheticSourceID(id)
		if !errors.Is(err, ErrAbsolutePathSourceID) {
			t.Errorf("ValidateSyntheticSourceID(%q) = %v, want ErrAbsolutePathSourceID", id, err)
			continue
		}
		want := ErrAbsolutePathSourceID.Error() + ": " + strconv.Quote(id) + absoluteStart
		if err.Error() != want {
			t.Errorf("ValidateSyntheticSourceID(%q):\n got %s\nwant %s", id, err, want)
		}
	}
}

// A source key is wrapped by the key rule, where a scheme is no remedy: a
// relative key holding one is joined under the root, and an absolute one is
// still refused.
func TestNormalizeSyntheticKey_TheAbsoluteRefusalGivesNoSchemeAdvice(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"/abs.yammm", "C:/x.yammm", "./C:/x.yammm"} {
		_, err := NormalizeSyntheticKey(key)
		if !errors.Is(err, ErrAbsolutePathSourceID) {
			t.Errorf("NormalizeSyntheticKey(%q) = %v, want ErrAbsolutePathSourceID", key, err)
			continue
		}
		if msg := err.Error(); !strings.HasSuffix(msg, absoluteStart) || strings.Contains(msg, "scheme") {
			t.Errorf("NormalizeSyntheticKey(%q): %s", key, msg)
		}
	}
}

// MustNewSourceID panics with the same text.
func TestMustNewSourceID_ThePanicStatesTheStarts(t *testing.T) {
	t.Parallel()
	defer func() {
		msg, _ := recover().(string)
		if want := "location.MustNewSourceID: " + ErrAbsolutePathSourceID.Error() + `: "/abs"` + absoluteStart; msg != want {
			t.Errorf("panic:\n got %s\nwant %s", msg, want)
		}
	}()
	MustNewSourceID("/abs")
}
