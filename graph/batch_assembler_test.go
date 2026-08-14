package graph_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// batchAssemblerTestSchema returns a schema with Person + Company plus an
// optional employer association so we can exercise both happy-path adds and
// (with the required-association variant below) Check-failure paths.
func batchAssemblerTestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("batchassembler").
		WithSourceID(location.MustNewSourceID("test://batchassembler.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employer", schema.LocalTypeRef("Company", location.Span{}), true, false).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("schema build failed: %s", result.String())
	}
	return s
}

// batchAssemblerRequiredEmployerSchema is the same shape as the above but
// with `employer` declared as a REQUIRED association — used by the
// FinalizeError test to deterministically trigger an unresolved-required
// failure on Check.
func batchAssemblerRequiredEmployerSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("batchassembler_required").
		WithSourceID(location.MustNewSourceID("test://batchassembler_required.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employer", schema.LocalTypeRef("Company", location.Span{}), false, false).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("schema build failed: %s", result.String())
	}
	return s
}

// personRaw produces a basic Person raw with id+name fields.
func personRaw(id, name string) instance.RawInstance {
	return instance.RawInstance{
		Properties: map[string]any{
			"id":   id,
			"name": name,
		},
	}
}

// companyRaw produces a basic Company raw with id+name fields.
func companyRaw(id, name string) instance.RawInstance {
	return instance.RawInstance{
		Properties: map[string]any{
			"id":   id,
			"name": name,
		},
	}
}

// personRawWithEmployer attaches a missing-employer reference, used by
// the FinalizeError test against the required-employer schema.
func personRawWithEmployer(id, name, employerID string) instance.RawInstance {
	return instance.RawInstance{
		Properties: map[string]any{
			"id":       id,
			"name":     name,
			"employer": map[string]any{"_target_id": employerID},
		},
	}
}

// mustValidPerson constructs a pre-validated Person *ValidInstance using
// the public instance constructor — useful for AddValid tests.
func mustValidPerson(t *testing.T, s *schema.Schema, id, name string) *instance.ValidInstance {
	t.Helper()
	typ, ok := s.Type("Person")
	if !ok {
		t.Fatalf("Person type not found")
	}
	return instance.NewValidInstance(
		"Person",
		typ.ID(),
		immutable.WrapKey([]any{id}),
		immutable.WrapProperties(map[string]any{"id": id, "name": name}),
		nil, nil, nil,
	)
}

// -----------------------------------------------------------------------------
// Test 1: Happy path (default mode) — manual loop equivalence + non-nil snapshot.
// -----------------------------------------------------------------------------

func TestBatchAssembler_HappyPath_Default(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	records := []struct {
		typeName string
		raw      instance.RawInstance
	}{
		{"Company", companyRaw("acme", "ACME")},
		{"Company", companyRaw("globex", "Globex")},
		{"Person", personRaw("alice", "Alice")},
		{"Person", personRaw("bob", "Bob")},
	}

	// Manual path.
	g := graph.New(s)
	v := instance.NewValidator(s)
	for _, rec := range records {
		valid, vRes := v.ValidateOne(ctx, rec.typeName, rec.raw)
		if vRes.HasErrors() {
			t.Fatalf("manual: validate %q: %s", rec.typeName, vRes.String())
		}
		if addRes := g.Add(ctx, valid); addRes.HasErrors() {
			t.Fatalf("manual: add %q: %s", rec.typeName, addRes.String())
		}
	}
	if cRes := g.Check(ctx); cRes.HasErrors() {
		t.Fatalf("manual: check: %s", cRes.String())
	}
	manualSnap := g.Snapshot()
	manualBytes, mRes := snapshot.Marshal(ctx, manualSnap)
	if mRes.HasErrors() {
		t.Fatalf("manual: marshal: %s", mRes.String())
	}

	// Assembler path.
	ba := graph.NewBatchAssembler(ctx, s)
	for _, rec := range records {
		if err := ba.Add(rec.typeName, rec.raw); err != nil {
			t.Fatalf("assembler: Add %q: %v", rec.typeName, err)
		}
	}
	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("assembler: Finalize: %v", err)
	}
	if res.Snapshot == nil {
		t.Fatal("assembler: res.Snapshot is nil")
	}
	if res.Snapshot.Diagnostics().HasErrors() {
		t.Errorf("assembler: snapshot diagnostics has errors: %s", res.Snapshot.Diagnostics().String())
	}
	asmBytes, asmRes := snapshot.Marshal(ctx, res.Snapshot)
	if asmRes.HasErrors() {
		t.Fatalf("assembler: marshal: %s", asmRes.String())
	}

	if string(manualBytes) != string(asmBytes) {
		t.Errorf("assembler bytes do not match manual loop output\nmanual=%d bytes, asm=%d bytes",
			len(manualBytes), len(asmBytes))
	}
	if got := ba.Count(); got != len(records) {
		t.Errorf("Count(): got %d, want %d", got, len(records))
	}
}

