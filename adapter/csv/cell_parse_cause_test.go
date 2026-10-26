package csv

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// A cell that does not parse is refused by its quoted head and the parse's
// cause alone: the strconv and time text, which repeats the whole cell, is
// left out, and a number's cause is still its strconv sentinel.
func TestCoerceStringValue_ARefusedCellNamesItsCauseAlone(t *testing.T) {
	t.Parallel()
	a := New()
	long := strings.Repeat("z", 100)
	rows := []struct {
		name, input string
		c           schema.Constraint
		want        string
		sentinel    error
	}{
		{"boolean", "maybe", schema.NewBooleanConstraint(), `cannot parse "maybe" as Boolean: invalid syntax`, strconv.ErrSyntax},
		{"integer out of range", "99999999999999999999", schema.NewIntegerConstraint(), `cannot parse "99999999999999999999" as Integer: value out of range`, strconv.ErrRange},
		{"float out of range", "1e400", schema.NewFloatConstraint(), `cannot parse "1e400" as Float: value out of range`, strconv.ErrRange},
		{"date out of range", "2026-13-01", schema.NewDateConstraint(), `cannot parse "2026-13-01" as Date (expected YYYY-MM-DD): month out of range`, nil},
		{"timestamp", "xyz", schema.NewTimestampConstraint(), `cannot parse "xyz" as Timestamp (expected RFC 3339): "xyz" does not match "2006"`, nil},
		{"date with extra text", "2026-01-01" + long, schema.NewDateConstraint(), `cannot parse "` + ("2026-01-01" + long)[:32] + `"… (110 bytes) as Date (expected YYYY-MM-DD): extra text: "` + long[:32] + `"… (100 bytes)`, nil},
		{"timestamp with extra text", "2026-01-01T00:00:00Z" + long, schema.NewTimestampConstraint(), `cannot parse "` + ("2026-01-01T00:00:00Z" + long)[:32] + `"… (120 bytes) as Timestamp (expected RFC 3339): extra text: "` + long[:32] + `"… (100 bytes)`, nil},
		{"long timestamp", long, schema.NewTimestampConstraintFormatted("2006-01-02"), `cannot parse "` + long[:32] + `"… (100 bytes) as Timestamp (expected layout 2006-01-02): "` + long[:32] + `"… (100 bytes) does not match "2006"`, nil},
	}
	for _, r := range rows {
		_, err := a.coerceStringValue(r.input, r.c)
		if err == nil || err.Error() != r.want {
			t.Errorf("%s: err = %v\nwant %s", r.name, err, r.want)
			continue
		}
		if r.sentinel != nil && !errors.Is(err, r.sentinel) {
			t.Errorf("%s: %v does not wrap %v", r.name, err, r.sentinel)
		}
	}
}
