package value_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// The refusal of a string that is not valid UTF-8 quotes at most the string's
// first 32 bytes, in whole characters, and states the string's length when it
// is longer.
func TestCheck_ARefusedStringIsQuotedCutToItsHead(t *testing.T) {
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
			{"string for pattern", schema.NewPatternConstraint(regexp.MustCompile("a"))},
			{"string for enum", schema.NewEnumConstraint([]string{"a", "b"})},
			{"date string or time.Time", schema.NewDateConstraint()},
			{"timestamp string or time.Time", schema.NewTimestampConstraint()},
			{"UUID string or uuid.UUID", schema.NewUUIDConstraint()},
		} {
			check := value.Check(c.text, k.c)
			if want := "expected " + k.want + ", got a string that is not valid UTF-8: " + c.quoted; check == nil || check.Error() != want {
				t.Errorf("%s, check at %s:\n got %.200v\nwant %s", c.name, k.c.Kind(), check, want)
			}
			_, coerce := value.Coerce(c.text, k.c)
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
func TestCheck_ARefusedStringsMessageIsBounded(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		strings.Repeat("a", 1<<20) + "\xff",
		strings.Repeat("\xff", 1<<20),
		strings.Repeat("\x00", 1<<20) + "\xff",
	} {
		err := value.Check(text, schema.NewStringConstraint())
		if err == nil {
			t.Fatal("the string was accepted")
		}
		if got := len(err.Error()); got > 256 {
			t.Errorf("a %d-byte string draws a %d-byte message, want at most 256", len(text), got)
		}
	}
}

// A value that is no string is named by its Go type at every text kind.
func TestCheck_AValueThatIsNoStringIsNamedByItsType(t *testing.T) {
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
		if err := value.Check(5, k.c); err == nil || err.Error() != k.want {
			t.Errorf("5 at %s: %v, want %q", k.c.Kind(), err, k.want)
		}
	}
}

// A struct whose one field is an embedded time.Time, a generated Date or
// Timestamp data type's shape, is refused at a Date or Timestamp position with
// a message that says to pass its .Time. A struct that holds a time.Time under
// a name or beside another field, and a nil pointer, are not told so.
func TestCheck_AGeneratedTimeTypeIsToldToPassItsTime(t *testing.T) {
	t.Parallel()
	type generatedDate struct{ time.Time }
	type namedTime struct{ At time.Time }
	type otherEmbed struct{ fmt.Stringer }
	type timeAndMore struct {
		time.Time
		X int
	}
	at := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		val  any
		c    schema.Constraint
		want string
	}{
		{generatedDate{at}, schema.NewDateConstraint(), "expected date string or time.Time (pass a generated type's .Time), got value_test.generatedDate"},
		{&generatedDate{at}, schema.NewDateConstraint(), "expected date string or time.Time (pass a generated type's .Time), got *value_test.generatedDate"},
		{generatedDate{at}, schema.NewTimestampConstraint(), "expected timestamp string or time.Time (pass a generated type's .Time), got value_test.generatedDate"},
		{namedTime{at}, schema.NewDateConstraint(), "expected date string or time.Time, got value_test.namedTime"},
		{otherEmbed{}, schema.NewDateConstraint(), "expected date string or time.Time, got value_test.otherEmbed"},
		{timeAndMore{Time: at}, schema.NewDateConstraint(), "expected date string or time.Time, got value_test.timeAndMore"},
		{&timeAndMore{Time: at}, schema.NewTimestampConstraint(), "expected timestamp string or time.Time, got *value_test.timeAndMore"},
		{(*generatedDate)(nil), schema.NewDateConstraint(), "expected date string or time.Time, got *value_test.generatedDate"},
	} {
		var ce *value.CheckError
		if err := value.Check(c.val, c.c); !errors.As(err, &ce) || ce.Kind != value.KindTypeMismatch || err.Error() != c.want {
			t.Errorf("%T at %s: %v, want a type mismatch %q", c.val, c.c.Kind(), err, c.want)
		}
	}
	if err := value.Check(generatedDate{at}.Time, schema.NewDateConstraint()); err != nil {
		t.Errorf("the struct's .Time is refused: %v", err)
	}
}

