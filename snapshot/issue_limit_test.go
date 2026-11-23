package snapshot_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// revalDocumentWithFailures is a document of n Things, each violating the count
// bound, so re-validation reports at least one finding per Thing.
func revalDocumentWithFailures(t *testing.T, s schema.Schema, n int) []byte {
	t.Helper()
	insts := make([]instance.ValidInstance, 0, n)
	for i := range n {
		id := fmt.Sprintf("t%d", i)
		insts = append(insts, instancetest.Valid(t, s, "Thing", map[string]any{
			"id": id, "count": int64(5),
			"items": []any{map[string]any{"sku": "sku-" + id}},
		}))
	}
	return revalDocumentOf(t, s, insts, revalEdit{`"count":5`, `"count":99`})
}

// refusedLoad loads data with each finding revalidated at Error and returns the
// failures the refusal holds. Every finding is then a failure the error carries,
// so the load's issue limit caps it.
func refusedLoad(t *testing.T, s schema.Schema, data []byte, opts ...snapshot.Option) diag.Result {
	t.Helper()
	snap, err := snapshot.Unmarshal(t.Context(), data, s, append([]snapshot.Option{snapshot.WithRevalidation(diag.Error)}, opts...)...)
	if err == nil || !snap.IsZero() {
		t.Fatalf("a revalidation at Error of a failing document loaded it: err=%v", err)
	}
	return diagtest.Failures(t, err)
}

// unlimitedFindings is the oracle for the capped runs: the whole walk's count,
// taken with no limit, is what a capped run's stored + dropped must add up to.
func unlimitedFindings(t *testing.T, s schema.Schema, data []byte) int {
	t.Helper()
	res := refusedLoad(t, s, data, snapshot.WithIssueLimit(0))
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

	res := refusedLoad(t, s, data, snapshot.WithIssueLimit(3))
	if got := res.Len(); got != 3 {
		t.Errorf("stored issues = %d, want 3", got)
	}
	if !res.LimitReached() {
		t.Error("LimitReached() = false, want true")
	}
	if got, want := res.DroppedCount(), total-3; got != want {
		t.Errorf("DroppedCount() = %d, want %d (the walk must complete)", got, want)
	}
	if got := res.SeverityCounts().Errors; got != total {
		t.Errorf("SeverityCounts().Errors = %d, want %d (seen-based, not stored-based)", got, total)
	}
	if res.TruncationNote() == "" {
		t.Error("TruncationNote() is empty on a truncated result")
	}
}

// TestLoad_IssueLimitDefaultsTo100 pins parity with schema.Load's cap:
// a refusal from a load that passes no limit holds at most 100 failures and
// counts the rest.
func TestLoad_IssueLimitDefaultsTo100(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocumentWithFailures(t, s, 120)
	total := unlimitedFindings(t, s, data)
	if total <= 100 {
		t.Fatalf("fixture produced %d findings, want more than 100", total)
	}

	res := refusedLoad(t, s, data)
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
	res := refusedLoad(t, s, revalDocumentWithFailures(t, s, 120), snapshot.WithIssueLimit(0))
	if res.LimitReached() || res.DroppedCount() != 0 {
		t.Errorf("WithIssueLimit(0) truncated: limitReached=%v dropped=%d", res.LimitReached(), res.DroppedCount())
	}
	if res.Len() <= 100 {
		t.Errorf("stored issues = %d, want more than 100 under no limit", res.Len())
	}
}

// TestLoad_WarningsTakeNoIssueLimit pins that the limit caps failures alone: a
// revalidation below Error delivers every finding of the walk to WithWarnings
// whatever the limit, and the load succeeds.
func TestLoad_WarningsTakeNoIssueLimit(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocumentWithFailures(t, s, 120)
	total := unlimitedFindings(t, s, data)
	for _, limit := range []int{0, 1, 100} {
		delivered := 0
		_, err := snapshot.Unmarshal(t.Context(), data, s, snapshot.WithRevalidation(diag.Warning),
			snapshot.WithIssueLimit(limit), snapshot.WithWarnings(func(diag.Issue) { delivered++ }))
		if err != nil {
			t.Fatalf("limit %d: a revalidation at Warning refused the document: %v", limit, err)
		}
		if delivered != total {
			t.Errorf("limit %d: %d warnings delivered, want every one of the walk's %d", limit, delivered, total)
		}
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
// capped refusal keeps. One Thing violates count, state and code, which the
// validator raises in that declaration order while a result sorts them code
// first; the cap keeps the findings raised first. Its owner edge resolves and
// its items are valid, so those three are the load's only findings.
func TestLoad_WithIssueLimit_KeepsTheFindingsRaisedFirst(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalThreeFindings(t, s)

	if got, want := findingProperties(refusedLoad(t, s, data, snapshot.WithIssueLimit(0))), []string{"code", "count", "state"}; !slices.Equal(got, want) {
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
			res := refusedLoad(t, s, data, snapshot.WithIssueLimit(tc.limit))
			if got := findingProperties(res); !slices.Equal(got, tc.want) {
				t.Errorf("kept findings = %v, want %v", got, tc.want)
			}
			if !res.LimitReached() {
				t.Error("LimitReached() = false, want true")
			}
			if got, want := res.DroppedCount(), 3-tc.limit; got != want {
				t.Errorf("DroppedCount() = %d, want %d", got, want)
			}
			if got := res.SeverityCounts().Errors; got != 3 {
				t.Errorf("SeverityCounts().Errors = %d, want 3", got)
			}
		})
	}
}

