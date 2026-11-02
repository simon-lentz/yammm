package graph_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
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

	inst := instancetest.Valid(t, s, "Parent", map[string]any{"id": "p1", "name": "Parent 1"})
	res := g.Add(ctx, inst)
	if !res.HasFatal() {
		t.Fatalf("a cancelled Add returned %s", res.String())
	}

	diags := g.Snapshot().Diagnostics()
	if !diags.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Fatalf("Snapshot.Diagnostics() hid the cancellation: %s", diags.String())
	}
}

// TestBatchAssembler_AddValid_ZeroIsAPerRecordError pins the documented
// consumer loop: instance.Validator.Validate returns a nil entry for each
// refused row, and feeding one back must not crash the batch.
func TestBatchAssembler_AddValid_ZeroIsAPerRecordError(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	ba := graph.NewBatchAssembler(t.Context(), s)

	err := ba.AddValid(instance.ValidInstance{})
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
	if strings.HasPrefix(ce.Tag, " ") || !strings.HasPrefix(ce.Tag, "zero-instance (attempt #") {
		t.Errorf("tag = %q, want a single token before the ordinal", ce.Tag)
	}
	// The record is absent, not mis-keyed.
	if !ce.Result.HasCode(diag.E_INTERNAL) {
		t.Errorf("want E_INTERNAL, got %s", ce.Result.String())
	}
	// Every other Add rejection reaches the snapshot; this one must too, or a
	// caller that discards per-record errors has no record of the loss.
	if !ba.Graph().Snapshot().Diagnostics().HasCode(diag.E_INTERNAL) {
		t.Error("the zero-instance rejection never reached Snapshot.Diagnostics()")
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
	withSpan := func() instance.ValidInstance {
		return instancetest.ValidRaw(t, s, "Parent", instance.RawInstance{
			Properties: map[string]any{"id": "p1", "name": "Parent 1"},
			Provenance: location.NewProvenance("people.json", path.Root().Key("people").Index(0), span),
		})
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

// TestAdd_ComposedRecordAttestsValues pins that a record with a composed child
// keeps the Values attestation true.
func TestAdd_ComposedRecordAttestsValues(t *testing.T) {
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

	valid := instancetest.Valid(t, s, "Car", map[string]any{
		"vin":    "v1",
		"wheels": []any{map[string]any{"position": "left"}},
	})
	g := graph.New(s)
	if r := g.Add(ctx, valid); !r.OK() {
		t.Fatalf("add: %s", r.String())
	}
	if att := g.Snapshot().Attestation(); !att.Values {
		t.Fatal("a fully validated record did not attest Values")
	}
}

// TestCheckStoredKey_FastPathAgreesWithTheRendering pins the kinds the scalar
// fast path may settle. Go equality and the canonical key rendering disagree on
// float64 (-0.0 == 0.0 but [-0] != [0]) and on nil (a typed nil is not == to an
// untyped one, but both render [null]), so neither may short-circuit. The
// schema layer allows only String, UUID, Date and Timestamp primary keys, so a
// float64 component reaches the check through an import of a rebuilt snapshot.
func TestCheckStoredKey_FastPathAgreesWithTheRendering(t *testing.T) {
	t.Parallel()
	negZero := math.Copysign(0, -1)
	if graph.FormatKey(negZero) == graph.FormatKey(float64(0)) {
		t.Skip("this toolchain renders -0.0 and 0.0 alike; the case cannot arise")
	}

	// Key 0.0 against property -0.0: the rendering separates them, so the key
	// is forged even though Go equality would call the pair equal.
	if res := importCar(t, float64(0), negZero); res.OK() {
		t.Error("a 0.0 key against a -0.0 property was accepted; the canonical rendering separates them")
	} else {
		assertHasCode(t, res, diag.E_GRAPH_INVALID_PK)
	}

	// The matching case still passes, so the guard is not simply refusing floats.
	if res := importCar(t, float64(1.5), float64(1.5)); !res.OK() {
		t.Errorf("a matching float key was refused: %s", res.String())
	}
}

// TestDiagnosticDetails_CarryBareValues pins two detail surfaces. A consumer
// matches on details, not wording, so a
// detail must carry a value it can compare: the graph's canonical tag form for
// a type name, and an unquoted name for expected / got.
func TestDiagnosticDetails_CarryBareValues(t *testing.T) {
	t.Parallel()
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
	// instance carries: a validator over the imported schema names the type
	// bare, and the importing graph qualifies it.
	main, common := testMultiSchemaSetup(t)
	mislabelled := instancetest.Valid(t, common, "Entity", map[string]any{"id": "e1", "name": "n"})
	g := graph.New(main)
	g.Add(ctx, mislabelled)
	res := g.Add(ctx, mislabelled)
	if got := detail(res, diag.E_DUPLICATE_PK, diag.DetailKeyTypeName); got != "c.Entity" {
		t.Errorf("type_name = %q, want the canonical tag form %q", got, "c.Entity")
	}
}
