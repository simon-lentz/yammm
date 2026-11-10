package eval_test

import (
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/instance/internal/eval"
)

// A datatype check applies the kind's rule, so a string that is not valid
// UTF-8 is no String to an invariant either.
func TestIsString_RefusesAStringThatIsNotValidUTF8(t *testing.T) {
	t.Parallel()
	ok, msg := eval.IsString()("a\xffb")
	if ok {
		t.Error("IsString accepts a string that is not valid UTF-8")
	}
	if !utf8.ValidString(msg) {
		t.Errorf("the message is not valid UTF-8: %q", msg)
	}
	ok, _ = eval.IsString()("a�b")
	if !ok {
		t.Error("IsString refuses U+FFFD, a valid code point")
	}
}
