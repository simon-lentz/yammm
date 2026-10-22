package location

import (
	"errors"
	"strings"
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
func TestValidateSyntheticSourceID_AdmitsPrintableCharactersAlone(t *testing.T) {
	t.Parallel()
	raceskip.Skip(t)
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		err := ValidateSyntheticSourceID("test://a" + string(r) + "b")
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

// A key is held to the same rule, with its ASCII space kept, and the refusal
// names the key and the character.
func TestNormalizeSyntheticKey_RefusesACharacterThatDoesNotPrint(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"a\x00b.yammm", "a\nb.yammm", "a\tb.yammm", "a\x7fb.yammm", "a\u202eb.yammm", "a\u2066b.yammm", "a\u200db.yammm", "a\ufeffb.yammm", "a\ue000b.yammm", "a\ufffeb.yammm"} {
		_, err := NormalizeSyntheticKey(key)
		if !errors.Is(err, ErrUnprintableSourceID) {
			t.Errorf("NormalizeSyntheticKey(%q) = %v, want ErrUnprintableSourceID", key, err)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "must be relative") || !strings.Contains(msg, "U+") {
			t.Errorf("NormalizeSyntheticKey(%q): %s", key, msg)
		}
	}
	for _, key := range []string{"my schema.yammm", "caf\u00e9/r\u00e9sum\u00e9.yammm", "\u65e5\u672c.yammm"} {
		if _, err := NormalizeSyntheticKey(key); err != nil {
			t.Errorf("NormalizeSyntheticKey(%q) = %v, want a key", key, err)
		}
	}
}
