package diag

import (
	"fmt"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector(100)

	if c.Len() != 0 {
		t.Errorf("Len() = %d; want 0", c.Len())
	}
	if !c.OK() {
		t.Error("OK() = false; want true for empty collector")
	}
	if c.LimitReached() {
		t.Error("LimitReached() = true; want false")
	}
}

func TestCollector_Collect(t *testing.T) {
	c := NewCollector(0) // No limit

	issue := NewIssue(Error, E_SYNTAX, "test error").Build()
	c.Collect(issue)

	if c.Len() != 1 {
		t.Errorf("Len() = %d; want 1", c.Len())
	}
	if c.OK() {
		t.Error("OK() = true; want false after collecting error")
	}
	if !c.HasErrors() {
		t.Error("HasErrors() = false; want true")
	}
}

func TestCollector_Collect_PanicOnZeroValue(t *testing.T) {
	c := NewCollector(0)

	defer func() {
		r := recover()
		if r == nil {
			t.Error("Collect(Issue{}) should panic")
		}
		if s, ok := r.(string); !ok || s != "diag.Collector.Collect: zero-value Issue" {
			t.Errorf("panic message = %v; want 'zero-value Issue'", r)
		}
	}()

	c.Collect(Issue{})
}

func TestCollector_Collect_PanicOnInvalidIssue(t *testing.T) {
	c := NewCollector(0)

	// Issue with code but no message
	invalidIssue := Issue{code: E_SYNTAX}

	defer func() {
		r := recover()
		if r == nil {
			t.Error("Collect(invalid issue) should panic")
		}
	}()

	c.Collect(invalidIssue)
}

func TestCollector_Collect_PanicOnInvalidSeverity(t *testing.T) {
	c := NewCollector(0)

	// Issue with invalid severity (255 is not a valid Severity value)
	invalidIssue := Issue{
		severity: Severity(255),
		code:     E_SYNTAX,
		message:  "test",
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Error("Collect(issue with invalid severity) should panic")
		}
	}()

	c.Collect(invalidIssue)
}

func TestCollector_CollectAll(t *testing.T) {
	c := NewCollector(0)

	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "error 1").Build(),
		NewIssue(Warning, E_INVALID_NAME, "warning").Build(),
		NewIssue(Error, E_TYPE_COLLISION, "error 2").Build(),
	}

	c.CollectAll(issues)

	if c.Len() != 3 {
		t.Errorf("Len() = %d; want 3", c.Len())
	}
}

func TestCollector_CollectAll_PanicOnInvalid(t *testing.T) {
	c := NewCollector(0)

	issues := []Issue{
		NewIssue(Error, E_SYNTAX, "valid").Build(),
		{}, // Zero value - invalid
	}

	defer func() {
		if r := recover(); r == nil {
			t.Error("CollectAll with invalid issue should panic")
		}
	}()

	c.CollectAll(issues)
}

func TestCollector_Merge(t *testing.T) {
	c1 := NewCollector(0)
	c1.Collect(NewIssue(Error, E_SYNTAX, "error 1").Build())
	c1.Collect(NewIssue(Warning, E_INVALID_NAME, "warning").Build())

	result := c1.Result()

	c2 := NewCollector(0)
	c2.Collect(NewIssue(Error, E_TYPE_COLLISION, "error 2").Build())
	c2.Merge(result)

	if c2.Len() != 3 {
		t.Errorf("Len() = %d; want 3 after merge", c2.Len())
	}
}

func TestCollector_Limit(t *testing.T) {
	c := NewCollector(2)

	c.Collect(NewIssue(Error, E_SYNTAX, "first").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "second").Build())

	if c.LimitReached() {
		t.Error("LimitReached() = true; want false (at limit but not over)")
	}

	c.Collect(NewIssue(Error, E_SYNTAX, "third").Build())

	if !c.LimitReached() {
		t.Error("LimitReached() = false; want true")
	}
	if c.Len() != 2 {
		t.Errorf("Len() = %d; want 2 (limit)", c.Len())
	}
	if c.DroppedCount() != 1 {
		t.Errorf("DroppedCount() = %d; want 1", c.DroppedCount())
	}
}

