package snapshot_test

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// revalDocumentWithFailures marshals n bypass-built Things, each violating
// the count bound, so re-validation reports at least one finding per Thing.
func revalDocumentWithFailures(t *testing.T, s *schema.Schema, n int) []byte {
	t.Helper()
	thing, _ := s.Type("Thing")
	g := graph.New(s)
	for i := range n {
		id := fmt.Sprintf("t%d", i)
		vi := instancetest.VI(
			"Thing",
			instancetest.TypeID(thing.ID()),
			instancetest.PK(id),
			instancetest.Props(map[string]any{"id": id, "count": int64(99)}),
			instancetest.Composed(map[string]immutable.Value{
				"ITEMS": immutable.Wrap([]*instance.ValidInstance{revalItem(t, s, "sku-"+id)}),
			}),
		)
		if r := g.Add(t.Context(), vi); !r.OK() {
			t.Fatalf("Add %s: %s", id, r.String())
		}
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres.String())
	}
	return data
}

// unlimitedFindings is the oracle for the capped runs: the whole walk's count,
// taken with no limit, is what a capped run's stored + dropped must add up to.
func unlimitedFindings(t *testing.T, s *schema.Schema, data []byte) int {
	t.Helper()
	_, res := snapshot.Load(t.Context(), data, s,
		snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(0))
	if res.LimitReached() {
		t.Fatal("an unlimited load reports a reached limit")
	}
	return res.Len()
}

// TestLoad_WithIssueLimit_BoundsTheStoredIssuesNotTheWalk pins A-126's shape:
// the collector stops storing at the limit, the walk continues, and the
// dropped count is therefore exact — stored plus dropped equals the unlimited
// total. An early exit when the collector fills would undercount the drops.
func TestLoad_WithIssueLimit_BoundsTheStoredIssuesNotTheWalk(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocumentWithFailures(t, s, 7)
	total := unlimitedFindings(t, s, data)
	if total < 7 {
		t.Fatalf("fixture produced %d findings, want at least 7", total)
	}

	snap, res := snapshot.Load(t.Context(), data, s,
		snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(3))
	if snap == nil {
		t.Fatalf("Load refused a warnings-only document: %s", res)
	}
	if got := res.Len(); got != 3 {
		t.Errorf("stored issues = %d, want 3", got)
	}
	if !res.LimitReached() {
		t.Error("LimitReached() = false, want true")
	}
	if got, want := res.DroppedCount(), total-3; got != want {
		t.Errorf("DroppedCount() = %d, want %d (the walk must complete)", got, want)
	}
	if got := res.SeverityCounts().Warnings; got != total {
		t.Errorf("SeverityCounts().Warnings = %d, want %d (seen-based, not stored-based)", got, total)
	}
	if res.TruncationNote() == "" {
		t.Error("TruncationNote() is empty on a truncated result")
	}
}

// TestLoad_IssueLimitDefaultsTo100 pins parity with schema.WithIssueLimit:
// an existing WithRevalidation caller that passes no limit now stores at
// most 100 issues and reports the rest.
func TestLoad_IssueLimitDefaultsTo100(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocumentWithFailures(t, s, 120)
	total := unlimitedFindings(t, s, data)
	if total <= 100 {
		t.Fatalf("fixture produced %d findings, want more than 100", total)
	}

	_, res := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Warning))
	if got := res.Len(); got != 100 {
		t.Errorf("stored issues without the option = %d, want the default 100", got)
	}
	if got, want := res.DroppedCount(), total-100; got != want {
		t.Errorf("DroppedCount() = %d, want %d", got, want)
	}
}

// TestLoad_WithIssueLimitZero_IsUnlimited pins the sibling's contract: 0 is
// diag.NoLimit, not a cap of zero.
func TestLoad_WithIssueLimitZero_IsUnlimited(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocumentWithFailures(t, s, 120)

	_, res := snapshot.Load(t.Context(), data, s,
		snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(0))
	if res.LimitReached() || res.DroppedCount() != 0 {
		t.Errorf("WithIssueLimit(0) truncated: limitReached=%v dropped=%d", res.LimitReached(), res.DroppedCount())
	}
	if res.Len() <= 100 {
		t.Errorf("stored issues = %d, want more than 100 under no limit", res.Len())
	}
}

// TestVerify_HonorsIssueLimit pins that the option reaches Verify through the
// same decoder Load uses.
func TestVerify_HonorsIssueLimit(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocumentWithFailures(t, s, 7)
	total := unlimitedFindings(t, s, data)

	res := snapshot.Verify(t.Context(), data, s,
		snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(2))
	if got := res.Len(); got != 2 {
		t.Errorf("stored issues = %d, want 2", got)
	}
	if got, want := res.DroppedCount(), total-2; got != want {
		t.Errorf("DroppedCount() = %d, want %d", got, want)
	}
}
