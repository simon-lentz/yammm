package graph

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// ErrAssemblerFinalized is returned by [BatchAssembler.Add] and
// [BatchAssembler.AddValid] when called after [BatchAssembler.Finalize].
//
// Consumers performing retry / cleanup logic check via errors.Is:
//
//	if err := ba.Add(typeName, raw); err != nil {
//	    if errors.Is(err, graph.ErrAssemblerFinalized) {
//	        // batch is complete; stop submitting
//	    }
//	    ...
//	}
var ErrAssemblerFinalized = errors.New("graph: BatchAssembler.Add called after Finalize")

// BatchAssembler composes a [Validator] and a [Graph] into a single
// call-surface for the common pipeline pattern:
//
//	validator.ValidateOne(...) → graph.Add(...) → graph.Check(...) → graph.Snapshot()
//
// The assembler encodes the ordering invariant (validate before add,
// check before snapshot) so consumers cannot get the sequence wrong.
// Diagnostics from each stage are surfaced as contextual errors,
// tagged with the record's type and index for locatability.
//
// # Thread safety
//
// BatchAssembler is safe for concurrent use: Add, AddValid, Count, Graph,
// and Finalize may all be called from multiple goroutines against the same
// instance. The assembler supports two validator-access modes, selected at
// construction time via options:
//
//   - Default mode (mutex-serialized). A single shared Validator is
//     serialized through an internal sync.Mutex. The lock covers
//     ValidateOne, Graph.Add, and the success-counter increment as one
//     atomic step. Appropriate for I/O-bound workloads where validation
//     is a tiny fraction of per-record wall-clock — the default;
//     suitable for rdata's pipelines and similar scrape-or-fetch-
//     dominated consumers.
//
//   - Pool mode (opt-in via [WithValidatorPool]). N pre-constructed
//     Validators are distributed through an internal buffered
//     chan *instance.Validator, matching the "goroutine-per-CPU-core"
//     shape. Each Add claims a validator, runs ValidateOne outside any
//     shared lock, returns the validator to the pool, then performs
//     Graph.Add (Graph is concurrent-safe by its own contract) and
//     increments the success counter via sync/atomic. When the pool is
//     exhausted (all N validators are claimed), subsequent Add calls
//     block on the pool's return channel until a validator is freed —
//     intentional back-pressure matching a bounded goroutine pool.
//     Appropriate for CPU-bound workloads where validation is material
//     relative to per-record wall-clock.
//
// Finalize is orthogonal to the validator-access mode: it sets a
// finalized flag, waits for every in-flight Add to commit (via an
// internal sync.WaitGroup), then runs Check + Snapshot. Concurrent
// Add calls that arrive during Finalize either complete before
// Finalize observes them (if they had already incremented the
// in-flight counter and committed to Graph.Add) or fail with
// [ErrAssemblerFinalized] (the one-shot-consumed contract).
//
// The concurrent-safety contract specifically supports the worker-pool
// pattern where one assembler is shared across N scraper goroutines,
// each calling Add for its per-record outputs, with a coordinator
// goroutine calling Finalize at end-of-batch. No external mutex
// required at call sites.
//
// # Mode-selection guidance
//
// Default mode for I/O-bound consumers. WithValidatorPool(runtime.NumCPU())
// for CPU-bound consumers where profiling shows validation as a material
// fraction of per-record wall-clock. Passing WithValidatorPool(1) is
// semantically equivalent to the default but adds channel-send/receive
// overhead per Add — prefer the default in that case.
//
// BatchAssembler is a convenience primitive. Consumers who need
// finer-grained control (per-record error handling, mid-batch
// validator switching, custom diagnostic aggregation) should use the
// underlying Validator + Graph primitives directly.
type BatchAssembler struct {
	ctx    context.Context //nolint:containedctx // captured at construction for ValidateOne / Graph.Add calls
	schema *schema.Schema
	graph  *Graph

	// Validator access — exactly one of these is populated based on mode.
	// Default mode populates `validator`; pool mode populates `pool`.
	validator *instance.Validator      // default mode
	pool      chan *instance.Validator // pool mode (cap == n, pre-filled)

	// addMu serializes ValidateOne + Graph.Add + counter ops in default
	// mode. Unused in pool mode (validators are claimed via channel
	// receive instead).
	addMu sync.Mutex

	// lifecycleMu coordinates Add lifecycle against Finalize.
	//
	//   - Add / AddValid acquire RLock for the entire duration of one
	//     Add (including the post-claim finalized re-check, ValidateOne,
	//     Graph.Add, and counter increment). Concurrent Adds run in
	//     parallel; the RLock is the cheap-on-the-fast-path barrier.
	//   - Finalize sets the finalized flag, then acquires the write
	//     lock — which by Go's RWMutex semantics waits for every
	//     outstanding RLock to release before granting. After the write
	//     lock is held, no in-flight Add can be in progress, and no
	//     new Add can acquire its RLock until Finalize releases.
	//
	// This pattern replaces the earlier sync.WaitGroup design, which
	// races when the WaitGroup counter is zero and an Add(1) call
	// concurrently happens with a Wait() call (Go's race detector
	// flags this as a programmer error even when functionally
	// well-ordered). The RWMutex carries the same INV-1 / INV-2
	// guarantees without the WaitGroup race window.
	lifecycleMu sync.RWMutex

	// finalized is the one-shot finalization flag. Once true, subsequent
	// Add / AddValid calls return ErrAssemblerFinalized. Read inside
	// the lifecycle RLock so the load happens-before any concurrent
	// Finalize's Lock acquisition.
	finalized atomic.Bool

	// Counters. attempts increments at the top of every Add / AddValid
	// call (regardless of outcome) and is used to label per-record errors
	// with a 1-indexed call number. successes increments only on
	// successful completion and is what Count() returns.
	attempts  atomic.Int64
	successes atomic.Int64
}

