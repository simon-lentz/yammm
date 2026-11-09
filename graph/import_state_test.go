package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
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
	g, res := graph.NewFromSnapshot(s, graph.New(s).Snapshot())
	if res.HasErrors() {
		t.Fatalf("import: %s", res)
	}
	mustOK(t, g.Add(t.Context(), rootInstance(t, s, "T", "a")))
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
	g, res := graph.NewFromSnapshot(s, seed)
	if res.HasErrors() {
		t.Fatalf("import: %s", res)
	}
	mustOK(t, g.Add(t.Context(), rootInstance(t, s, "T", "c")))
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
	mustOK(t, g.Add(ctx, instancetest.Valid(t, s, "R", map[string]any{
		"id": "r",
		"o":  map[string]any{"_target_id": "x"},
		"m":  []any{map[string]any{"_target_id": "x"}},
	})))
	seeded := mustImport(t, s, g.Snapshot())
	merged, res := seeded.AddOrMerge(ctx, instancetest.Valid(t, s, "R", map[string]any{
		"id": "r",
		"o":  map[string]any{"_target_id": "y"},
	}))
	if merged || !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("merged %v, %s; want an E_GRAPH_CARDINALITY refusal", merged, res)
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
	mustOK(t, g.Add(ctx, rootInstance(t, s, "R", "r")))
	res := g.Check(ctx)
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
	mustOK(t, g.Add(ctx, rootInstance(t, s, "T", "a")))
	if res := g.Add(ctx, rootInstance(t, s, "T", "a")); !res.HasCode(diag.E_DUPLICATE_PK) {
		t.Fatalf("second Add: %s, want %s", res, diag.E_DUPLICATE_PK)
	}
	dups := slices.Collect(g.Snapshot().Duplicates())
	if len(dups) != 1 || !dups[0].Instance().Validated() {
		t.Errorf("duplicates %v, want one whose instance is validated", dups)
	}
}

// A key shorter than the importing schema's is its own refusal, not a canonical
// position past its end.
func TestImport_AKeyShorterThanTheImportingSchemasIsRefused(t *testing.T) {
	t.Parallel()
	a := loadEntry(t, "schema \"m\"\ntype T {\n id String primary\n}\n")
	b := loadEntry(t, "schema \"m\"\ntype T {\n id String primary\n at Timestamp primary\n}\n")
	g := graph.New(a)
	mustOK(t, g.Add(t.Context(), rootInstance(t, a, "T", "x")))
	if _, res := graph.NewFromSnapshot(b, g.Snapshot()); !res.HasCode(diag.E_GRAPH_INVALID_PK) {
		t.Errorf("%s, want %s", res, diag.E_GRAPH_INVALID_PK)
	}
}

// A component no constraint renders stays as written, and the components after it
// are still canonical.
func TestLoad_CanonicalizesTheKeyComponentsAfterOneItCannotRender(t *testing.T) {
	t.Parallel()
	s := loadEntry(t, "schema \"m\"\ntype C {\n b Timestamp primary\n c UUID primary\n}\n")
	const upper, lower = "6BA7B810-9DAD-11D1-80B4-00C04FD430C8", "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	c := typeIDOf(t, s, "C")
	snap := snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID: c, PrimaryKey: immutable.WrapKey([]any{"not a time", upper}),
		Properties: immutable.WrapProperties(map[string]any{"b": "not a time", "c": upper}),
	}}})
	roots := slices.Collect(snap.InstancesOf(c))
	if len(roots) != 1 {
		t.Fatalf("read %d roots, want 1", len(roots))
	}
	key := roots[0].PrimaryKey()
	if got := key.Get(1).Unwrap(); got != lower {
		t.Errorf("second component %v, want %s", got, lower)
	}
	if got := key.Get(0).Unwrap(); got != "not a time" {
		t.Errorf("first component %v, want it as written", got)
	}
}

