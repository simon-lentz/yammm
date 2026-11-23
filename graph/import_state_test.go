package graph_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/snapshot"
)

const importStateSchema = `schema "import_state"

type T {
	id String primary
}

type R {
	id String primary
	--> O (_:one) T
	--> Q (one) T
	--> M (_:many) T
}
`

// An empty snapshot claims nothing about values, so an Add after importing one
// attests what was added.
func TestImport_AnEmptySnapshotLeavesTheNextAddsClaimStanding(t *testing.T) {
	t.Parallel()
	s := loadEntry(t, importStateSchema)
	g := graph.NewFromSnapshot(graph.New(s).Snapshot())
	if err := g.Add(t.Context(), rootInstance(t, s, "T", "a")); err != nil {
		t.Fatal(err)
	}
	if att := g.Snapshot().Attestation(); !att.Values {
		t.Errorf("attestation %+v, want Values true", att)
	}
}

// A document with no attestation claims nothing, and an Add does not claim for it.
func TestImport_ADocumentWithNoAttestationKeepsValuesUnattested(t *testing.T) {
	t.Parallel()
	s := loadEntry(t, importStateSchema)
	seed := snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: typeIDOf(t, s, "T"), PrimaryKey: immutable.WrapKey([]any{"b"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "b"}),
	}}})
	g := graph.NewFromSnapshot(seed)
	if err := g.Add(t.Context(), rootInstance(t, s, "T", "c")); err != nil {
		t.Fatal(err)
	}
	if att := g.Snapshot().Attestation(); att.Values {
		t.Errorf("attestation %+v, want Values false", att)
	}
}

// A seeded graph holds a forward reference as a target, so a second target under
// a (one) association is refused there as it is on the graph that wrote it.
func TestImport_AHeldTargetCountsTowardAOneAssociation(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := loadEntry(t, importStateSchema)
	g := graph.New(s)
	if err := g.Add(ctx, instancetest.Valid(t, s, "R", map[string]any{
		"id": "r",
		"o":  map[string]any{"_target_id": "x"},
		"m":  []any{map[string]any{"_target_id": "x"}},
	})); err != nil {
		t.Fatal(err)
	}
	seeded := graph.NewFromSnapshot(g.Snapshot())
	merged, err := seeded.AddOrMerge(ctx, instancetest.Valid(t, s, "R", map[string]any{
		"id": "r",
		"o":  map[string]any{"_target_id": "y"},
	}))
	if merged || !errors.Is(err, diag.E_GRAPH_CARDINALITY) {
		t.Errorf("merged %v, %s; want an E_GRAPH_CARDINALITY refusal", merged, err)
	}
	snap := seeded.Snapshot()
	var targets []string
	for u := range snap.Unresolved() {
		if u.Relation() == "O" {
			targets = append(targets, u.TargetKey())
		}
	}
	if !slices.Equal(targets, []string{`["x"]`}) {
		t.Errorf("O holds %q, want only x", targets)
	}
}

// The record Check reports for an absent association names the input field that
// would have filled it.
func TestCheck_AnAbsentRecordNamesItsField(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := loadEntry(t, importStateSchema)
	g := graph.New(s)
	if err := g.Add(ctx, rootInstance(t, s, "R", "r")); err != nil {
		t.Fatal(err)
	}
	res := diagtest.Failures(t, g.Check(ctx))
	found := false
	for issue := range res.Issues() {
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyJSONField {
				found = found || d.Value == "q"
			}
		}
	}
	if !found {
		t.Errorf("%s holds no %s detail naming q", res, diag.DetailKeyJSONField)
	}
}

// A duplicate came through the validator as the instance it conflicts with did, and
// says so.
func TestDuplicate_KeepsItsInstancesValidatedBit(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := loadEntry(t, importStateSchema)
	g := graph.New(s)
	if err := g.Add(ctx, rootInstance(t, s, "T", "a")); err != nil {
		t.Fatal(err)
	}
	if err := g.Add(ctx, rootInstance(t, s, "T", "a")); !errors.Is(err, diag.E_DUPLICATE_PK) {
		t.Fatalf("second Add: %s, want %s", err, diag.E_DUPLICATE_PK)
	}
	dups := slices.Collect(g.Snapshot().Duplicates())
	if len(dups) != 1 || !dups[0].Instance().Validated() {
		t.Errorf("duplicates %v, want one whose instance is validated", dups)
	}
}

// A component no constraint renders stays as written, and a component after it
// is still held to the text its kind stores: the reader refuses another
// spelling of it.
func TestLoad_JudgesTheKeyComponentsAfterOneItCannotRender(t *testing.T) {
	t.Parallel()
	s := loadEntry(t, "schema \"m\"\ntype C {\n b Timestamp primary\n c UUID primary\n}\n")
	const upper, lower = "6BA7B810-9DAD-11D1-80B4-00C04FD430C8", "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	c := typeIDOf(t, s, "C")
	parts := func(u string) snapparts.Parts {
		return snapparts.Parts{Instances: []snapparts.Instance{{
			TypeID: c, PrimaryKey: immutable.WrapKey([]any{"not a time", u}),
			Properties: immutable.WrapProperties(map[string]any{"b": "not a time", "c": upper}),
		}}}
	}
	roots := slices.Collect(snappartstest.Loaded(t, s, parts(lower)).InstancesOf(c))
	if len(roots) != 1 {
		t.Fatalf("read %d roots, want 1", len(roots))
	}
	if got := roots[0].PrimaryKey().String(); got != graph.FormatKey("not a time", lower) {
		t.Errorf("key %s, want the first component as written and the second as stored", got)
	}
	if _, err := snapshot.Unmarshal(t.Context(), snappartstest.Document(t, s, parts(upper)), s); !errors.Is(err, diag.E_SNAPSHOT_MALFORMED) {
		t.Errorf("a second component in upper case: %s; want %s", err, diag.E_SNAPSHOT_MALFORMED)
	}
}
