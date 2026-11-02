package graph_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

// checkedSnapshot ends a batch as a caller does: it checks the graph, then takes
// its snapshot, which holds the graph whatever the check said.
func checkedSnapshot(ctx context.Context, ba *graph.BatchAssembler) (graph.Snapshot, error) {
	res := ba.Graph().Check(ctx)
	snap := ba.Graph().Snapshot()
	if res.HasErrors() {
		return snap, res.WithContext("check")
	}
	return snap, nil
}

// batchAssemblerTestSchema returns a schema with Person + Company plus an
// optional employer association so we can exercise both happy-path adds and
// (with the required-association variant below) Check-failure paths.
func batchAssemblerTestSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "batchassembler"

type Company {
	id String primary
	name String required
}

type Person {
	id String primary
	name String required
	--> EMPLOYER (_:one) Company
}
`, "batchassembler.yammm")
	if result.HasErrors() {
		t.Fatalf("schema build failed: %s", result.String())
	}
	return s
}

// batchAssemblerRequiredEmployerSchema is the same shape as the above but
// with `employer` declared as a REQUIRED association — used by the
// unresolved-required test to deterministically trigger an unresolved-required
// failure on Check.
func batchAssemblerRequiredEmployerSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "batchassembler_required"

type Company {
	id String primary
	name String required
}

type Person {
	id String primary
	name String required
	--> EMPLOYER (one) Company
}
`, "batchassembler_required.yammm")
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
// the unresolved-required test against the required-employer schema.
func personRawWithEmployer(id, name, employerID string) instance.RawInstance {
	return instance.RawInstance{
		Properties: map[string]any{
			"id":       id,
			"name":     name,
			"employer": map[string]any{"_target_id": employerID},
		},
	}
}

// mustValidPerson constructs a pre-validated Person ValidInstance using
// the public instance constructor — useful for AddValid tests.
func mustValidPerson(t *testing.T, s schema.Schema, id, name string) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, "Person", map[string]any{"id": id, "name": name})
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
	res, err := checkedSnapshot(ctx, ba)
	if err != nil {
		t.Fatalf("assembler: Check: %v", err)
	}
	if res.IsZero() {
		t.Fatal("assembler: res is nil")
	}
	if res.Diagnostics().HasErrors() {
		t.Errorf("assembler: snapshot diagnostics has errors: %s", res.Diagnostics().String())
	}
	asmBytes, asmRes := snapshot.Marshal(ctx, res)
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
	wantTag := "Person (attempt #1)"
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

func TestBatchAssembler_CancelledAdd_ReachesSnapshotDiagnostics(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ba := graph.NewBatchAssembler(ctx, s)

	err := ba.Add("Person", instance.RawInstance{Properties: map[string]any{"id": "p1", "name": "A"}})
	ce, ok := errors.AsType[*diag.ContextualError](err)
	if !ok || !ce.Result.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Fatalf("Add with a cancelled assembler context: %v; want E_CONTEXT_CANCELLED", err)
	}

	res := ba.Graph().Snapshot()
	if !res.Diagnostics().HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Errorf("Snapshot.Diagnostics() = %s; want the cancellation Add returned", res.Diagnostics())
	}
}

func TestBatchAssembler_AddValidationFailure_ReachesSnapshotDiagnostics(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ba := graph.NewBatchAssembler(t.Context(), s)

	err := ba.Add("Person", instance.RawInstance{Properties: map[string]any{"name": "no-id"}})
	ce, ok := errors.AsType[*diag.ContextualError](err)
	if !ok {
		t.Fatalf("Add error is not *diag.ContextualError: %T (%v)", err, err)
	}

	res, err := checkedSnapshot(t.Context(), ba)
	if err != nil {
		t.Fatalf("Check over an empty graph: %v", err)
	}
	got := res.Diagnostics().CodeCounts(diag.Error)
	want := ce.Result.CodeCounts(diag.Error)
	if len(want) == 0 || !maps.Equal(got, want) {
		t.Errorf("Snapshot.Diagnostics() error codes = %v, want the Add error's %v", got, want)
	}
}

// A raw instance holding a Refused value is refused with an error that
// matches instance.ErrRefusedValue, adds nothing, and adds no diagnostic: its
// producer reported the value.
func TestBatchAssembler_ARefusedValueIsRefusedWithNoDiagnostic(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ba := graph.NewBatchAssembler(t.Context(), s)

	err := ba.Add("Person", instance.RawInstance{Properties: map[string]any{"id": "p1", "name": instance.Refused{Value: 7}}})
	if !errors.Is(err, instance.ErrRefusedValue) {
		t.Fatalf("Add = %v, want instance.ErrRefusedValue", err)
	}
	if want := "Person (attempt #1)"; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Add = %q, want it tagged %q", err, want)
	}
	if got := ba.Count(); got != 0 {
		t.Errorf("Count() = %d, want 0", got)
	}
	res, err := checkedSnapshot(t.Context(), ba)
	if err != nil {
		t.Fatalf("Check over an empty graph: %v", err)
	}
	if d := res.Diagnostics(); d.Len() != 0 {
		t.Errorf("Snapshot.Diagnostics() = %s, want none", d)
	}
}

