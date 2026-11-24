package location

import (
	"errors"
	"testing"
	"unicode"

	"github.com/simon-lentz/yammm/internal/raceskip"
)

// printable states the rule from Unicode's categories: a letter, mark, number,
// punctuation or symbol, or the ASCII space.
func printable(r rune) bool {
	return r == ' ' || unicode.In(r, unicode.L, unicode.M, unicode.N, unicode.P, unicode.S)
}

// Every Unicode scalar is admitted in a synthetic identity when it prints and
// refused with ErrUnprintableSourceID when it does not, a bidi control and a
// zero-width joiner among the refused.
func TestNewSourceID_AdmitsPrintableCharactersAlone(t *testing.T) {
	t.Parallel()
	raceskip.Skip(t)
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		_, err := NewSourceID("test://a" + string(r) + "b")
		if printable(r) {
			if err != nil {
				t.Errorf("U+%04X is refused: %v", r, err)
			}
			continue
		}
		if !errors.Is(err, ErrUnprintableSourceID) {
			t.Errorf("U+%04X: err = %v, want ErrUnprintableSourceID", r, err)
		}
	}
}
