package instance_test

import (
	"log/slog"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// WithIssueLimit caps the issues one instance stores, as schema.WithIssueLimit
// and snapshot.WithIssueLimit cap a load; 0 is unlimited and 100 the default.
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

// WithLogger receives a debug record when a property name is normalized.
func TestWithLogger_ReportsNormalization(t *testing.T) {
	h := yammmtest.NewRecordHandler(slog.LevelDebug)
	s := loadSrc(t, `schema "p"

type Person {
    id String primary
    fullName String
}
`)
	_, res := instance.NewValidator(s, instance.WithLogger(slog.New(h))).ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "p", "fullname": "x"}})
	if !res.OK() {
		t.Fatal(res)
	}
	records := h.Records()
	if !yammmtest.HasMessage(records, "property name normalized") || !yammmtest.HasAttr(records, "resolved", "fullName") {
		t.Errorf("no normalization record: %v", records)
	}
}

// The composed-nesting bound is the .ys wire's number; changing it is a wire
// event, so the value is pinned here.
func TestMaxComposedDepth_IsTheWireBound(t *testing.T) {
	if instance.MaxComposedDepth != 32 {
		t.Errorf("MaxComposedDepth = %d, want 32", instance.MaxComposedDepth)
	}
}
