package value_test

import (
	"errors"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

const offsetSecondsLayout = "2006-01-02 15:04:05 -07:00:00"

// A time.Time is judged as the text it is stored as, so a time whose text the
// same check refuses is refused, as that text is: a year outside 0 to 9999,
// and an offset of seconds west of UTC and under a minute, which a layout
// writes with the sign inside it.
func TestCheck_ATimeWhoseStoredTextDoesNotReadBackIsRefused(t *testing.T) {
	t.Parallel()
	far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)
	unnamed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 10800))
	secondsWest := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", -2))
	toTheSecond := schema.NewTimestampConstraintFormatted(offsetSecondsLayout)
	for _, c := range []struct {
		name string
		val  time.Time
		c    schema.Constraint
		want string
	}{
		{"past year 9999 at a Timestamp", far, schema.NewTimestampConstraint(), `invalid timestamp format: "10000-01-01T00:00:00Z"`},
		{"before year 0 at a Timestamp", before, schema.NewTimestampConstraint(), `invalid timestamp format: "-0001-01-01T00:00:00Z"`},
		{"past year 9999 at a Date", far, schema.NewDateConstraint(), `invalid date format: "10000-01-01" (expected YYYY-MM-DD)`},
		{"two seconds west under a layout that writes offset seconds", secondsWest, toTheSecond, `invalid timestamp format: "2024-01-02 03:04:05 +00:00:-02" (expected 2006-01-02 15:04:05 -07:00:00)`},
	} {
		err := value.Check(c.val, c.c)
		var checkErr *value.CheckError
		if err == nil || !errors.As(err, &checkErr) || checkErr.Kind != value.KindConstraintFail || err.Error() != c.want {
			t.Errorf("%s: Check = %v; want the constraint failure %q", c.name, err, c.want)
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
		{"year 0 at a Date", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), schema.NewDateConstraint()},
		{"two seconds east under a layout that writes offset seconds", time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 2)), toTheSecond},
		{"a minute and two seconds west under that layout", time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", -62)), toTheSecond},
		{"an unnamed zone at a Timestamp", unnamed, schema.NewTimestampConstraint()},
	} {
		if err := value.Check(c.val, c.c); err != nil {
			t.Errorf("%s: Check = %v; want nil", c.name, err)
		}
	}
}

// Text is judged as the text it is stored as too: text a layout reads and
// stores as text it does not read back is refused by the check and by the
// stored form alike, so a value the check passes is one that is stored.
func TestCheck_TextWhoseStoredTextDoesNotReadBackIsRefused(t *testing.T) {
	t.Parallel()
	toTheSecond := schema.NewTimestampConstraintFormatted(offsetSecondsLayout)
	const (
		given     = "2024-01-02 03:04:05 -00:00:02"
		wantCheck = `invalid timestamp format: "2024-01-02 03:04:05 -00:00:02" is stored as "2024-01-02 03:04:05 +00:00:-02", which does not read back as written (expected 2006-01-02 15:04:05 -07:00:00)`
		wantStore = `value: "2024-01-02 03:04:05 -00:00:02" is stored as "2024-01-02 03:04:05 +00:00:-02", which does not read back as written`
	)
	err := value.Check(given, toTheSecond)
	var checkErr *value.CheckError
	if err == nil || !errors.As(err, &checkErr) || checkErr.Kind != value.KindConstraintFail || err.Error() != wantCheck {
		t.Errorf("Check = %v; want the constraint failure %q", err, wantCheck)
	}
	if stored, err := value.Canonical(given, toTheSecond); err == nil || err.Error() != wantStore || stored != given {
		t.Errorf("Canonical = %#v, %v; want the text as given and %q", stored, err, wantStore)
	}
	if _, err := value.Coerce(given, toTheSecond); err == nil {
		t.Error("Coerce stored a value its check refuses")
	}

	// The controls: text in its stored form, and text stored as other text
	// the layout reads.
	for text, want := range map[string]string{
		"2024-01-02 03:04:05 +00:00:02": "2024-01-02 03:04:05 +00:00:02",
		"2024-01-02 03:04:05 -00:01:02": "2024-01-02 03:04:05 -00:01:02",
		"2024-01-02 03:04:05 -00:00:01": "2024-01-02 03:04:05 +00:00:00",
	} {
		if err := value.Check(text, toTheSecond); err != nil {
			t.Errorf("Check(%q) = %v; want nil", text, err)
		}
		if stored, err := value.Canonical(text, toTheSecond); err != nil || stored != want {
			t.Errorf("Canonical(%q) = %#v, %v; want %q", text, stored, err, want)
		}
	}
	// A fraction the layout does not declare is read, and stored without it.
	noFraction := schema.NewTimestampConstraintFormatted("2006-01-02T15:04:05Z07:00")
	if err := value.Check("2026-08-19T12:34:56.789+02:00", noFraction); err != nil {
		t.Errorf("Check of a fraction under a layout with none = %v; want nil", err)
	}
	if stored, err := value.Canonical("2026-08-19T12:34:56.789+02:00", noFraction); err != nil || stored != "2026-08-19T12:34:56+02:00" {
		t.Errorf("Canonical of a fraction under a layout with none = %#v, %v; want it stored without the fraction", stored, err)
	}
	for text, want := range map[string]string{
		"2024-01-02T03:04:05.500Z":      "2024-01-02T03:04:05.5Z",
		"2024-01-02T03:04:05+00:00":     "2024-01-02T03:04:05Z",
		"2024-01-02T03:04:05.000+24:00": "2024-01-02T03:04:05+24:00",
	} {
		if err := value.Check(text, schema.NewTimestampConstraint()); err != nil {
			t.Errorf("Check(%q) = %v; want nil", text, err)
		}
		if stored, err := value.Canonical(text, schema.NewTimestampConstraint()); err != nil || stored != want {
			t.Errorf("Canonical(%q) = %#v, %v; want %q", text, stored, err, want)
		}
	}
}

