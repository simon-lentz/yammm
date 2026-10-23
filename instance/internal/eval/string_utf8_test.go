package eval_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/schema"
)

type carrier string

// Every kind held as text refuses a string that is not valid UTF-8 as a type
// mismatch, before its own rule reads the bytes: the Enum and the layout here
// are constraints no schema constructor builds, each of which would otherwise
// accept the string.
func TestCheckValue_AStringIsValidUTF8AtEveryTextKind(t *testing.T) {
	t.Parallel()
	const bad = "a\xffb"
	ptr := bad
	for _, c := range []struct {
		name string
		val  any
		c    schema.Constraint
	}{
		{"String", bad, schema.NewStringConstraint()},
		{"String in a named type", carrier(bad), schema.NewStringConstraint()},
		{"String behind a pointer", &ptr, schema.NewStringConstraint()},
		{"Pattern its pattern matches", bad, schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a.b$")})},
		{"Enum holding the string", bad, schema.NewEnumConstraint([]string{bad, "c"})},
		{"Timestamp whose layout is the string", bad, schema.NewTimestampConstraintFormatted(bad)},
		{"Timestamp", bad, schema.NewTimestampConstraint()},
		{"Date", bad, schema.NewDateConstraint()},
		{"UUID", bad, schema.NewUUIDConstraint()},
		{"List element", []any{"ok", bad}, schema.NewListConstraint(schema.NewStringConstraint())},
		{"alias of String", bad, schema.NewAliasConstraint("Name", schema.NewStringConstraint())},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := eval.CheckValue(c.val, c.c)
			var ce *eval.CheckError
			if !errors.As(err, &ce) {
				t.Fatalf("CheckValue: got %v, want a *eval.CheckError", err)
			}
			if ce.Kind != eval.KindTypeMismatch {
				t.Errorf("kind: got %v, want %v", ce.Kind, eval.KindTypeMismatch)
			}
			if !utf8.ValidString(ce.Msg) {
				t.Errorf("the message is not valid UTF-8: %q", ce.Msg)
			}
			if want := `not valid UTF-8: "a\xffb"`; !strings.Contains(ce.Msg, want) {
				t.Errorf("the message %q does not hold %q", ce.Msg, want)
			}

			got, err := eval.CoerceValue(c.val, c.c)
			if err == nil {
				t.Fatal("CoerceValue accepts what CheckValue refuses")
			}
			if got != nil {
				t.Errorf("CoerceValue returned %v beside its error", got)
			}
			if !utf8.ValidString(err.Error()) {
				t.Errorf("the coercion error is not valid UTF-8: %q", err.Error())
			}
		})
	}
}

// The rule refuses bytes, not characters: U+FFFD and every other code point a
// string can hold pass, and the length bound still counts runes.
func TestCheckValue_ValidUTF8IsNotRefused(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "a�b", "é", "\U0001F600", strings.Repeat("é", 3)} {
		if err := eval.CheckValue(s, schema.NewStringConstraint()); err != nil {
			t.Errorf("CheckValue(%q): %v", s, err)
		}
		got, err := eval.CoerceValue(carrier(s), schema.NewStringConstraint())
		if err != nil {
			t.Fatalf("CoerceValue(%q): %v", s, err)
		}
		if got != s {
			t.Errorf("CoerceValue(%q) = %#v", s, got)
		}
	}
	if err := eval.CheckValue("ééé", schema.StringLenBetween(3, 3)); err != nil {
		t.Errorf("a string of three runes under a bound of three: %v", err)
	}
}

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
