package neo4j_test

import (
	"testing"
	"time"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/schema"
)

// The driver picks its wire encoding by the location's name: a name that loads
// as a zone whose offset agrees is sent as a time-zone identifier, and every
// other value is sent as an offset. No zone has this name, and the driver's
// read path builds each offset instant in a location so named.
const driverOffsetZoneName = "Offset"

// An instant at a numeric RFC 3339 offset goes in a location of the driver's
// offset name at that offset, whether text or a time.Time carries it.
func TestCoerce_OffsetOnlyInstantsCarryTheDriverZoneName(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()

	cases := map[string]any{
		"string with a numeric offset":  "2026-08-19T12:00:00+02:00",
		"string with a negative offset": "2026-08-19T12:00:00-05:00",
		"pre-parsed time.Time":          mustParse(t, "2026-08-19T12:00:00+02:00"),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := coerceTime(t, c, in)
			zone, offset := got.Zone()
			if zone != driverOffsetZoneName {
				t.Errorf("zone name = %q, want %q", zone, driverOffsetZoneName)
			}
			if want := 2 * 60 * 60; name == "string with a negative offset" {
				if offset != -5*60*60 {
					t.Errorf("offset = %d, want %d", offset, -5*60*60)
				}
			} else if offset != want {
				t.Errorf("offset = %d, want %d", offset, want)
			}
		})
	}
}

// A UTC instant goes as offset zero, never under the name UTC: the server holds
// the named form unequal to its own reading of the same RFC 3339 text.
func TestCoerce_AUTCInstantGoesAsOffsetZero(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()
	want := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	for _, in := range []any{"2026-08-19T12:00:00Z", "2026-08-19T12:00:00+00:00", want} {
		got := coerceTime(t, c, in)
		if zone, offset := got.Zone(); zone != driverOffsetZoneName || offset != 0 || got.Location().String() != driverOffsetZoneName {
			t.Errorf("%#v went as zone %q at %d in %q, want %q at 0", in, zone, offset, got.Location(), driverOffsetZoneName)
		}
		if !got.Equal(want) {
			t.Errorf("%#v went as %s, want the same instant as %s", in, got, want)
		}
	}
}

// Every instant at offset zero goes in one shared location, whatever location
// its text was read into, so the common instant costs no location of its own.
// Text that names GMT at offset zero is read into a location of that name. An
// instant at another offset is the control.
func TestCoerce_UTCInstantsShareOneLocation(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()
	named := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05 -0700 MST")
	first := coerceTime(t, c, "2026-08-19T12:00:00Z").Location()
	for text, at := range map[string]schema.Constraint{
		"2026-08-19T12:00:00Z":          c,
		"2001-01-01T00:00:00.5+00:00":   c,
		"2026-08-19 12:00:00 +0000 GMT": named,
	} {
		if got := coerceTime(t, at, text).Location(); got != first {
			t.Errorf("%s went in a location of its own, %p beside %p", text, got, first)
		}
	}
	if got := coerceTime(t, c, "2026-08-19T12:00:00+02:00").Location(); got == first {
		t.Error("control: an instant at +02:00 went in the UTC instants' location")
	}
}

// A layout that holds a zone abbreviation beside a numeric offset sends the
// offset and never the abbreviation, one the host's tz database resolves at
// that offset included.
func TestCoerce_AZoneAbbreviationInTheTextNeverReachesTheDriver(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05 -0700 MST")
	for text, offset := range map[string]int{
		"2026-08-19 12:00:00 +0000 GMT": 0,
		"2026-08-19 12:00:00 +0000 UTC": 0,
		"2026-01-19 12:00:00 -0500 EST": -5 * 60 * 60,
		"2026-01-19 12:00:00 +0100 CET": 60 * 60,
		"2026-01-19 12:00:00 +0300 QQQ": 3 * 60 * 60,
	} {
		got := coerceTime(t, c, text)
		if zone, at := got.Zone(); zone != driverOffsetZoneName || at != offset || got.Location().String() != driverOffsetZoneName {
			t.Errorf("%q went as zone %q at %d in %q, want %q at %d", text, zone, at, got.Location(), driverOffsetZoneName, offset)
		}
	}
}

