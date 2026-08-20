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
