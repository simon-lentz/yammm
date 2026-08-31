package graph_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
)

// TestAdd_ContextCancelled_ReachesSnapshotDiagnostics pins that a cancelled
// construction is visible on the snapshot. Every other Add rejection is
// merged into the cumulative collector; a cancellation that was not left a
// half-built graph reporting diag.OK() construction diagnostics.
func TestAdd_ContextCancelled_ReachesSnapshotDiagnostics(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	g := graph.New(s)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	inst := instancetest.VI(
		"Parent",
		instancetest.TypeID(mustTypeID(t, s, "Parent")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
	)
	res := g.Add(ctx, inst)
	if !res.HasFatal() {
		t.Fatalf("a cancelled Add returned %s", res.String())
	}

	diags := g.Snapshot().Diagnostics()
	if !diags.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Fatalf("Snapshot.Diagnostics() hid the cancellation: %s", diags.String())
	}
}

// TestAddComposed_ContextCancelled_ReachesSnapshotDiagnostics is Add's case on
// the composed path.
func TestAddComposed_ContextCancelled_ReachesSnapshotDiagnostics(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	g := graph.New(s)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	child := instancetest.VI(
		"Child",
		instancetest.TypeID(mustTypeID(t, s, "Child")),
		instancetest.PK("c1"),
		instancetest.Props(map[string]any{"id": "c1"}),
	)
	if res := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), `["p1"]`, "children", child); !res.HasFatal() {
		t.Fatalf("a cancelled AddComposed returned %s", res.String())
	}
	if !g.Snapshot().Diagnostics().HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Fatal("Snapshot.Diagnostics() hid the cancellation")
	}
}

// TestAddComposed_OneDuplicate_NamesTheAddressTheWritersAssign pins the
// composed-key detail against the writers' own rule: a keyed child is
// addressed by its key, so an operator querying by the reported primary_key
// finds the node.
func TestAddComposed_OneDuplicate_NamesTheAddressTheWritersAssign(t *testing.T) {
	t.Parallel()
	s := testSchemaWithOneComposition(t) // Parent -> (one) Child, Child has a PK
	g := graph.New(s)
	ctx := t.Context()

	parent := instancetest.VI(
		"Parent",
		instancetest.TypeID(mustTypeID(t, s, "Parent")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
	)
	if res := g.Add(ctx, parent); !res.OK() {
		t.Fatalf("parent add: %s", res.String())
	}

	child := func(id string) *instance.ValidInstance {
		return instancetest.VI(
			"Child",
			instancetest.TypeID(mustTypeID(t, s, "Child")),
			instancetest.PK(id),
			instancetest.Props(map[string]any{"id": id}),
		)
	}
	if res := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), `["p1"]`, "child", child("c1")); !res.OK() {
		t.Fatalf("first child: %s", res.String())
	}

	res := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), `["p1"]`, "child", child("c2"))
	if res.OK() {
		t.Fatal("a second child on a (one) composition was accepted")
	}

	want, err := graph.FormatComposedKey([]any{"p1"}, "child", []any{"c2"})
	if err != nil {
		t.Fatalf("FormatComposedKey: %v", err)
	}
	var got string
	for issue := range res.Issues() {
		if issue.Code() != diag.E_DUPLICATE_COMPOSED_PK {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyPrimaryKey {
				got = d.Value
			}
		}
	}
	if got != want {
		t.Errorf("primary_key detail = %q, want %q", got, want)
	}
}

// TestBatchAssembler_AddValid_NilIsAPerRecordError pins the documented
// consumer loop: instance.Validator.ValidateForComposition returns nil for
// each failed child, and feeding one back must not crash the batch.
func TestBatchAssembler_AddValid_NilIsAPerRecordError(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	ba := graph.NewBatchAssembler(t.Context(), s)

	err := ba.AddValid(nil)
	if err == nil {
		t.Fatal("AddValid(nil) returned no error")
	}
	if ba.Count() != 0 {
		t.Fatalf("a failed AddValid counted as a success (Count=%d)", ba.Count())
	}
	if _, ok := diag.AsContextualError(err, ""); !ok {
		t.Fatalf("AddValid(nil) error is not a *diag.ContextualError: %v", err)
	}
}