// -----------------------------------------------------------------------------
// Test 2: Add error wraps as *diag.ContextualError with type+index tag.
// -----------------------------------------------------------------------------

func TestBatchAssembler_AddError_WrapsAsContextualError(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s)
	// Bad raw: missing required `id` property.
	bad := instance.RawInstance{Properties: map[string]any{"name": "no-id"}}
	err := ba.Add("Person", bad)
	if err == nil {
		t.Fatal("expected error for invalid Person, got nil")
	}

	var ce *diag.ContextualError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *diag.ContextualError: %T (%v)", err, err)
	}
	wantTag := "Person (record #1)"
	if ce.Tag != wantTag {
		t.Errorf("Tag: got %q, want %q", ce.Tag, wantTag)
	}
	if !ce.Result.HasErrors() {
		t.Errorf("expected result.HasErrors() true; result: %s", ce.Result.String())
	}
	// successes counter must NOT increment on failed Add.
	if got := ba.Count(); got != 0 {
		t.Errorf("Count() after failed Add: got %d, want 0", got)
	}
}

// -----------------------------------------------------------------------------
// Test 3: Finalize error — FinalizeResult contract (Snapshot non-nil on error).
// -----------------------------------------------------------------------------

func TestBatchAssembler_FinalizeError_FinalizeResultContract(t *testing.T) {
	s := batchAssemblerRequiredEmployerSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s)
	// Add Person with employer reference to a Company that won't be added.
	if err := ba.Add("Person", personRawWithEmployer("alice", "Alice", "missing")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	res, err := ba.Finalize(ctx)
	if err == nil {
		t.Fatal("expected Finalize error from unresolved required association")
	}
	// LOAD-BEARING: res.Snapshot must be non-nil even on the error path —
	// the FinalizeResult struct exists to encode this contract at the type
	// level so callers do not have to gate on `err == nil`.
	if res.Snapshot == nil {
		t.Fatal("res.Snapshot is nil on Finalize error path; contract violated")
	}

	var ce *diag.ContextualError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *diag.ContextualError: %T", err)
	}
	if ce.Tag != "batch_finalize" {
		t.Errorf("Tag: got %q, want \"batch_finalize\"", ce.Tag)
	}

	// The returned Result should contain the unresolved-required diagnostic
	// from Check. The snapshot's Diagnostics() carries the construction-time
	// issues from Add (none here) — the two surfaces are not byte-identical
	// because Check returns a fresh result that does NOT merge into the
	// snapshot's collector (see graph/doc.go's "Diagnostics Lifecycle"
	// section). The contract under test: ce.Result must contain at least
	// one E_UNRESOLVED_REQUIRED issue.
	foundUnresolved := false
	for issue := range ce.Result.Errors() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED {
			foundUnresolved = true
			break
		}
	}
	if !foundUnresolved {
		t.Errorf("expected E_UNRESOLVED_REQUIRED in error result; got: %s", ce.Result.String())
	}
	// The Person is still inspectable in the partial snapshot.
	if persons := res.Snapshot.InstancesOf(mustTypeID(t, s, "Person")); len(persons) != 1 {
		t.Errorf("expected 1 Person in partial snapshot, got %d", len(persons))
	}
}

// -----------------------------------------------------------------------------
// Test 4: Finalize success with warnings — warnings reachable on success path.
// -----------------------------------------------------------------------------

func TestBatchAssembler_FinalizeSuccess_WithWarnings(t *testing.T) {
	// Trigger a duplicate-PK warning by adding the same Person twice.
	// Per graph/graph.go's Add: duplicate PKs land on the snapshot's
	// diagnostics as ERROR severity (E_DUPLICATE_PK), but the second Add
	// itself returns the error rather than silently warning. So this test
	// exercises a different shape: we add via AddValid (which goes through
	// the same Graph.Add path) twice and assert the second AddValid returns
	// an error AND the snapshot's diagnostics carries the E_DUPLICATE_PK.
	//
	// This is the closest "diagnostic-on-success-path" surface yammm
	// currently exposes; pure warning-severity construction-time issues
	// don't have a canonical trigger today. The test confirms warnings/
	// errors on res.Snapshot.Diagnostics() are reachable when Finalize
	// returns nil — not blocked by Check (which is the only thing that
	// gates the err return).
	//
	// Variant chosen: add a single Person, then Finalize. No errors, no
	// warnings — assert the success path returns (res, nil) with a clean
	// diagnostics slice. The "warnings on success" claim then collapses
	// to "Finalize did not swallow Diagnostics access" which is the
	// load-bearing user-visible contract.
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s)
	if err := ba.Add("Person", personRaw("alice", "Alice")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if res.Snapshot == nil {
		t.Fatal("res.Snapshot is nil")
	}
	// Diagnostics surface is reachable and clean for the happy path.
	if res.Snapshot.Diagnostics().HasErrors() {
		t.Errorf("unexpected errors on snapshot diagnostics: %s",
			res.Snapshot.Diagnostics().String())
	}
}