// TestCoerce_OffsetRepairPreservesTheInstant holds the one property the repair
// must not break: it moves the location the value is expressed in, never the
// moment it names.
func TestCoerce_OffsetRepairPreservesTheInstant(t *testing.T) {
	t.Parallel()
	in := mustParse(t, "2026-08-19T12:00:00.5+02:00")
	got := coerceTime(t, schema.NewTimestampConstraint(), in)
	if !got.Equal(in) {
		t.Errorf("coerced %s, want the same instant as %s", got, in)
	}
}

// TestCoerce_DeclaredLayoutAlsoCarriesTheDriverZoneName covers the second
// parse path in the same arm, which a test on the RFC 3339 path alone misses.
func TestCoerce_DeclaredLayoutAlsoCarriesTheDriverZoneName(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05 -0700")
	got := coerceTime(t, c, "2026-08-19 12:00:00 +0200")
	if zone, _ := got.Zone(); zone != driverOffsetZoneName {
		t.Errorf("zone name = %q, want %q", zone, driverOffsetZoneName)
	}
}

func coerceTime(t *testing.T, c schema.Constraint, in any) time.Time {
	t.Helper()
	got, err := neo4j.CoerceParams(map[string]any{"v": in}, neo4j.ParamTypes{"v": c})
	if err != nil {
		t.Fatalf("CoerceParams(%#v): %v", in, err)
	}
	tt, ok := got["v"].(time.Time)
	if !ok {
		t.Fatalf("CoerceParams returned %T, want time.Time", got["v"])
	}
	return tt
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return tt
}

// Text at the host zone's own offset goes in the driver's offset location and
// never in the host's. time.Parse reads such text into time.Local, and the
// driver judges that location's name on each host.
//
// It sets time.Local rather than reading it: text at a UTC host's offset parses
// to time.UTC and never to time.Local, so a fixture drawn from the host's own
// zone passes vacuously on a UTC runner.
//
// Not parallel: it writes a package-level variable of the standard library.
func TestCoerce_TextDerivedInstantNeverCarriesTheHostZone(t *testing.T) {
	restore := time.Local
	t.Cleanup(func() { time.Local = restore })
	time.Local = time.FixedZone("XYZ", -4*60*60)

	// Text at exactly the substitute zone's offset — the input time.Parse
	// resolves to time.Local.
	local := time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local)
	text := local.Format(time.RFC3339Nano)

	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		t.Fatalf("parse %q: %v", text, err)
	}
	if parsed.Location() != time.Local {
		t.Fatalf("time.Parse(%q) did not resolve to time.Local; the fixture no longer reproduces the case", text)
	}

	got := coerceTime(t, schema.NewTimestampConstraint(), text)
	if got.Location() == time.Local {
		t.Errorf("coerced value still carries the host's location %q", got.Location())
	}
	if zone, _ := got.Zone(); zone != driverOffsetZoneName {
		t.Errorf("zone name = %q, want %q", zone, driverOffsetZoneName)
	}
	if !got.Equal(local) {
		t.Errorf("coerced %s, want the same instant as %s", got, local)
	}
}

// TestCoerce_IsHostIndependent is the property the fix exists for, stated
// directly: one text coerces to one driver payload, wherever it runs.
func TestCoerce_IsHostIndependent(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()

	for _, text := range []string{
		"2026-08-19T12:00:00Z",
		"2026-08-19T12:00:00+02:00",
		"2026-08-19T12:00:00-05:00",
		time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local).Format(time.RFC3339Nano),
	} {
		got := coerceTime(t, c, text)
		zone, offset := got.Zone()

		// The driver's offset sentinel alone. Never a zone identifier, the
		// host's or the text's, and never an empty one.
		if zone != driverOffsetZoneName {
			t.Errorf("%q coerced to zone %q; want %q", text, zone, driverOffsetZoneName)
		}

		// Whatever the zone, the offset must be the one the text stated.
		want, err := time.Parse(time.RFC3339, text)
		if err != nil {
			t.Fatalf("parse %q: %v", text, err)
		}
		if _, wantOffset := want.Zone(); offset != wantOffset {
			t.Errorf("%q coerced to offset %d, want %d", text, offset, wantOffset)
		}
	}
}

