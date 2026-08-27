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
// Limit behavior: the retained set is the `limit` most severe issues seen, ties
// broken by arrival order. Once the store is full an incoming issue that is more
// severe than the least severe stored one takes its slot (see storeLocked), so a
// flood of warnings can never starve the errors that explain why an operation
// failed. Truncation never affects [Result.OK]; use [Result.LimitReached]
// to detect it. This design allows callers to handle truncated results
// appropriately without forcing failure semantics.
//
// Create a Collector with [NewCollector], then use [Collector.Collect] to add
// issues and [Collector.Result] to get an immutable snapshot.
type Collector struct {
	mu           sync.RWMutex
	issues       []storedIssue
	nextArrival  uint64
	limit        int
	limitReached bool
	droppedCount int

	// Precomputed severity counts for O(1) queries. Counts every issue SEEN,
	// including those dropped past the limit (see storeLocked), so the severity
	// queries stay truthful under truncation.
	counts SeverityCounts

	// Severity counts over the STORED issues only. Unlike counts, these shrink
	// when an issue is evicted, so storeLocked can answer "which severity is
	// currently the least severe stored?" in O(1) and only scan the slice when an
	// eviction actually happens.
	storedCounts SeverityCounts

	// Cached sorted result (invalidated on Collect)
	cachedResult *Result
}

// storedIssue is an issue in the collector's store with the sequence number it
// arrived under. Eviction overwrites a slot in place, so slice position stops
// tracking arrival order after the first eviction; the victim among equally
// severe issues is chosen by arrival, never by position.
type storedIssue struct {
	issue   Issue
	arrival uint64
}

// NoLimit is the sentinel value indicating unlimited issue collection.
// Use this constant with [NewCollector] for clarity:
//
//	c := diag.NewCollector(diag.NoLimit)
const NoLimit = 0

// NewCollector creates a collector with an optional issue limit.
//
// A limit of 0 means no limit (use [NoLimit] constant for clarity). Negative
// values are normalized to 0. Once the limit is reached the collector retains
// the most severe issues it has seen — evicting a stored issue for a more severe
// incoming one — and counts the rest as dropped, queryable via
// [Result.DroppedCount]. See [Collector.storeLocked] for the retention rule.
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
// This is more efficient than calling [Collector.Collect] multiple times when
// adding many issues at once.
//
// Panics if any issue is invalid (see [Collector.Collect]).
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

