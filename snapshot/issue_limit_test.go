package snapshot_test

import (
	"fmt"
	"slices"
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

// TestLoad_WithIssueLimit_BoundsTheStoredIssuesNotTheWalk pins the limit's shape:
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

// findingProperties lists the property each E_CONSTRAINT_FAIL finding in res
// names, in res's own order.
func findingProperties(res diag.Result) []string {
	var props []string
	for issue := range res.Issues() {
		if issue.Code() != diag.E_CONSTRAINT_FAIL {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyPropertyName {
				props = append(props, d.Value)
			}
		}
	}
	return props
}

// TestLoad_WithIssueLimit_KeepsTheFindingsRaisedFirst pins which findings a
// capped revalidating load keeps. One Thing violates count, state and code,
// which the validator raises in that declaration order while a result sorts
// them code first; the cap keeps the findings raised first. Its owner edge
// resolves and its items are valid, so those three are the load's only findings.
func TestLoad_WithIssueLimit_KeepsTheFindingsRaisedFirst(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocument(t, s,
		instancetest.Props(map[string]any{"id": "t1", "count": int64(99), "state": "neither", "code": "bad"}),
		instancetest.Composed(map[string]immutable.Value{
			"ITEMS": immutable.Wrap([]*instance.ValidInstance{revalItem(t, s, "sku-1")}),
		}),
		instancetest.Edges(map[string]*instance.ValidEdgeData{
			"OWNER": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{"x1"}), immutable.Properties{}),
			}),
		}),
	)

	_, all := snapshot.Load(t.Context(), data, s,
		snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(0))
	if got, want := findingProperties(all), []string{"code", "count", "state"}; !slices.Equal(got, want) {
		t.Fatalf("unlimited findings = %v, want %v", got, want)
	}

	cases := []struct {
		limit int
		want  []string
	}{
		{limit: 1, want: []string{"count"}},
		{limit: 2, want: []string{"count", "state"}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("limit_%d", tc.limit), func(t *testing.T) {
			t.Parallel()
			_, res := snapshot.Load(t.Context(), data, s,
				snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(tc.limit))
			if got := findingProperties(res); !slices.Equal(got, tc.want) {
				t.Errorf("kept findings = %v, want %v", got, tc.want)
			}
			if !res.LimitReached() {
				t.Error("LimitReached() = false, want true")
			}
			if got, want := res.DroppedCount(), 3-tc.limit; got != want {
				t.Errorf("DroppedCount() = %d, want %d", got, want)
			}
			if got := res.SeverityCounts().Warnings; got != 3 {
				t.Errorf("SeverityCounts().Warnings = %d, want 3", got)
			}
		})
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

// TestLoad_RevalidationSharesTheLoadsIssueLimit pins that the revalidator caps
// a row at the load's limit and no other: one Thing whose 101 composed Items
// each violate the sku pattern draws 101 findings, and the load one warning
// for its unresolved required association. Unlimited, every one is stored;
// under a limit, the load reports what it dropped.
func TestLoad_RevalidationSharesTheLoadsIssueLimit(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	thing, _ := s.Type("Thing")
	item, _ := s.Type("Item")
	g := graph.New(s)
	items := make([]*instance.ValidInstance, 0, 101)
	for i := range 101 {
		items = append(items, instancetest.VI("Item", instancetest.TypeID(item.ID()),
			instancetest.Props(map[string]any{"sku": fmt.Sprintf("bad-%d", i)})))
	}
	vi := instancetest.VI("Thing", instancetest.TypeID(thing.ID()), instancetest.PK("t1"),
		instancetest.Props(map[string]any{"id": "t1", "count": int64(5)}),
		instancetest.Composed(map[string]immutable.Value{"ITEMS": immutable.Wrap(items)}))
	if r := g.Add(t.Context(), vi); !r.OK() {
		t.Fatalf("Add: %s", r)
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres)
	}

	tests := []struct {
		name        string
		limit       int
		wantLen     int
		wantReached bool
		wantDropped int
	}{
		{name: "unlimited", limit: 0, wantLen: 102, wantReached: false, wantDropped: 0},
		{name: "limit 50", limit: 50, wantLen: 50, wantReached: true, wantDropped: 52},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, res := snapshot.Load(t.Context(), data, s,
				snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(tt.limit))
			if res.Len() != tt.wantLen || res.LimitReached() != tt.wantReached || res.DroppedCount() != tt.wantDropped {
				t.Errorf("Len=%d LimitReached=%t DroppedCount=%d; want %d, %t, %d",
					res.Len(), res.LimitReached(), res.DroppedCount(), tt.wantLen, tt.wantReached, tt.wantDropped)
			}
			if got := res.SeverityCounts().Warnings; got != 102 {
				t.Errorf("Warnings = %d; want 102, every finding seen", got)
			}
		})
	}
}
