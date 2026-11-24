package snapshot_test

import (
	"bytes"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// Revalidation judges a Timestamp as the validator does: it reports one at a
// UTC offset hour of 24, at a key and at a property, and not one at the widest
// offset the validator stores. The reader does not refuse such a key as
// respelled, since its kind stores no text for it.
func TestLoad_ATimestampAtAnOffsetHourOfTwentyFourIsReported(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), "schema \"offsets\"\n\ntype Sensor {\n\tobserved_at Timestamp primary\n\tseen Timestamp\n}\n", "offsets.yammm")
	if err != nil {
		t.Fatalf("load: %s", err)
	}
	const (
		key      = "2020-01-02T03:04:05+23:59"
		property = "2021-01-02T03:04:05+23:59"
	)
	g := graph.New(s)
	if err := g.Add(t.Context(), instancetest.Valid(t, s, "Sensor", map[string]any{"observed_at": key, "seen": property})); err != nil {
		t.Fatalf("add: %s", err)
	}
	data, readErr := snapshot.Marshal(t.Context(), g.Snapshot())
	if readErr != nil {
		t.Fatalf("marshal: %s", readErr)
	}
	revalidate := snapshot.WithRevalidation(diag.Error)
	if _, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(t.Context(), data, s, revalidate, snapshot.WithWarnings(warn))
	}); res.HasCode(diag.E_CONSTRAINT_FAIL) {
		t.Fatalf("control: revalidation reports the widest offset: %s", res)
	}
	for position, stamp := range map[string]string{"a key": key, "a property": property} {
		if !bytes.Contains(data, []byte(stamp)) {
			t.Fatalf("fixture: the document does not hold %s", stamp)
		}
		wide := stamp[:len(stamp)-len("+23:59")] + "+24:00"
		doc := rehashDocument(t, bytes.ReplaceAll(data, []byte(stamp), []byte(wide)))
		if _, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
			return snapshot.Unmarshal(t.Context(), doc, s, revalidate, snapshot.WithWarnings(warn))
		}); !res.HasCode(diag.E_CONSTRAINT_FAIL) {
			t.Errorf("Load of %s at %s: %s; want E_CONSTRAINT_FAIL", wide, position, res)
		}
		if res := diagtest.CaptureErr(t, func(warn func(diag.Issue)) error {
			return snapshot.Verify(t.Context(), doc, s, revalidate, snapshot.WithWarnings(warn))
		}); !res.HasCode(diag.E_CONSTRAINT_FAIL) {
			t.Errorf("Verify of %s at %s: %s; want E_CONSTRAINT_FAIL", wide, position, res)
		}
	}
}