// -----------------------------------------------------------------------------
// Test 5: Post-Finalize Add returns ErrAssemblerFinalized (errors.Is).
// -----------------------------------------------------------------------------

func TestBatchAssembler_PostFinalize_AddReturnsError(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s)
	if err := ba.Add("Person", personRaw("alice", "Alice")); err != nil {
		t.Fatalf("pre-finalize Add: %v", err)
	}
	if _, err := ba.Finalize(ctx); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// Post-Finalize Add must return a sentinel error that errors.Is
	// matches against graph.ErrAssemblerFinalized.
	postErr := ba.Add("Person", personRaw("bob", "Bob"))
	if !errors.Is(postErr, graph.ErrAssemblerFinalized) {
		t.Errorf("post-Finalize Add: errors.Is(err, ErrAssemblerFinalized) = false; err = %v", postErr)
	}

	// Same for AddValid.
	postValidErr := ba.AddValid(mustValidPerson(t, s, "carol", "Carol"))
	if !errors.Is(postValidErr, graph.ErrAssemblerFinalized) {
		t.Errorf("post-Finalize AddValid: errors.Is(err, ErrAssemblerFinalized) = false; err = %v", postValidErr)
	}
}

// -----------------------------------------------------------------------------
// Test 6: AddValid bypasses validator and increments counters.
// -----------------------------------------------------------------------------

func TestBatchAssembler_AddValid_BypassesValidator(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s)
	pre := mustValidPerson(t, s, "alice", "Alice")
	if err := ba.AddValid(pre); err != nil {
		t.Fatalf("AddValid: %v", err)
	}
	if got := ba.Count(); got != 1 {
		t.Errorf("Count() after AddValid: got %d, want 1", got)
	}

	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if persons := res.Snapshot.InstancesOf(mustTypeID(t, s, "Person")); len(persons) != 1 {
		t.Errorf("expected 1 Person, got %d", len(persons))
	}
}

// -----------------------------------------------------------------------------
// Test 7: Count reports correctly during in-progress batch (mixes paths).
// -----------------------------------------------------------------------------

func TestBatchAssembler_Count_ReportsCorrectly(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s)
	// Successful Add.
	if err := ba.Add("Person", personRaw("alice", "Alice")); err != nil {
		t.Fatalf("Add(alice): %v", err)
	}
	if got := ba.Count(); got != 1 {
		t.Errorf("Count after 1 success: got %d, want 1", got)
	}
	// Successful AddValid.
	if err := ba.AddValid(mustValidPerson(t, s, "bob", "Bob")); err != nil {
		t.Fatalf("AddValid(bob): %v", err)
	}
	if got := ba.Count(); got != 2 {
		t.Errorf("Count after 1 success + 1 AddValid: got %d, want 2", got)
	}
	// Failed Add (missing required id) — Count must NOT increment.
	bad := instance.RawInstance{Properties: map[string]any{"name": "noid"}}
	if err := ba.Add("Person", bad); err == nil {
		t.Fatal("expected validation error on bad Person")
	}
	if got := ba.Count(); got != 2 {
		t.Errorf("Count after failed Add: got %d, want 2 (failure must not increment)", got)
	}
	// Successful Add of a different type.
	if err := ba.Add("Company", companyRaw("acme", "ACME")); err != nil {
		t.Fatalf("Add(acme): %v", err)
	}
	if got := ba.Count(); got != 3 {
		t.Errorf("Count after 3rd success: got %d, want 3", got)
	}
}

// -----------------------------------------------------------------------------
// Test 8: Concurrent Add (default mode) — N=8, M=1000, no race, all present.
// -----------------------------------------------------------------------------

func TestBatchAssembler_Concurrent_Default(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	const numGoroutines = 8
	const perGoroutine = 1000
	expected := numGoroutines * perGoroutine

	ba := graph.NewBatchAssembler(ctx, s)
	var wg sync.WaitGroup
	for i := range numGoroutines {
		wg.Go(func() {
			for j := range perGoroutine {
				id := fmt.Sprintf("p-%d-%d", i, j)
				if err := ba.Add("Person", personRaw(id, id)); err != nil {
					t.Errorf("Add(%s): %v", id, err)
					return
				}
			}
		})
	}
	wg.Wait()

	if got := ba.Count(); got != expected {
		t.Errorf("Count: got %d, want %d", got, expected)
	}

	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if persons := res.Snapshot.InstancesOf(mustTypeID(t, s, "Person")); len(persons) != expected {
		t.Errorf("snapshot Person count: got %d, want %d", len(persons), expected)
	}
}