func TestCollector_Result_Sorted(t *testing.T) {
	source := location.MustNewSourceID("test://b.yammm")
	sourceA := location.MustNewSourceID("test://a.yammm")

	c := NewCollector(0)

	// Add issues in non-sorted order
	c.Collect(NewIssue(Error, E_SYNTAX, "b:10").WithSpan(location.Point(source, 10, 1)).Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "a:5").WithSpan(location.Point(sourceA, 5, 1)).Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "b:1").WithSpan(location.Point(source, 1, 1)).Build())

	result := c.Result()

	var messages []string
	for issue := range result.Issues() {
		messages = append(messages, issue.Message())
	}

	// Should be sorted: a.yammm first, then b.yammm by line
	expected := []string{"a:5", "b:1", "b:10"}
	for i, msg := range messages {
		if msg != expected[i] {
			t.Errorf("Issue[%d].Message() = %q; want %q", i, msg, expected[i])
		}
	}
}

func TestCollector_Result_Cached(t *testing.T) {
	c := NewCollector(0)
	c.Collect(NewIssue(Error, E_SYNTAX, "test").Build())

	result1 := c.Result()
	result2 := c.Result()

	// Results should be equal (cached)
	if result1.Len() != result2.Len() {
		t.Error("cached results should be equal")
	}

	// Collect invalidates cache
	c.Collect(NewIssue(Warning, E_INVALID_NAME, "another").Build())
	result3 := c.Result()

	if result3.Len() != 2 {
		t.Errorf("Len() = %d; want 2 after new collect", result3.Len())
	}
}

func TestCollector_Result_Independent(t *testing.T) {
	c := NewCollector(0)
	c.Collect(NewIssue(Error, E_SYNTAX, "first").Build())

	result1 := c.Result()

	c.Collect(NewIssue(Error, E_TYPE_COLLISION, "second").Build())

	// result1 should still have only 1 issue
	if result1.Len() != 1 {
		t.Errorf("result1.Len() = %d; want 1 (should be independent)", result1.Len())
	}

	result2 := c.Result()
	if result2.Len() != 2 {
		t.Errorf("result2.Len() = %d; want 2", result2.Len())
	}
}

func TestCollector_SeverityQueries(t *testing.T) {
	c := NewCollector(0)

	// Initially OK
	if !c.OK() {
		t.Error("empty collector should be OK")
	}
	if c.HasErrors() {
		t.Error("empty collector should not have errors")
	}
	if c.HasFatal() {
		t.Error("empty collector should not have fatal")
	}

	// Add warning - still OK
	c.Collect(NewIssue(Warning, E_INVALID_NAME, "warning").Build())
	if !c.OK() {
		t.Error("collector with only warnings should be OK")
	}

	// Add error - not OK
	c.Collect(NewIssue(Error, E_SYNTAX, "error").Build())
	if c.OK() {
		t.Error("collector with error should not be OK")
	}
	if !c.HasErrors() {
		t.Error("collector with error should have errors")
	}

	// Add fatal
	c.Collect(NewIssue(Fatal, E_LIMIT_REACHED, "fatal").Build())
	if !c.HasFatal() {
		t.Error("collector with fatal should have fatal")
	}
}

func TestCollector_ThreadSafety(t *testing.T) {
	c := NewCollector(0)

	var wg sync.WaitGroup
	numGoroutines := 10
	issuesPerGoroutine := 100

	// Concurrent writes
	for i := range numGoroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range issuesPerGoroutine {
				issue := NewIssue(Error, E_SYNTAX, "test").
					WithPath("data.json", "$.item").
					WithDetails(Detail{Key: "id", Value: string(rune('0' + id))}).
					WithDetails(Detail{Key: "j", Value: string(rune('0' + j%10))}).
					Build()
				c.Collect(issue)
			}
		}(i)
	}

	// Concurrent reads during writes
	for range numGoroutines / 2 {
		wg.Go(func() {
			for range issuesPerGoroutine {
				_ = c.OK()
				_ = c.HasErrors()
				_ = c.Len()
			}
		})
	}

	wg.Wait()

	expected := numGoroutines * issuesPerGoroutine
	if c.Len() != expected {
		t.Errorf("Len() = %d; want %d", c.Len(), expected)
	}
}