// Merge incorporates a Result into this collector under a single lock.
//
// Results are structurally guaranteed to contain only valid issues because
// the Result type has no public constructor accepting arbitrary issues.
// Valid Results can only be obtained via [Collector.Result], [OK], or
// package APIs that use Collector internally. Therefore, Merge does not
// re-validate issues.
//
// This differs from [Collector.Collect] and [Collector.CollectAll], which actively validate
// each issue because they accept Issue values directly.
//
// A truncated res merges losslessly in aggregate: its dropped issues cannot be
// re-listed, but their severity contributions and its truncation state
// ([Result.LimitReached] / [Result.DroppedCount]) are carried into the
// receiver. So a dropped error in res cannot flip the merged result to OK — the
// same guarantee [Collector] already gives for directly-collected issues.
//
// The receiver's own configured limit is unchanged: merging a truncated res
// into an unlimited collector yields LimitReached()==true with Limit()==0.
// [Result.LimitReached] and [Result.DroppedCount] are the authoritative
// truncation facts after a merge; Limit() remains the receiver's local cap, not
// the cap that produced res's drops.
func (c *Collector) Merge(res Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cachedResult = nil

	// Fold in res's seen-severity counts wholesale. Each Result's counts reflect
	// every issue it saw — stored and dropped past its own limit — so adding the
	// counts (rather than re-counting only the surviving issues) keeps
	// OK/HasErrors/ErrorCount truthful even when res was itself truncated and its
	// dropped issues are no longer enumerable.
	c.counts.addCounts(res.SeverityCounts())

	// res's own drops can never be recovered, so carry its truncation forward.
	if res.limitReached {
		c.limitReached = true
		c.droppedCount += res.droppedCount
	}

	// Store res's surviving issues, honoring c's own limit. storeLocked does not
	// re-count (the counts were folded in above); issues past c's limit are
	// dropped and flagged.
	for _, issue := range res.issues {
		c.storeLocked(issue)
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
	c.counts.add(issue.Severity())

	c.storeLocked(issue)
}

// storeLocked appends an issue to the stored slice, or — once the limit is
// reached — retains it in place of a less severe stored issue. Caller must hold
// c.mu and must have already updated the seen-severity counts: storeLocked
// governs storage, not counting, so an issue that is not retained still shows up
// in the severity totals (and thus in HasErrors/OK/ErrorCount).
//
// The retained set is the `limit` most severe issues seen, ties broken by
// arrival order. Truncation that simply dropped whatever arrived after the cap
// let a producer starve its own errors: a load collects warnings during
// inheritance linearization and errors in every later phase, so a schema with
// enough shadowed annotations filled the budget with warnings and stored none of
// the errors explaining why it failed to load. Eviction makes the budget a
// severity floor rather than an arrival-order race.
//
// droppedCount counts every issue not retained, whether the incoming one was
// rejected or a stored one was evicted, so it remains "seen minus retained" and
// [Result.TruncationNote] stays accurate.
func (c *Collector) storeLocked(issue Issue) {
	if c.limit > 0 && len(c.issues) >= c.limit {
		c.limitReached = true
		c.droppedCount++

		idx, ok := c.evictionSlotLocked(issue.Severity())
		if !ok {
			return // Nothing stored is less severe; the incoming issue is the drop.
		}
		c.storedCounts.sub(c.issues[idx].issue.Severity())
		c.storedCounts.add(issue.Severity())
		// Overwrite in place: Result sorts before returning, so slice position
		// carries no meaning and the retained SET is the whole contract.
		c.issues[idx] = storedIssue{issue: issue, arrival: c.nextArrival}
		c.nextArrival++
		return
	}

	c.issues = append(c.issues, storedIssue{issue: issue, arrival: c.nextArrival})
	c.nextArrival++
	c.storedCounts.add(issue.Severity())
}

// evictionSlotLocked returns the index of the stored issue that must yield to an
// incoming issue of severity sev, and whether one exists. The victim is the
// LATEST-ARRIVED issue of the least severe stored severity, so among equally
// severe issues the earliest-arrived are the ones retained. Arrival is the
// stored sequence number, not slice position, because an evicted slot is
// reused in place. Caller must hold c.mu.
func (c *Collector) evictionSlotLocked(sev Severity) (int, bool) {
	victim, ok := c.storedCounts.leastSevere()
	if !ok || !sev.IsMoreSevereThan(victim) {
		return 0, false
	}
	idx, found := 0, false
	for i, st := range c.issues {
		if st.issue.Severity() == victim && (!found || st.arrival > c.issues[idx].arrival) {
			idx, found = i, true
		}
	}
	return idx, found // found is always true: storedCounts said one exists.
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
	for i, st := range c.issues {
		sorted[i] = st.issue
	}

	// Sort by source, position, code
	slices.SortFunc(sorted, compareIssues)

	// Carry the collector's SEEN severity counts rather than recomputing from
	// the stored slice (what newResult does): under truncation the dropped
	// issues are absent from sorted, and recomputing would make Result.OK /
	// HasErrors blind to a dropped error exactly as the gates would be. In the
	// non-truncated case the two are identical (every collected issue is stored).
	result := newResultWithCounts(sorted, c.limit, c.limitReached, c.droppedCount, c.counts)
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

// HasErrors reports whether any Fatal or Error issue has been collected.
//
// This is an O(1) operation using precomputed counts.
func (c *Collector) HasErrors() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.counts.Fatal > 0 || c.counts.Errors > 0
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
	return c.counts.Fatal + c.counts.Errors
}
