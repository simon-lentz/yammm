package diag_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

func TestRouteWarnings_DeliversInArrivalOrderAndHoldsFailuresAlone(t *testing.T) {
	t.Parallel()
	// The merged result's warnings arrive w5 then w6 and sort w6 first: the
	// route receives them as they arrived.
	merged := diag.NewCollector(diag.NoLimit)
	merged.Collect(issueAt(diag.Warning, testCodeWarning, 20, "w5"))
	merged.Collect(issueAt(diag.Error, diag.E_SYNTAX, 21, "e3"))
	merged.Collect(issueAt(diag.Hint, testCodeWarning, 2, "w6"))

	c := diag.NewCollector(diag.NoLimit)
	var got []string
	c.RouteWarnings(func(issue diag.Issue) {
		// The route runs outside the collector's lock: a read here would
		// otherwise deadlock.
		_ = c.Result()
		got = append(got, issue.Message())
	})

	c.Collect(issueAt(diag.Warning, testCodeWarning, 9, "w1"))
	if !slices.Equal(got, []string{"w1"}) {
		t.Fatalf("after Collect returned, the route holds %q; want [w1]", got)
	}
	c.Collect(issueAt(diag.Error, diag.E_SYNTAX, 8, "e1"))
	c.Collect(issueAt(diag.Info, testCodeWarning, 7, "w2"))
	c.CollectAll([]diag.Issue{
		issueAt(diag.Warning, testCodeWarning, 6, "w3"),
		issueAt(diag.Fatal, diag.E_INTERNAL, 5, "f1"),
		issueAt(diag.Warning, testCodeWarning, 4, "w4"),
	})
	c.Merge(merged.Result())

	if want := []string{"w1", "w2", "w3", "w4", "w5", "w6"}; !slices.Equal(got, want) {
		t.Errorf("route received %q; want %q", got, want)
	}
	res := c.Result()
	if want := (diag.SeverityCounts{Fatal: 1, Errors: 2}); res.SeverityCounts() != want {
		t.Errorf("SeverityCounts() = %+v; want %+v", res.SeverityCounts(), want)
	}
	if got := messages(slices.Collect(res.Issues())); !slices.Equal(got, []string{"f1", "e1", "e3"}) {
		t.Errorf("stored = %q; want [f1 e1 e3]", got)
	}
	if !errors.Is(c.Err(), diag.E_INTERNAL) {
		t.Errorf("Err() = %v; want it to hold E_INTERNAL", c.Err())
	}
}

func TestRouteWarnings_NilDiscards(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(diag.NoLimit)
	c.RouteWarnings(nil)
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "dropped").Build())
	if res := c.Result(); res.Len() != 0 || res.HasWarnings() {
		t.Errorf("Result() = %v; want no issue", res)
	}
}

func TestRouteWarnings_ARoutedCollectorKeepsItsCapForFailures(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(1)
	warnings := 0
	c.RouteWarnings(func(diag.Issue) { warnings++ })
	for range 3 {
		c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "w").Build())
	}
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "e1").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "e2").Build())
	if warnings != 3 {
		t.Errorf("route received %d warnings; want 3", warnings)
	}
	re, _ := errors.AsType[*diag.ResultError](c.Err())
	if re == nil || re.Result.Len() != 1 || re.Result.DroppedCount() != 1 {
		t.Errorf("Err() = %v; want one failure stored and one dropped", c.Err())
	}
}

// truncatedBelowFailure returns a Result that saw two warnings and an error
// and holds one warning beside the error.
func truncatedBelowFailure() diag.Result {
	c := diag.NewCollector(2)
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "kept").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "e").Build())
	c.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "dropped").Build())
	return c.Result()
}

func TestRouteWarnings_MergePanicsOnADroppedWarning(t *testing.T) {
	t.Parallel()
	res := truncatedBelowFailure()
	if res.SeverityCounts().Warnings != 2 || res.Len() != 2 {
		t.Fatalf("fixture = %v; want two warnings seen and two issues held", res)
	}
	routed := func() *diag.Collector {
		c := diag.NewCollector(diag.NoLimit)
		c.RouteWarnings(func(diag.Issue) {})
		return c
	}
	for _, tc := range []struct {
		entry string
		merge func(*diag.Collector)
	}{
		{"Merge", func(c *diag.Collector) { c.Merge(res) }},
		{"MergeFunc", func(c *diag.Collector) { c.MergeFunc(res, func(i diag.Issue) diag.Issue { return i }) }},
		// The failures retag to warnings, so a dropped failure is a dropped
		// warning once merged.
		{"MergeRetag", func(c *diag.Collector) {
			failures := diag.NewCollector(1)
			failures.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "a").Build())
			failures.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "b").Build())
			c.MergeRetag(failures.Result(),
				func(diag.Severity, diag.Code) (diag.Severity, bool) { return diag.Warning, true },
				func(i diag.Issue) diag.Issue { return diag.NewIssue(diag.Warning, i.Code(), i.Message()).Build() })
		}},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			t.Parallel()
			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, "diag.Collector."+tc.entry) || !strings.Contains(msg, "saw 2 issues below failure severity and holds 1") {
					t.Errorf("panic = %q; want diag.Collector.%s naming 2 seen and 1 held", msg, tc.entry)
				}
			}()
			tc.merge(routed())
		})
	}

	// An unrouted collector merges the same result as before.
	c := diag.NewCollector(diag.NoLimit)
	c.Merge(res)
	if c.Result().DroppedCount() != 1 {
		t.Errorf("unrouted merge DroppedCount() = %d; want 1", c.Result().DroppedCount())
	}
}