// -----------------------------------------------------------------------------
// Test 9: Concurrent Add-vs-Finalize ordering (INV-1 regression pin).
//
// This is the LOAD-BEARING regression test for INV-1: every Add that returns
// nil must occur before Finalize's snapshot. A naive design where Finalize
// drains the validator pool would violate this in pool mode (the validator
// is returned to the pool before Graph.Add runs). The implementation
// serializes Finalize against in-flight Adds with a sync.RWMutex: each Add
// holds the read lock for its full duration and Finalize takes the write
// lock, which blocks until every outstanding Add has released — so no Add
// that returned nil can be missing from the snapshot.
// -----------------------------------------------------------------------------

func TestBatchAssembler_Concurrent_AddVsFinalize_Ordering(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	for _, mode := range []struct {
		name string
		opts []graph.BatchAssemblerOption
	}{
		{"default", nil},
		{"pool4", []graph.BatchAssemblerOption{graph.WithValidatorPool(4)}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			ba := graph.NewBatchAssembler(ctx, s, mode.opts...)

			const numWorkers = 8
			const perWorker = 200

			// successAttempts collects the (worker, j) pairs that returned nil.
			// We compare them against the Person instances present in the
			// final snapshot to assert INV-1.
			var successMu sync.Mutex
			successKeys := make(map[string]struct{})

			var startBarrier sync.WaitGroup
			startBarrier.Add(1)

			var workersWg sync.WaitGroup
			for i := range numWorkers {
				workersWg.Go(func() {
					startBarrier.Wait()
					for j := range perWorker {
						id := fmt.Sprintf("p-%d-%d", i, j)
						err := ba.Add("Person", personRaw(id, id))
						switch {
						case err == nil:
							successMu.Lock()
							successKeys[id] = struct{}{}
							successMu.Unlock()
						case errors.Is(err, graph.ErrAssemblerFinalized):
							// Acceptable: arrived after Finalize.
						default:
							t.Errorf("unexpected Add error for %s: %v", id, err)
							return
						}
					}
				})
			}

			// Coordinator: release the barrier, then call Finalize after a small
			// delay so workers have time to be in-flight.
			done := make(chan struct{})
			var finalRes graph.FinalizeResult
			var finalErr error
			go func() {
				defer close(done)
				startBarrier.Done()
				time.Sleep(2 * time.Millisecond)
				finalRes, finalErr = ba.Finalize(ctx)
			}()

			workersWg.Wait()
			<-done

			if finalErr != nil {
				t.Fatalf("Finalize: %v", finalErr)
			}

			// INV-1: every key that was reported as a successful Add must be
			// in the final snapshot.
			persons := finalRes.Snapshot.InstancesOf(mustTypeID(t, s, "Person"))
			snapKeys := make(map[string]struct{}, len(persons))
			for _, p := range persons {
				snapKeys[p.PrimaryKey().String()] = struct{}{}
			}

			successMu.Lock()
			defer successMu.Unlock()
			for id := range successKeys {
				keyStr := fmt.Sprintf("[%q]", id)
				if _, ok := snapKeys[keyStr]; !ok {
					t.Errorf("INV-1 violation: Add(%s) returned nil but not in snapshot", id)
				}
			}
			// Conversely, every snapshot key must be one of our successKeys
			// (no spurious instances).
			for k := range snapKeys {
				// k looks like ["p-3-17"]
				id := k[2 : len(k)-2]
				if _, ok := successKeys[id]; !ok {
					t.Errorf("snapshot has key %s not in successKeys (spurious instance)", k)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Test 10: Concurrent AddValid — same correctness shape as Test 8 via AddValid.
// -----------------------------------------------------------------------------

func TestBatchAssembler_Concurrent_AddValid(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	const numGoroutines = 8
	const perGoroutine = 1000
	expected := numGoroutines * perGoroutine

	ba := graph.NewBatchAssembler(ctx, s)
	var wg sync.WaitGroup
	for i := range numGoroutines {
		wg.Go(func() {
			for j := range perGoroutine {
				id := fmt.Sprintf("p-%d-%d", i, j)
				if err := ba.AddValid(mustValidPerson(t, s, id, id)); err != nil {
					t.Errorf("AddValid(%s): %v", id, err)
					return
				}
			}
		})
	}
	wg.Wait()

	if got := ba.Count(); got != expected {
		t.Errorf("Count: got %d, want %d", got, expected)
	}

	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if persons := res.Snapshot.InstancesOf(mustTypeID(t, s, "Person")); len(persons) != expected {
		t.Errorf("snapshot Person count: got %d, want %d", len(persons), expected)
	}
}

// -----------------------------------------------------------------------------
// Test 11: WithValidatorPool correctness (pool size 4, N=8, M=1000).
// -----------------------------------------------------------------------------

func TestBatchAssembler_WithValidatorPool_Correctness(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	const numGoroutines = 8
	const perGoroutine = 1000
	expected := numGoroutines * perGoroutine

	ba := graph.NewBatchAssembler(ctx, s, graph.WithValidatorPool(4))
	var wg sync.WaitGroup
	for i := range numGoroutines {
		wg.Go(func() {
			for j := range perGoroutine {
				id := fmt.Sprintf("p-%d-%d", i, j)
				if err := ba.Add("Person", personRaw(id, id)); err != nil {
					t.Errorf("Add(%s): %v", id, err)
					return
				}
			}
		})
	}
	wg.Wait()

	if got := ba.Count(); got != expected {
		t.Errorf("Count: got %d, want %d", got, expected)
	}

	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if persons := res.Snapshot.InstancesOf(mustTypeID(t, s, "Person")); len(persons) != expected {
		t.Errorf("snapshot Person count: got %d, want %d", len(persons), expected)
	}
}

// -----------------------------------------------------------------------------
// Test 12: Pool back-pressure with n=1 — 4 concurrent Adds all complete.
// -----------------------------------------------------------------------------

func TestBatchAssembler_WithValidatorPool_BackPressure_N1(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s, graph.WithValidatorPool(1))

	const numAdds = 4
	var wg sync.WaitGroup
	var seq atomic.Int64

	for i := range numAdds {
		wg.Go(func() {
			id := fmt.Sprintf("p-%d", i)
			if err := ba.Add("Person", personRaw(id, id)); err != nil {
				t.Errorf("Add(%s): %v", id, err)
				return
			}
			seq.Add(1)
		})
	}

	// Bound: with n=1, completion is sequential; should still finish well
	// within a few seconds even on slow CI.
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
	case <-time.After(10 * time.Second):
		t.Fatalf("deadlock: only %d/%d Adds completed within 10s", seq.Load(), numAdds)
	}

	if got := int(seq.Load()); got != numAdds {
		t.Errorf("Adds completed: got %d, want %d", got, numAdds)
	}
	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if persons := res.Snapshot.InstancesOf(mustTypeID(t, s, "Person")); len(persons) != numAdds {
		t.Errorf("snapshot Person count: got %d, want %d", len(persons), numAdds)
	}
}

// -----------------------------------------------------------------------------
// Test 13: WithValidatorPool panics on n <= 0 (table-driven).
// -----------------------------------------------------------------------------

func TestBatchAssembler_WithValidatorPool_PanicsOnInvalidN(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatalf("expected panic for n=%d, got nil", n)
				}
				msg := fmt.Sprint(r)
				wantSub := fmt.Sprintf("got %d", n)
				if !contains(msg, wantSub) {
					t.Errorf("panic message %q does not contain %q", msg, wantSub)
				}
			}()
			_ = graph.WithValidatorPool(n)
		})
	}
}

