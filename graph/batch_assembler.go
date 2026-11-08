package graph

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// BatchAssembler composes a
// [github.com/simon-lentz/yammm/instance.Validator] and a [Graph] into a single
// call-surface for the common pipeline pattern, one validator serving every
// goroutine:
//
//	validator.ValidateOne(...) → graph.Add(...)
//
// The end of the batch reads the graph through [BatchAssembler.Graph]: check it
// with [Graph.Check], then take [Graph.Snapshot]. An add can reach the graph
// until the caller stops making them, so the coordinator waits for its workers
// before the check.
//
// A record that fails validation or the add is surfaced as a contextual
// error tagged with its type and the call's attempt ordinal, or, for a zero
// instance, "zero-instance" and the ordinal; a record whose one fault is an
// [instance.Refused] value, as an error that matches
// [instance.ErrRefusedValue] under the same tag.
//
// # Thread safety
//
// BatchAssembler is safe for concurrent use: Add, AddValid, AddValidOrMerge,
// Count and Graph may all be called from multiple goroutines against the same
// instance, with no external mutex at call sites. An internal sync.Mutex covers
// ValidateOne, Graph.Add and the success-counter increment as one atomic step.
//
// BatchAssembler is a convenience primitive. Consumers who need
// finer-grained control (per-record error handling, mid-batch
// validator switching, custom diagnostic aggregation) should use the
// underlying Validator + Graph primitives directly.
type BatchAssembler struct {
	ctx    context.Context //nolint:containedctx // captured at construction for ValidateOne / Graph.Add calls
	schema schema.Schema
	graph  *Graph

	validator *instance.Validator

	// addMu serializes ValidateOne + Graph.Add + counter ops.
	addMu sync.Mutex

	// attempts increments on every Add / AddValid / AddValidOrMerge call,
	// whatever its outcome, and labels per-record errors with a 1-indexed attempt
	// ordinal. successes increments only when a call adds an instance, never on a
	// merge, and is what Count() returns.
	attempts  atomic.Int64
	successes atomic.Int64
}

// NewBatchAssembler constructs an assembler bound to schema s.
//
// The assembler's underlying graph starts empty. To resume assembly on
// top of a previously-persisted snapshot, use
// [NewBatchAssemblerFromSnapshot].
//
// The supplied ctx is captured and used for the assembler's internal
// ValidateOne and Graph.Add calls. Cancelling ctx cancels in-flight
// validation; Add then returns a *diag.ContextualError whose result holds
// Fatal E_CONTEXT_CANCELLED, so match the code rather than context.Canceled.
//
// Panics if s is zero or ctx is nil (consistent with [graph.New] and
// [instance.NewValidator]).
func NewBatchAssembler(ctx context.Context, s schema.Schema) *BatchAssembler {
	if s.IsZero() {
		panic("graph.NewBatchAssembler: zero schema")
	}
	if ctx == nil {
		panic("graph.NewBatchAssembler: nil context")
	}

	return newBatchAssembler(ctx, s, New(s))
}

