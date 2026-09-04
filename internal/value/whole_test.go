package value_test

import (
	"math"
	"testing"

	"github.com/simon-lentz/yammm/internal/value"
)

// IsWholeNumber answers only what its name says; GetInt64FromFloat adds the
// int64 range. A whole float outside int64 is whole, and does not convert.
func TestIsWholeNumber_AnswersOnlyWholeness(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		f     float64
		whole bool
		fits  bool
	}{
		{3, true, true},
		{3.5, false, false},
		{math.Copysign(0, -1), true, true},
		{1e19, true, false},
		{-1e19, true, false},
		{1e300, true, false},
		{float64(math.MaxInt64), true, false}, // rounds up to 2^63
		{-float64(1 << 63), true, true},
		{math.NaN(), false, false},
		{math.Inf(1), false, false},
	} {
		if got := value.IsWholeNumber(tc.f); got != tc.whole {
			t.Errorf("IsWholeNumber(%g) = %v, want %v", tc.f, got, tc.whole)
		}
		if _, got := value.GetInt64FromFloat(tc.f); got != tc.fits {
			t.Errorf("GetInt64FromFloat(%g) ok = %v, want %v", tc.f, got, tc.fits)
		}
	}
}