func TestCollector_ThreadSafety_Result(t *testing.T) {
	c := NewCollector(0)

	var wg sync.WaitGroup

	// Writers
	for range 5 {
		wg.Go(func() {
			for range 50 {
				c.Collect(NewIssue(Error, E_SYNTAX, "test").Build())
			}
		})
	}

	// Readers requesting Result during writes
	for range 3 {
		wg.Go(func() {
			for range 20 {
				result := c.Result()
				// Just access the result to ensure no race
				_ = result.Len()
				_ = result.OK()
			}
		})
	}

	wg.Wait()
}

func TestCollector_ThreadSafety_Merge(t *testing.T) {
	// Create a source result
	source := NewCollector(0)
	for range 10 {
		source.Collect(NewIssue(Error, E_SYNTAX, "source").Build())
	}
	sourceResult := source.Result()

	// Concurrent merges
	c := NewCollector(0)
	var wg sync.WaitGroup

	for range 5 {
		wg.Go(func() {
			c.Merge(sourceResult)
		})
	}

	wg.Wait()

	// Should have 50 issues (5 merges * 10 issues each)
	if c.Len() != 50 {
		t.Errorf("Len() = %d; want 50", c.Len())
	}
}

func TestCollector_NoLimit(t *testing.T) {
	c := NewCollector(0) // 0 means no limit

	// Add many issues
	for range 1000 {
		c.Collect(NewIssue(Error, E_SYNTAX, "test").Build())
	}

	if c.Len() != 1000 {
		t.Errorf("Len() = %d; want 1000", c.Len())
	}
	if c.LimitReached() {
		t.Error("LimitReached() = true; want false (no limit)")
	}
}

func TestCollector_NegativeLimit(t *testing.T) {
	c := NewCollector(-1) // Negative means no limit

	for range 100 {
		c.Collect(NewIssue(Error, E_SYNTAX, "test").Build())
	}

	if c.Len() != 100 {
		t.Errorf("Len() = %d; want 100", c.Len())
	}
	if c.LimitReached() {
		t.Error("LimitReached() = true; want false (negative = no limit)")
	}
}

// -----------------------------------------------------------------------------
// Deterministic Ordering Tests
// -----------------------------------------------------------------------------

func TestCompareIssues_SpanBackedBeforePathOnly(t *testing.T) {
	source := location.MustNewSourceID("test://a.yammm")

	spanBacked := NewIssue(Error, E_SYNTAX, "span-backed").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	pathOnly := NewIssue(Error, E_SYNTAX, "path-only").
		WithPath("data.json", "$.root").
		Build()

	// Span-backed should sort before path-only
	if cmp := compareIssues(spanBacked, pathOnly); cmp >= 0 {
		t.Errorf("compareIssues(spanBacked, pathOnly) = %d; want < 0", cmp)
	}
	if cmp := compareIssues(pathOnly, spanBacked); cmp <= 0 {
		t.Errorf("compareIssues(pathOnly, spanBacked) = %d; want > 0", cmp)
	}
}

func TestCompareIssues_PathOnlyOrdering(t *testing.T) {
	// Path-only issues should sort by sourceName, then path
	issue1 := NewIssue(Error, E_SYNTAX, "msg").
		WithPath("a.json", "$.x").
		Build()
	issue2 := NewIssue(Error, E_SYNTAX, "msg").
		WithPath("a.json", "$.y").
		Build()
	issue3 := NewIssue(Error, E_SYNTAX, "msg").
		WithPath("b.json", "$.x").
		Build()

	// a.json:$.x < a.json:$.y
	if cmp := compareIssues(issue1, issue2); cmp >= 0 {
		t.Errorf("compareIssues(a.json:$.x, a.json:$.y) = %d; want < 0", cmp)
	}

	// a.json:$.y < b.json:$.x (sourceName takes precedence)
	if cmp := compareIssues(issue2, issue3); cmp >= 0 {
		t.Errorf("compareIssues(a.json:$.y, b.json:$.x) = %d; want < 0", cmp)
	}
}