// FinalizeResult is the structured return value from
// [BatchAssembler.Finalize]. It carries the finalized graph snapshot;
// diagnostics are reached via Snapshot.Diagnostics() at the call site.
//
// Snapshot is always non-nil. This is the load-bearing contract the
// struct exists to encode: callers read res.Snapshot directly without
// gating on err == nil, because the graph state at the point of
// finalization is useful for diagnostics on either outcome path
// (operators logging a failed batch want to see which instances were
// assembled before the check tripped, not just the check's error
// message).
//
// Every diag.Issue produced during Add, Check, and snapshot construction
// is reachable via res.Snapshot.Diagnostics() — the Snapshot surface is
// the single authoritative source.
type FinalizeResult struct {
	// Snapshot is the graph state at the point of finalization.
	// Always non-nil. On success, this is the completed snapshot ready
	// for downstream use (marshaling, verification, logging). On
	// failure, this is the partial snapshot at the point Graph.Check
	// tripped — all instances successfully added before the check
	// failure are present and inspectable.
	Snapshot *Snapshot
}

// BatchAssemblerOption configures the assembler at construction time.
type BatchAssemblerOption func(*batchAssemblerConfig)

// batchAssemblerConfig holds internal configuration for a BatchAssembler.
type batchAssemblerConfig struct {
	validatorOpts []instance.Option
	graphOpts     []Option
	poolSize      int // 0 == default mutex mode; n>=1 == pool mode
}

// WithValidatorOptions forwards [instance.Option] values to the underlying
// Validator. Use [instance.RecommendedOptions] for the standard preset.
func WithValidatorOptions(opts ...instance.Option) BatchAssemblerOption {
	return func(c *batchAssemblerConfig) {
		c.validatorOpts = append(c.validatorOpts, opts...)
	}
}

// WithGraphOptions forwards [Option] values to the underlying Graph.
func WithGraphOptions(opts ...Option) BatchAssemblerOption {
	return func(c *batchAssemblerConfig) {
		c.graphOpts = append(c.graphOpts, opts...)
	}
}