// NewBatchAssemblerFromSnapshot constructs an assembler bound to schema s
// whose underlying graph starts pre-populated from snap, rather than
// empty as with [NewBatchAssembler]. It is the assembler-level
// counterpart to [NewFromSnapshot], for resume consumers: persist a
// batch, load the resulting snapshot on a later run, seed a new
// assembler from it, and Add only the records still outstanding.
//
// Seeding follows [NewFromSnapshot] semantics. All instances, resolved
// edges and duplicate records are imported. A snapshot bound to s brings
// its unresolved edge records as they stand; one bound to another schema
// has them derived again under s, as [NewFromSnapshot] states. Subsequent
// Add / AddValid calls interact with the imported state exactly as if it
// had been assembled in this batch:
//
//   - A new instance may resolve an unresolved edge imported from snap,
//     and new forward references resolve against seeded instances.
//   - A new instance whose (type, primary key) collides with a seeded
//     instance is rejected as a duplicate (E_DUPLICATE_PK) by Add and
//     AddValid; AddValidOrMerge merges it into the seeded instance, or
//     refuses it, by the package doc's Merge on a Duplicate Key section.
//   - [Graph.Check] covers seeded and new state alike: a required
//     association imported from snap and still unresolved reports
//     E_UNRESOLVED_REQUIRED.
//
// Two accounting consequences of seeding:
//
//   - [BatchAssembler.Count] counts only records added through this
//     assembler; seeded instances are not counted.
//   - Construction diagnostics are not carried over from snap — a later
//     snapshot's Diagnostics() reflects only this assembler's
//     Add / AddValid / AddValidOrMerge calls and any add through
//     [BatchAssembler.Graph]. Snapshots loaded from .ys
//     files carry no construction diagnostics in the first place (diagnostics are
//     transient by design). A document persists the instances, the edges, and
//     the Duplicates and Unresolved records, and the seeding imports those.
//
// snap is held to s as [NewFromSnapshot] holds it, under the package doc's
// "Structural facts": a snapshot [NewFromSnapshot] refuses draws a nil assembler
// and one diagnostic for each break, naming a position, save a break that reads a
// unit another diagnostic refused. A snapshot taken from a
// [Graph] bound to s, or loaded via
// [github.com/simon-lentz/yammm/snapshot.Load] against s, is never refused.
//
// Every other contract matches [NewBatchAssembler].
//
// Panics if s or snap is zero or ctx is nil (consistent with [NewBatchAssembler]
// and [NewFromSnapshot]).
func NewBatchAssemblerFromSnapshot(ctx context.Context, s schema.Schema, snap Snapshot) (*BatchAssembler, diag.Result) {
	if s.IsZero() {
		panic("graph.NewBatchAssemblerFromSnapshot: zero schema")
	}
	if ctx == nil {
		panic("graph.NewBatchAssemblerFromSnapshot: nil context")
	}
	if snap.IsZero() {
		panic("graph.NewBatchAssemblerFromSnapshot: zero Snapshot")
	}

	g, res := NewFromSnapshot(s, snap)
	if res.HasErrors() {
		return nil, res
	}
	return newBatchAssembler(ctx, s, g), res
}

// newBatchAssembler wires a single shared validator around an
// already-constructed graph. Both exported constructors funnel here; they
// differ only in how the graph is built (empty via [New], or seeded via
// [NewFromSnapshot]).
func newBatchAssembler(ctx context.Context, s schema.Schema, g *Graph) *BatchAssembler {
	ba := &BatchAssembler{
		ctx:    ctx,
		schema: s,
		graph:  g,
	}

	ba.validator = instance.NewValidator(s)

	return ba
}

// Add validates raw against typeName and adds the resulting instance
// to the underlying Graph. Safe for concurrent use from multiple
// goroutines; validator calls are serialized internally via mutex.
//
// Returns an error annotated with typeName and the current call index
// if validation or add fails; the returned error's cause is a
// [*diag.ContextualError] suitable for errors.As extraction. A raw instance
// whose one fault is an [instance.Refused] value returns one that matches
// [instance.ErrRefusedValue] instead, and adds no diagnostic: its producer
// reported the value.
//
// Add does not call Graph.Check or Graph.Snapshot.
func (ba *BatchAssembler) Add(typeName string, raw instance.RawInstance) error {
	attemptN := ba.attempts.Add(1)

	ba.addMu.Lock()
	defer ba.addMu.Unlock()

	valid, vRes := ba.validator.ValidateOne(ba.ctx, typeName, raw)
	// A caller that discards per-record errors reads a validation failure
	// from Snapshot.Diagnostics(), as it reads a Graph.Add refusal; a refused
	// value's issue is in its producer's result alone.
	ba.graph.collector.Merge(vRes)
	if vRes.HasErrors() {
		return ba.wrapAddError(typeName, attemptN, vRes)
	}
	// The raw instance holds an [instance.Refused] value, which its producer
	// reported: no issue is owed here, and nothing reaches the graph.
	if valid.IsZero() {
		return fmt.Errorf("%s (attempt #%d): %w", typeName, attemptN, instance.ErrRefusedValue)
	}
	addRes := ba.graph.Add(ba.ctx, valid)
	if addRes.HasErrors() {
		return ba.wrapAddError(typeName, attemptN, addRes)
	}
	ba.successes.Add(1)
	return nil
}

// AddValid adds a validated instance directly to the Graph, with no validation
// pass of its own, and is safe for concurrent use. It counts in
// [BatchAssembler.Count] as [BatchAssembler.Add] does. It panics on an instance
// validated under another schema load, as [Graph.Add] does.
func (ba *BatchAssembler) AddValid(valid instance.ValidInstance) error {
	_, err := ba.addValid(valid, false)
	return err
}

