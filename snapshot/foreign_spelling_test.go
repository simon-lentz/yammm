package snapshot_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const (
	canonicalStamp = "2020-01-02T03:04:05Z"
	rawStamp       = "2020-01-02T03:04:05+00:00"
)

// foreignSpellingSchema declares a Timestamp key at the root and at a keyed
// composed child, plus an optional association, so one document can carry a
// duplicate record and an unresolved-edge record whose addresses are all
// canonicalizing.
func foreignSpellingSchema(t *testing.T) schema.Schema {
	t.Helper()
	const src = `schema "foreign_spelling"

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
	s, res := schema.LoadString(t.Context(), src, "foreign_spelling.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestLoad_RecordAddressesResolveFromAForeignSpelling pins the reader's
// record-addressing sites (#22): a duplicate record's conflict address and an
// unresolved record's source address are each canonicalized before the instance
// index is read. A foreign writer that spelled the shared instant the other way
// still resolves against a document the graph indexed canonically; reverting
// either turns its lookup into a dangling reference.
func TestLoad_RecordAddressesResolveFromAForeignSpelling(t *testing.T) {
	t.Parallel()
	s := foreignSpellingSchema(t)
	// The association names a Station that is never added, so the snapshot
	// carries an unresolved record whose SOURCE is the Timestamp-keyed root.
	sensor := instancetest.Valid(t, s, "Sensor", map[string]any{
		"observed_at": canonicalStamp,
		"at":          map[string]any{"_target_id": "missing"},
		"readings":    []any{map[string]any{"taken_at": canonicalStamp}},
	})

	g := graph.New(s)
	if r := g.Add(t.Context(), sensor); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	// A second root at the same address makes a duplicate record, which carries
	// a conflict address.
	if r := g.Add(t.Context(), instancetest.Valid(t, s, "Sensor", map[string]any{"observed_at": canonicalStamp})); r.OK() {
		t.Fatal("a duplicate root was accepted")
	}

	snap := g.Snapshot()
	if len(slices.Collect(snap.Duplicates())) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(slices.Collect(snap.Duplicates())))
	}
	if len(slices.Collect(snap.Unresolved())) != 1 {
		t.Fatalf("unresolved = %d, want 1", len(slices.Collect(snap.Unresolved())))
	}

	data, mr := snapshot.Marshal(t.Context(), snap)
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	// Respell every rendering of the instant the other way, as a foreign
	// writer would. The reader canonicalizes each address it reads back.
	doc := rehashDocument(t, bytes.ReplaceAll(data, []byte(canonicalStamp), []byte(rawStamp)))

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
