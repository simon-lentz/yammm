package value_test

import (
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// One Timestamp text reads as one instant in one location, and stores as one
// text, whatever zone the host is in. time.Parse reads an offset the host's
// zone holds into time.Local, so the location it gives follows the host.
//
// Not parallel: it writes a package-level variable of the standard library.
func TestParseTimestamp_TheHostsZoneDecidesNothing(t *testing.T) {
	restore := time.Local
	t.Cleanup(func() { time.Local = restore })

	plain := schema.NewTimestampConstraint()
	both := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05 -0700 MST")
	offset := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05 -0700")
	wall := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05")
	hosts := []*time.Location{
		time.UTC,
		time.FixedZone("EDT", -4*60*60),
		time.FixedZone("CEST", 2*60*60),
	}
	for _, tc := range []struct {
		text string
		c    schema.Constraint
	}{
		{"2026-08-19T12:00:00Z", plain},
		{"2026-08-19T12:00:00-04:00", plain},
		{"2026-08-19T12:00:00+02:00", plain},
		{"2026-08-19 12:00:00 -0400 EDT", both},
		{"2026-08-19 12:00:00 +0200 CEST", both},
		{"2026-08-19 12:00:00 +0000 UTC", both},
		{"2026-08-19 12:00:00 +0000 GMT", both},
		{"2026-08-19 12:00:00 -0400", offset},
		{"2026-08-19 12:00:00 +0200", offset},
		{"2026-08-19 12:00:00", wall},
	} {
		type reading struct {
			instant  time.Time
			location string
			zone     string
			offset   int
			stored   any
		}
		var first reading
		for i, host := range hosts {
			time.Local = host
			at, ok := value.ParseTimestamp(tc.text, tc.c)
			if !ok {
				t.Fatalf("%q on a host in %s: not read", tc.text, host)
			}
			stored, err := value.Canonical(tc.text, tc.c)
			if err != nil {
				t.Fatalf("%q on a host in %s: Canonical: %v", tc.text, host, err)
			}
			zone, offset := at.Zone()
			got := reading{at, at.Location().String(), zone, offset, stored}
			if i == 0 {
				first = got
				continue
			}
			if !got.instant.Equal(first.instant) || got.location != first.location || got.zone != first.zone || got.offset != first.offset || got.stored != first.stored {
				t.Errorf("%q reads as %s in %q (zone %q at %d, stored %v) on a host in %s, and as %s in %q (zone %q at %d, stored %v) on a host in %s",
					tc.text, got.instant, got.location, got.zone, got.offset, got.stored, host,
					first.instant, first.location, first.zone, first.offset, first.stored, hosts[0])
			}
		}
	}
}