// AddValidOrMerge is [BatchAssembler.AddValid] that merges, and reports whether
// it merged: a pre-validated instance whose type and primary key the graph
// already holds, and which carries no composed children, merges its association
// records into the held instance and keeps nothing else of itself. A merge
// records no [Duplicate], raises no error and adds nothing to
// [BatchAssembler.Count], and one that would give a (one) association a second
// target is refused with an E_GRAPH_CARDINALITY for each such association.
// merged is false whenever err is not nil. It panics on an instance validated
// under another schema load, as [Graph.Add] does, and the package doc's Merge on a
// Duplicate Key section states the rules.
func (ba *BatchAssembler) AddValidOrMerge(valid instance.ValidInstance) (merged bool, err error) {
	return ba.addValid(valid, true)
}

// addValid is AddValid, and with merge set AddValidOrMerge.
func (ba *BatchAssembler) addValid(valid instance.ValidInstance, merge bool) (bool, error) {
	attemptN := ba.attempts.Add(1)

	if valid.IsZero() {
		// E_INTERNAL, not E_GRAPH_INVALID_PK: the record is absent, not
		// mis-keyed, and a consumer routing remediation off the code would be
		// sent to inspect a primary key that does not exist.
		method := "AddValid"
		if merge {
			method = "AddValidOrMerge"
		}
		issue := diag.NewIssue(diag.Error, diag.E_INTERNAL,
			method+" received a zero instance; a validator returns the zero ValidInstance for an instance that failed validation or holds a refused value").Build()
		collector := diag.NewCollector(0)
		collector.Collect(issue)
		// Every other Add rejection but a refused value's reaches
		// Snapshot.Diagnostics(); this one has to as well, or a caller that
		// discards per-record errors — the pattern docs/API.md accommodates —
		// has no record of the loss at all. The assembler cannot tell a zero entry a
		// refused value left from any other.
		ba.graph.collector.Collect(issue)
		return false, ba.wrapZeroInstanceError(attemptN, collector.Result())
	}

	// AddValid does not need validator access, but the success-counter
	// increment must stay coherent with concurrent Adds, so it takes the
	// same addMu to keep the increment + Graph.Add atomic.
	ba.addMu.Lock()
	defer ba.addMu.Unlock()

	merged, addRes := ba.graph.add(ba.ctx, valid, merge)
	if addRes.HasErrors() {
		return false, ba.wrapAddError(ba.graph.tagForm(valid.Type().ID()), attemptN, addRes)
	}
	if !merged {
		ba.successes.Add(1)
	}
	return merged, nil
}

// wrapZeroInstanceError tags a zero-instance rejection. There is no type name to
// substitute, and an empty one would render " (attempt #N)" with a leading
// space — breaking the single-token shape docs/API.md states.
func (ba *BatchAssembler) wrapZeroInstanceError(attemptN int64, res diag.Result) error {
	//nolint:wrapcheck // returning *diag.ContextualError directly is the documented contract; consumers errors.As against it
	return res.WithContext(fmt.Sprintf("zero-instance (attempt #%d)", attemptN))
}

// wrapAddError formats an Add / AddValid / AddValidOrMerge failure as a
// *diag.ContextualError tagged with the type name and the assembler-wide
// attempt ordinal. The ordinal counts calls across every goroutine sharing the assembler, so it
// identifies the call and not the caller's input row.
func (ba *BatchAssembler) wrapAddError(typeName string, attemptN int64, res diag.Result) error {
	tag := fmt.Sprintf("%s (attempt #%d)", typeName, attemptN)
	//nolint:wrapcheck // returning *diag.ContextualError directly is the documented contract; consumers errors.As against it
	return res.WithContext(tag)
}

// Count returns the number of records successfully added so far.
// Useful for progress reporting in long-running batches.
//
// Counts successful Add and AddValid calls equally, and an
// AddValidOrMerge call that added an instance; one that merged into a held
// instance, and failed records (validation errors, duplicate PKs, etc.), are
// not counted. Instances
// seeded at construction by [NewBatchAssemblerFromSnapshot] are not
// counted either — Count reflects only records added through this
// assembler. Safe to call concurrently with in-flight Add operations —
// the returned value is a point-in-time read of an atomic counter.
func (ba *BatchAssembler) Count() int {
	return int(ba.successes.Load())
}

// Graph returns the underlying [Graph], which the end of the batch checks with
// [Graph.Check] and reads with [Graph.Snapshot].
//
// Graph does not serialize against in-flight Add calls: the returned *Graph is
// concurrent-safe by its own contract, and a read reflects the instances
// committed at the time of the read.
func (ba *BatchAssembler) Graph() *Graph {
	return ba.graph
}