// WithValidatorPool configures the assembler to use a pool of n
// pre-constructed Validators distributed via an internal buffered
// chan *instance.Validator, replacing the default mutex-serialized
// single-validator shape. Each Add claims a validator from the pool
// for the duration of its ValidateOne call and returns it on
// completion; concurrent Adds proceed in parallel up to the pool size,
// after which they block on a channel receive until a validator is
// freed.
//
// Use this for CPU-bound workloads where validation cost is material
// relative to per-record wall-clock. For I/O-bound consumers (rdata's
// pipelines are the canonical example), the default mutex-serialized
// path is preferable: validation is a tiny fraction of per-record
// wall-clock, and passing this option adds memory overhead (n
// validator instances) without measurable throughput gain.
//
// # Sizing guidance
//
// n = runtime.NumCPU() matches the "goroutine-per-CPU-core" shape and
// is the suggested starting point for pool mode. Values below
// NumCPU() under-use available parallelism; values above NumCPU()
// add memory overhead without throughput gain (the extra validators
// contend for the same CPUs). n must be >= 1; the constructor panics
// on n <= 0 since that is a programmer error, not a runtime
// condition.
//
// # Semantics when n == 1
//
// Equivalent to the default mutex-serialized mode but adds
// channel-send/receive overhead per Add. Prefer the default in that
// case — the option exists for n > 1 use cases.
//
// Pool mode changes the internal concurrency shape but preserves
// every external contract (error shapes, Finalize one-shot semantics,
// success-count monotonicity). Switching a call site between default
// and pool mode is a one-option change with no ripple into caller
// code.
func WithValidatorPool(n int) BatchAssemblerOption {
	if n <= 0 {
		panic(fmt.Sprintf("graph.WithValidatorPool: pool size n must be >= 1, got %d", n))
	}
	return func(c *batchAssemblerConfig) {
		c.poolSize = n
	}
}

// NewBatchAssembler constructs an assembler bound to schema s.
//
// The supplied ctx is captured and used for the assembler's internal
// ValidateOne and Graph.Add calls. Cancelling ctx cancels in-flight
// validation; Add returns the cancellation error directly. Finalize
// takes its own ctx parameter independent of the construction-time
// context.
//
// Panics if s is nil (consistent with [graph.New] and
// [instance.NewValidator]).
func NewBatchAssembler(ctx context.Context, s *schema.Schema, opts ...BatchAssemblerOption) *BatchAssembler {
	if s == nil {
		panic("graph.NewBatchAssembler: nil schema")
	}
	if ctx == nil {
		panic("graph.NewBatchAssembler: nil context")
	}

	cfg := batchAssemblerConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	ba := &BatchAssembler{
		ctx:    ctx,
		schema: s,
		graph:  New(s, cfg.graphOpts...),
	}

	if cfg.poolSize == 0 {
		ba.validator = instance.NewValidator(s, cfg.validatorOpts...)
	} else {
		ba.pool = make(chan *instance.Validator, cfg.poolSize)
		for range cfg.poolSize {
			ba.pool <- instance.NewValidator(s, cfg.validatorOpts...)
		}
	}

	return ba
}

// Add validates raw against typeName and adds the resulting instance
// to the underlying Graph. Safe for concurrent use from multiple
// goroutines; in default mode validator calls are serialized
// internally via mutex, in pool mode via a bounded validator pool.
//
// Returns an error annotated with typeName and the current call index
// if validation or add fails; the returned error's cause is a
// [*diag.ErrorWithContext] suitable for errors.As extraction.
//
// Returns [ErrAssemblerFinalized] (matchable via errors.Is) when called
// after [BatchAssembler.Finalize].
//
// Add does not call Graph.Check or Graph.Snapshot — those happen in
// Finalize.
func (ba *BatchAssembler) Add(typeName string, raw instance.RawInstance) error {
	// INV-1 + INV-2: take the lifecycle RLock for the entire duration
	// of this Add. Finalize's Lock() will not be granted until this
	// RUnlock fires (deferred), so every Add committed past this line
	// happens-before Finalize's snapshot. A late Add that arrives after
	// Finalize has already taken the write lock blocks on RLock until
	// Finalize releases, then re-checks `finalized` and returns the
	// post-finalize error.
	ba.lifecycleMu.RLock()
	defer ba.lifecycleMu.RUnlock()

	if ba.finalized.Load() {
		return ErrAssemblerFinalized
	}

	attemptN := ba.attempts.Add(1)

	if ba.pool != nil {
		return ba.addPooled(typeName, raw, attemptN)
	}
	return ba.addSerial(typeName, raw, attemptN)
}