// The stored text is shown whole beside the given text's head, so a refusal
// shows where the two differ: here in an offset past the given text's cut.
// Under no layout the offset +24:60 is one such text, which Go reads as 25
// hours and writes as text it does not read.
func TestCheck_AnUnsettledTextsRefusalShowsTheStoredTextWhole(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		c         schema.Constraint
		given     string
		wantCheck string
		wantStore string
	}{
		{
			schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05.000000000 -0700"),
			"2026-08-19 12:00:00.000000000 +2460",
			`invalid timestamp format: "2026-08-19 12:00:00.000000000 +2"… (35 bytes) is stored as "2026-08-19 12:00:00.000000000 +2500", which does not read back as written (expected 2006-01-02 15:04:05.000000000 -0700)`,
			`value: "2026-08-19 12:00:00.000000000 +2"… (35 bytes) is stored as "2026-08-19 12:00:00.000000000 +2500", which does not read back as written`,
		},
		{
			schema.NewTimestampConstraint(),
			"2024-01-02T03:04:05+24:60",
			`invalid timestamp format: "2024-01-02T03:04:05+24:60" is stored as "2024-01-02T03:04:05+25:00", which does not read back as written`,
			`value: "2024-01-02T03:04:05+24:60" is stored as "2024-01-02T03:04:05+25:00", which does not read back as written`,
		},
	} {
		if err := value.Check(row.given, row.c); err == nil || err.Error() != row.wantCheck {
			t.Errorf("Check(%q) = %v; want %s", row.given, err, row.wantCheck)
		}
		if stored, err := value.Canonical(row.given, row.c); err == nil || err.Error() != row.wantStore || stored != row.given {
			t.Errorf("Canonical(%q) = %#v, %v; want the text as given and %s", row.given, stored, err, row.wantStore)
		}
	}
	// The control: the widest offset Go reads back is stored.
	if stored, err := value.Canonical("2024-01-02T03:04:05+24:59", schema.NewTimestampConstraint()); err != nil || stored != "2024-01-02T03:04:05+24:59" {
		t.Errorf("Canonical of an offset of 24:59 = %#v, %v; want it stored", stored, err)
	}
}

// Stored text is text the layout stores as itself. Under "23_2", a day, an
// hour and the day again, Go reads "22122" as hour 12 of the 2nd and writes
// that as "212 2", which it reads as hour 2 and writes as "22 2": a second read
// would move the value, so the text is refused, and comes back as given.
func TestCheck_TextWhoseStoredTextIsStoredAsOtherTextIsRefused(t *testing.T) {
	t.Parallel()
	type carrier string
	dayHourDay := schema.NewTimestampConstraintFormatted("23_2")
	if err := schema.CheckConstraint(dayHourDay); err != nil {
		t.Fatalf("fixture: the layout is refused: %v", err)
	}
	const wantCheck = `invalid timestamp format: "22122" is stored as "212 2", which does not read back as written (expected 23_2)`
	for _, given := range []any{"22122", carrier("22122")} {
		if err := value.Check(given, dayHourDay); err == nil || err.Error() != wantCheck {
			t.Errorf("Check(%#v) = %v; want %s", given, err, wantCheck)
		}
		stored, err := value.Canonical(given, dayHourDay)
		if err == nil || err.Error() != `value: "22122" is stored as "212 2", which does not read back as written` || stored != given {
			t.Errorf("Canonical(%#v) = %#v, %v; want the value as given and the refusal", given, stored, err)
		}
		if _, err := value.Coerce(given, dayHourDay); err == nil {
			t.Errorf("Coerce(%#v) stored a value its check refuses", given)
		}
	}
	// The controls: the stored text does read, and other text is stored as itself.
	if _, err := time.ParseInLocation("23_2", "212 2", time.UTC); err != nil {
		t.Errorf("fixture: the stored text is unread: %v", err)
	}
	if stored, err := value.Canonical("101010", dayHourDay); err != nil || stored != "101010" {
		t.Errorf("Canonical of settled text = %#v, %v; want it stored as given", stored, err)
	}
}