// A validator answers a refused value with a nil instance and no error, so the
// nil a caller passes on to AddValid names that cause beside a failed one.
func TestBatchAssembler_AddValidOfANilNamesARefusedValue(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	valid, vRes := instance.NewValidator(s).ValidateOne(t.Context(), "Person",
		instance.RawInstance{Properties: map[string]any{"id": "p1", "name": instance.Refused{Value: 7}}})
	if !valid.IsZero() || !vRes.OK() {
		t.Fatalf("ValidateOne = %v, %s; want nil and no issue", valid, vRes)
	}
	ba := graph.NewBatchAssembler(t.Context(), s)
	ce, ok := errors.AsType[*diag.ContextualError](ba.AddValid(valid))
	if !ok || !strings.Contains(ce.Result.String(), "holds a refused value") {
		t.Errorf("AddValid(nil) = %v; want the message to name a refused value", ce)
	}
}

// -----------------------------------------------------------------------------
// Test 3: Check reports an unresolved required association, and the snapshot
// holds the graph it judged.
// -----------------------------------------------------------------------------

func TestBatchAssembler_UnresolvedRequired_CheckReportsAndTheSnapshotHoldsTheAdds(t *testing.T) {
	s := batchAssemblerRequiredEmployerSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s)
	// Add Person with employer reference to a Company that won't be added.
	if err := ba.Add("Person", personRawWithEmployer("alice", "Alice", "missing")); err != nil {
		t.Fatalf("Add: %v", err)
	}

	res := ba.Graph().Check(ctx)
	if !res.HasErrors() {
		t.Fatal("expected a Check error from the unresolved required association")
	}
	// Check's result does not merge into the snapshot's collector; see
	// graph/doc.go's "Diagnostics Lifecycle" section.
	if !res.HasCode(diag.E_UNRESOLVED_REQUIRED) {
		t.Errorf("expected E_UNRESOLVED_REQUIRED in the result; got: %s", res.String())
	}
	snap := ba.Graph().Snapshot()
	if snap.IsZero() {
		t.Fatal("zero snapshot after a failed Check")
	}
	if persons := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person"))); len(persons) != 1 {
		t.Errorf("expected 1 Person in the snapshot, got %d", len(persons))
	}
}

// -----------------------------------------------------------------------------
// Test 4: a clean Check leaves clean snapshot diagnostics.
// -----------------------------------------------------------------------------

