package value_test

import (
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// A time.Time is judged as the text it renders as, so a time whose text the
// same check refuses is refused, as that text is: a year outside 0 to 9999,
// and a UTC offset of 24 hours or more.
func TestCheck_ATimeWhoseTextIsNotStoredIsRefused(t *testing.T) {
	t.Parallel()
	far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)
	at := func(offsetSeconds int) time.Time {
		return time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", offsetSeconds))
	}
	for _, c := range []struct {
		name string
		val  time.Time
		c    schema.Constraint
		want string
	}{
		{"past year 9999 at a Timestamp", far, schema.NewTimestampConstraint(), `invalid timestamp format: "10000-01-01T00:00:00Z"`},
		{"before year 0 at a Timestamp", before, schema.NewTimestampConstraint(), `invalid timestamp format: "-0001-01-01T00:00:00Z"`},
		{"before year 0 with a fraction at a Timestamp", before.Add(-time.Nanosecond), schema.NewTimestampConstraint(), `invalid timestamp format: "-0002-12-31T23:59:59.999999999Z"`},
		{"past year 9999 at a Date", far, schema.NewDateConstraint(), `invalid date format: "10000-01-01" (expected YYYY-MM-DD)`},
		{"an offset of 25 hours at a Timestamp", at(25 * 3600), schema.NewTimestampConstraint(), `invalid timestamp format: "2024-01-02T03:04:05+25:00"`},
		{"an offset of 25 hours west at a Timestamp", at(-25 * 3600), schema.NewTimestampConstraint(), `invalid timestamp format: "2024-01-02T03:04:05-25:00"`},
		{"an offset of 24 hours at a Timestamp", at(24 * 3600), schema.NewTimestampConstraint(), `invalid timestamp format: "2024-01-02T03:04:05+24:00"`},
		{"an offset of 24 hours west at a Timestamp", at(-24 * 3600), schema.NewTimestampConstraint(), `invalid timestamp format: "2024-01-02T03:04:05-24:00"`},
		{"an offset of 24:59 at a Timestamp", at(24*3600 + 59*60), schema.NewTimestampConstraint(), `invalid timestamp format: "2024-01-02T03:04:05+24:59"`},
	} {
		err := value.Check(c.val, c.c)
		var checkErr *value.CheckError
		if err == nil || !errors.As(err, &checkErr) || checkErr.Kind != value.KindConstraintFail || err.Error() != c.want {
			t.Errorf("%s: Check = %v; want the constraint failure %q", c.name, err, c.want)
		}
		if stored, err := value.Canonical(c.val, c.c); err == nil || stored != any(c.val) {
			t.Errorf("%s: Canonical = %#v, %v; want the time as given and a refusal", c.name, stored, err)
		}
		if _, err := value.Coerce(c.val, c.c); err == nil {
			t.Errorf("%s: Coerce stored a value its check refuses", c.name)
		}
	}
	for _, c := range []struct {
		name string
		val  time.Time
		c    schema.Constraint
	}{
		{"year 9999 at a Timestamp", time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), schema.NewTimestampConstraint()},
		{"year 0 at a Timestamp", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), schema.NewTimestampConstraint()},
		{"year 0 at a Date", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), schema.NewDateConstraint()},
		{"two seconds east", at(2), schema.NewTimestampConstraint()},
		{"two seconds west", at(-2), schema.NewTimestampConstraint()},
		{"a minute and two seconds west", at(-62), schema.NewTimestampConstraint()},
		{"an unnamed zone", at(10800), schema.NewTimestampConstraint()},
		{"an offset of 23:59", at(23*3600 + 59*60), schema.NewTimestampConstraint()},
		{"an offset of 23:59 and 59 seconds west", at(-(24*3600 - 1)), schema.NewTimestampConstraint()},
	} {
		if err := value.Check(c.val, c.c); err != nil {
			t.Errorf("%s: Check = %v; want nil", c.name, err)
		}
		if _, err := value.Coerce(c.val, c.c); err != nil {
			t.Errorf("%s: Coerce = %v; want the value stored", c.name, err)
		}
	}
}

