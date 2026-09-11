package snapshot

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestMergeResults_KeepsTruncation holds what UpdateMetadataOrReMarshal
// returns when its fallback fails. Both failing fallback legs return
// mergeResults' result as it is, so a truncated input must stay truncated,
// with every issue it saw still counted.
func TestMergeResults_KeepsTruncation(t *testing.T) {
	t.Parallel()

	codes := []diag.Code{diag.E_SNAPSHOT_MALFORMED, diag.E_SNAPSHOT_IO, diag.E_SNAPSHOT_UNKNOWN_TYPE}
	c := diag.NewCollector(2)
	for _, code := range codes {
		c.Collect(diag.NewIssue(diag.Error, code, "probe").Build())
	}
	truncated := c.Result()
	if !truncated.LimitReached() || truncated.DroppedCount() != 1 {
		t.Fatalf("the fixture is not truncated: LimitReached %v, DroppedCount %d", truncated.LimitReached(), truncated.DroppedCount())
	}
	w := diag.NewCollector(0)
	w.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_VALUE_DROPPED, "probe").Build())

	merged := mergeResults(truncated, w.Result())

	everyCode := true
	for _, code := range codes {
		if merged.CodeCounts(diag.Error)[code] != 1 {
			everyCode = false
		}
	}

	rows := []struct {
		name   string
		ok     bool
		detail string
	}{
		{"the merged result is still truncated", merged.LimitReached(), fmt.Sprintf("LimitReached %v", merged.LimitReached())},
		{"the dropped issue is still counted", merged.DroppedCount() == 1, fmt.Sprintf("DroppedCount %d, want 1", merged.DroppedCount())},
		{
			"every error the truncated result saw is still counted", merged.SeverityCounts().Errors == len(codes),
			fmt.Sprintf("%d errors, want %d", merged.SeverityCounts().Errors, len(codes)),
		},
		{"every code the truncated result saw is still counted", everyCode, fmt.Sprintf("error codes %v, want one each of %v", merged.CodeCounts(diag.Error), codes)},
		{"the other result's issue is kept", merged.HasCode(diag.W_SNAPSHOT_VALUE_DROPPED), "the warning is gone"},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if !row.ok {
				t.Error(row.detail)
			}
		})
	}
}
