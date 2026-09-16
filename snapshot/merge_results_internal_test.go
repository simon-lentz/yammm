package snapshot

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestMergeResults_KeepsTruncation holds what UpdateMetadataOrReMarshal
// returns when its fallback fails. Both failing fallback legs return
// mergeResults' result as it is, and either argument can arrive truncated: the
// second is Load's or Marshal's result, capped at an issue limit. A truncated
// input must stay truncated, with every issue it saw still counted.
func TestMergeResults_KeepsTruncation(t *testing.T) {
	t.Parallel()

	firstCodes := []diag.Code{diag.E_SNAPSHOT_MALFORMED, diag.E_SNAPSHOT_IO, diag.E_SNAPSHOT_UNKNOWN_TYPE}
	secondCodes := []diag.Code{diag.E_SNAPSHOT_TYPE_MISMATCH, diag.E_SNAPSHOT_DANGLING_REFERENCE, diag.E_SNAPSHOT_INVALID_ROOT}

	cases := []struct {
		name                    string
		firstLimit, secondLimit int
		wantDropped             int
	}{
		{"the first result truncated", 2, diag.NoLimit, 1},
		{"the second result truncated", diag.NoLimit, 1, 2},
		{"both results truncated", 2, 1, 3},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			first := collectErrors(firstCodes, tc.firstLimit)
			second := collectErrors(secondCodes, tc.secondLimit)
			if got := first.DroppedCount() + second.DroppedCount(); got != tc.wantDropped {
				t.Fatalf("the inputs drop %d issues, want %d", got, tc.wantDropped)
			}

			merged := mergeResults(first, second)

			if !merged.LimitReached() {
				t.Error("the merged result is not truncated")
			}
			if got := merged.DroppedCount(); got != tc.wantDropped {
				t.Errorf("DroppedCount() = %d, want %d", got, tc.wantDropped)
			}
			if got, want := merged.SeverityCounts().Errors, len(firstCodes)+len(secondCodes); got != want {
				t.Errorf("the merged result counts %d errors, want %d", got, want)
			}
			counts := merged.CodeCounts(diag.Error)
			for _, code := range slices.Concat(firstCodes, secondCodes) {
				if counts[code] != 1 {
					t.Errorf("the merged result counts %s %d times, want 1", code, counts[code])
				}
			}
			if got, want := merged.Len(), first.Len()+second.Len(); got != want {
				t.Errorf("the merged result keeps %d issues, want every surviving issue of both: %d", got, want)
			}
		})
	}
}

// collectErrors returns the result of collecting one Error per code into a
// collector capped at limit.
func collectErrors(codes []diag.Code, limit int) diag.Result {
	c := diag.NewCollector(limit)
	for _, code := range codes {
		c.Collect(diag.NewIssue(diag.Error, code, "probe").Build())
	}
	return c.Result()
}
