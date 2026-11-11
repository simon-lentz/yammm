package neo4j

import (
	"testing"
	"time"

	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"
	"github.com/simon-lentz/yammm/schema"
)

// A Date carries no time of day. dbtype.Date holds a whole time.Time, and a
// driver before v6.4.0 derived its day number by a division that truncated
// toward zero, so a pre-1970 instant with a time of day landed a day late —
// measured against a live server as 1969-07-20T18:00:00Z stored as 1969-07-21.
//
// It drives [CoerceParams], not calendarDate, and covers all three forms a
// caller hands a Date in: the rule holds only where it is wired in.
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
	forms := []struct {
		form string
		wrap func(time.Time) any
	}{
		{"time.Time", func(t time.Time) any { return t }},
		{"dbtype.Date", func(t time.Time) any { return dbtype.Date(t) }},
		{"string", func(t time.Time) any { return t.Format(time.DateOnly) }},
	}

	types := ParamTypes{"d": schema.NewDateConstraint()}
	for _, form := range forms {
		for _, tc := range instants {
			out, err := CoerceParams(map[string]any{"d": form.wrap(tc.in)}, types)
			if err != nil {
				t.Errorf("%s/%s: CoerceParams: %v", form.form, tc.name, err)
				continue
			}
			d, ok := out["d"].(dbtype.Date)
			if !ok {
				t.Errorf("%s/%s: CoerceParams returned %T, want dbtype.Date", form.form, tc.name, out["d"])
				continue
			}
			got := time.Time(d)
			if got.Format(time.DateOnly) != tc.want {
				t.Errorf("%s/%s: CoerceParams(%s) = %s, want %s",
					form.form, tc.name, tc.in.Format(time.RFC3339), got.Format(time.DateOnly), tc.want)
			}
			if h, m, sec := got.Clock(); h|m|sec != 0 || got.Location() != time.UTC {
				t.Errorf("%s/%s: a date reached the driver carrying %02d:%02d:%02d in %q",
					form.form, tc.name, h, m, sec, got.Location())
			}
		}
	}
}