// Text is stored only as text time.Time's own encoder writes. Go reads an
// offset hour of 24 and its encoder writes none, so the check and the stored
// form refuse such text alike, as they refuse text Go does not read.
func TestCheck_TextAtAnOffsetHourOfTwentyFourIsRefused(t *testing.T) {
	t.Parallel()
	type carrier string
	c := schema.NewTimestampConstraint()
	for _, row := range []struct {
		given any
		shown string
	}{
		{"2024-01-02T03:04:05+24:00", `"2024-01-02T03:04:05+24:00"`},
		{carrier("2024-01-02T03:04:05-24:00"), `"2024-01-02T03:04:05-24:00"`},
		{"2024-01-02T03:04:05+24:59", `"2024-01-02T03:04:05+24:59"`},
		// Go reads an offset minute of 60 as the next hour.
		{"2024-01-02T03:04:05+23:60", `"2024-01-02T03:04:05+23:60"`},
		{"2024-01-02T03:04:05-24:60", `"2024-01-02T03:04:05-24:60"`},
		{"2024-01-02T03:04:05.123456789+24:00", `"2024-01-02T03:04:05.123456789+24"… (35 bytes)`},
	} {
		err := value.Check(row.given, c)
		var checkErr *value.CheckError
		if want := "invalid timestamp format: " + row.shown; err == nil || !errors.As(err, &checkErr) || checkErr.Kind != value.KindConstraintFail || err.Error() != want {
			t.Errorf("Check(%#v) = %v; want the constraint failure %s", row.given, err, want)
		}
		if stored, err := value.Canonical(row.given, c); err == nil || err.Error() != "value: "+row.shown+" is not an RFC 3339 timestamp" || stored != row.given {
			t.Errorf("Canonical(%#v) = %#v, %v; want the value as given and a refusal that shows %s", row.given, stored, err, row.shown)
		}
		if _, err := value.Coerce(row.given, c); err == nil {
			t.Errorf("Coerce(%#v) stored a value its check refuses", row.given)
		}
	}

	// The controls: text in its stored form, and text stored as other text.
	// The widest offset a Timestamp holds is 23:59.
	for text, want := range map[string]string{
		"2024-01-02T03:04:05.5Z":        "2024-01-02T03:04:05.5Z",
		"2024-01-02T03:04:05.500Z":      "2024-01-02T03:04:05.5Z",
		"2024-01-02T03:04:05+00:00":     "2024-01-02T03:04:05Z",
		"2024-01-02T03:04:05-00:00":     "2024-01-02T03:04:05Z",
		"2024-01-02T03:04:05.000+23:00": "2024-01-02T03:04:05+23:00",
		"2024-01-02T03:04:05+23:59":     "2024-01-02T03:04:05+23:59",
		"2024-01-02T03:04:05-23:59":     "2024-01-02T03:04:05-23:59",
		"2024-01-02T03:04:05+22:60":     "2024-01-02T03:04:05+23:00",
	} {
		if err := value.Check(text, c); err != nil {
			t.Errorf("Check(%q) = %v; want nil", text, err)
		}
		if stored, err := value.Canonical(text, c); err != nil || stored != want {
			t.Errorf("Canonical(%q) = %#v, %v; want %q", text, stored, err, want)
		}
	}
}

// Stored text is stored as itself, so no second read moves a stored value,
// and it is the text time.Time's own encoder writes for the instant it reads
// as. The times run past both ends of what RFC 3339 text holds, in the year
// and in the offset.
func TestCanonical_StoredTextIsStoredAsItself(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()
	r := rand.New(rand.NewPCG(3, 9)) //nolint:gosec // a fixed seed makes the times reproducible
	stored, refused := 0, 0
	for range 20000 {
		zone := time.FixedZone("", r.IntN(2*26*3600)-26*3600)
		at := time.Date(r.IntN(10030)-15, time.Month(1+r.IntN(12)), 1+r.IntN(28), r.IntN(24), r.IntN(60), r.IntN(60), r.IntN(3)*r.IntN(1_000_000_000), zone)
		rendered := at.Format(time.RFC3339Nano)
		first, err := value.Canonical(at, c)
		fromText, textErr := value.Canonical(rendered, c)
		if (err == nil) != (textErr == nil) || (err == nil && first != fromText) {
			t.Fatalf("%s: the time is stored as %#v (%v) and its text as %#v (%v)", rendered, first, err, fromText, textErr)
		}
		if (err == nil) != (value.Check(at, c) == nil) || (err == nil) != (value.Check(rendered, c) == nil) {
			t.Fatalf("%s: Check and Canonical disagree (Canonical: %v)", rendered, err)
		}
		if err != nil {
			refused++
			continue
		}
		stored++
		again, err := value.Canonical(first, c)
		if err != nil || again != first {
			t.Fatalf("%s is stored as %#v, which is stored as %#v (%v)", rendered, first, again, err)
		}
		// The stored text keeps the wall clock; it drops an offset's seconds.
		const wall = "2006-01-02T15:04:05.999999999"
		back, ok := value.ParseTimestamp(first.(string))
		if !ok || back.Format(wall) != at.Format(wall) {
			t.Fatalf("%s is stored as %#v, which reads as %v (ok=%v)", rendered, first, back, ok)
		}
		if written, err := back.MarshalText(); err != nil || string(written) != first {
			t.Fatalf("%s is stored as %#v, which time.Time writes as %q (%v)", rendered, first, written, err)
		}
	}
	if stored < 15000 || refused < 100 {
		t.Errorf("stored %d times and refused %d; want both in number", stored, refused)
	}
}