// addSerial handles the default-mode (mutex-serialized) Add path.
func (ba *BatchAssembler) addSerial(typeName string, raw instance.RawInstance, attemptN int64) error {
	ba.addMu.Lock()
	defer ba.addMu.Unlock()

	// Optimistic re-check: if Finalize set the flag while we were
	// blocked on addMu, bail early. Not strictly required for
	// correctness (the lifecycle RLock already orders our completion
	// before Finalize's snapshot), but avoids wasted validate +
	// Graph.Add work when the caller has already moved on.
	if ba.finalized.Load() {
		return ErrAssemblerFinalized
	}

	valid, vRes := ba.validator.ValidateOne(ba.ctx, typeName, raw)
	if vRes.HasErrors() {
		return ba.wrapAddError(typeName, attemptN, vRes)
	}
	addRes := ba.graph.Add(ba.ctx, valid)
	if addRes.HasErrors() {
		return ba.wrapAddError(typeName, attemptN, addRes)
	}
	ba.successes.Add(1)
	return nil
}

// addPooled handles the pool-mode Add path. Claims a validator from
// the bounded pool, runs ValidateOne outside any shared lock, returns
// the validator, then performs Graph.Add (which is concurrent-safe by
// Graph's own contract) and increments the success counter atomically.
func (ba *BatchAssembler) addPooled(typeName string, raw instance.RawInstance, attemptN int64) error {
	var v *instance.Validator
	select {
	case v = <-ba.pool:
	case <-ba.ctx.Done():
		return ba.ctx.Err() //nolint:wrapcheck // ctx cancellation propagates verbatim
	}
	defer func() { ba.pool <- v }()

	// Optimistic re-check: if Finalize set the flag while we were
	// blocked on the pool channel, bail early. Same reasoning as
	// addSerial's mid-mutex re-check.
	if ba.finalized.Load() {
		return ErrAssemblerFinalized
	}

	valid, vRes := v.ValidateOne(ba.ctx, typeName, raw)
	if vRes.HasErrors() {
		return ba.wrapAddError(typeName, attemptN, vRes)
	}
	addRes := ba.graph.Add(ba.ctx, valid)
	if addRes.HasErrors() {
		return ba.wrapAddError(typeName, attemptN, addRes)
	}
	ba.successes.Add(1)
	return nil
}

// AddValid adds a pre-validated instance directly to the Graph,
// bypassing the validator. Useful when the caller has its own
// validator or is re-adding instances from a prior snapshot. Safe
// for concurrent use from multiple goroutines.
//
// AddValid still respects the post-finalize contract and the in-flight
// tracking, and increments both attempts and successes counters so
// Count() reflects the total successful additions across both Add and
// AddValid call paths.
//
// Returns [ErrAssemblerFinalized] (matchable via errors.Is) when called
// after [BatchAssembler.Finalize].
func (ba *BatchAssembler) AddValid(valid *instance.ValidInstance) error {
	// Same lifecycle-RLock pattern as Add — see Add's Godoc and the
	// lifecycleMu field comment for the INV-1 / INV-2 reasoning.
	ba.lifecycleMu.RLock()
	defer ba.lifecycleMu.RUnlock()

	if ba.finalized.Load() {
		return ErrAssemblerFinalized
	}

	attemptN := ba.attempts.Add(1)

	// AddValid does not need validator access, but the success-counter
	// increment must still be coherent with concurrent default-mode
	// Adds. Take the addMu in default mode to keep the increment +
	// Graph.Add atomic; pool mode relies on Graph's own concurrency
	// guarantee and atomic counter.
	if ba.pool == nil {
		ba.addMu.Lock()
		defer ba.addMu.Unlock()
	}

	addRes := ba.graph.Add(ba.ctx, valid)
	if addRes.HasErrors() {
		typeName := ""
		if valid != nil {
			typeName = valid.TypeName()
		}
		return ba.wrapAddError(typeName, attemptN, addRes)
	}
	ba.successes.Add(1)
	return nil
}

// wrapAddError formats an Add / AddValid failure as a *diag.ErrorWithContext
// tagged with the type name and the 1-indexed call number, so the offending
// record is locatable from the error alone.
func (ba *BatchAssembler) wrapAddError(typeName string, attemptN int64, res diag.Result) error {
	tag := fmt.Sprintf("%s (record #%d)", typeName, attemptN)
	//nolint:wrapcheck // returning *diag.ErrorWithContext directly is the documented contract; consumers errors.As against it
	return res.WithContext(tag).Err()
}