// Each pair below reads one snapshot under another schema, and the structure rules
// judge what that schema makes of it.
func TestImport_StructureRulesJudgeTheImportingSchemasFacts(t *testing.T) {
	t.Parallel()
	const ts1, ts2, ts3 = "2020-01-02T03:04:05.000Z", "2020-01-02T03:04:05.00Z", "2020-01-02T03:04:05Z"
	for name, c := range map[string]struct {
		from, to string
		rows     []map[string]any
		root     string
		want     []diag.Code
		absent   []diag.Code
	}{
		"siblings of a type the slot does not hold are no duplicate": {
			from:   "schema \"m\"\npart type P {\n k String primary\n}\ntype R {\n id String primary\n *-> K (_:many) P\n}\n",
			to:     "schema \"m\"\npart type P {\n k Timestamp primary\n}\npart type Q {\n k Timestamp primary\n}\ntype R {\n id String primary\n *-> K (_:many) Q\n}\n",
			root:   "R",
			rows:   []map[string]any{{"id": "r", "k": []any{map[string]any{"k": ts1}, map[string]any{"k": ts3}}}},
			absent: []diag.Code{diag.E_DUPLICATE_COMPOSED_PK},
		},
		"two sound roots after a refused one at their address are a duplicate": {
			from: "schema \"m\"\ntype X {\n id String primary\n alt String\n}\n",
			to:   "schema \"m\"\ntype X {\n id String\n alt Timestamp primary\n}\n",
			root: "X",
			rows: []map[string]any{{"id": ts1}, {"id": ts2, "alt": ts2}, {"id": ts3, "alt": ts3}},
			want: []diag.Code{diag.E_GRAPH_INVALID_PK, diag.E_DUPLICATE_PK},
		},
		"a refused root's records are no count under a (one)": {
			from:   "schema \"m\"\ntype T {\n id String primary\n}\ntype X {\n id String primary\n alt String\n --> M (_:many) T\n}\n",
			to:     "schema \"m\"\ntype T {\n id String primary\n}\ntype X {\n id String\n alt String primary\n --> M (_:one) T\n}\n",
			root:   "X",
			rows:   []map[string]any{{"id": "x", "m": []any{map[string]any{"_target_id": "p"}, map[string]any{"_target_id": "q"}}}},
			want:   []diag.Code{diag.E_GRAPH_INVALID_PK},
			absent: []diag.Code{diag.E_GRAPH_CARDINALITY},
		},
		"an empty record under a name the schema makes a composition": {
			from: "schema \"m\"\ntype T {\n id String primary\n}\npart type P {\n k String primary\n}\ntype X {\n id String primary\n --> M (one:many) T\n}\n",
			to:   "schema \"m\"\ntype T {\n id String primary\n}\npart type P {\n k String primary\n}\ntype X {\n id String primary\n *-> M (_:many) P\n}\n",
			root: "X",
			rows: []map[string]any{{"id": "x", "m": []any{}}},
			want: []diag.Code{diag.E_GRAPH_UNKNOWN_RELATION},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			from, to := loadEntry(t, c.from), loadEntry(t, c.to)
			g := graph.New(from)
			for _, row := range c.rows {
				mustOK(t, g.Add(ctx, instancetest.Valid(t, from, c.root, row)))
			}
			_, res := graph.NewFromSnapshot(to, g.Snapshot())
			if !res.HasErrors() {
				t.Fatal("the import is accepted")
			}
			for _, code := range c.want {
				if !res.HasCode(code) {
					t.Errorf("%s, want %s", res, code)
				}
			}
			for _, code := range c.absent {
				if res.HasCode(code) {
					t.Errorf("%s, want no %s", res, code)
				}
			}
		})
	}
}

// The import's cardinality refusal names the source's key, as the merge's does.
func TestImport_ACardinalityRefusalNamesTheSourceKey(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	from := loadEntry(t, "schema \"m\"\ntype T {\n id String primary\n}\ntype X {\n id String primary\n --> M (_:many) T\n}\n")
	to := loadEntry(t, "schema \"m\"\ntype T {\n id String primary\n}\ntype X {\n id String primary\n --> M (_:one) T\n}\n")
	g := graph.New(from)
	mustOK(t, g.Add(ctx, instancetest.Valid(t, from, "X", map[string]any{
		"id": "x", "m": []any{map[string]any{"_target_id": "p"}, map[string]any{"_target_id": "q"}},
	})))
	_, res := graph.NewFromSnapshot(to, g.Snapshot())
	var keys []string
	for issue := range res.Issues() {
		if issue.Code() != diag.E_GRAPH_CARDINALITY {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == diag.DetailKeyPrimaryKey {
				keys = append(keys, d.Value)
			}
		}
	}
	if !slices.Equal(keys, []string{`["x"]`}) {
		t.Errorf("E_GRAPH_CARDINALITY primary keys %q, want [[\"x\"]]: %s", keys, res)
	}
}
