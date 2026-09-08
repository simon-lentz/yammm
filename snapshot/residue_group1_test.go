package snapshot_test

import (
	"bytes"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const (
	residueCanonStamp = "2020-01-02T03:04:05Z"
	residueRawStamp   = "2020-01-02T03:04:05+00:00"
)

// residueRecordSchema declares a Timestamp key at the root and at a keyed
// composed child, plus an optional association, so one document can carry a
// composed duplicate record and an unresolved-edge record whose addresses
// are all canonicalizing.
func residueRecordSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "residue_records"

type Station {
	id String primary
}

type Sensor {
	observed_at Timestamp primary
	--> AT (_) Station
	*-> READINGS (_:many) Reading
}

part type Reading {
	taken_at Timestamp primary
}
`
	s, res := schema.LoadString(t.Context(), src, "residue_records.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestLoad_RecordAddressesResolveFromAForeignSpelling pins the reader's
// three record-addressing sites (#22): a duplicate record's conflict
// address, a duplicate record's parent address and an unresolved record's
// source address are each canonicalized before the instance index is read.
// A foreign writer that spelled the shared instant the other way still
// resolves against a document the graph indexed canonically; reverting any
// one of the three turns its lookup into a dangling reference.
func TestLoad_RecordAddressesResolveFromAForeignSpelling(t *testing.T) {
	t.Parallel()
	s := residueRecordSchema(t)
	sensorType, _ := s.Type("Sensor")
	readingType, _ := s.Type("Reading")

	reading := func() *instance.ValidInstance {
		return instance.NewValidInstance("Reading", readingType.ID(),
			immutable.WrapKey([]any{residueCanonStamp}),
			immutable.WrapProperties(map[string]any{"taken_at": residueCanonStamp}),
			nil, nil, nil)
	}
	// The association names a Station that is never added, so the snapshot
	// carries an unresolved record whose SOURCE is the Timestamp-keyed root.
	sensor := instance.NewValidInstance("Sensor", sensorType.ID(),
		immutable.WrapKey([]any{residueCanonStamp}),
		immutable.WrapProperties(map[string]any{"observed_at": residueCanonStamp}),
		map[string]*instance.ValidEdgeData{"AT": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{"missing"}), immutable.WrapProperties(nil)),
		})},
		map[string]immutable.Value{"READINGS": immutable.Wrap([]any{reading()})}, nil)

	g := graph.New(s)
	if r := g.Add(t.Context(), sensor); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	// A second child at the same address makes a COMPOSED duplicate record,
	// which carries both a parent address and a conflict address.
	if r := g.AddComposed(t.Context(), sensorType.ID(), graph.FormatKey(residueCanonStamp),
		"READINGS", reading()); r.OK() {
		t.Fatal("a duplicate sibling was accepted")
	}

	snap := g.Snapshot()
	if len(snap.Duplicates()) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(snap.Duplicates()))
	}
	if len(snap.Unresolved()) != 1 {
		t.Fatalf("unresolved = %d, want 1", len(snap.Unresolved()))
	}

	data, mr := snapshot.Marshal(t.Context(), snap)
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	// Respell every rendering of the instant the other way, as a foreign
	// writer would. The reader canonicalizes each address it reads back.
	doc := rehashDocument(t, bytes.ReplaceAll(data, []byte(residueCanonStamp), []byte(residueRawStamp)))

	if _, res := snapshot.Load(t.Context(), doc, s); res.HasErrors() {
		t.Errorf("Load of the foreign spelling: %s", res)
	}
	if res := snapshot.Verify(t.Context(), doc, s); res.HasErrors() {
		t.Errorf("Verify of the foreign spelling: %s", res)
	}
	if hasCode(snapshot.Verify(t.Context(), doc, s), diag.E_SNAPSHOT_DANGLING_REFERENCE) {
		t.Error("a record address was read without canonicalization")
	}
}