// A refusal at a Date or Timestamp position allocates no more than the same
// refusal at a UUID position, whose message names no generated type: the check
// for a generated time type's shape costs a refusal nothing, however many
// fields the refused struct has, and a hinted refusal is built in place of the
// plain one. The detector makes sync.Pool drop a quarter of what fmt returns to
// it, so a refusal's count varies under -race alone.
func TestCheck_ATimeRefusalAllocatesNoMoreThanAUUIDRefusal(t *testing.T) {
	raceskip.Skip(t)
	type wide struct{ A, B, C, D, E, F, G, H int }
	type generatedDate struct{ time.Time }
	at := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	var fields []reflect.StructField
	for i := range 300 {
		fields = append(fields, reflect.StructField{Name: fmt.Sprintf("F%d", i), Type: reflect.TypeFor[int]()})
	}
	widest := reflect.New(reflect.StructOf(fields))
	uuid := schema.NewUUIDConstraint()
	for _, c := range []schema.Constraint{schema.NewDateConstraint(), schema.NewTimestampConstraint()} {
		for _, r := range []struct {
			name string
			v    any
		}{
			{"int", 5},
			{"bool", true},
			{"[]any", []any{}},
			{"wide", wide{}},
			{"*wide", &wide{}},
			{"nil *wide", (*wide)(nil)},
			{"300 fields", widest.Elem().Interface()},
			{"*300 fields", widest.Interface()},
			{"generated", generatedDate{at}},
			{"*generated", &generatedDate{at}},
		} {
			want := testing.AllocsPerRun(100, func() { _ = value.Check(r.v, uuid) })
			got := testing.AllocsPerRun(100, func() { _ = value.Check(r.v, c) })
			if got > want {
				t.Errorf("%s at %s: %v allocations, want at most %v, a UUID refusal's", r.name, c.Kind(), got, want)
			}
		}
	}
}

// A number literal in a message is cut to whole characters, so the message is
// valid UTF-8 whatever the literal holds.
func TestCoerce_ACutLiteralKeepsWholeCharacters(t *testing.T) {
	t.Parallel()
	lit := json.Number("a" + strings.Repeat("é", 20))
	_, err := value.Coerce(lit, schema.NewFloatConstraint())
	if err == nil {
		t.Fatal("the literal was coerced")
	}
	if !utf8.ValidString(err.Error()) || strings.Contains(err.Error(), `\x`) {
		t.Errorf("the message splits a character: %q", err.Error())
	}
}

// A checker that quotes the value it refuses quotes it as strconv.Quote does,
// and cuts a long one to its head.
func TestCheck_ARefusedValueIsQuoted(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 100)
	cut := `"` + long[:32] + `"… (100 bytes)`
	coerced := func(_ any, err error) error { return err }
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"UUID", value.Check("xyz", schema.NewUUIDConstraint()), `invalid UUID: "xyz"`},
		{"enum", value.Check("c", schema.NewEnumConstraint([]string{"a", "b"})), `value "c" not in enum [a b]`},
		{"pattern", value.Check("b", schema.NewPatternConstraint(regexp.MustCompile("^a$"))), `value "b" does not match pattern ^a$`},
		{
			"a literal that is no number", coerced(value.Coerce(json.Number(long), schema.NewFloatConstraint())),
			`cannot coerce json.Number "` + long[:32] + `"… (100 bytes) to float64: no JSON number literal`,
		},
		{"a long UUID", value.Check(long, schema.NewUUIDConstraint()), `invalid UUID: ` + cut},
		{"a long enum value", value.Check(long, schema.NewEnumConstraint([]string{"a", "b"})), `value ` + cut + ` not in enum [a b]`},
		{
			"a long pattern value", value.Check(long, schema.NewPatternConstraint(regexp.MustCompile("^a$"))),
			`value ` + cut + ` does not match pattern ^a$`,
		},
		{"a long date", value.Check(long, schema.NewDateConstraint()), `invalid date format: ` + cut + ` (expected YYYY-MM-DD)`},
		{"a long timestamp", value.Check(long, schema.NewTimestampConstraint()), `invalid timestamp format: ` + cut},
	} {
		if c.err == nil || c.err.Error() != c.want {
			t.Errorf("%s: %v, want %q", c.name, c.err, c.want)
		}
	}
}
