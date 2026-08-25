package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// validatedInstance runs the real validator, so the result carries the
// token no bypass helper can mint.
func validatedInstance(t *testing.T, s *schema.Schema, typeName string, props map[string]any) *instance.ValidInstance {
	t.Helper()
	v := instance.NewValidator(s)
	vi, res := v.ValidateOne(t.Context(), typeName, instance.RawInstance{Properties: props})
	if res.HasErrors() {
		t.Fatalf("ValidateOne(%s): %s", typeName, res.String())
	}
	return vi
}

func TestSnapshotAttestation_ValidatorBuiltAttestsTrue(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t)
	g := graph.New(s)

	mustOK(t, g.Add(t.Context(), validatedInstance(t, s, "Company", map[string]any{"id": "c1", "name": "n"})))
	mustOK(t, g.Add(t.Context(), validatedInstance(t, s, "Person", map[string]any{
		"id":       "p1",
		"name":     "n",
		"employer": map[string]any{"_target_id": "c1"},
	})))

	snap := g.Snapshot()
	att := snap.Attestation()
	if !att.Values || !att.Associations {
		t.Fatalf("validator-built snapshot attests %+v, want both true", att)
	}

	// The per-instance bit survives the snapshot clone.
	insts := snap.InstancesOf(mustTypeID(t, s, "Company"))
	if len(insts) != 1 || !insts[0].Validated() {
		t.Fatal("cloned snapshot instance lost its Validated bit")
	}
}

func TestSnapshotAttestation_BypassFlipsValuesFalse(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t)

	ba := graph.NewBatchAssembler(t.Context(), s)
	if err := ba.AddValid(mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "n"})); err != nil {
		t.Fatalf("AddValid: %v", err)
	}
	fr, err := ba.Finalize(t.Context())
	if err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	att := fr.Snapshot.Attestation()
	if att.Values {
		t.Fatal("a bypass-built instance did not flip Values false")
	}
	insts := fr.Snapshot.InstancesOf(mustTypeID(t, s, "Company"))
	if len(insts) != 1 || insts[0].Validated() {
		t.Fatal("bypass-built snapshot instance reports Validated")
	}
}

func TestSnapshotAttestation_EmptyGraphAttestsFalse(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t)

	att := graph.New(s).Snapshot().Attestation()
	if att.Values {
		t.Fatal("an empty snapshot attests Values true; a vacuous truth must not be re-marshalled as an attestation")
	}
	if !att.Associations {
		t.Fatal("an empty snapshot has no Required unresolved record; Associations must be true")
	}
}

func TestSnapshotAttestation_RequiredUnresolvedFlipsAssociations(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t)
	g := graph.New(s)

	// The employer association is required; the validator defers its
	// enforcement to the graph, so a validated Person without one is the
	// honest-token case Associations exists for.
	mustOK(t, g.Add(t.Context(), validatedInstance(t, s, "Person", map[string]any{"id": "p1", "name": "n"})))

	att := g.Snapshot().Attestation()
	if att.Associations {
		t.Fatal("a Required unresolved record did not flip Associations false")
	}
	if !att.Values {
		t.Fatal("Values must stay true for validator-built instances")
	}
}

func TestSnapshotAttestation_SeededGraphANDsTheLoadedClaim(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t)

	// A true claim survives a seed followed by validated adds.
	g1 := graph.New(s)
	mustOK(t, g1.Add(t.Context(), validatedInstance(t, s, "Company", map[string]any{"id": "c1", "name": "n"})))
	g2 := graph.NewFromSnapshot(s, g1.Snapshot())
	mustOK(t, g2.Add(t.Context(), validatedInstance(t, s, "Company", map[string]any{"id": "c2", "name": "n"})))
	if att := g2.Snapshot().Attestation(); !att.Values {
		t.Fatalf("seed from a true claim plus validated adds attests %+v, want Values true", att)
	}

	// A false claim poisons the accumulator: later validated adds cannot
	// launder the unproven imported data.
	g3 := graph.New(s)
	mustOK(t, g3.Add(t.Context(), mustValidInstance(t, s, "Company", []any{"c3"}, map[string]any{"id": "c3", "name": "n"})))
	g4 := graph.NewFromSnapshot(s, g3.Snapshot())
	mustOK(t, g4.Add(t.Context(), validatedInstance(t, s, "Company", map[string]any{"id": "c4", "name": "n"})))
	if att := g4.Snapshot().Attestation(); att.Values {
		t.Fatal("seed from a false claim ANDed to true after a validated add")
	}
}

// mustOK fails the test on a non-OK graph result.
func mustOK(t *testing.T, res interface {
	OK() bool
	String() string
},
) {
	t.Helper()
	if !res.OK() {
		t.Fatalf("graph operation failed: %s", res.String())
	}
}
