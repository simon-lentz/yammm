package eval_test

import (
	"errors"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/schema"
)

// A time.Time is judged as the text it is stored as, so a time whose text the
// same check refuses is refused, as that text is: a year outside 0 to 9999,
// and a zone with no abbreviation under a layout that writes one.
func TestCheckValue_ATimeWhoseStoredTextDoesNotReadBackIsRefused(t *testing.T) {
	t.Parallel()
	far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(-1, 1, 1, 0, 0, 0, 0, time.UTC)
	unnamed := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("", 10800))
	abbreviated := schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05 MST")
	for _, c := range []struct {
		name string
		val  time.Time
		c    schema.Constraint
		want string
	}{
		{"past year 9999 at a Timestamp", far, schema.NewTimestampConstraint(), `invalid timestamp format: "10000-01-01T00:00:00Z"`},
		{"before year 0 at a Timestamp", before, schema.NewTimestampConstraint(), `invalid timestamp format: "-0001-01-01T00:00:00Z"`},
		{"past year 9999 at a Date", far, schema.NewDateConstraint(), `invalid date format: "10000-01-01" (expected YYYY-MM-DD)`},
		{"an unnamed zone under an abbreviation layout", unnamed, abbreviated, `invalid timestamp format: "2024-01-02 03:04:05 +0300" (expected 2006-01-02 15:04:05 MST)`},
	} {
		err := eval.CheckValue(c.val, c.c)
		var checkErr *eval.CheckError
		if err == nil || !errors.As(err, &checkErr) || checkErr.Kind != eval.KindConstraintFail || err.Error() != c.want {
			t.Errorf("%s: CheckValue = %v; want the constraint failure %q", c.name, err, c.want)
		}
		if _, err := eval.CoerceValue(c.val, c.c); err == nil {
			t.Errorf("%s: CoerceValue stored a value its check refuses", c.name)
		}
	}
	for _, c := range []struct {
		name string
		val  time.Time
		c    schema.Constraint
	}{
		{"year 9999 at a Timestamp", time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC), schema.NewTimestampConstraint()},
		{"year 0 at a Date", time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), schema.NewDateConstraint()},
		{"a named zone under an abbreviation layout", time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("CEST", 7200)), abbreviated},
		{"an unnamed zone at a Timestamp", unnamed, schema.NewTimestampConstraint()},
	} {
		if err := eval.CheckValue(c.val, c.c); err != nil {
			t.Errorf("%s: CheckValue = %v; want nil", c.name, err)
		}
	}
}