// TestBatchAssembler_CleanBatch_CarriesCleanDiagnostics pins the success
// path's diagnostics surface. It does not pin warnings: no construction-time
// diagnostic in this package is warning-severity, so there is nothing to
// build a warning fixture from.
func TestBatchAssembler_CleanBatch_CarriesCleanDiagnostics(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	ba := graph.NewBatchAssembler(ctx, s)
	if err := ba.Add("Person", personRaw("alice", "Alice")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	res, err := checkedSnapshot(ctx, ba)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.IsZero() {
		t.Fatal("res is nil")
	}
	// Diagnostics surface is reachable and clean for the happy path.
	if res.Diagnostics().HasErrors() {
		t.Errorf("unexpected errors on snapshot diagnostics: %s",
			res.Diagnostics().String())
	}
}

// -----------------------------------------------------------------------------
// Test 6: AddValid runs no validator and increments counters.
// -----------------------------------------------------------------------------

func TestBatchAssembler_AddValid_RunsNoValidator(t *testing.T) {
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

	res, err := checkedSnapshot(ctx, ba)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if persons := slices.Collect(res.InstancesOf(mustTypeID(t, s, "Person"))); len(persons) != 1 {
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

	res, err := checkedSnapshot(ctx, ba)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if persons := slices.Collect(res.InstancesOf(mustTypeID(t, s, "Person"))); len(persons) != expected {
		t.Errorf("snapshot Person count: got %d, want %d", len(persons), expected)
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

	res, err := checkedSnapshot(ctx, ba)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if persons := slices.Collect(res.InstancesOf(mustTypeID(t, s, "Person"))); len(persons) != expected {
		t.Errorf("snapshot Person count: got %d, want %d", len(persons), expected)
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
func buildSeedSnapshot(t *testing.T, s schema.Schema, records ...seedRecord) graph.Snapshot {
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
		{"nil_schema", func() { graph.NewBatchAssemblerFromSnapshot(t.Context(), schema.Schema{}, seed) }},
		{"nil_snapshot", func() { graph.NewBatchAssemblerFromSnapshot(t.Context(), s, graph.Snapshot{}) }},
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

	ba := mustSeed(t, ctx, s, seed)
	// bob's employer reference resolves against the SEEDED acme.
	if err := ba.Add("Person", personRawWithEmployer("bob", "Bob", "acme")); err != nil {
		t.Fatalf("Add(bob): %v", err)
	}
	if got := ba.Count(); got != 1 {
		t.Errorf("Count(): got %d, want 1 (seeded instances must not be counted)", got)
	}

	res, err := checkedSnapshot(ctx, ba)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got := len(slices.Collect(res.InstancesOf(mustTypeID(t, s, "Person")))); got != 2 {
		t.Errorf("Person count: got %d, want 2 (seeded alice + new bob)", got)
	}
	if got := len(slices.Collect(res.InstancesOf(mustTypeID(t, s, "Company")))); got != 1 {
		t.Errorf("Company count: got %d, want 1", got)
	}
	edges := slices.Collect(res.Edges())
	if len(edges) != 1 {
		t.Fatalf("edge count: got %d, want 1", len(edges))
	}
	if edges[0].Relation() != "EMPLOYER" ||
		edges[0].Source().PrimaryKey().String() != `["bob"]` ||
		edges[0].Target().PrimaryKey().String() != `["acme"]` {
		t.Errorf(`edge: got %s %s->%s, want employer ["bob"]->["acme"]`,
			edges[0].Relation(), edges[0].Source().PrimaryKey().String(), edges[0].Target().PrimaryKey().String())
	}

	// The source snapshot is independent of the resumed batch.
	if got := len(slices.Collect(seed.InstancesOf(mustTypeID(t, s, "Person")))); got != 1 {
		t.Errorf("seed Person count mutated: got %d, want 1", got)
	}
	if got := len(slices.Collect(seed.Edges())); got != 0 {
		t.Errorf("seed edge count mutated: got %d, want 0", got)
	}
}

// -----------------------------------------------------------------------------
// Test 17: A new Add through the assembler resolves an unresolved REQUIRED
// edge imported from the seed, so Check succeeds.
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
	if got := len(slices.Collect(seed.Unresolved())); got != 1 {
		t.Fatalf("seed unresolved: got %d, want 1", got)
	}

	ba := mustSeed(t, ctx, s, seed)
	if err := ba.Add("Company", companyRaw("acme", "ACME")); err != nil {
		t.Fatalf("Add(acme): %v", err)
	}

	res, err := checkedSnapshot(ctx, ba)
	if err != nil {
		t.Fatalf("Check: %v (the resumed batch supplied the missing target)", err)
	}
	if got := len(slices.Collect(res.Edges())); got != 1 {
		t.Errorf("edge count: got %d, want 1 (seeded pending edge resolved by new Add)", got)
	}
	if got := len(slices.Collect(res.Unresolved())); got != 0 {
		t.Errorf("unresolved count: got %d, want 0", got)
	}
}

// -----------------------------------------------------------------------------
// Test 18: An unresolved REQUIRED edge imported from the seed and never
// completed fails Check, which covers seeded and new state alike.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_SeededUnresolvedFailsCheck(t *testing.T) {
	s := batchAssemblerRequiredEmployerSchema(t)
	ctx := t.Context()

	seed := buildSeedSnapshot(
		t, s,
		seedRecord{"Person", personRawWithEmployer("alice", "Alice", "missing")},
	)

	// Add nothing: the imported unresolved REQUIRED edge must fail Check.
	ba := mustSeed(t, ctx, s, seed)
	res := ba.Graph().Check(ctx)
	if !res.HasCode(diag.E_UNRESOLVED_REQUIRED) {
		t.Errorf("expected E_UNRESOLVED_REQUIRED from the seeded association; got: %s", res.String())
	}
	if got := len(slices.Collect(ba.Graph().Snapshot().Unresolved())); got != 1 {
		t.Errorf("unresolved count on the snapshot: got %d, want 1", got)
	}
}

// -----------------------------------------------------------------------------
// Test 19: A new Add colliding with a seeded (type, PK) is rejected as a
// duplicate; the rejection is recorded on the snapshot, not by Check.
// -----------------------------------------------------------------------------

func TestNewBatchAssemblerFromSnapshot_DuplicateAgainstSeeded(t *testing.T) {
	s := batchAssemblerTestSchema(t)
	ctx := t.Context()

	seed := buildSeedSnapshot(
		t, s,
		seedRecord{"Person", personRaw("alice", "Alice")},
	)

	ba := mustSeed(t, ctx, s, seed)
	err := ba.Add("Person", personRaw("alice", "Alice The Second"))
	if err == nil {
		t.Fatal("expected duplicate-PK error against seeded instance")
	}
	var ce *diag.ContextualError
	if !errors.As(err, &ce) {
		t.Fatalf("error is not *diag.ContextualError: %T", err)
	}
	// Attempt numbering starts fresh after seeding: this is attempt #1.
	if want := "Person (attempt #1)"; ce.Tag != want {
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

	// Duplicates do not gate the batch (Check covers unresolved-required
	// only); the rejection is recorded on the snapshot instead.
	res, err := checkedSnapshot(ctx, ba)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if got := len(slices.Collect(res.InstancesOf(mustTypeID(t, s, "Person")))); got != 1 {
		t.Errorf("Person count: got %d, want 1 (seeded alice only)", got)
	}
	if got := len(slices.Collect(res.Duplicates())); got != 1 {
		t.Errorf("Duplicates count: got %d, want 1", got)
	}
	// The seed contributed no construction diagnostics; the duplicate from
	// THIS batch is the only issue on the snapshot.
	if !res.Diagnostics().HasErrors() {
		t.Error("expected E_DUPLICATE_PK on snapshot diagnostics")
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
	g := mustImport(t, s, seed)
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

	{
		t.Run("default", func(t *testing.T) {
			ba := mustSeed(t, ctx, s, seed)
			for _, rec := range resumeRecords {
				if err := ba.Add(rec.typeName, rec.raw); err != nil {
					t.Fatalf("Add(%s): %v", rec.typeName, err)
				}
			}
			res, err := checkedSnapshot(ctx, ba)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			snapshottest.DiffSnapshots(t, manualSnap, res)
			asmBytes, asmRes := snapshot.Marshal(ctx, res)
			if asmRes.HasErrors() {
				t.Fatalf("marshal: %s", asmRes.String())
			}
			if string(manualBytes) != string(asmBytes) {
				t.Errorf("marshal bytes differ from manual path (manual=%d, assembler=%d)",
					len(manualBytes), len(asmBytes))
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
	res1, err := checkedSnapshot(ctx, ba1)
	if err != nil {
		t.Fatalf("run 1: Check: %v", err)
	}
	data, mRes := snapshot.Marshal(ctx, res1)
	if mRes.HasErrors() {
		t.Fatalf("run 1: marshal: %s", mRes.String())
	}

	// Run 2: load the persisted snapshot and resume on top of it.
	loaded, lRes := snapshot.Load(ctx, data, s)
	if lRes.HasErrors() {
		t.Fatalf("run 2: load: %s", lRes.String())
	}
	if !loaded.Diagnostics().OK() {
		t.Fatalf("run 2: loaded snapshot diagnostics not OK: %s", loaded.Diagnostics().String())
	}

	ba2 := mustSeed(t, ctx, s, loaded)
	if err := ba2.Add("Person", personRawWithEmployer("bob", "Bob", "acme")); err != nil {
		t.Fatalf("run 2: Add(bob): %v", err)
	}
	if got := ba2.Count(); got != 1 {
		t.Errorf("run 2: Count(): got %d, want 1", got)
	}
	res2, err := checkedSnapshot(ctx, ba2)
	if err != nil {
		t.Fatalf("run 2: Check: %v", err)
	}

	if got := len(slices.Collect(res2.InstancesOf(mustTypeID(t, s, "Person")))); got != 2 {
		t.Errorf("Person count: got %d, want 2", got)
	}
	if got := len(slices.Collect(res2.InstancesOf(mustTypeID(t, s, "Company")))); got != 1 {
		t.Errorf("Company count: got %d, want 1", got)
	}
	if got := len(slices.Collect(res2.Edges())); got != 2 {
		t.Errorf("edge count: got %d, want 2 (alice edge survives the round trip; bob edge resolves against the loaded instance)", got)
	}

	// The resumed batch's snapshot persists cleanly in turn.
	if _, mRes2 := snapshot.Marshal(ctx, res2); mRes2.HasErrors() {
		t.Fatalf("run 2: marshal: %s", mRes2.String())
	}
}

// -----------------------------------------------------------------------------
// helpers
// -----------------------------------------------------------------------------

// _ keeps the unused-import linter quiet on context for subtests that
// happen to not use it. The package-level imports are all genuinely used,
// but referencing context here documents the binding to caller intent.
var _ = context.Background
