package diag

import (
	"fmt"
	"slices"
	"sync"

	"github.com/simon-lentz/yammm/location"
)

// Collector provides concurrent issue collection with precomputed severity counts.
//
// Collector is thread-safe and can be used from multiple goroutines. It provides
// O(1) severity queries via precomputed counts that are updated during collection.
//
// Limit behavior: When the issue limit is reached, additional issues are dropped
// but [Collector.OK] is not affected. Use [Collector.LimitReached] to detect
// truncated results. This design allows callers to handle truncated results
// appropriately without forcing failure semantics.
//
// Create a Collector with [NewCollector], then use [Collector.Collect] to add
// issues and [Collector.Result] to get an immutable snapshot.
type Collector struct {
	mu           sync.RWMutex
	issues       []Issue
	limit        int
	limitReached bool
	droppedCount int

	// Precomputed severity counts for O(1) queries
	fatalCount   int
	errorCount   int
	warningCount int
	infoCount    int
	hintCount    int

	// Cached sorted result (invalidated on Collect)
	cachedResult *Result
}

// NoLimit is the sentinel value indicating unlimited issue collection.
// Use this constant with [NewCollector] for clarity:
//
//	c := diag.NewCollector(diag.NoLimit)
const NoLimit = 0

// NewCollector creates a collector with an optional issue limit.
//
// A limit of 0 means no limit (use [NoLimit] constant for clarity). Negative
// values are normalized to 0. When the limit is reached, additional issues
// are counted as dropped and can be queried via [Result.DroppedCount].
func NewCollector(limit int) *Collector {
	if limit < 0 {
		limit = 0
	}
	return &Collector{
		limit: limit,
	}
}

// NewCollectorUnlimited creates a collector with no issue limit.
//
// This is equivalent to NewCollector(NoLimit) but provides clearer intent
// at call sites where unlimited collection is deliberate.
func NewCollectorUnlimited() *Collector {
	return NewCollector(NoLimit)
}

// Collect adds an issue to the collector.
//
// This method is thread-safe. If the limit is reached, the issue is counted
// as dropped but not stored.
//
// Collect panics if the issue is a zero value or is invalid. Use [NewIssue]
// and [IssueBuilder] to construct valid issues. This panic behavior catches
// programmer errors where issues are constructed via direct struct literals
// rather than the builder pattern.
func (c *Collector) Collect(issue Issue) {
	c.validateIssue(issue)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.collectLocked(issue)
}

// CollectAll adds multiple issues efficiently under a single lock.
//
// This is more efficient than calling [Collect] multiple times when adding
// many issues at once.
//
// Panics if any issue is invalid (see [Collect]).
func (c *Collector) CollectAll(issues []Issue) {
	// Validate all issues before acquiring lock
	for _, issue := range issues {
		c.validateIssue(issue)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, issue := range issues {
		c.collectLocked(issue)
	}
}

// Merge incorporates all issues from a Result under a single lock.
//
// Results are structurally guaranteed to contain only valid issues because
// the Result type has no public constructor accepting arbitrary issues.
// Valid Results can only be obtained via [Collector.Result], [OK], or
// package APIs that use Collector internally. Therefore, Merge does not
// re-validate issues.
//
// This differs from [Collect] and [CollectAll], which actively validate
// each issue because they accept Issue values directly.
func (c *Collector) Merge(res Result) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for issue := range res.Issues() {
		c.collectLocked(issue)
	}
}

// validateIssue panics if the issue is invalid.
func (c *Collector) validateIssue(issue Issue) {
	if issue.IsZero() {
		panic("diag.Collector.Collect: zero-value Issue")
	}
	if !issue.IsValid() {
		panic(fmt.Sprintf("diag.Collector.Collect: invalid Issue (code=%s, message=%q)",
			issue.Code().String(), issue.Message()))
	}
}

// collectLocked adds an issue. Caller must hold c.mu.
func (c *Collector) collectLocked(issue Issue) {
	// Invalidate cached result
	c.cachedResult = nil

	// Check limit
	if c.limit > 0 && len(c.issues) >= c.limit {
		c.limitReached = true
		c.droppedCount++
		return
	}

	c.issues = append(c.issues, issue)

	// Update severity counts
	switch issue.Severity() {
	case Fatal:
		c.fatalCount++
	case Error:
		c.errorCount++
	case Warning:
		c.warningCount++
	case Info:
		c.infoCount++
	case Hint:
		c.hintCount++
	}
}

// Result produces a sorted, immutable snapshot.
//
// The returned Result is independent of the Collector; subsequent Collect
// calls do not affect it. Results are cached until the next Collect call.
//
// Issues are sorted by source, position, and code for deterministic output.
func (c *Collector) Result() Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedResult != nil {
		return *c.cachedResult
	}

	// Copy issues into a new slice for sorting (don't mutate c.issues)
	sorted := make([]Issue, len(c.issues))
	copy(sorted, c.issues)

	// Sort by source, position, code
	slices.SortFunc(sorted, compareIssues)

	result := newResult(sorted, c.limit, c.limitReached, c.droppedCount)
	c.cachedResult = &result
	return result
}

