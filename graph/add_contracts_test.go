package graph_test

import (
	"context"
	"math"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

// A cancelled Add returns its cancellation and installs nothing, so a graph a
// cancellation interrupted holds only what earlier calls installed.
func TestAdd_ACancelledContextInstallsNothing(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	g := graph.New(s)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	inst := instancetest.Valid(t, s, "Parent", map[string]any{"id": "p1", "name": "Parent 1"})
	res := g.Add(ctx, inst)
	if !res.HasFatal() || !res.HasCode(diag.E_CONTEXT_CANCELLED) {
		t.Fatalf("a cancelled Add returned %s", res.String())
	}
	if n := len(slices.Collect(g.Snapshot().AllInstances())); n != 0 {
		t.Errorf("a cancelled Add installed %d instances", n)
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
