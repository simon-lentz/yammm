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

func TestRecordHandler_CopySemantics(t *testing.T) {
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
}

// flattenAttrs walks a record's attributes, flattening group nesting into
// dotted keys, so assertions read the same regardless of group structure.
func flattenAttrs(r slog.Record) map[string]string {
	out := make(map[string]string)
	var walk func(prefix string, a slog.Attr)
	walk = func(prefix string, a slog.Attr) {
		key := a.Key
		if prefix != "" {
			key = prefix + "." + a.Key
		}
		if a.Value.Kind() == slog.KindGroup {
			for _, ga := range a.Value.Group() {
				walk(key, ga)
			}
			return
		}
		out[key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		walk("", a)
		return true
	})
	return out
}

func TestRecordHandler_CapturesBoundAttrsAndGroups(t *testing.T) {
	h := NewRecordHandler(slog.LevelInfo)

	// Logger.With binds attrs on a derived handler; records must land in
	// the same store, carrying the bound attr before the call-site attr.
	component := slog.New(h).With(slog.String("component", "server"))
	component.Info("ready", slog.String("k", "v"))

	// Group scoping qualifies both bind-time and call-site attrs.
	grouped := slog.New(h).WithGroup("req").With(slog.String("id", "42"))
	grouped.Info("handled", slog.String("path", "/x"))

	recs := h.Records()
	if len(recs) != 2 {
		t.Fatalf("captured %d records, want 2", len(recs))
	}

	flat0 := flattenAttrs(recs[0])
	if flat0["component"] != "server" {
		t.Errorf("bound attr not captured: %v", flat0)
	}
	if flat0["k"] != "v" {
		t.Errorf("call-site attr lost: %v", flat0)
	}
	var firstKey string
	recs[0].Attrs(func(a slog.Attr) bool {
		firstKey = a.Key
		return false
	})
	if firstKey != "component" {
		t.Errorf("bound attrs must precede call-site attrs, first key = %q", firstKey)
	}

	flat1 := flattenAttrs(recs[1])
	if flat1["req.id"] != "42" {
		t.Errorf("group-qualified bound attr not captured: %v", flat1)
	}
	if flat1["req.path"] != "/x" {
		t.Errorf("group-qualified call-site attr not captured: %v", flat1)
	}
}

func TestRecordScanners(t *testing.T) {
	h := NewRecordHandler(slog.LevelInfo)
	slog.New(h).WithGroup("req").Info("handled", slog.String("id", "42"))
	recs := h.Records()

	if !HasAttr(recs, "id", "42") {
		t.Error("HasAttr should match attributes nested in groups")
	}
	if HasAttr(recs, "id", "7") {
		t.Error("HasAttr should not match a different value")
	}
	if !HasMessage(recs, "handled") {
		t.Error("HasMessage should match the record message")
	}
	if CountLevel(recs, slog.LevelInfo) != 1 {
		t.Error("CountLevel should count records at the level")
	}
	if CountLevel(recs, slog.LevelWarn) != 0 {
		t.Error("CountLevel should ignore other levels")
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
