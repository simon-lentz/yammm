package graph_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
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

	// The key must name the occupant that exists — the attached c1, not the
	// rejected c2.
	want := graph.FormatKey("c1")
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
	ce, ok := diag.AsContextualError(err, "")
	if !ok {
		t.Fatalf("AddValid(nil) error is not a *diag.ContextualError: %v", err)
	}
	// A single token before the ordinal: an empty type name would render
	// " (attempt #1)" and break a consumer splitting on the first space.
	if strings.HasPrefix(ce.Tag, " ") || !strings.HasPrefix(ce.Tag, "nil-instance (attempt #") {
		t.Errorf("tag = %q, want a single token before the ordinal", ce.Tag)
	}
	// The record is absent, not mis-keyed.
	if !ce.Result.HasCode(diag.E_INTERNAL) {
		t.Errorf("want E_INTERNAL, got %s", ce.Result.String())
	}
	// Every other Add rejection reaches the snapshot; this one must too, or a
	// caller that discards per-record errors has no record of the loss.
	if !ba.Graph().Snapshot().Diagnostics().HasCode(diag.E_INTERNAL) {
		t.Error("the nil-instance rejection never reached Snapshot.Diagnostics()")
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

// TestAdd_AttestationFoldsOverComposedDescendants pins the fold the builder
// performs over every descendant, not the root alone. A bypass-built child
// under a validator-built root must sink the Values attestation, or the
// snapshot claims every value passed validation when one did not.
func TestAdd_AttestationFoldsOverComposedDescendants(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	const src = `schema "fleet"

type Car {
	vin String primary

	*-> WHEELS (many) Wheel
}

part type Wheel {
	position String required
}
`
	s, res := schema.LoadString(ctx, src, "test://attest_composed.yammm")
	if res.HasErrors() {
		t.Fatalf("schema load: %s", res.String())
	}

	// A validated root, so the root's own bit is true.
	v := instance.NewValidator(s)
	valid, vres := v.ValidateOne(ctx, "Car", instance.RawInstance{Properties: map[string]any{
		"vin":    "v1",
		"wheels": []any{map[string]any{"position": "left"}},
	}})
	if !vres.OK() {
		t.Fatalf("validation: %s", vres.String())
	}
	g := graph.New(s)
	if r := g.Add(ctx, valid); !r.OK() {
		t.Fatalf("add: %s", r.String())
	}
	if att := g.Snapshot().Attestation(); !att.Values {
		t.Fatal("a fully validated record did not attest Values")
	}

	// The same shape with a bypass-built child under a bypass-built root:
	// every descendant's bit folds in, so the claim sinks.
	bypass := graph.New(s)
	car := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v2"),
		instancetest.Props(map[string]any{"vin": "v2"}),
		instancetest.Composed(map[string]immutable.Value{
			"WHEELS": immutable.Wrap([]any{
				instancetest.VI(
					"Wheel",
					instancetest.TypeID(mustTypeID(t, s, "Wheel")),
					instancetest.Props(map[string]any{"position": "left"}),
				),
			}),
		}),
	)
	if r := bypass.Add(ctx, car); !r.OK() {
		t.Fatalf("bypass add: %s", r.String())
	}
	if att := bypass.Snapshot().Attestation(); att.Values {
		t.Error("an unvalidated composed child left the Values attestation true")
	}
}

// TestAdd_CompositeKeyArity pins the arity guard against a key shorter than the
// type declares — a truncated address that InstanceByKey would then miss.
func TestAdd_CompositeKeyArity(t *testing.T) {
	t.Parallel()
	s := testSchemaWithCompositeKey(t) // Record: (region, id)
	g := graph.New(s)

	short := instancetest.VI(
		"Record",
		instancetest.TypeID(mustTypeID(t, s, "Record")),
		instancetest.PK("us-east"), // one component; the type declares two
		instancetest.Props(map[string]any{"value": "x"}),
	)
	res := g.Add(t.Context(), short)
	if res.OK() {
		t.Fatal("a one-component key installed under a two-component type")
	}
	assertHasCode(t, res, diag.E_GRAPH_INVALID_PK)
}

// TestCheckInstanceKey_FastPathAgreesWithTheRendering pins the kinds the scalar
// fast path may settle. Go equality and the canonical key rendering disagree on
// float64 (-0.0 == 0.0 but [-0] != [0]) and on nil (a typed nil is not == to an
// untyped one, but both render [null]), so neither may short-circuit. The
// schema layer allows only String, UUID, Date and Timestamp primary keys, so a
// float64 component reaches the check through a bypass caller alone.
func TestCheckInstanceKey_FastPathAgreesWithTheRendering(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t) // Car: vin String primary
	carID := mustTypeID(t, s, "Car")

	negZero := math.Copysign(0, -1)
	if graph.FormatKey(negZero) == graph.FormatKey(float64(0)) {
		t.Skip("this toolchain renders -0.0 and 0.0 alike; the case cannot arise")
	}

	// Key -0.0 against property 0.0: the rendering separates them, so the key
	// is forged even though Go equality would call the pair equal.
	forged := instancetest.VI(
		"Car",
		instancetest.TypeID(carID),
		instancetest.PK(negZero),
		instancetest.Props(map[string]any{"vin": float64(0)}),
	)
	if res := graph.New(s).Add(t.Context(), forged); res.OK() {
		t.Error("a -0.0 key against a 0.0 property was accepted; the canonical rendering separates them")
	} else {
		assertHasCode(t, res, diag.E_GRAPH_INVALID_PK)
	}

	// The matching case still passes, so the guard is not simply refusing floats.
	ok := instancetest.VI(
		"Car",
		instancetest.TypeID(carID),
		instancetest.PK(float64(1.5)),
		instancetest.Props(map[string]any{"vin": float64(1.5)}),
	)
	if res := graph.New(s).Add(t.Context(), ok); !res.OK() {
		t.Errorf("a matching float key was refused: %s", res.String())
	}
}

