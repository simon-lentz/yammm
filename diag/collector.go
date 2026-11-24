package diag

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"sync"
)

// Collector provides concurrent issue collection with precomputed severity counts.
//
// Collector is thread-safe and can be used from multiple goroutines. It provides
// O(1) severity queries via precomputed counts that are updated during collection.
//
// Limit behavior: the retained set is the `limit` most severe issues collected
// into the collector, ties broken by arrival order. Once the store is full an
// incoming issue that is more severe than the least severe stored one takes its
// slot (see storeLocked), so a flood of warnings can never starve the errors
// that explain why an operation failed. A result merged in ([Collector.Merge])
// adds its dropped issues to the counts but cannot supply them, so after a
// truncated result is merged a less severe issue can hold a slot one of them
// would have taken. Truncation never hides a failure from [Collector.Err] or
// [Result.SeverityCounts]; use [Result.LimitReached] to detect it. This design
// allows callers to handle truncated results appropriately without forcing
// failure semantics.
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

	// Per-severity, per-code counts over every issue SEEN, kept beside counts
	// by the same rule and for the same reason.
	codeCounts codeCounts

	// Severity counts over the STORED issues only. Unlike counts, these shrink
	// when an issue is evicted, so storeLocked can answer "which severity is
	// currently the least severe stored?" in O(1) and only scan the slice when an
	// eviction actually happens.
	storedCounts SeverityCounts

	// Cached sorted result, invalidated whenever the collector changes
	cachedResult *Result

	// warn, when set, receives each issue below failure severity in place of
	// the store and the counts; see [Collector.RouteWarnings].
	warn func(Issue)
}