func TestCompareIssues_SeverityTieBreaker(t *testing.T) {
	source := location.MustNewSourceID("test://a.yammm")

	// Same span, same code, different severity
	errorIssue := NewIssue(Error, E_SYNTAX, "same message").
		WithSpan(location.Point(source, 1, 1)).
		Build()
	warningIssue := NewIssue(Warning, E_SYNTAX, "same message").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	// Error (severity 1) < Warning (severity 2) numerically
	if cmp := compareIssues(errorIssue, warningIssue); cmp >= 0 {
		t.Errorf("compareIssues(Error, Warning) = %d; want < 0", cmp)
	}
}

func TestCompareIssues_MessageTieBreaker(t *testing.T) {
	source := location.MustNewSourceID("test://a.yammm")

	// Same span, same code, same severity, different message
	issueA := NewIssue(Error, E_SYNTAX, "aaa").
		WithSpan(location.Point(source, 1, 1)).
		Build()
	issueB := NewIssue(Error, E_SYNTAX, "bbb").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	if cmp := compareIssues(issueA, issueB); cmp >= 0 {
		t.Errorf("compareIssues(aaa, bbb) = %d; want < 0", cmp)
	}
}

func TestCompareIssues_HintTieBreaker(t *testing.T) {
	source := location.MustNewSourceID("test://a.yammm")

	// Same everything except hint
	issueA := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithHint("hint A").
		Build()
	issueB := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithHint("hint B").
		Build()

	if cmp := compareIssues(issueA, issueB); cmp >= 0 {
		t.Errorf("compareIssues(hintA, hintB) = %d; want < 0", cmp)
	}
}

func TestCompareIssues_DetailsTieBreaker(t *testing.T) {
	source := location.MustNewSourceID("test://a.yammm")

	// Same everything except details
	issueA := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithDetails(Detail{Key: "key", Value: "a"}).
		Build()
	issueB := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithDetails(Detail{Key: "key", Value: "b"}).
		Build()

	if cmp := compareIssues(issueA, issueB); cmp >= 0 {
		t.Errorf("compareIssues(detailA, detailB) = %d; want < 0", cmp)
	}

	// Fewer details sorts before more details
	issueNoDetails := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		Build()
	issueWithDetails := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithDetails(Detail{Key: "key", Value: "val"}).
		Build()

	if cmp := compareIssues(issueNoDetails, issueWithDetails); cmp >= 0 {
		t.Errorf("compareIssues(noDetails, withDetails) = %d; want < 0", cmp)
	}
}

func TestCompareIssues_RelatedTieBreaker(t *testing.T) {
	source := location.MustNewSourceID("test://a.yammm")
	relSource := location.MustNewSourceID("test://related.yammm")

	// Same everything except related info
	issueA := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithRelated(location.RelatedInfo{
			Span:    location.Point(relSource, 1, 1),
			Message: "related A",
		}).
		Build()
	issueB := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithRelated(location.RelatedInfo{
			Span:    location.Point(relSource, 1, 1),
			Message: "related B",
		}).
		Build()

	if cmp := compareIssues(issueA, issueB); cmp >= 0 {
		t.Errorf("compareIssues(relatedA, relatedB) = %d; want < 0", cmp)
	}
}

func TestCompareIssues_TotalOrder_IdenticalIssuesEqual(t *testing.T) {
	source := location.MustNewSourceID("test://a.yammm")

	issue := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithHint("hint").
		WithDetails(Detail{Key: "k", Value: "v"}).
		Build()

	// Identical issues should compare equal
	if cmp := compareIssues(issue, issue); cmp != 0 {
		t.Errorf("compareIssues(issue, issue) = %d; want 0", cmp)
	}
}

