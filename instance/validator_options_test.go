package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
)

// WithIssueLimit caps the issues one instance stores, as snapshot.WithIssueLimit
// caps a load; 0 is unlimited and 100 the default.
func TestWithIssueLimit_CapsPerInstanceAndReportsTruncation(t *testing.T) {
	s := loadSrc(t, personOnly)
	props := manyUnknownFields(150)

	_, res := instance.NewValidator(s, instance.WithIssueLimit(10)).ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props})
	if res.Len() != 10 || !res.LimitReached() || res.DroppedCount() != 140 || res.SeverityCounts().Errors != 150 {
		t.Errorf("limit 10: len=%d limitReached=%v dropped=%d errors=%d", res.Len(), res.LimitReached(), res.DroppedCount(), res.SeverityCounts().Errors)
	}

	_, res = instance.NewValidator(s, instance.WithIssueLimit(0)).ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props})
	if res.Len() != 150 || res.LimitReached() {
		t.Errorf("limit 0: len=%d limitReached=%v", res.Len(), res.LimitReached())
	}

	_, res = instance.NewValidator(s).ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: props})
	if res.Len() != 100 {
		t.Errorf("default cap: len=%d, want 100", res.Len())
	}
}

// The composed-nesting bound is the .ys wire's number; changing it is a wire
// event, so the value is pinned here.
func TestMaxComposedDepth_IsTheWireBound(t *testing.T) {
	if instance.MaxComposedDepth != 32 {
		t.Errorf("MaxComposedDepth = %d, want 32", instance.MaxComposedDepth)
	}
}