// TestAddComposed_KeylessOneSlot_NamesThePositionalAddress pins the address a
// keyless part type gets. adapter/neo4j writes `_composed_key` from the child's
// position when the part type declares no primary key, so a diagnostic naming
// the slot instead names a node that was never created.
func TestAddComposed_KeylessOneSlot_NamesThePositionalAddress(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t) // Car *-> SPARE (one) Wheel; Wheel has no primary key
	g := graph.New(s)
	ctx := t.Context()

	car := instancetest.VI(
		"Car",
		instancetest.TypeID(mustTypeID(t, s, "Car")),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
	)
	if r := g.Add(ctx, car); !r.OK() {
		t.Fatalf("car add: %s", r.String())
	}
	wheel := func(pos string) *instance.ValidInstance {
		return instancetest.VI(
			"Wheel",
			instancetest.TypeID(mustTypeID(t, s, "Wheel")),
			instancetest.Props(map[string]any{"position": pos}),
		)
	}
	carID := mustTypeID(t, s, "Car")
	if r := g.AddComposed(ctx, carID, `["v1"]`, "SPARE", wheel("left")); !r.OK() {
		t.Fatalf("first wheel: %s", r.String())
	}
	res := g.AddComposed(ctx, carID, `["v1"]`, "SPARE", wheel("right"))
	if res.OK() {
		t.Fatal("a second child on a (one) slot was accepted")
	}

	// The occupant is a keyless part, so there is no primary key to report and
	// the detail is absent rather than carrying a synthesized stand-in. The
	// type, relation and field details still locate the slot.
	want := ""
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
		t.Errorf("primary_key detail = %q, want %q — a keyless part has no key to report", got, want)
	}
}

// TestDiagnosticDetails_CarryBareValues pins the two detail surfaces the
// fix-diff round found wrong. A consumer matches on details, not wording, so a
// detail must carry a value it can compare: the graph's canonical tag form for
// a type name, and an unquoted name for expected / got.
func TestDiagnosticDetails_CarryBareValues(t *testing.T) {
	t.Parallel()
	s := loadGuardSchema(t)
	carID := mustTypeID(t, s, "Car")
	ctx := t.Context()

	detail := func(res diag.Result, code diag.Code, key string) string {
		t.Helper()
		for issue := range res.Issues() {
			if issue.Code() != code {
				continue
			}
			for _, d := range issue.Details() {
				if d.Key == key {
					return d.Value
				}
			}
		}
		return ""
	}

	// type_name must be the tag form the graph renders, not the name the
	// instance carries — they differ for a bypass-built instance.
	mislabelled := instancetest.VI(
		"NOT-THE-TAG-FORM",
		instancetest.TypeID(carID),
		instancetest.PK("v1"),
		instancetest.Props(map[string]any{"vin": "v1"}),
		instancetest.Edges(edgeData("bogus", nil, []any{"x"})),
	)
	res := graph.New(s).Add(ctx, mislabelled)
	if got := detail(res, diag.E_GRAPH_UNKNOWN_RELATION, diag.DetailKeyTypeName); got != "Car" {
		t.Errorf("type_name = %q, want the canonical tag form %q", got, "Car")
	}

	// expected / got must be bare: quoting belongs to the message.
	wrong := instancetest.VI(
		"Car",
		instancetest.TypeID(carID),
		instancetest.PK("v2"),
		instancetest.Props(map[string]any{"vin": "v2"}),
		instancetest.Composed(map[string]immutable.Value{
			"SPARE": immutable.Wrap([]any{instancetest.VI(
				"Car",
				instancetest.TypeID(carID),
				instancetest.PK("v3"),
				instancetest.Props(map[string]any{"vin": "v3"}),
			)}),
		}),
	)
	res = graph.New(s).Add(ctx, wrong)
	for _, key := range []string{diag.DetailKeyExpected, diag.DetailKeyGot} {
		got := detail(res, diag.E_GRAPH_INVALID_COMPOSITION, key)
		if got == "" {
			t.Errorf("%s detail is absent", key)
			continue
		}
		if strings.HasPrefix(got, `"`) {
			t.Errorf("%s = %s — a structured detail must carry the bare name, not a quoted one", key, got)
		}
	}
}