// revalThreeFindings is a document of one Thing that violates count, state and
// code, and nothing else.
func revalThreeFindings(t *testing.T, s schema.Schema) []byte {
	t.Helper()
	owned := revalThing()
	owned["owner"] = map[string]any{"_target_id": "x1"}
	return revalDocument(t, s, owned,
		revalEdit{`"count":5`, `"count":99`},
		revalEdit{`"state":"on"`, `"state":"neither"`},
		revalEdit{`"code":"c-9"`, `"code":"bad"`},
	)
}

// TestLoad_WarningsArriveInTheOrderFound pins that WithWarnings receives each
// finding of a revalidation below Error in the order the validator raised it,
// before Unmarshal returns, under a limit smaller than the findings. The
// callback holds no lock, so a concurrent delivery fails under -race.
func TestLoad_WarningsArriveInTheOrderFound(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalThreeFindings(t, s)

	var order []string
	inCall := true
	_, err := snapshot.Unmarshal(t.Context(), data, s,
		snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(1),
		snapshot.WithWarnings(func(issue diag.Issue) {
			if !inCall {
				t.Error("a warning arrived after Unmarshal returned")
			}
			for _, d := range issue.Details() {
				if issue.Code() == diag.E_CONSTRAINT_FAIL && d.Key == diag.DetailKeyPropertyName {
					order = append(order, d.Value)
				}
			}
		}))
	inCall = false
	if err != nil {
		t.Fatalf("a revalidation at Warning refused the document: %v", err)
	}
	if want := []string{"count", "state", "code"}; !slices.Equal(order, want) {
		t.Errorf("warnings arrived as %v, want %v", order, want)
	}
}

// TestVerify_HonorsIssueLimit pins that the option reaches Verify through the
// same decoder Load uses.
func TestVerify_HonorsIssueLimit(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocumentWithFailures(t, s, 7)
	total := unlimitedFindings(t, s, data)

	err := snapshot.Verify(t.Context(), data, s, snapshot.WithRevalidation(diag.Error), snapshot.WithIssueLimit(2))
	if err == nil {
		t.Fatal("Verify accepted a document a revalidation at Error refuses")
	}
	res := diagtest.Failures(t, err)
	if got := res.Len(); got != 2 {
		t.Errorf("stored issues = %d, want 2", got)
	}
	if got, want := res.DroppedCount(), total-2; got != want {
		t.Errorf("DroppedCount() = %d, want %d", got, want)
	}
}

// revalItemsDocument is one Thing whose 101 composed Items each violate the sku
// pattern: 101 findings in one row, and one more for its unresolved required
// association.
func revalItemsDocument(t *testing.T, s schema.Schema) []byte {
	t.Helper()
	items := make([]any, 0, 101)
	for i := range 101 {
		items = append(items, map[string]any{"sku": fmt.Sprintf("sku-%d", i)})
	}
	return revalDocumentOf(t, s, []instance.ValidInstance{
		instancetest.Valid(t, s, "Thing", map[string]any{"id": "t1", "count": int64(5), "items": items}),
	}, revalEdit{`"sku":"sku-`, `"sku":"bad-`})
}

// TestLoad_RevalidationSharesTheLoadsIssueLimit pins that at Error a
// revalidation's failures take the load's limit and no other. Unlimited, every
// one of the 102 failures is stored; under a limit, the refusal counts what it
// dropped.
func TestLoad_RevalidationSharesTheLoadsIssueLimit(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalItemsDocument(t, s)

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
			res := refusedLoad(t, s, data, snapshot.WithIssueLimit(tt.limit))
			if res.Len() != tt.wantLen || res.LimitReached() != tt.wantReached || res.DroppedCount() != tt.wantDropped {
				t.Errorf("Len=%d LimitReached=%t DroppedCount=%d; want %d, %t, %d",
					res.Len(), res.LimitReached(), res.DroppedCount(), tt.wantLen, tt.wantReached, tt.wantDropped)
			}
			if got := res.SeverityCounts().Errors; got != 102 {
				t.Errorf("Errors = %d; want 102, every finding seen", got)
			}
		})
	}
}

// TestLoad_RevalidatorTakesNoCapBelowFailure pins that below failure severity
// no cap drops a finding of a row before it is delivered: each of the row's 102
// findings reaches WithWarnings at every limit, the row's own included.
func TestLoad_RevalidatorTakesNoCapBelowFailure(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalItemsDocument(t, s)
	for _, limit := range []int{0, 1, 50} {
		delivered := 0
		_, err := snapshot.Unmarshal(t.Context(), data, s,
			snapshot.WithRevalidation(diag.Warning), snapshot.WithIssueLimit(limit),
			snapshot.WithWarnings(func(diag.Issue) { delivered++ }))
		if err != nil {
			t.Fatalf("limit %d: a revalidation at Warning refused the document: %v", limit, err)
		}
		if delivered != 102 {
			t.Errorf("limit %d: %d warnings delivered, want 102", limit, delivered)
		}
	}
}