func TestCompareIssues_HybridIssues_DifferentPaths(t *testing.T) {
	// This test verifies the fix for the total order bug where hybrid issues
	// with identical spans but different paths would incorrectly compare equal.
	source := location.MustNewSourceID("test://schema.yammm")

	// Two hybrid issues: same span, same everything, but different instance paths
	issue1 := NewIssue(Error, E_TYPE_MISMATCH, "expected integer").
		WithSpan(location.Point(source, 10, 5)).
		WithPath("data.json", "$.users[0].age").
		Build()

	issue2 := NewIssue(Error, E_TYPE_MISMATCH, "expected integer").
		WithSpan(location.Point(source, 10, 5)).
		WithPath("data.json", "$.users[1].age").
		Build()

	// They must NOT compare equal (total order requires distinct issues to be distinguishable)
	if cmp := compareIssues(issue1, issue2); cmp == 0 {
		t.Error("compareIssues(issue1, issue2) = 0; want non-zero for distinct hybrid issues")
	}

	// Verify ordering: $.users[0] < $.users[1]
	if cmp := compareIssues(issue1, issue2); cmp >= 0 {
		t.Errorf("compareIssues($.users[0], $.users[1]) = %d; want < 0", cmp)
	}
	if cmp := compareIssues(issue2, issue1); cmp <= 0 {
		t.Errorf("compareIssues($.users[1], $.users[0]) = %d; want > 0", cmp)
	}
}

func TestCompareIssues_HybridIssues_DifferentSourceNames(t *testing.T) {
	// Verify sourceName tie-breaker for hybrid issues
	source := location.MustNewSourceID("test://schema.yammm")

	issue1 := NewIssue(Error, E_TYPE_MISMATCH, "expected integer").
		WithSpan(location.Point(source, 10, 5)).
		WithPath("a.json", "$.value").
		Build()

	issue2 := NewIssue(Error, E_TYPE_MISMATCH, "expected integer").
		WithSpan(location.Point(source, 10, 5)).
		WithPath("b.json", "$.value").
		Build()

	// a.json < b.json
	if cmp := compareIssues(issue1, issue2); cmp >= 0 {
		t.Errorf("compareIssues(a.json, b.json) = %d; want < 0", cmp)
	}
}

func TestCompareIssues_SchemaOnlyBeforeHybrid(t *testing.T) {
	// When span, code, severity, message, hint are all equal,
	// schema-only (sourceName="", path="") should sort before hybrid
	source := location.MustNewSourceID("test://schema.yammm")

	schemaOnly := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		Build()

	hybrid := NewIssue(Error, E_SYNTAX, "msg").
		WithSpan(location.Point(source, 1, 1)).
		WithPath("data.json", "$.x").
		Build()

	// "" < "data.json", so schema-only sorts first
	if cmp := compareIssues(schemaOnly, hybrid); cmp >= 0 {
		t.Errorf("compareIssues(schemaOnly, hybrid) = %d; want < 0", cmp)
	}
}

func TestCollector_DeterministicOrdering_Concurrent(t *testing.T) {
	// This test verifies that Result() produces deterministic output
	// regardless of the order in which issues are collected concurrently.
	const (
		numRuns       = 5
		numGoroutines = 10
		issuesPerG    = 20
	)

	source := location.MustNewSourceID("test://a.yammm")

	// Run multiple times to detect non-determinism
	var referenceOrder []string

	for run := range numRuns {
		c := NewCollector(0)
		var wg sync.WaitGroup

		// Collect issues concurrently with intentionally overlapping attributes
		for g := range numGoroutines {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for i := range issuesPerG {
					// Create issues that differ only by message (tie-breaker test).
					// Each message is unique (A00-A19, B00-B19, etc.) to ensure
					// any reordering instability is detectable.
					msg := fmt.Sprintf("%c%02d", 'A'+goroutineID, i)
					issue := NewIssue(Error, E_SYNTAX, msg).
						WithSpan(location.Point(source, 1, 1)).
						Build()
					c.Collect(issue)
				}
			}(g)
		}

		wg.Wait()

		// Extract ordered messages
		result := c.Result()
		var messages []string
		for issue := range result.Issues() {
			messages = append(messages, issue.Message())
		}

		if run == 0 {
			referenceOrder = messages
		} else {
			// Verify same order as first run
			if len(messages) != len(referenceOrder) {
				t.Fatalf("run %d: got %d issues; want %d", run, len(messages), len(referenceOrder))
			}
			for i, msg := range messages {
				if msg != referenceOrder[i] {
					t.Errorf("run %d: Issue[%d] = %q; want %q (non-deterministic ordering)",
						run, i, msg, referenceOrder[i])
					break
				}
			}
		}
	}
}