// A time.Time is stored as its rendered text is stored, so a time and the text
// it renders as reach one spelling. RFC 3339 writes an offset under a minute
// as +00:00, which is stored as Z.
func TestCheck_ATimeIsStoredAsItsRenderedTextIs(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()
	for _, row := range []struct {
		name     string
		val      time.Time
		rendered string
		stored   string
	}{
		{"an offset under a minute", time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", 2)), "2026-08-19T12:00:00+00:00", "2026-08-19T12:00:00Z"},
		{"an offset under a minute, west", time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", -59)), "2026-08-19T12:00:00+00:00", "2026-08-19T12:00:00Z"},
		{"an offset of whole minutes", time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", 120)), "2026-08-19T12:00:00+00:02", "2026-08-19T12:00:00+00:02"},
		{"an offset of minutes and seconds", time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", -(5*3600+30))), "2026-08-19T12:00:00-05:00", "2026-08-19T12:00:00-05:00"},
		{"a named zone", time.Date(2026, 8, 19, 12, 0, 0, 7, time.FixedZone("CEST", 7200)), "2026-08-19T12:00:00.000000007+02:00", "2026-08-19T12:00:00.000000007+02:00"},
	} {
		if got := row.val.Format(time.RFC3339Nano); got != row.rendered {
			t.Fatalf("fixture: %s renders as %q; want %q", row.name, got, row.rendered)
		}
		if err := value.Check(row.val, c); err != nil {
			t.Errorf("%s: Check = %v; want nil", row.name, err)
		}
		for _, given := range []any{row.val, row.rendered, row.stored} {
			if stored, err := value.Canonical(given, c); err != nil || stored != row.stored {
				t.Errorf("%s: Canonical(%#v) = %#v, %v; want %q", row.name, given, stored, err, row.stored)
			}
		}
	}
}

// Text in its stored form costs one read: the check allocates what
// time.ParseInLocation alone allocates for it, and the stored form adds the
// box of the text it returns as given. A time that renders as its stored text
// adds the rendering. The stored text is built on the stack.
func TestCheck_TextInItsStoredFormAllocatesWhatOneReadDoes(t *testing.T) {
	raceskip.Skip(t)
	c := schema.NewTimestampConstraint()
	for _, text := range []string{
		"2024-01-02T03:04:05.12Z",
		"2024-01-02T03:04:05.12+05:30",
		"2024-01-02T03:04:05.123456789-23:59",
	} {
		var err error
		var at time.Time
		read := testing.AllocsPerRun(100, func() { at, err = time.ParseInLocation(time.RFC3339Nano, text, time.UTC) })
		if err != nil {
			t.Fatalf("fixture: %q: %v", text, err)
		}
		var given, instant any = text, at
		for _, call := range []struct {
			name      string
			val       any
			check     float64
			canonical float64
		}{
			{"the text", given, read, read + 1},
			{"its time", instant, read + 1, read + 2},
		} {
			if allocs := testing.AllocsPerRun(100, func() { err = value.Check(call.val, c) }); allocs != call.check || err != nil {
				t.Errorf("%q: Check of %s allocated %v times, %v; want %v of one read's %v", text, call.name, allocs, err, call.check, read)
			}
			var stored any
			if allocs := testing.AllocsPerRun(100, func() { stored, err = value.Canonical(call.val, c) }); allocs != call.canonical || err != nil || stored != given {
				t.Errorf("%q: Canonical of %s = %#v, %v, allocated %v times; want the text and %v allocations of one read's %v", text, call.name, stored, err, allocs, call.canonical, read)
			}
		}
	}
}
