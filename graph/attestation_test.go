package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
)

func TestSnapshotAttestation_ValidatorBuiltAttestsTrue(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t)
	g := graph.New(s)

	mustOK(t, g.Add(t.Context(), instancetest.Valid(t, s, "Company", map[string]any{"id": "c1", "name": "n"})))
	mustOK(t, g.Add(t.Context(), instancetest.Valid(t, s, "Person", map[string]any{
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
	insts := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Company")))
	if len(insts) != 1 || !insts[0].Validated() {
		t.Fatal("cloned snapshot instance lost its Validated bit")
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
	mustOK(t, g.Add(t.Context(), instancetest.Valid(t, s, "Person", map[string]any{"id": "p1", "name": "n"})))

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
	mustOK(t, g1.Add(t.Context(), instancetest.Valid(t, s, "Company", map[string]any{"id": "c1", "name": "n"})))
	g2 := mustImport(t, s, g1.Snapshot())
	mustOK(t, g2.Add(t.Context(), instancetest.Valid(t, s, "Company", map[string]any{"id": "c2", "name": "n"})))
	if att := g2.Snapshot().Attestation(); !att.Values {
		t.Fatalf("seed from a true claim plus validated adds attests %+v, want Values true", att)
	}

	// A false claim poisons the accumulator: later validated adds cannot
	// launder the unproven imported data.
	unproven := rebuild(s, snapparts.Parts{
		Instances: []snapparts.Instance{{
			TypeID:     mustTypeID(t, s, "Company"),
			PrimaryKey: immutable.WrapKey([]any{"c3"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "c3", "name": "n"}),
		}},
		Attestation: &snapparts.Attestation{Values: false, Associations: true},
	})
	g4 := mustImport(t, s, unproven)
	mustOK(t, g4.Add(t.Context(), instancetest.Valid(t, s, "Company", map[string]any{"id": "c4", "name": "n"})))
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
