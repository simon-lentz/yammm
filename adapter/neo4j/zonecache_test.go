package neo4j

import (
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/schema"
)

// Both outcomes are memoized: a name the host cannot resolve must not cost a
// zoneinfo lookup on every row of a batch write. An unresolvable name caches an
// explicit nil location, so the miss is recorded rather than repeated.
func TestZoneNamed_CachesEitherAnswer(t *testing.T) {
	t.Parallel()
	for name, wantResolves := range map[string]bool{
		"Nowhere/Nope": false,
		"EST":          true,
	} {
		if got := zoneNamed(name); (got != nil) != wantResolves {
			t.Fatalf("zoneNamed(%q) resolved = %v, want %v", name, got != nil, wantResolves)
		}
		cached, ok := resolvedZones.Load(name)
		if !ok {
			t.Errorf("%q was not cached, so every row of a batch would pay the lookup", name)
			continue
		}
		loc, _ := cached.(*time.Location)
		if (loc != nil) != wantResolves {
			t.Errorf("%q cached as %v, want resolves=%v", name, loc, wantResolves)
		}
	}
}

// A resolvable zone NAME whose offset contradicts the value's goes as a fixed
// offset, not as the name. time.Parse mints exactly this from any layout with a
// zone-abbreviation token, and the server re-derives the offset from a name it
// is sent — seven hours for "MST" carried at +00:00.
//
// Mutation: dropping driverZone's carried-versus-resolved comparison turns this
// red.
func TestDriverZone_ResolvableNameWithContradictingOffsetGoesAsOffset(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"MST", "EST", "CET"} {
		fabricated := time.Date(2024, 6, 1, 12, 0, 0, 0, time.FixedZone(name, 0))
		got := driverZone(fabricated)

		if zone, _ := got.Zone(); zone != driverOffsetZoneName {
			t.Errorf("driverZone kept the zone name %q, so the server would re-derive its real offset and move the instant; got zone %q, want %q",
				name, zone, driverOffsetZoneName)
		}
		if !got.Equal(fabricated) {
			t.Errorf("driverZone moved the instant: %s != %s", got, fabricated)
		}
	}
}

// A location whose name AND offset both agree with the host's tz database is
// kept, so a genuinely zoned value still reaches the server as a zone
// identifier rather than being flattened to an offset.
func TestDriverZone_AgreeingNameIsKept(t *testing.T) {
	t.Parallel()
	loc, err := time.LoadLocation("America/Denver")
	if err != nil {
		t.Skipf("host has no tz database entry for America/Denver: %v", err)
	}
	in := time.Date(2024, 6, 1, 12, 0, 0, 0, loc)

	got := driverZone(in)
	if got.Location() != loc {
		t.Errorf("driverZone re-expressed an agreeing zone as %v, want it kept as %v", got.Location(), loc)
	}
}

// A Date carries no time of day. dbtype.Date holds a whole time.Time and the
// driver derives its day number by an integer division that truncates toward
// zero, so a pre-1970 instant with a time of day landed a day late — measured
// against a live server as 1969-07-20T18:00:00Z stored as 1969-07-21.
//
// It drives [Coerce], not calendarDate, and covers all THREE Date arms: the
// rule holds only where it is wired in, and the dbtype.Date arm passed its
// input through for a release after the time.Time arm was fixed.
func TestCoerceDate_KeepsTheWallClockDate(t *testing.T) {
	t.Parallel()
	instants := []struct {
		name string
		in   time.Time
		want string
	}{
		{"pre-1970 with a time of day", time.Date(1969, 7, 20, 18, 0, 0, 0, time.UTC), "1969-07-20"},
		{"pre-1970 at midnight", time.Date(1969, 7, 20, 0, 0, 0, 0, time.UTC), "1969-07-20"},
		{"post-1970 with a time of day", time.Date(2001, 7, 20, 18, 0, 0, 0, time.UTC), "2001-07-20"},
		{
			"a non-UTC zone keeps its own wall-clock date",
			time.Date(2001, 7, 20, 23, 0, 0, 0, time.FixedZone("X", -5*3600)), "2001-07-20",
		},
	}
	arms := []struct {
		arm  string
		wrap func(time.Time) any
	}{
		{"time.Time", func(t time.Time) any { return t }},
		{"dbtype.Date", func(t time.Time) any { return dbtype.Date(t) }},
	}

	for _, arm := range arms {
		for _, tc := range instants {
			out, err := Coerce(schema.DateConstraint{}, arm.wrap(tc.in))
			if err != nil {
				t.Errorf("%s/%s: Coerce: %v", arm.arm, tc.name, err)
				continue
			}
			d, ok := out.(dbtype.Date)
			if !ok {
				t.Errorf("%s/%s: Coerce returned %T, want dbtype.Date", arm.arm, tc.name, out)
				continue
			}
			got := time.Time(d)
			if got.Format(time.DateOnly) != tc.want {
				t.Errorf("%s/%s: Coerce(%s) = %s, want %s",
					arm.arm, tc.name, tc.in.Format(time.RFC3339), got.Format(time.DateOnly), tc.want)
			}
			if h, m, sec := got.Clock(); h|m|sec != 0 {
				t.Errorf("%s/%s: a date reached the driver carrying %02d:%02d:%02d",
					arm.arm, tc.name, h, m, sec)
			}
		}
	}

	// The string arm parses with time.DateOnly, which is midnight by
	// construction; it is here so all three arms are named in one place.
	out, err := Coerce(schema.DateConstraint{}, "1969-07-20")
	if err != nil {
		t.Fatalf("string arm: Coerce: %v", err)
	}
	if got := time.Time(out.(dbtype.Date)); got.Format(time.DateOnly) != "1969-07-20" {
		t.Errorf("string arm: Coerce = %s, want 1969-07-20", got.Format(time.DateOnly))
	}
}