// Finalize runs Graph.Check, takes a Snapshot, and returns both via
// a [FinalizeResult] struct plus a paired error.
//
// Return-value contract:
//
//   - On success (no error-severity diagnostics): returns
//     (FinalizeResult{Snapshot: snap}, nil). Warning-severity
//     diagnostics surface on res.Snapshot.Diagnostics(); they do not
//     block the batch.
//   - On failure (error-severity diagnostics from Graph.Check): returns
//     (FinalizeResult{Snapshot: snap}, err) where res.Snapshot is
//     always non-nil (the partial snapshot at the point of failure)
//     and err is a [*diag.ErrorWithContext] tagged "batch_finalize"
//     whose Result matches res.Snapshot.Diagnostics() for the
//     error-severity issues from Check.
//
// The [FinalizeResult] struct makes the "Snapshot is always non-nil"
// contract explicit in the type system. Callers check err alone for
// success/failure:
//
//	res, err := ba.Finalize(ctx)
//	if err != nil { return fmt.Errorf("batch: %w", err) }
//	// res.Snapshot is always non-nil; no gating needed.
//	qualityCollector.Merge(res.Snapshot.Diagnostics())
//
// The "batch_finalize" tag is fixed (not configurable via options) so
// downstream log consumers have a stable signal to filter on; callers
// who want richer context wrap further via fmt.Errorf("%s: %w", ...).
//
// After Finalize, the assembler is effectively consumed; subsequent
// Add / AddValid calls return [ErrAssemblerFinalized]. Calling
// Finalize twice is supported but the second call returns the same
// FinalizeResult with no second Check pass — the snapshot is
// re-taken from the same underlying graph state.
//
// Panics if ctx is nil (consistent with [Graph.Check]).
func (ba *BatchAssembler) Finalize(ctx context.Context) (FinalizeResult, error) {
	if ctx == nil {
		panic("graph.BatchAssembler.Finalize: nil context")
	}

	// INV-2: flip the finalized flag first so any Add still in its
	// pre-RLock window observes the new value and returns the
	// post-finalize error after acquiring its RLock. The store is
	// loaded inside the RLock by every concurrent Add (see Add /
	// AddValid).
	ba.finalized.Store(true)

	// INV-1: take the lifecycle write lock. Go's RWMutex semantics
	// guarantee this call blocks until every outstanding RLock is
	// released — i.e., every in-flight Add has finished its
	// Graph.Add and counter increment. After this point no in-flight
	// Add can be in progress, and no new Add can acquire its RLock
	// until we release. This is the barrier that makes "every Add
	// that returned nil occurred before Finalize's snapshot" hold
	// across both default and pool modes.
	ba.lifecycleMu.Lock()
	defer ba.lifecycleMu.Unlock()

	checkRes := ba.graph.Check(ctx)
	snap := ba.graph.Snapshot()
	res := FinalizeResult{Snapshot: snap} // always non-nil

	if checkRes.HasErrors() {
		//nolint:wrapcheck // returning *diag.ErrorWithContext directly is the documented contract; consumers errors.As against it
		return res, checkRes.WithContext("batch_finalize").Err()
	}
	return res, nil
}

// Count returns the number of records successfully added so far.
// Useful for progress reporting in long-running batches.
//
// Counts successful Add and AddValid calls equally; failed records
// (validation errors, duplicate PKs, etc.) are not counted. Safe to
// call concurrently with in-flight Add operations — the returned
// value is a point-in-time read of an atomic counter.
func (ba *BatchAssembler) Count() int {
	return int(ba.successes.Load())
}

// Graph exposes the underlying [Graph] for advanced use cases
// (custom traversal, direct inspection).
//
// Prefer NOT to call this. The returned *Graph shares lifecycle with
// the assembler; calling Graph.Check or Graph.Snapshot directly
// bypasses the assembler's Finalize invariant and is not supported —
// Finalize's "check before snapshot" ordering is the invariant the
// BatchAssembler exists to enforce. Reading instance state mid-batch
// (e.g., for progress diagnostics or partial inspection) is the
// intended use; anything that would mutate the assembly sequence
// should go through Add / AddValid / Finalize. If you find yourself
// reaching for Graph() to drive the final Check + Snapshot, use the
// underlying Validator + Graph primitives directly instead — that's
// the supported escape hatch, not this accessor.
//
// Graph() does NOT serialize against in-flight Add calls; the
// returned *Graph is concurrent-safe by its own contract, and reads
// against it (e.g., Snapshot()) reflect whatever instances have been
// committed at the time of the read.
func (ba *BatchAssembler) Graph() *Graph {
	return ba.graph
}