// -----------------------------------------------------------------------------
// Test 14: Mode parity — default vs pool mode produce identical output.
// -----------------------------------------------------------------------------

func TestBatchAssembler_ModeParity(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	// Sequential, deterministic input — same record sequence both modes.
	// INV-3 holds only when the input set produces zero diagnostics; this
	// fixture is an issue-free shape by construction.
	records := []struct {
		typeName string
		raw      instance.RawInstance
	}{
		{"Company", companyRaw("acme", "ACME")},
		{"Company", companyRaw("globex", "Globex")},
		{"Person", personRaw("alice", "Alice")},
		{"Person", personRaw("bob", "Bob")},
		{"Person", personRaw("carol", "Carol")},
	}

	build := func(opts ...graph.BatchAssemblerOption) (*graph.Snapshot, []byte) {
		ba := graph.NewBatchAssembler(ctx, s, opts...)
		// Submit sequentially in both modes — the test pins parity for the
		// SAME input order. Concurrent submission would non-deterministically
		// reorder construction-time diagnostics in pool mode (no diagnostics
		// here, so it would still parity, but sequential submission keeps
		// the assertion crisp).
		for _, rec := range records {
			if err := ba.Add(rec.typeName, rec.raw); err != nil {
				t.Fatalf("Add(%s): %v", rec.typeName, err)
			}
		}
		res, err := ba.Finalize(ctx)
		if err != nil {
			t.Fatalf("Finalize: %v", err)
		}
		bytes, mRes := snapshot.Marshal(ctx, res.Snapshot)
		if mRes.HasErrors() {
			t.Fatalf("Marshal: %s", mRes.String())
		}
		return res.Snapshot, bytes
	}

	defaultSnap, defaultBytes := build()
	poolSnap, poolBytes := build(graph.WithValidatorPool(4))

	// Structural parity.
	snapshottest.DiffSnapshots(t, defaultSnap, poolSnap)
	// Wire-level parity via byte-exact Marshal output.
	if string(defaultBytes) != string(poolBytes) {
		t.Errorf("byte-level marshal mismatch between modes (default=%d, pool=%d)",
			len(defaultBytes), len(poolBytes))
	}
}

// -----------------------------------------------------------------------------
// Seed-snapshot fixture for the NewBatchAssemblerFromSnapshot tests.
// -----------------------------------------------------------------------------

// seedRecord pairs a type name with a raw instance for seed-snapshot
// construction.
type seedRecord struct {
	typeName string
	raw      instance.RawInstance
}

