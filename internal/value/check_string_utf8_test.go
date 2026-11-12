package value_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

type carrier string

// Every kind held as text refuses a string that is not valid UTF-8 as a type
// mismatch, before its own rule reads the bytes: the Enum here is a constraint
// no schema constructor builds, which would otherwise accept the string.
func TestCheck_AStringIsValidUTF8AtEveryTextKind(t *testing.T) {
	t.Parallel()
	const bad = "a\xffb"
	for _, c := range []struct {
		name string
		val  any
		c    schema.Constraint
	}{
		{"String", bad, schema.NewStringConstraint()},
		{"String in a named type", carrier(bad), schema.NewStringConstraint()},
		{"Pattern its pattern matches", bad, schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a.b$")})},
		{"Enum holding the string", bad, schema.NewEnumConstraint([]string{bad, "c"})},
		{"Timestamp", bad, schema.NewTimestampConstraint()},
		{"Date", bad, schema.NewDateConstraint()},
		{"UUID", bad, schema.NewUUIDConstraint()},
		{"List element", []any{"ok", bad}, schema.NewListConstraint(schema.NewStringConstraint())},
		{"alias of String", bad, schema.NewAliasConstraint("Name", schema.NewStringConstraint())},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := value.Check(c.val, c.c)
			var ce *value.CheckError
			if !errors.As(err, &ce) {
				t.Fatalf("Check: got %v, want a *value.CheckError", err)
			}
			if ce.Kind != value.KindTypeMismatch {
				t.Errorf("kind: got %v, want %v", ce.Kind, value.KindTypeMismatch)
			}
			if !utf8.ValidString(ce.Msg) {
				t.Errorf("the message is not valid UTF-8: %q", ce.Msg)
			}
			if want := `not valid UTF-8: "a\xffb"`; !strings.Contains(ce.Msg, want) {
				t.Errorf("the message %q does not hold %q", ce.Msg, want)
			}

			got, err := value.Coerce(c.val, c.c)
			if err == nil {
				t.Fatal("Coerce accepts what Check refuses")
			}
			if got != nil {
				t.Errorf("Coerce returned %v beside its error", got)
			}
			if !utf8.ValidString(err.Error()) {
				t.Errorf("the coercion error is not valid UTF-8: %q", err.Error())
			}
		})
	}
}

// The rule refuses bytes, not characters: U+FFFD and every other code point a
// string can hold pass, and the length bound still counts runes.
func TestCheck_ValidUTF8IsNotRefused(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "a�b", "é", "\U0001F600", strings.Repeat("é", 3)} {
		if err := value.Check(s, schema.NewStringConstraint()); err != nil {
			t.Errorf("Check(%q): %v", s, err)
		}
		got, err := value.Coerce(carrier(s), schema.NewStringConstraint())
		if err != nil {
			t.Fatalf("Coerce(%q): %v", s, err)
		}
		if got != s {
			t.Errorf("Coerce(%q) = %#v", s, got)
		}
	}
	if err := value.Check("ééé", schema.StringLenBetween(3, 3)); err != nil {
		t.Errorf("a string of three runes under a bound of three: %v", err)
	}
}
