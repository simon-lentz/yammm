package eval_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/schema"
)

// The refusal of a string that is not valid UTF-8 quotes at most the string's
// first 32 bytes, in whole characters, and states the string's length when it
// is longer.
func TestCheckValue_ARefusedStringIsQuotedCutToItsHead(t *testing.T) {
	t.Parallel()
	thirtyOne := strings.Repeat("a", 31)
	for _, c := range []struct {
		name, text, quoted string
	}{
		{"three bytes", "a\xffb", `"a\xffb"`},
		{"32 bytes, the bound", thirtyOne + "\xff", `"` + thirtyOne + `\xff"`},
		{"33 bytes", thirtyOne + "\xffb", `"` + thirtyOne + `\xff"… (33 bytes)`},
		{"the invalid byte past the cut", strings.Repeat("a", 100000) + "\xff", `"` + thirtyOne + `a"… (100001 bytes)`},
		{"a character across the cut is left out whole", thirtyOne + "é\xff", `"` + thirtyOne + `"… (34 bytes)`},
		{"invalid bytes alone", strings.Repeat("\xff", 40), `"` + strings.Repeat(`\xff`, 32) + `"… (40 bytes)`},
		{"U+FFFD across the cut is left out whole", thirtyOne + string(rune(0xFFFD)) + "\xff", `"` + thirtyOne + `"… (35 bytes)`},
		{"a character past ASCII is kept as itself", "é\xff", `"é\xff"`},
		{"a character past ASCII is kept as itself in a cut head", "é" + strings.Repeat("a", 30) + "\xffb", `"é` + strings.Repeat("a", 30) + `"… (34 bytes)`},
	} {
		for _, k := range []struct {
			want string
			c    schema.Constraint
		}{
			{"string", schema.NewStringConstraint()},
			{"string for pattern", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("a")})},
			{"string for enum", schema.NewEnumConstraint([]string{"a", "b"})},
			{"date string or time.Time", schema.NewDateConstraint()},
			{"timestamp string or time.Time", schema.NewTimestampConstraint()},
			{"UUID string or uuid.UUID", schema.NewUUIDConstraint()},
		} {
			check := eval.CheckValue(c.text, k.c)
			if want := "expected " + k.want + ", got a string that is not valid UTF-8: " + c.quoted; check == nil || check.Error() != want {
				t.Errorf("%s, check at %s:\n got %.200v\nwant %s", c.name, k.c.Kind(), check, want)
			}
			_, coerce := eval.CoerceValue(c.text, k.c)
			if want := "cannot coerce a string that is not valid UTF-8: " + c.quoted; coerce == nil || coerce.Error() != want {
				t.Errorf("%s, coercion at %s:\n got %.200v\nwant %s", c.name, k.c.Kind(), coerce, want)
			}
			for _, err := range []error{check, coerce} {
				if err != nil && !utf8.ValidString(err.Error()) {
					t.Errorf("%s at %s: the message is not valid UTF-8: %.200q", c.name, k.c.Kind(), err.Error())
				}
			}
		}
	}
}

// The message's length does not follow the string's: the longest head is 32
// bytes of four characters each when quoted.
func TestCheckValue_ARefusedStringsMessageIsBounded(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		strings.Repeat("a", 1<<20) + "\xff",
		strings.Repeat("\xff", 1<<20),
		strings.Repeat("\x00", 1<<20) + "\xff",
	} {
		err := eval.CheckValue(text, schema.NewStringConstraint())
		if err == nil {
			t.Fatal("the string was accepted")
		}
		if got := len(err.Error()); got > 256 {
			t.Errorf("a %d-byte string draws a %d-byte message, want at most 256", len(text), got)
		}
	}
}

// A value that is no string is named by its Go type at every text kind.
func TestCheckValue_AValueThatIsNoStringIsNamedByItsType(t *testing.T) {
	t.Parallel()
	for _, k := range []struct {
		want string
		c    schema.Constraint
	}{
		{"expected string, got int", schema.NewStringConstraint()},
		{"expected string for enum, got int", schema.NewEnumConstraint([]string{"a", "b"})},
		{"expected date string or time.Time, got int", schema.NewDateConstraint()},
		{"expected UUID string or uuid.UUID, got int", schema.NewUUIDConstraint()},
	} {
		if err := eval.CheckValue(5, k.c); err == nil || err.Error() != k.want {
			t.Errorf("5 at %s: %v, want %q", k.c.Kind(), err, k.want)
		}
	}
}

// A number literal in a message is cut to whole characters, so the message is
// valid UTF-8 whatever the literal holds.
func TestCoerceValue_ACutLiteralKeepsWholeCharacters(t *testing.T) {
	t.Parallel()
	lit := json.Number("a" + strings.Repeat("é", 20))
	_, err := eval.CoerceValue(lit, schema.NewFloatConstraint())
	if err == nil {
		t.Fatal("the literal was coerced")
	}
	if !utf8.ValidString(err.Error()) || strings.Contains(err.Error(), `\x`) {
		t.Errorf("the message splits a character: %q", err.Error())
	}
}

// A checker that quotes the value it refuses quotes it as strconv.Quote does,
// and cuts a long one to its head.
func TestCheckValue_ARefusedValueIsQuoted(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100)
	cut := `"` + long[:32] + `"… (100 bytes)`
	coerced := func(_ any, err error) error { return err }
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"UUID", eval.CheckValue("xyz", schema.NewUUIDConstraint()), `invalid UUID: "xyz"`},
		{"enum", eval.CheckValue("c", schema.NewEnumConstraint([]string{"a", "b"})), `value "c" not in enum [a b]`},
		{"pattern", eval.CheckValue("b", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a$")})), `value "b" does not match pattern ^a$`},
		{
			"a literal that is no number", coerced(eval.CoerceValue(json.Number(long), schema.NewFloatConstraint())),
			`cannot coerce json.Number "` + long[:32] + `"… (100 bytes) to float64: no JSON number literal`,
		},
		{"a long UUID", eval.CheckValue(long, schema.NewUUIDConstraint()), `invalid UUID: ` + cut},
		{"a long enum value", eval.CheckValue(long, schema.NewEnumConstraint([]string{"a", "b"})), `value ` + cut + ` not in enum [a b]`},
		{
			"a long pattern value", eval.CheckValue(long, schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a$")})),
			`value ` + cut + ` does not match pattern ^a$`,
		},
		{"a long date", eval.CheckValue(long, schema.NewDateConstraint()), `invalid date format: ` + cut + ` (expected YYYY-MM-DD)`},
		{"a long timestamp", eval.CheckValue(long, schema.NewTimestampConstraint()), `invalid timestamp format: ` + cut},
		{
			"a long timestamp under a declared format", eval.CheckValue(long, schema.NewTimestampConstraintFormatted(time.DateOnly)),
			`invalid timestamp format: ` + cut + ` (expected 2006-01-02)`,
		},
	} {
		if c.err == nil || c.err.Error() != c.want {
			t.Errorf("%s: %v, want %q", c.name, c.err, c.want)
		}
	}
}
