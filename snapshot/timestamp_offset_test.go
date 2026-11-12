package snapshot_test

import (
	"bytes"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The reader's two value checks judge a Timestamp as the validator does: each
// reports one at a UTC offset hour of 24, at a key and at a property, and
// neither reports one at the widest offset the validator stores.
func TestLoad_ATimestampAtAnOffsetHourOfTwentyFourIsReported(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"offsets\"\n\ntype Sensor {\n\tobserved_at Timestamp primary\n\tseen Timestamp\n}\n", "offsets.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	const (
		key      = "2020-01-02T03:04:05+23:59"
		property = "2021-01-02T03:04:05+23:59"
	)
	g := graph.New(s)
	if r := g.Add(t.Context(), instancetest.Valid(t, s, "Sensor", map[string]any{"observed_at": key, "seen": property})); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	checks := map[string]struct {
		option snapshot.LoadOption
		code   diag.Code
	}{
		"WithRevalidation":     {snapshot.WithRevalidation(diag.Error), diag.E_CONSTRAINT_FAIL},
		"WithValueConformance": {snapshot.WithValueConformance(true), diag.W_SNAPSHOT_VALUE_NONCONFORMING},
	}
	for name, check := range checks {
		if _, res := snapshot.Load(t.Context(), data, s, check.option); res.HasCode(check.code) {
			t.Fatalf("control: %s reports the widest offset: %s", name, res)
		}
	}
	for position, stamp := range map[string]string{"a key": key, "a property": property} {
		if !bytes.Contains(data, []byte(stamp)) {
			t.Fatalf("fixture: the document does not hold %s", stamp)
		}
		wide := stamp[:len(stamp)-len("+23:59")] + "+24:00"
		doc := rehashDocument(t, bytes.ReplaceAll(data, []byte(stamp), []byte(wide)))
		for name, check := range checks {
			if _, res := snapshot.Load(t.Context(), doc, s, check.option); !res.HasCode(check.code) {
				t.Errorf("Load under %s of %s at %s: %s; want %v", name, wide, position, res, check.code)
			}
			if res := snapshot.Verify(t.Context(), doc, s, check.option); !res.HasCode(check.code) {
				t.Errorf("Verify under %s of %s at %s: %s; want %v", name, wide, position, res, check.code)
			}
		}
	}
}
