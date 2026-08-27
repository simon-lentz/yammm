package snapshot_test

import (
	"bytes"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const attWireSchema = `schema "attwire"

type Thing {
	id String primary
}
`

// attWireFixture marshals one validator-built Thing and returns the schema
// and document bytes; the document attests {true, true}.
func attWireFixture(t *testing.T) (*schema.Schema, []byte) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), attWireSchema, "attwire.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	v := instance.NewValidator(s)
	vi, vres := v.ValidateOne(t.Context(), "Thing", instance.RawInstance{Properties: map[string]any{"id": "t1"}})
	if vres.HasErrors() {
		t.Fatalf("ValidateOne: %s", vres.String())
	}
	g := graph.New(s)
	if r := g.Add(t.Context(), vi); !r.OK() {
		t.Fatalf("Add: %s", r.String())
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres.String())
	}
	return s, data
}

const attTrueSegment = `,"attestation":{"values":true,"associations":true}`

func TestAttestation_ValidatorBuiltRoundTripsTrue(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)

	if !bytes.Contains(data, []byte(attTrueSegment)) {
		t.Fatalf("marshalled header does not carry the attestation: %s", data)
	}

	snap, res := snapshot.Load(t.Context(), data, s)
	if res.HasErrors() {
		t.Fatalf("Load: %s", res.String())
	}
	if att := snap.Attestation(); !att.Values || !att.Associations {
		t.Fatalf("loaded snapshot attests %+v, want both true", att)
	}

	// The claim rides the header; the rebuilt instances stay unproven.
	thing, _ := s.Type("Thing")
	insts := snap.InstancesOf(thing.ID())
	if len(insts) != 1 || insts[0].Validated() {
		t.Fatal("a loaded instance reports Validated; the wire carries a claim, not a proof")
	}
}

func TestAttestation_BypassBuiltRoundTripsFalse(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), attWireSchema, "attwire.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	thing, _ := s.Type("Thing")

	g := graph.New(s)
	vi := instancetest.VI(
		"Thing",
		instancetest.TypeID(thing.ID()),
		instancetest.PK("t1"),
		instancetest.Props(map[string]any{"id": "t1"}),
	)
	if r := g.Add(t.Context(), vi); !r.OK() {
		t.Fatalf("Add: %s", r.String())
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres.String())
	}
	if !bytes.Contains(data, []byte(`"attestation":{"values":false`)) {
		t.Fatal("a bypass-built document did not write values=false")
	}

	snap, lres := snapshot.Load(t.Context(), data, s)
	if lres.HasErrors() {
		t.Fatalf("Load: %s", lres.String())
	}
	if snap.Attestation().Values {
		t.Fatal("a bypass-built document loaded with Values true")
	}
}

func TestAttestation_HeaderOnlySeesTheClaim(t *testing.T) {
	t.Parallel()
	_, data := attWireFixture(t)

	header, res := snapshot.HeaderOnly(t.Context(), data)
	if res.HasErrors() {
		t.Fatalf("HeaderOnly: %s", res.String())
	}
	if header.Attestation == nil || !header.Attestation.Values || !header.Attestation.Associations {
		t.Fatalf("HeaderOnly attestation = %+v, want both true", header.Attestation)
	}
}

func TestUpdateMetadata_PreservesTheAttestation(t *testing.T) {
	t.Parallel()
	_, data := attWireFixture(t)

	out, res := snapshot.UpdateMetadata(t.Context(), data, map[string]string{"k": "v"})
	if res.HasErrors() {
		t.Fatalf("UpdateMetadata: %s", res.String())
	}
	if !bytes.Contains(out, []byte(attTrueSegment)) {
		t.Fatal("UpdateMetadata dropped the attestation field")
	}
}

func TestUpdateMetadata_DoesNotFabricateAnAttestation(t *testing.T) {
	t.Parallel()
	_, data := attWireFixture(t)

	// A pre-v0.15.0 document: the same bytes with the attestation removed.
	// UpdateMetadata never verifies integrity, so the edit is safe here.
	stripped := bytes.Replace(data, []byte(attTrueSegment), nil, 1)
	if bytes.Equal(stripped, data) {
		t.Fatal("fixture did not carry the attestation segment")
	}

	out, res := snapshot.UpdateMetadata(t.Context(), stripped, map[string]string{"k": "v"})
	if res.HasErrors() {
		t.Fatalf("UpdateMetadata: %s", res.String())
	}
	if bytes.Contains(out, []byte(`"attestation"`)) {
		t.Fatal("UpdateMetadata fabricated an attestation on a pre-v0.15.0 document")
	}
}

func TestLoad_AbsentAttestationReadsFalse(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)

	stripped := bytes.Replace(data, []byte(attTrueSegment), nil, 1)
	snap, res := snapshot.Load(t.Context(), stripped, s, snapshot.WithIntegrityCheck(false))
	if res.HasErrors() {
		t.Fatalf("Load: %s", res.String())
	}
	if att := snap.Attestation(); att.Values || att.Associations {
		t.Fatalf("an absent attestation loaded as %+v, want both false", att)
	}
}