// A time.Time is stored as its rendered text is stored, so a time and the text
// it renders as reach one spelling, and a time whose text is refused is
// refused. RFC 3339 writes an offset under a minute as +00:00, which is stored
// as Z.
func TestCheck_ATimeIsStoredAsItsRenderedTextIs(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name     string
		val      time.Time
		c        schema.Constraint
		rendered string
		stored   string
	}{
		{"an offset under a minute", time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", 2)), schema.NewTimestampConstraint(), "2026-08-19T12:00:00+00:00", "2026-08-19T12:00:00Z"},
		{"an offset of whole minutes", time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("", 120)), schema.NewTimestampConstraint(), "2026-08-19T12:00:00+00:02", "2026-08-19T12:00:00+00:02"},
		{"a year whose two-digit field reads into another century", time.Date(1950, 6, 1, 0, 0, 0, 0, time.UTC), schema.NewTimestampConstraintFormatted("2006 06"), "1950 50", "2050 50"},
	} {
		tc, _ := row.c.(schema.TimestampConstraint)
		layout := tc.Format()
		if layout == "" {
			layout = time.RFC3339Nano
		}
		if got := row.val.Format(layout); got != row.rendered {
			t.Fatalf("fixture: %s renders as %q; want %q", row.name, got, row.rendered)
		}
		if err := value.Check(row.val, row.c); err != nil {
			t.Errorf("%s: Check = %v; want nil", row.name, err)
		}
		for _, given := range []any{row.val, row.rendered, row.stored} {
			if stored, err := value.Canonical(given, row.c); err != nil || stored != row.stored {
				t.Errorf("%s: Canonical(%#v) = %#v, %v; want %q", row.name, given, stored, err, row.stored)
			}
		}
	}

	// Under "21 2" the second of November renders as "211 2", which Go reads
	// as the first of the month 21 and stores as "21 2", text it does not read.
	dayMonthDay := schema.NewTimestampConstraintFormatted("21 2")
	if err := schema.CheckConstraint(dayMonthDay); err != nil {
		t.Fatalf("fixture: the layout is refused: %v", err)
	}
	november := time.Date(1950, 11, 2, 0, 12, 7, 0, time.UTC)
	const wantCheck = `invalid timestamp format: "211 2" is stored as "21 2", which does not read back as written (expected 21 2)`
	if err := value.Check(november, dayMonthDay); err == nil || err.Error() != wantCheck {
		t.Errorf("Check of a time whose text is refused = %v; want %s", err, wantCheck)
	}
	if stored, err := value.Canonical(november, dayMonthDay); err == nil || err.Error() != `value: "211 2" is stored as "21 2", which does not read back as written` || stored != any(november) {
		t.Errorf("Canonical of a time whose text is refused = %#v, %v; want the time as given and the refusal", stored, err)
	}
	if _, err := value.Coerce(november, dayMonthDay); err == nil {
		t.Error("Coerce stored a time its check refuses")
	}
}

// Text in its stored form costs one read: the check allocates what
// time.ParseInLocation alone allocates for it, and the stored form adds the
// box of the text it returns as given. A time that renders as its stored text
// adds the rendering. A stored text of 64 bytes or fewer is built on the stack.
func TestCheck_TextInItsStoredFormAllocatesWhatOneReadDoes(t *testing.T) {
	raceskip.Skip(t)
	const long = "Monday, 02 January 2006 15:04:05.000000000 -07:00"
	for _, row := range []struct {
		c      schema.Constraint
		layout string
		text   string
	}{
		{schema.NewTimestampConstraint(), time.RFC3339Nano, "2024-01-02T03:04:05.12Z"},
		{schema.NewTimestampConstraint(), time.RFC3339Nano, "2024-01-02T03:04:05.12+05:30"},
		{schema.NewTimestampConstraintFormatted(long), long, "Tuesday, 02 January 2024 03:04:05.120000000 +05:30"},
	} {
		var err error
		var at time.Time
		read := testing.AllocsPerRun(100, func() { at, err = time.ParseInLocation(row.layout, row.text, time.UTC) })
		if err != nil {
			t.Fatalf("fixture: %q: %v", row.text, err)
		}
		var given, instant any = row.text, at
		for _, call := range []struct {
			name      string
			val       any
			check     float64
			canonical float64
		}{
			{"the text", given, read, read + 1},
			{"its time", instant, read + 1, read + 2},
		} {
			if allocs := testing.AllocsPerRun(100, func() { err = value.Check(call.val, row.c) }); allocs != call.check || err != nil {
				t.Errorf("%q: Check of %s allocated %v times, %v; want %v of one read's %v", row.text, call.name, allocs, err, call.check, read)
			}
			var stored any
			if allocs := testing.AllocsPerRun(100, func() { stored, err = value.Canonical(call.val, row.c) }); allocs != call.canonical || err != nil || stored != given {
				t.Errorf("%q: Canonical of %s = %#v, %v, allocated %v times; want the text and %v allocations of one read's %v", row.text, call.name, stored, err, allocs, call.canonical, read)
			}
		}
	}
}
