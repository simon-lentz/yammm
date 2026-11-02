package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

// The spellings every pin below uses: one instant, two renderings, of which
// the second is the canonical one.
const (
	rawStamp   = "2020-01-02T03:04:05+00:00"
	canonStamp = "2020-01-02T03:04:05Z"
)

const keyedDuplicateSource = `schema "keyed_duplicates"

type Sensor {
	observed_at Timestamp primary
	*-> READINGS (_:many) Reading
}

part type Reading {
	taken_at Timestamp primary
	note String
}
`

// keyedDuplicateSchema declares a canonicalizing key at a root and at a keyed
// composed child, which is what the duplicate records address.
func keyedDuplicateSchema(t *testing.T) schema.Schema {
	t.Helper()
	return loadSchemaAt(t, keyedDuplicateSource, "keyed_duplicates.yammm")
}

// TestAdd_RootDuplicateRecordCarriesTheCanonicalAddress pins the rejected
// duplicate's key and properties: the record renders the canonical address,
// whichever spelling the caller handed the validator.
func TestAdd_RootDuplicateRecordCarriesTheCanonicalAddress(t *testing.T) {
	t.Parallel()
	s := keyedDuplicateSchema(t)

	sensor := func(spelling string) instance.ValidInstance {
		return instancetest.Valid(t, s, "Sensor", map[string]any{"observed_at": spelling})
	}

	g := graph.New(s)
	if r := g.Add(t.Context(), sensor(canonStamp)); !r.OK() {
		t.Fatalf("first add: %s", r)
	}
	if r := g.Add(t.Context(), sensor(rawStamp)); r.OK() {
		t.Fatal("a second spelling of one address was accepted as a second instance")
	}

	dups := slices.Collect(g.Snapshot().Duplicates())
	if len(dups) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(dups))
	}
	rejected := dups[0].Instance()
	if got, want := rejected.PrimaryKey().String(), graph.FormatKey(canonStamp); got != want {
		t.Errorf("rejected duplicate key = %s, want the canonical %s", got, want)
	}
	v, ok := rejected.Properties().Get("observed_at")
	if !ok {
		t.Fatal(`rejected duplicate has no "observed_at" property`)
	}
	if got := v.Unwrap(); got != canonStamp {
		t.Errorf("rejected duplicate property observed_at = %v, want the canonical %s", got, canonStamp)
	}
}

// TestRebuild_DuplicateRecordResolvesFromEverySpelling pins that a duplicate
// record spelled raw still resolves its conflict, because its key is rewritten
// before the index is read. Reverting the rewrite turns the lookup into a miss.
func TestRebuild_DuplicateRecordResolvesFromEverySpelling(t *testing.T) {
	t.Parallel()
	s := keyedDuplicateSchema(t)
	sensorID := mustTypeID(t, s, "Sensor")

	snap := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{sensorID},
		Instances: []snapparts.Instance{
			{
				TypeID:     sensorID,
				PrimaryKey: immutable.WrapKey([]any{canonStamp}),
				Properties: immutable.WrapProperties(map[string]any{"observed_at": canonStamp}),
			},
		},
		// Every address in the record is spelled RAW.
		Duplicates: []snapparts.Instance{{
			TypeID:     sensorID,
			PrimaryKey: immutable.WrapKey([]any{rawStamp}),
			Properties: immutable.WrapProperties(map[string]any{"observed_at": rawStamp}),
		}},
	})

	dups := slices.Collect(snap.Duplicates())
	if len(dups) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(dups))
	}
	d := dups[0]
	if d.Conflict().IsZero() {
		t.Error("the conflict did not resolve from the raw spelling")
	} else if got, want := d.Conflict().PrimaryKey().String(), graph.FormatKey(canonStamp); got != want {
		t.Errorf("conflict key = %s, want %s", got, want)
	}
	if got, want := d.Instance().PrimaryKey().String(), graph.FormatKey(canonStamp); got != want {
		t.Errorf("rejected instance key = %s, want the canonical %s", got, want)
	}
}

// TestRebuild_EdgePropertyAloneKeepsTheCanonicalizerActive pins the term edge
// properties contribute to the canonicalizer's inactive flag. Every key and every
// instance property here is a String; the one canonicalizing kind in the schema is
// an EDGE property, so dropping that term from the flag makes the canonicalizer
// inactive and the edge property arrives raw.
func TestRebuild_EdgePropertyAloneKeepsTheCanonicalizerActive(t *testing.T) {
	t.Parallel()
	const src = `schema "edge_only_canon"

type Note {
	id String primary
}

type Doc {
	id String primary
	--> CITES (_) Note {
		seen_at Timestamp
	}
}
`
	s, res := schema.LoadString(t.Context(), src, "edge_only_canon.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	docID, noteID := mustTypeID(t, s, "Doc"), mustTypeID(t, s, "Note")

	snap := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{docID, noteID},
		Instances: []snapparts.Instance{
			{
				TypeID:     docID,
				PrimaryKey: immutable.WrapKey([]any{"d1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "d1"}),
			},
			{
				TypeID:     noteID,
				PrimaryKey: immutable.WrapKey([]any{"n1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "n1"}),
			},
		},
		Edges: []snapparts.Edge{{
			Relation:   "CITES",
			SourceType: docID, SourceKey: immutable.WrapKey([]any{"d1"}),
			TargetKey:  immutable.WrapKey([]any{"n1"}),
			Properties: immutable.WrapProperties(map[string]any{"seen_at": rawStamp}),
		}},
	})

	edges := slices.Collect(snap.Edges())
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	v, ok := edges[0].Properties().Get("seen_at")
	if !ok {
		t.Fatal(`the edge carries no "seen_at" property`)
	}
	if got := v.Unwrap(); got != canonStamp {
		t.Errorf("edge property seen_at = %v, want the canonical %s", got, canonStamp)
	}
}
