package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
)

const (
	upperA = "6BA7B810-9DAD-11D1-80B4-00C04FD430C8"
	lowerA = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	upperB = "A0EEBC99-9C0B-4EF8-BB6D-6BB9BD380A11"
	lowerB = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
)

// Each snapshot below is built under a schema that keys T by String and read
// under one that keys it by UUID, so two spellings of one UUID fold to one target.
func TestImport_TwoRecordsNamingOneTargetAreRefused(t *testing.T) {
	t.Parallel()
	const stringKeyed = "schema \"m\"\ntype T {\n id String primary\n}\ntype X {\n id String primary\n --> M (_:many) T\n}\n"
	const uuidKeyed = "schema \"m\"\ntype T {\n id UUID primary\n}\ntype X {\n id String primary\n --> M (_:many) T\n}\n"
	listing := func(id string, keys ...string) map[string]any {
		m := make([]any, len(keys))
		for i, k := range keys {
			m[i] = map[string]any{"_target_id": k}
		}
		return map[string]any{"id": id, "m": m}
	}
	for name, c := range map[string]struct {
		from, to string
		targets  []string
		rows     []map[string]any
		want     []diag.Code
		absent   []diag.Code
	}{
		"an edge and a record": {
			from: stringKeyed, to: uuidKeyed,
			targets: []string{lowerA},
			rows:    []map[string]any{listing("x", lowerA, upperA)},
			want:    []diag.Code{diag.E_DUPLICATE_EDGE},
		},
		"two records": {
			from: stringKeyed, to: uuidKeyed,
			rows: []map[string]any{listing("x", upperB, lowerB)},
			want: []diag.Code{diag.E_DUPLICATE_EDGE},
		},
		"two edges to roots that fold to one address draw the duplicate alone": {
			from: stringKeyed, to: uuidKeyed,
			targets: []string{upperA, lowerA},
			rows:    []map[string]any{listing("x", upperA, lowerA)},
			want:    []diag.Code{diag.E_DUPLICATE_PK},
			absent:  []diag.Code{diag.E_DUPLICATE_EDGE},
		},
		"the records of a source two roots stand at are not judged": {
			from:   stringKeyed,
			to:     "schema \"m\"\ntype T {\n id UUID primary\n}\ntype X {\n id UUID primary\n --> M (_:many) T\n}\n",
			rows:   []map[string]any{listing(upperA, upperB, lowerB), listing(lowerA, upperB, lowerB)},
			want:   []diag.Code{diag.E_DUPLICATE_PK},
			absent: []diag.Code{diag.E_DUPLICATE_EDGE},
		},
		"records naming a root whose key the key rule refuses are its break": {
			from:    "schema \"m\"\ntype T {\n id String primary\n alt String\n}\ntype X {\n id String primary\n --> M (_:many) T\n}\n",
			to:      "schema \"m\"\ntype T {\n id String\n alt UUID primary\n}\ntype X {\n id String primary\n --> M (_:many) T\n}\n",
			targets: []string{lowerA},
			rows:    []map[string]any{listing("x", lowerA, upperA)},
			want:    []diag.Code{diag.E_GRAPH_INVALID_PK},
			absent:  []diag.Code{diag.E_DUPLICATE_EDGE},
		},
		"a (one) association's count judges its records": {
			from:   stringKeyed,
			to:     "schema \"m\"\ntype T {\n id UUID primary\n}\ntype X {\n id String primary\n --> M (_:one) T\n}\n",
			rows:   []map[string]any{listing("x", upperB, lowerB)},
			want:   []diag.Code{diag.E_GRAPH_CARDINALITY},
			absent: []diag.Code{diag.E_DUPLICATE_EDGE},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			from, to := loadEntry(t, c.from), loadEntry(t, c.to)
			g := graph.New(from)
			for _, id := range c.targets {
				mustOK(t, g.Add(ctx, instancetest.Valid(t, from, "T", map[string]any{"id": id})))
			}
			for _, row := range c.rows {
				mustOK(t, g.Add(ctx, instancetest.Valid(t, from, "X", row)))
			}
			imported, res := graph.NewFromSnapshot(to, g.Snapshot())
			if imported != nil || !res.HasErrors() {
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

// The refusal names the source, the association and the one target both
// records name, each key in its canonical form under the importing schema.
func TestImport_ADuplicateEdgeNamesItsSourceAndTarget(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	from := loadEntry(t, "schema \"m\"\ntype T {\n id String primary\n}\ntype X {\n id String primary\n --> M (_:many) T\n}\n")
	to := loadEntry(t, "schema \"m\"\ntype T {\n id UUID primary\n}\ntype X {\n id UUID primary\n --> M (_:many) T\n}\n")
	g := graph.New(from)
	mustOK(t, g.Add(ctx, instancetest.Valid(t, from, "X", map[string]any{
		"id": upperA,
		"m": []any{
			map[string]any{"_target_id": "A0eebc99-9C0B-4ef8-BB6D-6bb9bd380a11"},
			map[string]any{"_target_id": "a0EEBC99-9c0b-4EF8-bb6d-6BB9BD380A11"},
		},
	})))
	_, res := graph.NewFromSnapshot(to, g.Snapshot())
	var got [][]diag.Detail
	for issue := range res.Issues() {
		if issue.Code() == diag.E_DUPLICATE_EDGE {
			got = append(got, issue.Details())
		}
	}
	want := []diag.Detail{
		{Key: diag.DetailKeyTypeName, Value: "X"},
		{Key: diag.DetailKeyPrimaryKey, Value: `["` + lowerA + `"]`},
		{Key: diag.DetailKeyRelationName, Value: "M"},
		{Key: diag.DetailKeyTargetPK, Value: `["` + lowerB + `"]`},
	}
	if len(got) != 1 || !slices.Equal(got[0], want) {
		t.Errorf("E_DUPLICATE_EDGE details %v, want one issue holding %v: %s", got, want, res)
	}
}
