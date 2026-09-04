package diag_test

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
)

// MergeFunc is Merge with a transform on each stored issue: the receiver
// carries res's severity counts and truncation facts exactly as Merge does,
// and stores fn(issue) in place of issue.
func TestCollector_MergeFunc_TransformsEachIssueAndCarriesTruncation(t *testing.T) {
	capped := diag.NewCollector(2)
	for _, msg := range []string{"a", "b", "c"} {
		capped.Collect(diag.NewIssue(diag.Error, diag.E_UNKNOWN_FIELD, msg).Build())
	}
	res := capped.Result()
	if !res.LimitReached() || res.DroppedCount() != 1 {
		t.Fatalf("the source result is not truncated as expected: %s", res)
	}

	batch := diag.NewCollectorUnlimited()
	batch.MergeFunc(res, func(is diag.Issue) diag.Issue {
		return diag.FromIssue(is).WithDetail(diag.DetailKeyInstanceIndex, "7").Build()
	})
	out := batch.Result()
	if !out.LimitReached() || out.DroppedCount() != 1 || out.SeverityCounts().Errors != 3 || out.Len() != 2 {
		t.Errorf("limitReached=%v dropped=%d errors=%d len=%d", out.LimitReached(), out.DroppedCount(), out.SeverityCounts().Errors, out.Len())
	}
	for is := range out.Issues() {
		var stamped bool
		for _, d := range is.Details() {
			stamped = stamped || (d.Key == diag.DetailKeyInstanceIndex && d.Value == "7")
		}
		if !stamped {
			t.Errorf("not stamped: %v", is)
		}
	}
}

// fn runs outside the collector's lock: a transform that reads or writes the
// receiver — the collector it is merging into — must not deadlock. Every
// transformed issue is built into a local slice first and stored after.
func TestCollector_MergeFunc_AppliesTheTransformOutsideTheLock(t *testing.T) {
	src := diag.NewCollectorUnlimited()
	src.Collect(diag.NewIssue(diag.Error, diag.E_UNKNOWN_FIELD, "a").Build())
	batch := diag.NewCollectorUnlimited()

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		batch.MergeFunc(src.Result(), func(is diag.Issue) diag.Issue {
			_ = batch.Result() // takes the receiver's lock
			return diag.FromIssue(is).WithDetail(diag.DetailKeyInstanceIndex, "1").Build()
		})
		close(done)
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("MergeFunc applied fn under its own lock: reading the receiver from fn deadlocked")
	}
	wg.Wait()
	if got := batch.Result().Len(); got != 1 {
		t.Errorf("Len = %d, want 1", got)
	}
}

// The severity counts are folded from res before fn runs, so fn must keep
// each issue's severity and code. Only a from-scratch NewIssue can change
// them — the builder has no setter for either — and doing so panics, naming
// MergeFunc.
func TestCollector_MergeFunc_PanicsWhenTheTransformChangesSeverityOrCode(t *testing.T) {
	src := diag.NewCollectorUnlimited()
	src.Collect(diag.NewIssue(diag.Error, diag.E_UNKNOWN_FIELD, "a").Build())
	for name, fn := range map[string]func(diag.Issue) diag.Issue{
		"severity": func(diag.Issue) diag.Issue { return diag.NewIssue(diag.Warning, diag.E_UNKNOWN_FIELD, "a").Build() },
		"code":     func(diag.Issue) diag.Issue { return diag.NewIssue(diag.Error, diag.E_TYPE_MISMATCH, "a").Build() },
	} {
		t.Run(name, func(t *testing.T) {
			batch := diag.NewCollectorUnlimited()
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("no panic")
				}
				if msg, _ := r.(string); !strings.Contains(msg, "MergeFunc") {
					t.Errorf("panic %v does not name MergeFunc", r)
				}
			}()
			batch.MergeFunc(src.Result(), fn)
		})
	}
}