// TestBatchAssembler_ErrorTagNamesTheAttemptOrdinal pins what the tag claims.
// The ordinal counts calls across every goroutine sharing the assembler, so
// naming it a record index would promise a row number nothing can supply.
func TestBatchAssembler_ErrorTagNamesTheAttemptOrdinal(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	ba := graph.NewBatchAssembler(t.Context(), s)

	if err := ba.Add("Parent", instance.RawInstance{Properties: map[string]any{"id": "p1", "name": "Parent 1"}}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := ba.Add("Parent", instance.RawInstance{Properties: map[string]any{"id": "p1", "name": "Parent 1"}})
	if err == nil {
		t.Fatal("a duplicate primary key was accepted")
	}
	ce, ok := diag.AsContextualError(err, "")
	if !ok {
		t.Fatalf("not a contextual error: %v", err)
	}
	if !strings.Contains(ce.Tag, "attempt #2") {
		t.Errorf("tag = %q, want it to name attempt #2", ce.Tag)
	}
}

// TestBatchAssembler_Finalize_SecondCallIsMemoized pins the documented
// contract: the second call returns the first call's result, so a later
// cancelled context cannot turn a finished batch into a failed one.
func TestBatchAssembler_Finalize_SecondCallIsMemoized(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	ba := graph.NewBatchAssembler(t.Context(), s)

	if err := ba.Add("Parent", instance.RawInstance{Properties: map[string]any{"id": "p1", "name": "Parent 1"}}); err != nil {
		t.Fatalf("add: %v", err)
	}

	first, err := ba.Finalize(t.Context())
	if err != nil {
		t.Fatalf("first Finalize: %v", err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	second, err := ba.Finalize(cancelled)
	if err != nil {
		t.Fatalf("second Finalize with a cancelled context: %v", err)
	}
	if second.Snapshot != first.Snapshot {
		t.Error("the second Finalize re-took the snapshot")
	}
}

// TestNewFromSnapshot_ComposedDuplicate_KeepsItsComposingCoordinates pins the
// import path's parent and relation: dropping them demotes a composed
// duplicate to a root duplicate, and the re-marshalled document then loses
// the slot the record was rejected from.
func TestNewFromSnapshot_ComposedDuplicate_KeepsItsComposingCoordinates(t *testing.T) {
	t.Parallel()
	s := testSchemaWithOneComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	parent := instancetest.VI(
		"Parent",
		instancetest.TypeID(mustTypeID(t, s, "Parent")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1"}),
	)
	if res := g.Add(ctx, parent); !res.OK() {
		t.Fatalf("parent add: %s", res.String())
	}
	child := func(id string) *instance.ValidInstance {
		return instancetest.VI(
			"Child",
			instancetest.TypeID(mustTypeID(t, s, "Child")),
			instancetest.PK(id),
			instancetest.Props(map[string]any{"id": id}),
		)
	}
	if res := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), `["p1"]`, "child", child("c1")); !res.OK() {
		t.Fatalf("first child: %s", res.String())
	}
	if res := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), `["p1"]`, "child", child("c2")); res.OK() {
		t.Fatal("the overflow child was accepted")
	}

	seeded := graph.NewFromSnapshot(s, g.Snapshot()).Snapshot()
	dups := seeded.Duplicates()
	if len(dups) != 1 {
		t.Fatalf("seeded snapshot carries %d duplicates, want 1", len(dups))
	}
	if dups[0].Relation != "child" {
		t.Errorf("Duplicate.Relation = %q, want \"child\"", dups[0].Relation)
	}
	if dups[0].Parent == nil || dups[0].Parent.PrimaryKey().String() != `["p1"]` {
		t.Error("the imported duplicate lost its composing parent")
	}
	if dups[0].Conflict == nil {
		t.Error("the imported duplicate lost its conflict")
	}
}

// TestAdd_DuplicatePK_CarriesTheProvenanceSpan pins the span attachment, which
// is what lets the renderer show the offending record's excerpt.
func TestAdd_DuplicatePK_CarriesTheProvenanceSpan(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	span := location.Span{
		Start: location.Position{Line: 7, Column: 3},
		End:   location.Position{Line: 7, Column: 9},
	}
	withSpan := func() *instance.ValidInstance {
		return instancetest.VI(
			"Parent",
			instancetest.TypeID(mustTypeID(t, s, "Parent")),
			instancetest.PK("p1"),
			instancetest.Props(map[string]any{"id": "p1"}),
			instancetest.Provenance(location.NewProvenance("people.json", path.Root().Key("people").Index(0), span)),
		)
	}
	if res := g.Add(ctx, withSpan()); !res.OK() {
		t.Fatalf("first add: %s", res.String())
	}
	res := g.Add(ctx, withSpan())
	if res.OK() {
		t.Fatal("the duplicate primary key was accepted")
	}
	for issue := range res.Issues() {
		if issue.Code() != diag.E_DUPLICATE_PK {
			continue
		}
		if issue.Span() != span {
			t.Errorf("E_DUPLICATE_PK span = %v, want %v", issue.Span(), span)
		}
		return
	}
	t.Fatal("no E_DUPLICATE_PK issue")
}