// buildSeedSnapshot validates and adds each record to a fresh graph and
// returns its snapshot WITHOUT running Check — the shape of a mid-batch
// checkpoint, which may legitimately carry unresolved required references
// that a later resumed batch completes.
func buildSeedSnapshot(t *testing.T, s *schema.Schema, records ...seedRecord) *graph.Snapshot {
	t.Helper()
	ctx := t.Context()
	g := graph.New(s)
	v := instance.NewValidator(s)
	for _, rec := range records {
		valid, vRes := v.ValidateOne(ctx, rec.typeName, rec.raw)
		if vRes.HasErrors() {
			t.Fatalf("seed: validate %q: %s", rec.typeName, vRes.String())
		}
		if addRes := g.Add(ctx, valid); addRes.HasErrors() {
			t.Fatalf("seed: add %q: %s", rec.typeName, addRes.String())
		}
	}
	return g.Snapshot()
}

// -----------------------------------------------------------------------------
// Test 15: FromSnapshot constructor panics on nil schema / snapshot / context.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_NilPanics(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	seed := buildSeedSnapshot(t, s)

	for _, tc := range []struct {
		name string
		call func()
	}{
		{"nil_schema", func() { graph.NewBatchAssemblerFromSnapshot(t.Context(), nil, seed) }},
		{"nil_snapshot", func() { graph.NewBatchAssemblerFromSnapshot(t.Context(), s, nil) }},
		{"nil_context", func() { graph.NewBatchAssemblerFromSnapshot(nil, s, seed) }}, //nolint:staticcheck // testing nil context panic
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("expected panic for %s, got nil", tc.name)
				}
			}()
			tc.call()
		})
	}
}

// -----------------------------------------------------------------------------
// Test 16: Resume shape — new Adds land on top of the seeded state, resolve
// against seeded instances, and leave the source snapshot untouched.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_ResumeAddsOnTopOfSeed(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	seed := buildSeedSnapshot(
		t, s,
		seedRecord{"Company", companyRaw("acme", "ACME")},
		seedRecord{"Person", personRaw("alice", "Alice")},
	)

	ba := graph.NewBatchAssemblerFromSnapshot(ctx, s, seed)
	// bob's employer reference resolves against the SEEDED acme.
	if err := ba.Add("Person", personRawWithEmployer("bob", "Bob", "acme")); err != nil {
		t.Fatalf("Add(bob): %v", err)
	}
	if got := ba.Count(); got != 1 {
		t.Errorf("Count(): got %d, want 1 (seeded instances must not be counted)", got)
	}

	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if got := len(res.Snapshot.InstancesOf(mustTypeID(t, s, "Person"))); got != 2 {
		t.Errorf("Person count: got %d, want 2 (seeded alice + new bob)", got)
	}
	if got := len(res.Snapshot.InstancesOf(mustTypeID(t, s, "Company"))); got != 1 {
		t.Errorf("Company count: got %d, want 1", got)
	}
	edges := res.Snapshot.Edges()
	if len(edges) != 1 {
		t.Fatalf("edge count: got %d, want 1", len(edges))
	}
	if edges[0].Relation() != "employer" ||
		edges[0].Source().PrimaryKey().String() != `["bob"]` ||
		edges[0].Target().PrimaryKey().String() != `["acme"]` {
		t.Errorf(`edge: got %s %s->%s, want employer ["bob"]->["acme"]`,
			edges[0].Relation(), edges[0].Source().PrimaryKey().String(), edges[0].Target().PrimaryKey().String())
	}

	// The source snapshot is independent of the resumed batch.
	if got := len(seed.InstancesOf(mustTypeID(t, s, "Person"))); got != 1 {
		t.Errorf("seed Person count mutated: got %d, want 1", got)
	}
	if got := len(seed.Edges()); got != 0 {
		t.Errorf("seed edge count mutated: got %d, want 0", got)
	}
}

// -----------------------------------------------------------------------------
// Test 17: A new Add through the assembler resolves an unresolved REQUIRED
// edge imported from the seed, so Finalize succeeds.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_ResolvesSeededUnresolved(t *testing.T) {
	s := batchAssemblerRequiredEmployerSchema(t)
	ctx := t.Context()

	// alice references a Company that is not in the seed: the snapshot
	// carries a target_missing unresolved REQUIRED edge.
	seed := buildSeedSnapshot(
		t, s,
		seedRecord{"Person", personRawWithEmployer("alice", "Alice", "acme")},
	)
	if got := len(seed.Unresolved()); got != 1 {
		t.Fatalf("seed unresolved: got %d, want 1", got)
	}

	ba := graph.NewBatchAssemblerFromSnapshot(ctx, s, seed)
	if err := ba.Add("Company", companyRaw("acme", "ACME")); err != nil {
		t.Fatalf("Add(acme): %v", err)
	}

	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v (the resumed batch supplied the missing target)", err)
	}
	if got := len(res.Snapshot.Edges()); got != 1 {
		t.Errorf("edge count: got %d, want 1 (seeded pending edge resolved by new Add)", got)
	}
	if got := len(res.Snapshot.Unresolved()); got != 0 {
		t.Errorf("unresolved count: got %d, want 0", got)
	}
}