func TestCollector_DeterministicOrdering_MixedIssueTypes(t *testing.T) {
	// Verify ordering with mix of span-backed, path-only, and hybrid issues
	sourceA := location.MustNewSourceID("test://a.yammm")
	sourceB := location.MustNewSourceID("test://b.yammm")

	c := NewCollector(0)

	// Add in deliberately scrambled order
	c.Collect(NewIssue(Error, E_SYNTAX, "path-only-2").WithPath("data.json", "$.b").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "span-b-10").WithSpan(location.Point(sourceB, 10, 1)).Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "path-only-1").WithPath("data.json", "$.a").Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "span-a-1").WithSpan(location.Point(sourceA, 1, 1)).Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "span-a-5").WithSpan(location.Point(sourceA, 5, 1)).Build())
	c.Collect(NewIssue(Warning, E_SYNTAX, "span-a-1-warn").WithSpan(location.Point(sourceA, 1, 1)).Build())
	c.Collect(NewIssue(Error, E_SYNTAX, "hybrid").WithSpan(location.Point(sourceA, 1, 1)).WithPath("data.json", "$.x").Build())

	result := c.Result()
	var messages []string
	for issue := range result.Issues() {
		messages = append(messages, issue.Message())
	}

	// Expected order:
	// 1. Span-backed first, sorted by source then position then severity then message
	//    - a.yammm:1:1 with Error + "hybrid" (has both span and path, span takes precedence)
	//    - a.yammm:1:1 with Error + "span-a-1"
	//    - a.yammm:1:1 with Warning + "span-a-1-warn"
	//    - a.yammm:5:1 + "span-a-5"
	//    - b.yammm:10:1 + "span-b-10"
	// 2. Path-only issues, sorted by sourceName then path
	//    - data.json:$.a + "path-only-1"
	//    - data.json:$.b + "path-only-2"
	expected := []string{
		"hybrid",        // a.yammm:1:1, Error, "hybrid" < "span-a-1"
		"span-a-1",      // a.yammm:1:1, Error
		"span-a-1-warn", // a.yammm:1:1, Warning (severity 2 > 1)
		"span-a-5",      // a.yammm:5:1
		"span-b-10",     // b.yammm:10:1
		"path-only-1",   // data.json:$.a
		"path-only-2",   // data.json:$.b
	}

	if len(messages) != len(expected) {
		t.Fatalf("got %d issues; want %d", len(messages), len(expected))
	}
	for i, msg := range messages {
		if msg != expected[i] {
			t.Errorf("Issue[%d] = %q; want %q", i, msg, expected[i])
		}
	}
}

// TestNewCollector_NormalizesNegativeLimit verifies that negative limits
// are normalized to 0 (unlimited) in NewCollector.
func TestNewCollector_NormalizesNegativeLimit(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{-100, 0},
		{-1, 0},
		{0, 0},
		{1, 1},
		{100, 100},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("limit=%d", tt.input), func(t *testing.T) {
			c := NewCollector(tt.input)
			result := c.Result()

			if result.Limit() != tt.expected {
				t.Errorf("NewCollector(%d).Result().Limit() = %d; want %d",
					tt.input, result.Limit(), tt.expected)
			}
		})
	}
}

// TestNewCollector_NegativeLimitActsAsUnlimited verifies that negative limits
// result in unlimited collection (no issues are dropped).
func TestNewCollector_NegativeLimitActsAsUnlimited(t *testing.T) {
	c := NewCollector(-1)

	// Collect many issues
	for i := range 100 {
		issue := NewIssue(Error, E_SYNTAX, fmt.Sprintf("error %d", i)).Build()
		c.Collect(issue)
	}

	if c.Len() != 100 {
		t.Errorf("Len() = %d; want 100 (unlimited)", c.Len())
	}
	if c.LimitReached() {
		t.Error("LimitReached() = true; want false (unlimited)")
	}
	if c.DroppedCount() != 0 {
		t.Errorf("DroppedCount() = %d; want 0 (unlimited)", c.DroppedCount())
	}
}
