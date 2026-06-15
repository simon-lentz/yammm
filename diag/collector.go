package diag

import (
	"cmp"
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

	// Severity counts reflect every issue SEEN, not just those stored under the
	// limit, so HasErrors/OK/ErrorCount stay truthful when collection is
	// truncated. The loader and completer gate on ErrorCount deltas to decide
	// whether a schema failed; an error dropped at the cap that left these
	// counts untouched would read as success and let a broken schema escape the
	// all-or-nothing contract. Len() remains the stored count.
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

	// At the limit, the issue is counted (above) but not stored.
	if c.limit > 0 && len(c.issues) >= c.limit {
		c.limitReached = true
		c.droppedCount++
		return
	}

	c.issues = append(c.issues, issue)
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

	// Carry the collector's SEEN severity counts rather than recomputing from
	// the stored slice (what newResult does): under truncation the dropped
	// issues are absent from sorted, and recomputing would make Result.OK /
	// HasErrors blind to a dropped error exactly as the gates would be. In the
	// non-truncated case the two are identical (every collected issue is stored).
	result := Result{
		issues:       sorted,
		limit:        c.limit,
		limitReached: c.limitReached,
		droppedCount: c.droppedCount,
		fatalCount:   c.fatalCount,
		errorCount:   c.errorCount,
		warningCount: c.warningCount,
		infoCount:    c.infoCount,
		hintCount:    c.hintCount,
	}
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
		if c := location.Compare(a.span, b.span); c != 0 {
			return c
		}
	} else {
		// 3. Both path-only: compare by sourceName, then path
		if c := cmp.Compare(a.sourceName, b.sourceName); c != 0 {
			return c
		}
		if c := cmp.Compare(a.path, b.path); c != 0 {
			return c
		}
	}

	// 4. Common tie-breakers: Code, Severity, Message
	if c := cmp.Compare(a.code.value, b.code.value); c != 0 {
		return c
	}
	if c := cmp.Compare(a.severity, b.severity); c != 0 {
		return c
	}
	if c := cmp.Compare(a.message, b.message); c != 0 {
		return c
	}

	// 5. Extended tie-breakers for true total order
	if c := cmp.Compare(a.hint, b.hint); c != 0 {
		return c
	}

	// Include provenance fields even for span-backed issues to ensure total order.
	// This handles hybrid issues (span + path) that may share identical spans but
	// differ in instance path. Without this, two hybrid issues with the same span
	// but different paths would compare equal, violating the total order claim.
	if c := cmp.Compare(a.sourceName, b.sourceName); c != 0 {
		return c
	}
	if c := cmp.Compare(a.path, b.path); c != 0 {
		return c
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
	for i := range min(len(a), len(b)) {
		if c := cmp.Compare(a[i].Key, b[i].Key); c != 0 {
			return c
		}
		if c := cmp.Compare(a[i].Value, b[i].Value); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
}

// compareRelated compares two RelatedInfo slices lexicographically.
func compareRelated(a, b []location.RelatedInfo) int {
	for i := range min(len(a), len(b)) {
		if c := location.Compare(a[i].Span, b[i].Span); c != 0 {
			return c
		}
		if c := cmp.Compare(a[i].Message, b[i].Message); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a), len(b))
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

// ErrorCount returns the number of Fatal and Error issues collected so far.
//
// This is an O(1) operation using precomputed counts. Callers that share
// one collector across nested operations can compare counts taken before
// and after an operation to determine whether that specific operation
// contributed errors — [Collector.HasErrors] only answers whether any
// operation has.
func (c *Collector) ErrorCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.fatalCount + c.errorCount
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
