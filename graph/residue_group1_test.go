package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// The spellings every pin below uses: one instant, two renderings, of which
// the second is the canonical one.
const (
	rawStamp   = "2020-01-02T03:04:05+00:00"
	canonStamp = "2020-01-02T03:04:05Z"
)

// residueKeyedSchema declares a canonicalizing key at a root and at a keyed
// composed child, which is what the duplicate records address.
func residueKeyedSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "residue_keyed"

type Sensor {
	observed_at Timestamp primary
	*-> READINGS (_:many) Reading
}

part type Reading {
	taken_at Timestamp primary
	note String
}
`
	s, res := schema.LoadString(t.Context(), src, "residue_keyed.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// TestAdd_RootDuplicateRecordCarriesTheCanonicalAddress pins the rejected
// duplicate's key and properties (#24): the record is built from the
// instance the graph WOULD have stored, so it renders the canonical address
// whichever spelling the caller handed in. Reverting either to the caller's
// ValidInstance turns this red.
func TestAdd_RootDuplicateRecordCarriesTheCanonicalAddress(t *testing.T) {
	t.Parallel()
	s := residueKeyedSchema(t)
	sensorID := mustTypeID(t, s, "Sensor")

	sensor := func(spelling string) *instance.ValidInstance {
		return instance.NewValidInstance("Sensor", sensorID,
			immutable.WrapKey([]any{spelling}),
			immutable.WrapProperties(map[string]any{"observed_at": spelling}),
			nil, nil, nil)
	}

	g := graph.New(s)
	if r := g.Add(t.Context(), sensor(canonStamp)); !r.OK() {
		t.Fatalf("first add: %s", r)
	}
	if r := g.Add(t.Context(), sensor(rawStamp)); r.OK() {
		t.Fatal("a second spelling of one address was accepted as a second instance")
	}

	dups := g.Snapshot().Duplicates()
	if len(dups) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(dups))
	}
	rejected := dups[0].Instance
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

// addSensorWithReading adds one Sensor carrying one Reading, both spelled
// canonically, and returns the graph.
func addSensorWithReading(t *testing.T, s *schema.Schema) *graph.Graph {
	t.Helper()
	sensorID, readingID := mustTypeID(t, s, "Sensor"), mustTypeID(t, s, "Reading")
	reading := instance.NewValidInstance("Reading", readingID,
		immutable.WrapKey([]any{canonStamp}),
		immutable.WrapProperties(map[string]any{"taken_at": canonStamp, "note": "first"}),
		nil, nil, nil)
	sensor := instance.NewValidInstance("Sensor", sensorID,
		immutable.WrapKey([]any{canonStamp}),
		immutable.WrapProperties(map[string]any{"observed_at": canonStamp}),
		nil, map[string]immutable.Value{"READINGS": immutable.Wrap([]any{reading})}, nil)

	g := graph.New(s)
	if r := g.Add(t.Context(), sensor); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	return g
}

// TestAddComposed_SiblingDuplicateReportsTheCanonicalAddress pins the
// sibling-duplicate message and its primary_key detail (G7), and the
// rejected child's own key and properties (G8). A raw-spelled sibling is the
// same address as the canonical occupant, and every position the refusal
// reports names that one address rather than the caller's spelling.
func TestAddComposed_SiblingDuplicateReportsTheCanonicalAddress(t *testing.T) {
	t.Parallel()
	s := residueKeyedSchema(t)
	sensorID, readingID := mustTypeID(t, s, "Sensor"), mustTypeID(t, s, "Reading")
	g := addSensorWithReading(t, s)

	second := instance.NewValidInstance("Reading", readingID,
		immutable.WrapKey([]any{rawStamp}),
		immutable.WrapProperties(map[string]any{"taken_at": rawStamp, "note": "second"}),
		nil, nil, nil)
	result := g.AddComposed(t.Context(), sensorID, graph.FormatKey(canonStamp), "READINGS", second)
	if result.OK() {
		t.Fatal("a second spelling of one sibling address was accepted")
	}
	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)

	wantKey := graph.FormatKey(canonStamp)
	for issue := range result.Issues() {
		if issue.Code() != diag.E_DUPLICATE_COMPOSED_PK {
			continue
		}
		// G7: the message and the primary_key detail.
		if got := issue.Message(); !strings.Contains(got, wantKey) {
			t.Errorf("message = %q, want it to name the canonical address %s", got, wantKey)
		}
		var sawPK bool
		for _, d := range issue.Details() {
			if d.Key != diag.DetailKeyPrimaryKey {
				continue
			}
			sawPK = true
			if d.Value != wantKey {
				t.Errorf("primary_key detail = %q, want the canonical %s", d.Value, wantKey)
			}
		}
		if !sawPK {
			t.Error("the refusal carries no primary_key detail")
		}
	}

	// G8: the rejected child's own key and properties.
	dups := g.Snapshot().Duplicates()
	if len(dups) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(dups))
	}
	rejected := dups[0].Instance
	if got := rejected.PrimaryKey().String(); got != wantKey {
		t.Errorf("rejected child key = %s, want the canonical %s", got, wantKey)
	}
	v, ok := rejected.Properties().Get("taken_at")
	if !ok {
		t.Fatal(`rejected child has no "taken_at" property`)
	}
	if got := v.Unwrap(); got != canonStamp {
		t.Errorf("rejected child property taken_at = %v, want the canonical %s", got, canonStamp)
	}
}

// TestRebuildSnapshot_DuplicateRecordResolvesFromEverySpelling pins two of
// the three key rewrites [canonicalizer.duplicate] makes (#25): a duplicate
// record whose conflict and parent addresses are spelled raw still resolves,
// because both are rewritten before the index is read. Reverting either
// rewrite turns the lookup into a miss and the record into a Fatal.
func TestRebuildSnapshot_DuplicateRecordResolvesFromEverySpelling(t *testing.T) {
	t.Parallel()
	s := residueKeyedSchema(t)
	sensorID, readingID := mustTypeID(t, s, "Sensor"), mustTypeID(t, s, "Reading")

	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{sensorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			sensorID: {{
				TypeName: "Sensor", TypeID: sensorID,
				PrimaryKey: immutable.WrapKey([]any{canonStamp}),
				Properties: immutable.WrapProperties(map[string]any{"observed_at": canonStamp}),
				Composed: map[string][]graph.InstanceParts{
					"READINGS": {{
						TypeName: "Reading", TypeID: readingID,
						PrimaryKey: immutable.WrapKey([]any{canonStamp}),
						Properties: immutable.WrapProperties(map[string]any{"taken_at": canonStamp, "note": "first"}),
					}},
				},
			}},
		},
		// Every address in the record is spelled RAW.
		Duplicates: []graph.DuplicateParts{{
			Type: readingID,
			Key:  immutable.WrapKey([]any{rawStamp}),
			Instance: graph.InstanceParts{
				TypeName: "Reading", TypeID: readingID,
				PrimaryKey: immutable.WrapKey([]any{rawStamp}),
				Properties: immutable.WrapProperties(map[string]any{"taken_at": rawStamp, "note": "second"}),
			},
			ConflictType: readingID,
			ConflictKey:  immutable.WrapKey([]any{rawStamp}),
			ParentType:   sensorID,
			ParentKey:    immutable.WrapKey([]any{rawStamp}),
			Relation:     "READINGS",
		}},
	})
	if res.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", res)
	}

	dups := snap.Duplicates()
	if len(dups) != 1 {
		t.Fatalf("duplicates = %d, want 1", len(dups))
	}
	d := dups[0]
	if d.Conflict == nil {
		t.Error("the conflict did not resolve from the raw spelling")
	} else if got, want := d.Conflict.PrimaryKey().String(), graph.FormatKey(canonStamp); got != want {
		t.Errorf("conflict key = %s, want %s", got, want)
	}
	if d.Parent == nil {
		t.Error("the parent did not resolve from the raw spelling")
	} else if got, want := d.Parent.PrimaryKey().String(), graph.FormatKey(canonStamp); got != want {
		t.Errorf("parent key = %s, want %s", got, want)
	}
	if got, want := d.Instance.PrimaryKey().String(), graph.FormatKey(canonStamp); got != want {
		t.Errorf("rejected instance key = %s, want the canonical %s", got, want)
	}
}

// TestRebuildSnapshot_DuplicateRefusalNamesTheCanonicalAddress pins the
// third key rewrite (#25): a duplicate record's own Key is rewritten before
// the refusal reports it, so the defense-in-depth arm names the address the
// snapshot uses rather than the caller's spelling.
func TestRebuildSnapshot_DuplicateRefusalNamesTheCanonicalAddress(t *testing.T) {
	t.Parallel()
	s := residueKeyedSchema(t)
	sensorID, readingID := mustTypeID(t, s, "Sensor"), mustTypeID(t, s, "Reading")

	// A duplicate instance carrying composed children is refused; the message
	// renders dp.Key.
	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{sensorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			sensorID: {{
				TypeName: "Sensor", TypeID: sensorID,
				PrimaryKey: immutable.WrapKey([]any{canonStamp}),
				Properties: immutable.WrapProperties(map[string]any{"observed_at": canonStamp}),
			}},
		},
		Duplicates: []graph.DuplicateParts{{
			Type: sensorID,
			Key:  immutable.WrapKey([]any{rawStamp}),
			Instance: graph.InstanceParts{
				TypeName: "Sensor", TypeID: sensorID,
				PrimaryKey: immutable.WrapKey([]any{rawStamp}),
				Properties: immutable.WrapProperties(map[string]any{"observed_at": rawStamp}),
				Composed: map[string][]graph.InstanceParts{
					"READINGS": {{TypeName: "Reading", TypeID: readingID}},
				},
			},
			ConflictType: sensorID,
			ConflictKey:  immutable.WrapKey([]any{canonStamp}),
		}},
	})
	if !res.HasFatal() {
		t.Fatal("a duplicate instance carrying composed children was admitted")
	}
	assertHasCode(t, res, diag.E_INTERNAL)
	wantKey := graph.FormatKey(canonStamp)
	var named bool
	for issue := range res.Issues() {
		if strings.Contains(issue.Message(), wantKey) {
			named = true
		}
	}
	if !named {
		t.Errorf("no refusal names the canonical address %s; messages: %s", wantKey, res)
	}
}

// TestRebuildSnapshot_EdgePropertyAloneKeepsTheCanonicalizerActive pins the
// term edge properties contribute to the canonicalizer's inactive flag
// (#26). Every key and every instance property here is a String; the one
// canonicalizing kind in the schema is an EDGE property, so dropping that
// term from the flag makes the canonicalizer inactive and the edge property
// arrives raw.
func TestRebuildSnapshot_EdgePropertyAloneKeepsTheCanonicalizerActive(t *testing.T) {
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

	snap, rres := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{docID, noteID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			docID: {{
				TypeName: "Doc", TypeID: docID,
				PrimaryKey: immutable.WrapKey([]any{"d1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "d1"}),
			}},
			noteID: {{
				TypeName: "Note", TypeID: noteID,
				PrimaryKey: immutable.WrapKey([]any{"n1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "n1"}),
			}},
		},
		Edges: []graph.EdgeParts{{
			Relation:   "CITES",
			SourceType: docID, SourceKey: immutable.WrapKey([]any{"d1"}),
			TargetType: noteID, TargetKey: immutable.WrapKey([]any{"n1"}),
			Properties: immutable.WrapProperties(map[string]any{"seen_at": rawStamp}),
		}},
	})
	if rres.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", rres)
	}

	edges := snap.Edges()
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