// RouteWarnings sends every issue below failure severity that reaches c,
// collected or merged, to fn instead of storing or counting it, so c holds and
// counts failures alone. fn runs outside c's lock, on the goroutine that
// collects or merges, in the order the issues arrive; a nil fn discards them.
// A routed merge panics on a Result that dropped an issue the merge would hand
// fn: one below failure severity after [Collector.MergeRetag]'s retag, of a code
// the merge keeps. Only a collector that is a warning's last stop may cap it.
func (c *Collector) RouteWarnings(fn func(Issue)) {
	if fn == nil {
		fn = func(Issue) {}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.warn = fn
}

// route returns c's warning route, or nil when c routes nothing.
func (c *Collector) route() func(Issue) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.warn
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
// A limit of 0 or less means no limit (use [NoLimit] constant for clarity).
// Once the limit is reached the collector retains
// the most severe issues collected into it — evicting a stored issue for a more
// severe incoming one — and counts the rest as dropped, queryable via
// [Result.DroppedCount]. See [Collector.storeLocked] for the retention rule.
func NewCollector(limit int) *Collector {
	return &Collector{
		limit: limit,
	}
}

// Collect adds an issue to the collector.
//
// This method is thread-safe. Once the limit is reached, the issue takes the
// slot of a less severe stored issue, which is dropped, or is dropped itself;
// either drop is counted (see [NewCollector]).
//
// Collect panics if the issue is a zero value or is invalid. Use [NewIssue]
// and [IssueBuilder] to construct valid issues. This panic behavior catches
// programmer errors where issues are constructed via direct struct literals
// rather than the builder pattern.
func (c *Collector) Collect(issue Issue) {
	c.validateIssue("Collect", issue)
	c.mu.Lock()
	if warn := c.warn; warn != nil && !issue.Severity().isFailure() {
		c.mu.Unlock()
		warn(issue)
		return
	}
	defer c.mu.Unlock()

	c.collectLocked(issue)
}

// CollectAll collects each of issues in order, as [Collector.Collect] would.
// Without a route ([Collector.RouteWarnings]) it takes the lock once for all of
// them; under a route it collects each one as Collect does, so a failure is
// stored before the warning after it is delivered.
//
// Panics if any issue is invalid (see [Collector.Collect]).
func (c *Collector) CollectAll(issues []Issue) {
	// Validate all issues before acquiring lock
	for _, issue := range issues {
		c.validateIssue("CollectAll", issue)
	}
	if warn := c.route(); warn != nil {
		// Each issue keeps its place in the order: a failure is stored before
		// the warning after it is delivered.
		for _, issue := range issues {
			c.Collect(issue)
		}
		return
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
// Valid Results can only be obtained via [Collector.Result] or package APIs
// that use Collector internally. Therefore, Merge does not
// re-validate issues.
//
// This differs from [Collector.Collect] and [Collector.CollectAll], which actively validate
// each issue because they accept Issue values directly.
//
// A truncated res merges losslessly in aggregate: its dropped issues cannot be
// re-listed, but their severity contributions and its truncation state
// ([Result.LimitReached] / [Result.DroppedCount]) are carried into the
// receiver. So a dropped error in res cannot leave the receiver's [Collector.Err]
// nil — the same guarantee [Collector] already gives for directly-collected
// issues.
//
// The receiver keeps its own limit, which governs only what it stores. A cap
// is a collector's setting, so a Result reports none: [Result.LimitReached]
// and [Result.DroppedCount] are the truncation facts, and they hold across a
// merge.
func (c *Collector) Merge(res Result) {
	c.fold("Merge", res.SeverityCounts(), res.codeCounts, res.droppedCount, res.inArrivalOrder())
}

// MergeFunc is [Collector.Merge] with fn applied to each of res's surviving
// issues before it is stored. The severity counts and truncation facts carry
// over exactly as Merge carries them, so a caller that must re-wrap each issue
// with batch context — an instance index, a relation — keeps res's dropped
// errors and its LimitReached state instead of re-collecting the survivors one
// by one and losing both. fn must return a valid issue; [FromIssue] preserves
// validity.
func (c *Collector) MergeFunc(res Result, fn func(Issue) Issue) {
	// fn runs outside c.mu, so a transform may read or write this collector.
	// The severity counts are folded from res wholesale in fold, so a
	// transform that changed an issue's severity or code would leave the
	// counts describing issues that are no longer stored: only a from-scratch
	// NewIssue can change either, and doing so is a programmer error.
	ordered := res.inArrivalOrder()
	transformed := make([]Issue, len(ordered))
	for i, issue := range ordered {
		out := fn(issue)
		c.validateIssue("MergeFunc", out)
		if out.Severity() != issue.Severity() || out.Code() != issue.Code() {
			panic(fmt.Sprintf("diag.Collector.MergeFunc: fn changed an issue's severity or code (%s %s -> %s %s); rebuild with FromIssue",
				issue.Severity(), issue.Code(), out.Severity(), out.Code()))
		}
		transformed[i] = out
	}
	c.fold("MergeFunc", res.SeverityCounts(), res.codeCounts, res.droppedCount, transformed)
}

// MergeRetag is [Collector.MergeFunc] for a merge that changes what an issue is:
// retag declares, for each severity and code, the severity an issue is stored at
// and whether it is kept, and fn builds each kept issue at that severity. retag
// runs once per severity and code res counted, in (severity, code) order, so the
// counts and truncation facts stay exact when res was truncated. It panics on the
// first kept code declared a severity outside Fatal to Hint, naming the severity
// and code it was asked for, or when fn's issue differs from the declaration; the
// survivors are stored in arrival order.
func (c *Collector) MergeRetag(res Result, retag func(Severity, Code) (Severity, bool), fn func(Issue) Issue) {
	type key struct {
		sev  Severity
		code Code
	}
	type decision struct {
		to   Severity
		keep bool
	}
	decisions := make(map[key]decision)
	var counts SeverityCounts
	var codes codeCounts
	seen := 0
	for _, sev := range slices.Sorted(maps.Keys(res.codeCounts)) {
		byCode := res.codeCounts[sev]
		for _, code := range slices.SortedFunc(maps.Keys(byCode), func(a, b Code) int { return cmp.Compare(a.value, b.value) }) {
			to, keep := retag(sev, code)
			decisions[key{sev, code}] = decision{to: to, keep: keep}
			if !keep {
				continue
			}
			// A code res only counted reaches no issue's checks, so its
			// severity is checked here.
			if to < Fatal || to > Hint {
				panic(fmt.Sprintf("diag.Collector.MergeRetag: retag declared invalid severity %d for %s %s", to, sev, code))
			}
			n := byCode[code]
			counts.addN(to, n)
			codes.addN(to, code, n)
			seen += n
		}
	}
	ordered := res.inArrivalOrder()
	kept := make([]Issue, 0, len(ordered))
	for _, issue := range ordered {
		d := decisions[key{issue.Severity(), issue.Code()}]
		if !d.keep {
			continue
		}
		out := fn(issue)
		c.validateIssue("MergeRetag", out)
		if out.Severity() != d.to || out.Code() != issue.Code() {
			panic(fmt.Sprintf("diag.Collector.MergeRetag: fn built %s %s where retag declared %s %s",
				out.Severity(), out.Code(), d.to, issue.Code()))
		}
		kept = append(kept, out)
	}
	c.fold("MergeRetag", counts, codes, seen-len(kept), kept)
}

// fold folds a merged result's seen counts, its drops and its surviving issues,
// as the caller wants them stored, into c. Under [Collector.RouteWarnings] it
// keeps the failure half alone and delivers each surviving issue below failure
// severity to the route, after the lock is released and in arrival order. It
// panics, naming entry, when the result saw more such issues than it holds,
// because the route cannot be handed an issue the result dropped.
func (c *Collector) fold(entry string, counts SeverityCounts, codes codeCounts, dropped int, issues []Issue) {
	warn := c.route()
	seenBelow := counts.Warnings + counts.Info + counts.Hints
	if warn == nil || seenBelow == 0 {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.foldLocked(counts, codes, dropped, issues)
		return
	}
	var failures, below []Issue
	for _, issue := range issues {
		if issue.Severity().isFailure() {
			failures = append(failures, issue)
			continue
		}
		below = append(below, issue)
	}
	if seenBelow > len(below) {
		panic(fmt.Sprintf("diag.Collector.%s: a routed merge was given a result that saw %d issues below failure severity and holds %d; only the collector a warning reaches last may cap it",
			entry, seenBelow, len(below)))
	}
	if failureCounts := (SeverityCounts{Fatal: counts.Fatal, Errors: counts.Errors}); failureCounts.Fatal+failureCounts.Errors > 0 {
		c.mu.Lock()
		c.foldLocked(failureCounts, codes.failures(), failureCounts.Fatal+failureCounts.Errors-len(failures), failures)
		c.mu.Unlock()
	}
	for _, issue := range below {
		warn(issue)
	}
}

// foldLocked folds a merged result's seen counts and drops into c and stores
// issues, its surviving issues as the caller wants them stored. Caller must hold
// c.mu.
func (c *Collector) foldLocked(counts SeverityCounts, codes codeCounts, dropped int, issues []Issue) {
	c.cachedResult = nil

	// Fold in the seen-severity counts wholesale. A Result's counts reflect every
	// issue it saw — stored and dropped past its own limit — so adding the counts
	// (rather than re-counting only the surviving issues) keeps
	// Err/HasErrors/ErrorCount truthful even when the result was itself truncated
	// and its dropped issues are no longer enumerable.
	c.counts.addCounts(counts)
	c.codeCounts.addCounts(codes)

	// The merged result's drops can never be recovered, so carry them forward.
	if dropped > 0 {
		c.limitReached = true
		c.droppedCount += dropped
	}

	// Store the surviving issues, honoring c's own limit. storeLocked does not
	// re-count (the counts were folded in above); issues past c's limit are
	// dropped and flagged.
	for _, issue := range issues {
		c.storeLocked(issue)
	}
}

// validateIssue panics if the issue is invalid, naming the entry point that
// received it.
func (c *Collector) validateIssue(entry string, issue Issue) {
	if issue.IsZero() {
		panic("diag.Collector." + entry + ": zero-value Issue")
	}
	if !issue.IsValid() {
		panic(fmt.Sprintf("diag.Collector.%s: invalid Issue (code=%s, message=%q)",
			entry, issue.Code().String(), issue.Message()))
	}
}

// collectLocked adds an issue. Caller must hold c.mu.
func (c *Collector) collectLocked(issue Issue) {
	// Invalidate cached result
	c.cachedResult = nil

	// Severity counts reflect every issue SEEN, not just those stored under the
	// limit, so Err/HasErrors/ErrorCount stay truthful when collection is
	// truncated. The loader and completer gate on ErrorCount deltas to decide
	// whether a schema failed; an error dropped at the cap that left these
	// counts untouched would read as success and let a broken schema escape the
	// all-or-nothing contract. [Result.Len] remains the stored count.
	c.counts.add(issue.Severity())
	c.codeCounts.add(issue.Severity(), issue.Code())

	c.storeLocked(issue)
}

// storeLocked appends an issue to the stored slice, or — once the limit is
// reached — retains it in place of a less severe stored issue. Caller must hold
// c.mu and must have already updated the seen-severity counts: storeLocked
// governs storage, not counting, so an issue that is not retained still shows up
// in the severity totals (and thus in Err/HasErrors/ErrorCount).
//
// The retained set is the `limit` most severe issues that reach this function,
// ties broken by arrival order. Truncation that simply dropped whatever arrived after the cap
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
	if !ok || !sev.isMoreSevereThan(victim) {
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
// calls do not affect it. Results are cached until the collector next changes,
// through Collect, CollectAll, Merge, MergeFunc or MergeRetag.
//
// Issues are sorted by location, then by code, for deterministic output:
// span-backed issues first, in [location.Span.Compare] order, then path-only issues
// by source name and path.
func (c *Collector) Result() Result {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedResult != nil {
		return *c.cachedResult
	}

	sorted, arrival := sortStored(c.issues)

	// Carry the collector's SEEN severity counts, not counts of the stored
	// slice: under truncation the dropped issues are absent from sorted, and
	// counting the slice would make Result.SeverityCounts blind to a dropped
	// error.
	result := newResult(sorted, c.limitReached, c.droppedCount, c.counts, c.codeCounts.clone(), arrival)
	c.cachedResult = &result
	return result
}

// Err returns nil when c has counted no failure, dropped ones included, and
// otherwise a *[ResultError] whose Result holds c's failures alone, as [go/scanner.ErrorList.Err] builds
// an error from its list. The Result counts every failure c saw, and its
// [Result.DroppedCount] is the failures seen less the failures stored; no
// issue below failure severity is in it, stored or counted.
func (c *Collector) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	seen := c.counts.Fatal + c.counts.Errors
	if seen == 0 {
		return nil
	}
	stored := make([]storedIssue, 0, c.storedCounts.Fatal+c.storedCounts.Errors)
	for _, st := range c.issues {
		if st.issue.Severity().isFailure() {
			stored = append(stored, st)
		}
	}
	sorted, arrival := sortStored(stored)
	dropped := seen - len(sorted)
	counts := SeverityCounts{Fatal: c.counts.Fatal, Errors: c.counts.Errors}
	return &ResultError{Result: newResult(sorted, dropped > 0, dropped, counts, c.codeCounts.failures(), arrival)}
}

// sortStored returns the issues of stored in [compareIssues] order beside the
// arrival number of each, by which a Merge puts them back in collection order.
// stored itself keeps its order.
func sortStored(stored []storedIssue) ([]Issue, []uint64) {
	ordered := slices.Clone(stored)
	slices.SortFunc(ordered, func(a, b storedIssue) int { return compareIssues(a.issue, b.issue) })
	sorted := make([]Issue, len(ordered))
	arrival := make([]uint64, len(ordered))
	for i, st := range ordered {
		sorted[i], arrival[i] = st.issue, st.arrival
	}
	return sorted, arrival
}

// compareIssues compares two issues for deterministic sorting.
//
// Ordering rules:
//  1. Span-backed issues before path-only issues
//  2. Span-backed: [location.Span.Compare] — source, start and end position,
//     then start and end byte offset
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
		if c := a.span.Compare(b.span); c != 0 {
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
func compareRelated(a, b []RelatedInfo) int {
	for i := range min(len(a), len(b)) {
		if c := a[i].Span.Compare(b[i].Span); c != 0 {
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