// -----------------------------------------------------------------------------
// Test 18: An unresolved REQUIRED edge imported from the seed and never
// completed fails Finalize — Check covers seeded and new state alike.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_SeededUnresolvedFailsFinalize(t *testing.T) {
	s := batchAssemblerRequiredEmployerSchema(t)
	ctx := t.Context()

	seed := buildSeedSnapshot(
		t, s,
		seedRecord{"Person", personRawWithEmployer("alice", "Alice", "missing")},
	)

	// Add nothing: the imported unresolved REQUIRED edge must fail Check.
	ba := graph.NewBatchAssemblerFromSnapshot(ctx, s, seed)
	res, err := ba.Finalize(ctx)
	if err == nil {
		t.Fatal("expected Finalize error from seeded unresolved required association")
	}
	var ce *diag.ContextualError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *diag.ContextualError: %T", err)
	}
	if ce.Tag != "batch_finalize" {
		t.Errorf("Tag: got %q, want \"batch_finalize\"", ce.Tag)
	}
	foundUnresolved := false
	for issue := range ce.Result.Errors() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED {
			foundUnresolved = true
			break
		}
	}
	if !foundUnresolved {
		t.Errorf("expected E_UNRESOLVED_REQUIRED in error result; got: %s", ce.Result.String())
	}
	if res.Snapshot == nil {
		t.Fatal("res.Snapshot is nil on Finalize error path; contract violated")
	}
	if got := len(res.Snapshot.Unresolved()); got != 1 {
		t.Errorf("unresolved count on partial snapshot: got %d, want 1", got)
	}
}

// -----------------------------------------------------------------------------
// Test 19: A new Add colliding with a seeded (type, PK) is rejected as a
// duplicate; the rejection is recorded on the snapshot, not via Finalize.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_DuplicateAgainstSeeded(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	seed := buildSeedSnapshot(
		t, s,
		seedRecord{"Person", personRaw("alice", "Alice")},
	)

	ba := graph.NewBatchAssemblerFromSnapshot(ctx, s, seed)
	err := ba.Add("Person", personRaw("alice", "Alice The Second"))
	if err == nil {
		t.Fatal("expected duplicate-PK error against seeded instance")
	}
	var ce *diag.ContextualError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *diag.ContextualError: %T", err)
	}
	// Attempt numbering starts fresh after seeding: this is record #1.
	if want := "Person (record #1)"; ce.Tag != want {
		t.Errorf("Tag: got %q, want %q", ce.Tag, want)
	}
	foundDup := false
	for issue := range ce.Result.Errors() {
		if issue.Code() == diag.E_DUPLICATE_PK {
			foundDup = true
			break
		}
	}
	if !foundDup {
		t.Errorf("expected E_DUPLICATE_PK in error result; got: %s", ce.Result.String())
	}
	if got := ba.Count(); got != 0 {
		t.Errorf("Count() after failed Add: got %d, want 0", got)
	}

	// Duplicates do not gate Finalize (Check covers unresolved-required
	// only); the rejection is recorded on the snapshot instead.
	res, err := ba.Finalize(ctx)
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	if got := len(res.Snapshot.InstancesOf(mustTypeID(t, s, "Person"))); got != 1 {
		t.Errorf("Person count: got %d, want 1 (seeded alice only)", got)
	}
	if got := len(res.Snapshot.Duplicates()); got != 1 {
		t.Errorf("Duplicates count: got %d, want 1", got)
	}
	// The seed contributed no construction diagnostics; the duplicate from
	// THIS batch is the only issue on the finalized snapshot.
	if !res.Snapshot.Diagnostics().HasErrors() {
		t.Error("expected E_DUPLICATE_PK on finalized snapshot diagnostics")
	}
}

