package neo4j_test

import (
	"testing"
	"time"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/schema"
)

// The driver picks its wire encoding by the zone abbreviation: an offset is
// sent only when Zone() reports exactly this name, and every other value is
// sent as a time-zone identifier taken from Location().String().
const driverOffsetZoneName = "Offset"

// TestCoerce_OffsetOnlyInstantsCarryTheDriverZoneName pins the repair. A
// numeric RFC 3339 offset parses into an unnamed location, whose empty
// identifier the server rejects with "Illegal epoch adjustment" — so a
// validated timestamp outside UTC could not be written at all.
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
				t.Errorf("zone name = %q, want %q — the driver would send an empty time-zone identifier",
					zone, driverOffsetZoneName)
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

// TestCoerce_UTCAndNamedZonesAreUntouched is the control. A named location is
// a zone identifier the server resolves and carries more than an offset does,
// so the repair must leave both alone.
func TestCoerce_UTCAndNamedZonesAreUntouched(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()

	utc := coerceTime(t, c, "2026-08-19T12:00:00Z")
	if zone, _ := utc.Zone(); zone != "UTC" {
		t.Errorf("a Z timestamp landed in zone %q, want UTC", zone)
	}

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	in := time.Date(2026, 8, 19, 12, 0, 0, 0, berlin)
	got := coerceTime(t, c, in)
	if got.Location().String() != "Europe/Berlin" {
		t.Errorf("a named location became %q, want Europe/Berlin", got.Location())
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
	got, err := neo4j.Coerce(c, in)
	if err != nil {
		t.Fatalf("Coerce(%#v): %v", in, err)
	}
	tt, ok := got.(time.Time)
	if !ok {
		t.Fatalf("Coerce returned %T, want time.Time", got)
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

// TestCoerce_TextDerivedInstantNeverCarriesTheHostZone is the regression pin for
// the host-dependence v0.13.0 made reachable.
//
// time.Parse returns time.Local, not an unnamed zone, when the parsed offset
// equals the local zone's offset at that instant. Zone() then reports a
// non-empty abbreviation, so the unnamed-zone rescue does not fire, and the
// driver sends Location().String() as a time-zone identifier — "Local" on a host
// with no TZ set, which the server rejects with "Illegal zone identifier".
//
// It sets time.Local rather than reading it. A test that derives its fixture
// from the host's own zone cannot reproduce the case on a UTC host, because
// UTC-offset text parses to time.UTC and never to time.Local — so it would pass
// vacuously on every CI runner, which is where a regression would land. The
// substitute zone carries a name no tz database holds, which is what the driver
// would send and the server would refuse.
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
	if name, _ := parsed.Zone(); name == "" {
		t.Fatalf("time.Parse(%q) produced an unnamed zone; the unnamed rescue would cover it and this test proves nothing", text)
	}

	got := coerceTime(t, schema.NewTimestampConstraint(), text)
	if got.Location() == time.Local {
		t.Errorf("coerced value still carries the host's location %q", got.Location())
	}
	if zone, _ := got.Zone(); zone != driverOffsetZoneName {
		t.Errorf("zone name = %q, want %q — the driver would send it as a time-zone identifier",
			zone, driverOffsetZoneName)
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

		// Either UTC, or the driver's offset sentinel. Never a host-supplied
		// zone identifier, and never an empty one.
		switch zone {
		case "UTC", driverOffsetZoneName:
		default:
			t.Errorf("%q coerced to zone %q; only UTC and %q are host-independent",
				text, zone, driverOffsetZoneName)
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

// TestCoerce_ResolvableCallerLocationIsKept holds the boundary: a caller's
// location is kept when the host's tz database resolves its name — an IANA
// name carries more than an offset does — and only then.
// TestCoerce_UnresolvableZoneNameIsSentAsOffset is the other side.
func TestCoerce_ResolvableCallerLocationIsKept(t *testing.T) {
	t.Parallel()
	c := schema.NewTimestampConstraint()

	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	got := coerceTime(t, c, time.Date(2026, 8, 19, 12, 0, 0, 0, berlin))
	if got.Location().String() != "Europe/Berlin" {
		t.Errorf("a caller-supplied IANA location became %q", got.Location())
	}

	// A legacy name the database still holds is a zone identifier too.
	est := coerceTime(t, c, time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone("EST", -5*60*60)))
	if est.Location().String() != "EST" {
		t.Errorf("a resolvable legacy name became %q", est.Location())
	}

	// The driver's own offset sentinel is already the shape it sends.
	in := time.Date(2026, 8, 19, 12, 0, 0, 0, time.FixedZone(driverOffsetZoneName, 2*60*60))
	got = coerceTime(t, c, in)
	if zone, offset := got.Zone(); zone != driverOffsetZoneName || offset != 2*60*60 {
		t.Errorf("the offset sentinel moved to %q/%d", zone, offset)
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

// TestCoerce_HostLocalTimeValueIsSentAsOffset covers the time.Time arm, which
// the text-arm tests above never reach: a value built in time.Local — what
// time.Now produces — must never carry the host's location to the driver,
// whatever name that location resolves to on this host.
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

// TestCoerce_UnresolvableZoneNameIsSentAsOffset pins the rule for a caller
// location whose name the host cannot resolve: the driver would send the
// name as a time-zone identifier and the server would refuse it, so the
// instant is re-expressed in its offset. "Local" is included because
// time.LoadLocation resolves that name to time.Local, which would keep it.
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