// compareIssues compares two issues for deterministic sorting.
//
// Ordering rules (per architecture spec "Deterministic Ordering (Codes)"):
//  1. Span-backed issues before path-only issues
//  2. Span-backed: Source, Start position, End position
//  3. Path-only: SourceName, Path
//  4. Common tie-breakers: Code, Severity, Message, Hint
//  5. Provenance tie-breakers: SourceName, Path (for hybrid issue total order)
//  6. Final tie-breakers: Details, Related (for true total order)
//
// This function implements a total order: distinct issues never compare equal.
// This guarantees deterministic output from Collector.Result() regardless of
// collection order or concurrency.
func compareIssues(a, b Issue) int {
	// 1. Span-backed vs path-only discriminant
	// Span-backed issues (including hybrid) sort before path-only issues
	aHasSpan := !a.span.IsZero()
	bHasSpan := !b.span.IsZero()
	if aHasSpan != bHasSpan {
		if aHasSpan {
			return -1 // span-backed sorts before path-only
		}
		return 1
	}

	// 2. Both span-backed: compare by span geometry
	if aHasSpan {
		if cmp := location.Compare(a.span, b.span); cmp != 0 {
			return cmp
		}
	} else {
		// 3. Both path-only: compare by sourceName, then path
		if a.sourceName != b.sourceName {
			if a.sourceName < b.sourceName {
				return -1
			}
			return 1
		}
		if a.path != b.path {
			if a.path < b.path {
				return -1
			}
			return 1
		}
	}

	// 4. Common tie-breakers: Code, Severity, Message
	if a.code.value != b.code.value {
		if a.code.value < b.code.value {
			return -1
		}
		return 1
	}

	if a.severity != b.severity {
		if a.severity < b.severity {
			return -1
		}
		return 1
	}

	if a.message != b.message {
		if a.message < b.message {
			return -1
		}
		return 1
	}

	// 5. Extended tie-breakers for true total order
	if a.hint != b.hint {
		if a.hint < b.hint {
			return -1
		}
		return 1
	}

	// Include provenance fields even for span-backed issues to ensure total order.
	// This handles hybrid issues (span + path) that may share identical spans but
	// differ in instance path. Without this, two hybrid issues with the same span
	// but different paths would compare equal, violating the total order claim.
	if a.sourceName != b.sourceName {
		if a.sourceName < b.sourceName {
			return -1
		}
		return 1
	}
	if a.path != b.path {
		if a.path < b.path {
			return -1
		}
		return 1
	}

	// Compare details lexicographically
	if cmp := compareDetails(a.details, b.details); cmp != 0 {
		return cmp
	}

	// Compare related info lexicographically
	return compareRelated(a.related, b.related)
}

// compareDetails compares two Detail slices lexicographically.
func compareDetails(a, b []Detail) int {
	minLen := min(len(a), len(b))
	for i := range minLen {
		if a[i].Key != b[i].Key {
			if a[i].Key < b[i].Key {
				return -1
			}
			return 1
		}
		if a[i].Value != b[i].Value {
			if a[i].Value < b[i].Value {
				return -1
			}
			return 1
		}
	}
	// Shorter slice sorts first
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return 0
}

// compareRelated compares two RelatedInfo slices lexicographically.
func compareRelated(a, b []location.RelatedInfo) int {
	minLen := min(len(a), len(b))
	for i := range minLen {
		if cmp := location.Compare(a[i].Span, b[i].Span); cmp != 0 {
			return cmp
		}
		if a[i].Message != b[i].Message {
			if a[i].Message < b[i].Message {
				return -1
			}
			return 1
		}
	}
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return 0
}

// HasFatal reports whether any Fatal issue has been collected.
//
// This is an O(1) operation using precomputed counts.
func (c *Collector) HasFatal() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fatalCount > 0
}

// HasErrors reports whether any Fatal or Error issue has been collected.
//
// This is an O(1) operation using precomputed counts.
func (c *Collector) HasErrors() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fatalCount > 0 || c.errorCount > 0
}

// OK reports whether no Fatal or Error issues have been collected.
//
// This is an O(1) operation using precomputed counts.
func (c *Collector) OK() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fatalCount == 0 && c.errorCount == 0
}

// Len returns the number of collected issues.
func (c *Collector) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.issues)
}

// LimitReached reports whether the limit was reached.
func (c *Collector) LimitReached() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.limitReached
}

// DroppedCount returns how many issues were dropped after hitting the limit.
func (c *Collector) DroppedCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.droppedCount
}