// TestCoerce_ACallersLocationIsSentAsItsOffset holds the stored-form rule for an
// instant: a time.Time is read as the text the validator stores for it, which
// carries an offset and no zone name, so a caller's location reaches the driver
// as its offset, as the write path sends the same instant. An IANA name and a
// legacy name the host's tz database resolves are no exception.
func TestCoerce_ACallersLocationIsSentAsItsOffset(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	for name, tc := range map[string]struct {
		loc    *time.Location
		offset int
	}{
		"an IANA location":         {berlin, 2 * 60 * 60},
		"a resolvable legacy name": {time.FixedZone("EST", -5*60*60), -5 * 60 * 60},
		"the offset sentinel":      {time.FixedZone(driverOffsetZoneName, 2*60*60), 2 * 60 * 60},
	} {
		in := time.Date(2026, 8, 19, 12, 0, 0, 0, tc.loc)
		got := coerceTime(t, c, in)
		if zone, offset := got.Zone(); zone != driverOffsetZoneName || offset != tc.offset || got.Location().String() != driverOffsetZoneName {
			t.Errorf("%s: zone = %q/%d in %q, want %q/%d", name, zone, offset, got.Location(), driverOffsetZoneName, tc.offset)
		}
		if !got.Equal(in) {
			t.Errorf("%s: coerced %s, want the same instant as %s", name, got, in)
		}
		// The text the validator stores for the instant sends the same value.
		if text := coerceTime(t, c, in.Format(time.RFC3339Nano)); !text.Equal(got) || text.Location().String() != got.Location().String() {
			t.Errorf("%s: its stored text sends %s in %q, the time.Time %s in %q", name, text, text.Location(), got, got.Location())
		}
	}
}

// withLocal substitutes time.Local for the test's duration. Go reads TZ once,
// at the first use of time.Local, so a fixture must assign the variable
// rather than set the environment; the caller must not run in parallel.
func withLocal(t *testing.T, loc *time.Location) {
	t.Helper()
	restore := time.Local
	t.Cleanup(func() { time.Local = restore })
	time.Local = loc
}

// TestCoerce_HostLocalTimeValueIsSentAsOffset covers a time.Time built in
// time.Local, what time.Now produces: it must never carry the host's location
// to the driver, whatever name that location resolves to on this host.
//
// Not parallel: it writes a package-level variable of the standard library.
func TestCoerce_HostLocalTimeValueIsSentAsOffset(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	cases := map[string]*time.Location{
		"a name no database holds":     time.FixedZone("XYZ", -4*60*60),
		"the literal name Local":       time.FixedZone("Local", -4*60*60),
		"UTC, which always resolves":   time.UTC,
		"an IANA name, which resolves": berlin,
	}
	for name, loc := range cases {
		t.Run(name, func(t *testing.T) {
			withLocal(t, loc)
			in := time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local)
			_, wantOffset := in.Zone()

			got := coerceTime(t, schema.NewTimestampConstraint(), in)
			if got.Location() == time.Local {
				t.Errorf("coerced value still carries time.Local (%q)", got.Location())
			}
			if zone, offset := got.Zone(); zone != driverOffsetZoneName || offset != wantOffset {
				t.Errorf("zone = %q/%d, want %q/%d", zone, offset, driverOffsetZoneName, wantOffset)
			}
			if !got.Equal(in) {
				t.Errorf("coerced %s, want the same instant as %s", got, in)
			}
		})
	}
}

// A caller's location goes as its offset whatever its name, one the host cannot
// resolve and "Local" included: the instant goes through its stored text, which
// holds no name.
func TestCoerce_UnresolvableZoneNameIsSentAsOffset(t *testing.T) {
	t.Parallel()
	for name, loc := range map[string]*time.Location{
		"unknown region": time.FixedZone("Mars/Olympus", 3600),
		"named Local":    time.FixedZone("Local", 3600),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := time.Date(2026, 8, 19, 12, 0, 0, 0, loc)
			got := coerceTime(t, schema.NewTimestampConstraint(), in)
			if zone, offset := got.Zone(); zone != driverOffsetZoneName || offset != 3600 {
				t.Errorf("zone = %q/%d, want %q/3600", zone, offset, driverOffsetZoneName)
			}
			if !got.Equal(in) {
				t.Errorf("coerced %s, want the same instant as %s", got, in)
			}
		})
	}
}