func TestRouteWarnings_MergeKeepsAFailureDropCount(t *testing.T) {
	t.Parallel()
	failures := diag.NewCollector(1)
	failures.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "a").Build())
	failures.Collect(diag.NewIssue(diag.Error, diag.E_UNKNOWN_TYPE, "b").Build())
	failures.Collect(diag.NewIssue(diag.Warning, testCodeWarning, "w").Build())

	c := diag.NewCollector(diag.NoLimit)
	c.RouteWarnings(func(diag.Issue) { t.Error("a warning reached the route from a result that held none") })
	re, _ := errors.AsType[*diag.ResultError](failures.Err())
	c.Merge(re.Result)
	res := c.Result()
	if res.DroppedCount() != 1 || res.SeverityCounts().Errors != 2 || !errors.Is(c.Err(), diag.E_UNKNOWN_TYPE) {
		t.Errorf("merged = %v; want two errors seen, one dropped, E_UNKNOWN_TYPE among them", res)
	}
}

// TestRouteWarnings_CollectAllStoresAFailureBeforeTheWarningAfterIt pins that
// under a route CollectAll stores each failure before it delivers the warning
// that follows it, so the route reads the collector as the issues arrived.
func TestRouteWarnings_CollectAllStoresAFailureBeforeTheWarningAfterIt(t *testing.T) {
	t.Parallel()
	c := diag.NewCollector(diag.NoLimit)
	var seen []int
	c.RouteWarnings(func(diag.Issue) { seen = append(seen, c.ErrorCount()) })
	c.CollectAll([]diag.Issue{
		diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_PATH_EXTENSION, "w1").Build(),
		diag.NewIssue(diag.Error, diag.E_SYNTAX, "e1").Build(),
		diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_PATH_EXTENSION, "w2").Build(),
	})
	if !slices.Equal(seen, []int{0, 1}) {
		t.Errorf("error counts at each delivery = %v; want [0 1]", seen)
	}
}

// TestRouteWarnings_MergePanicsOnADroppedInfoOrHint pins that a routed merge
// of a result that dropped an Info or a Hint panics, as one that dropped a
// Warning does: the route cannot be handed an issue the result dropped.
func TestRouteWarnings_MergePanicsOnADroppedInfoOrHint(t *testing.T) {
	t.Parallel()
	for _, sev := range []diag.Severity{diag.Info, diag.Hint} {
		src := diag.NewCollector(1)
		src.Collect(diag.NewIssue(sev, diag.W_SNAPSHOT_PATH_EXTENSION, "kept").Build())
		src.Collect(diag.NewIssue(sev, diag.W_SNAPSHOT_PATH_EXTENSION, "dropped").Build())
		c := diag.NewCollector(diag.NoLimit)
		c.RouteWarnings(func(diag.Issue) {})
		msg := func() (msg string) {
			defer func() { msg, _ = recover().(string) }()
			c.Merge(src.Result())
			return ""
		}()
		if !strings.Contains(msg, "a routed merge was given a result that saw 2 issues below failure severity and holds 1") {
			t.Errorf("routed merge of a result that dropped a %s: panic %q", sev, msg)
		}
	}
}

// TestRouteWarnings_MergeCountsNoWarningCode pins that a routed collector
// counts the merged failures' codes alone: the routed warning's code is in no
// count of its result.
func TestRouteWarnings_MergeCountsNoWarningCode(t *testing.T) {
	t.Parallel()
	src := diag.NewCollector(diag.NoLimit)
	src.Collect(diag.NewIssue(diag.Warning, diag.W_SNAPSHOT_PATH_EXTENSION, "w").Build())
	src.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, "e").Build())
	c := diag.NewCollector(diag.NoLimit)
	c.RouteWarnings(func(diag.Issue) {})
	c.Merge(src.Result())
	res := c.Result()
	if got := res.CodeCounts(diag.Warning); len(got) != 0 {
		t.Errorf("CodeCounts(Warning) = %v; want none", got)
	}
	if got := res.CodeCounts(diag.Error); got[diag.E_SYNTAX] != 1 {
		t.Errorf("CodeCounts(Error) = %v; want E_SYNTAX once", got)
	}
}