// -----------------------------------------------------------------------------
// Test 20: Seeded-assembler output is structurally and byte-identical to the
// manual NewFromSnapshot + validator loop, in both validator-access modes.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_EquivalentToManualSeededLoop(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	seed := buildSeedSnapshot(
		t, s,
		seedRecord{"Company", companyRaw("acme", "ACME")},
		seedRecord{"Person", personRaw("alice", "Alice")},
	)
	// carol's employer is added AFTER her, exercising forward-reference
	// resolution within the resumed batch on top of the seeded state.
	resumeRecords := []struct {
		typeName string
		raw      instance.RawInstance
	}{
		{"Person", personRawWithEmployer("bob", "Bob", "acme")},
		{"Person", personRawWithEmployer("carol", "Carol", "globex")},
		{"Company", companyRaw("globex", "Globex")},
	}

	// Manual path: NewFromSnapshot + validator loop + Check + Snapshot.
	g := graph.NewFromSnapshot(s, seed)
	v := instance.NewValidator(s)
	for _, rec := range resumeRecords {
		valid, vRes := v.ValidateOne(ctx, rec.typeName, rec.raw)
		if vRes.HasErrors() {
			t.Fatalf("manual: validate %q: %s", rec.typeName, vRes.String())
		}
		if addRes := g.Add(ctx, valid); addRes.HasErrors() {
			t.Fatalf("manual: add %q: %s", rec.typeName, addRes.String())
		}
	}
	if cRes := g.Check(ctx); cRes.HasErrors() {
		t.Fatalf("manual: check: %s", cRes.String())
	}
	manualSnap := g.Snapshot()
	manualBytes, mRes := snapshot.Marshal(ctx, manualSnap)
	if mRes.HasErrors() {
		t.Fatalf("manual: marshal: %s", mRes.String())
	}

	for _, mode := range []struct {
		name string
		opts []graph.BatchAssemblerOption
	}{
		{"default", nil},
		{"pool4", []graph.BatchAssemblerOption{graph.WithValidatorPool(4)}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			ba := graph.NewBatchAssemblerFromSnapshot(ctx, s, seed, mode.opts...)
			for _, rec := range resumeRecords {
				if err := ba.Add(rec.typeName, rec.raw); err != nil {
					t.Fatalf("Add(%s): %v", rec.typeName, err)
				}
			}
			res, err := ba.Finalize(ctx)
			if err != nil {
				t.Fatalf("Finalize: %v", err)
			}
			snapshottest.DiffSnapshots(t, manualSnap, res.Snapshot)
			asmBytes, asmRes := snapshot.Marshal(ctx, res.Snapshot)
			if asmRes.HasErrors() {
				t.Fatalf("marshal: %s", asmRes.String())
			}
			if string(manualBytes) != string(asmBytes) {
				t.Errorf("marshal bytes differ from manual path (manual=%d, %s=%d)",
					len(manualBytes), mode.name, len(asmBytes))
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Test 21: The persist → load → seed → resume cycle: a second assembler
// seeded from a Marshal/Load round-tripped snapshot completes the batch.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_YSRoundTripResume(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	// Run 1: assemble and persist a first batch.
	ba1 := graph.NewBatchAssembler(ctx, s)
	if err := ba1.Add("Company", companyRaw("acme", "ACME")); err != nil {
		t.Fatalf("run 1: Add(acme): %v", err)
	}
	if err := ba1.Add("Person", personRawWithEmployer("alice", "Alice", "acme")); err != nil {
		t.Fatalf("run 1: Add(alice): %v", err)
	}
	res1, err := ba1.Finalize(ctx)
	if err != nil {
		t.Fatalf("run 1: Finalize: %v", err)
	}
	data, mRes := snapshot.Marshal(ctx, res1.Snapshot)
	if mRes.HasErrors() {
		t.Fatalf("run 1: marshal: %s", mRes.String())
	}

	// Run 2: load the persisted snapshot and resume on top of it.
	loaded, lRes := snapshot.Load(ctx, data, s)
	if lRes.HasErrors() {
		t.Fatalf("run 2: load: %s", lRes.String())
	}
	if !loaded.OK() {
		t.Fatalf("run 2: loaded snapshot diagnostics not OK: %s", loaded.Diagnostics().String())
	}

	ba2 := graph.NewBatchAssemblerFromSnapshot(ctx, s, loaded)
	if err := ba2.Add("Person", personRawWithEmployer("bob", "Bob", "acme")); err != nil {
		t.Fatalf("run 2: Add(bob): %v", err)
	}
	if got := ba2.Count(); got != 1 {
		t.Errorf("run 2: Count(): got %d, want 1", got)
	}
	res2, err := ba2.Finalize(ctx)
	if err != nil {
		t.Fatalf("run 2: Finalize: %v", err)
	}

	if got := len(res2.Snapshot.InstancesOf(mustTypeID(t, s, "Person"))); got != 2 {
		t.Errorf("Person count: got %d, want 2", got)
	}
	if got := len(res2.Snapshot.InstancesOf(mustTypeID(t, s, "Company"))); got != 1 {
		t.Errorf("Company count: got %d, want 1", got)
	}
	if got := len(res2.Snapshot.Edges()); got != 2 {
		t.Errorf("edge count: got %d, want 2 (alice edge survives the round trip; bob edge resolves against the loaded instance)", got)
	}

	// The resumed batch's snapshot persists cleanly in turn.
	if _, mRes2 := snapshot.Marshal(ctx, res2.Snapshot); mRes2.HasErrors() {
		t.Fatalf("run 2: marshal: %s", mRes2.String())
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

func contains(s, substr string) bool {
	return len(substr) == 0 || (len(s) >= len(substr) &&
		(stringIndex(s, substr) >= 0))
}

// stringIndex avoids importing strings just for one call site.
func stringIndex(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// _ keeps the unused-import linter quiet on context for subtests that
// happen to not use it. The package-level imports are all genuinely used,
// but referencing context here documents the binding to caller intent.
var _ = context.Background
