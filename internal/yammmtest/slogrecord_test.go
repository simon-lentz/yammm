package yammmtest

import (
	"log/slog"
	"sync"
	"testing"
)

func TestRecordHandler_CapturesAtOrAboveLevel(t *testing.T) {
	h := NewRecordHandler(slog.LevelWarn)
	logger := slog.New(h)

	logger.Debug("dropped")
	logger.Info("dropped too")
	logger.Warn("kept", slog.String("k", "v"))
	logger.Error("kept too")

	recs := h.Records()
	if len(recs) != 2 {
		t.Fatalf("captured %d records, want 2", len(recs))
	}
	if recs[0].Message != "kept" || recs[0].Level != slog.LevelWarn {
		t.Errorf("first record = %q/%v", recs[0].Message, recs[0].Level)
	}

	var sawAttr bool
	recs[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "k" && a.Value.String() == "v" {
			sawAttr = true
		}
		return true
	})
	if !sawAttr {
		t.Error("call-site attr not captured in record")
	}
}

func TestRecordHandler_ClearAndCopySemantics(t *testing.T) {
	h := NewRecordHandler(slog.LevelInfo)
	logger := slog.New(h)

	logger.Info("one")
	snapshot := h.Records()
	logger.Info("two")

	if len(snapshot) != 1 {
		t.Fatalf("Records() must be a point-in-time copy, got len %d", len(snapshot))
	}
	if len(h.Records()) != 2 {
		t.Fatalf("handler should now hold 2 records")
	}

	h.Clear()
	if len(h.Records()) != 0 {
		t.Error("Clear should discard captured records")
	}
}

func TestRecordHandler_ConcurrentHandle(t *testing.T) {
	h := NewRecordHandler(slog.LevelInfo)
	logger := slog.New(h)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 25 {
				logger.Info("concurrent")
			}
		})
	}
	wg.Wait()

	if got := len(h.Records()); got != 200 {
		t.Errorf("captured %d records, want 200", got)
	}
}
