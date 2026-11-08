package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// The structural rules report one break once: a unit a rule refused feeds no
// later rule. The snapshot package's single-break table holds the reader to it
// over every shape a document can state; the rows here are the import's.

func failures(res diag.Result) int {
	n := 0
	for issue := range res.Issues() {
		if issue.Severity().IsFailure() {
			n++
		}
	}
	return n
}

func loadAs(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "entry.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// snapshotOf validates each raw instance of typeName under s and adds it.
func snapshotOf(t *testing.T, s schema.Schema, typeName string, raws ...map[string]any) graph.Snapshot {
	t.Helper()
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, raw := range raws {
		vi, res := v.ValidateOne(t.Context(), typeName, instance.RawInstance{Properties: raw})
		if res.HasErrors() {
			t.Fatalf("validate: %s", res)
		}
		if res := g.Add(t.Context(), vi); res.HasErrors() {
			t.Fatalf("add: %s", res)
		}
	}
	return g.Snapshot()
}

// Each import binds a snapshot to a schema under which it breaks one fact.
func TestNewFromSnapshot_OneBreakDrawsOneIssue(t *testing.T) {
	t.Parallel()
	const (
		z      = "2024-01-01T00:00:00Z"
		offset = "2024-01-01T00:00:00+00:00"
		later  = "2030-01-01T00:00:00Z"
	)
	next := map[string]any{"_target_at": later}
	for _, c := range []struct {
		name         string
		built, bound string
		typeName     string
		raws         []map[string]any
		code         diag.Code
		phrase       string
	}{
		{
			name:     "two roots the importing schema keys as one, each with a (one) edge",
			built:    "schema \"entry\"\n\ntype Run {\n\tat String primary\n\t--> NEXT (_:one) Run\n}\n",
			bound:    "schema \"entry\"\n\ntype Run {\n\tat Timestamp primary\n\t--> NEXT (_:one) Run\n}\n",
			typeName: "Run",
			raws:     []map[string]any{{"at": later}, {"at": z, "NEXT": next}, {"at": offset, "NEXT": next}},
			code:     diag.E_DUPLICATE_PK, phrase: "two instances of type",
		},
		{
			name:     "an edge holding a property the importing schema lacks, beside another under what it declares (one)",
			built:    "schema \"entry\"\n\ntype Person {\n\tid String primary\n\t--> LINK (_:many) Person {\n\t\tnote String\n\t}\n}\n",
			bound:    "schema \"entry\"\n\ntype Person {\n\tid String primary\n\t--> LINK (_:one) Person\n}\n",
			typeName: "Person",
			raws: []map[string]any{{"id": "p2"}, {"id": "p3"}, {"id": "p1", "LINK": []any{
				map[string]any{"_target_id": "p2", "note": "n"}, map[string]any{"_target_id": "p3"},
			}}},
			code: diag.E_UNKNOWN_EDGE_FIELD, phrase: `holds edge property "note"`,
		},
		{
			name:     "a target_missing record holding a property the importing schema lacks, beside an edge under what it declares (one)",
			built:    "schema \"entry\"\n\ntype Person {\n\tid String primary\n\t--> LINK (_:many) Person {\n\t\tnote String\n\t}\n}\n",
			bound:    "schema \"entry\"\n\ntype Person {\n\tid String primary\n\t--> LINK (_:one) Person\n}\n",
			typeName: "Person",
			raws: []map[string]any{{"id": "p2"}, {"id": "p1", "LINK": []any{
				map[string]any{"_target_id": "p9", "note": "n"}, map[string]any{"_target_id": "p2"},
			}}},
			code: diag.E_UNKNOWN_EDGE_FIELD, phrase: `holds edge property "note"`,
		},
		{
			name:     "two edges under what the importing schema declares (one)",
			built:    "schema \"entry\"\n\ntype Person {\n\tid String primary\n\t--> LINK (_:many) Person\n}\n",
			bound:    "schema \"entry\"\n\ntype Person {\n\tid String primary\n\t--> LINK (_:one) Person\n}\n",
			typeName: "Person",
			raws: []map[string]any{{"id": "p2"}, {"id": "p3"}, {"id": "p1", "LINK": []any{
				map[string]any{"_target_id": "p2"}, map[string]any{"_target_id": "p3"},
			}}},
			code: diag.E_GRAPH_CARDINALITY, phrase: `(one) association "LINK"`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap := snapshotOf(t, loadAs(t, c.built), c.typeName, c.raws...)
			bound := loadAs(t, c.bound)
			g, res := graph.NewFromSnapshot(bound, snap)
			if g != nil {
				t.Error("a refused import returned a graph")
			}
			if n := failures(res); n != 1 {
				t.Errorf("%d issues, want 1: %s", n, res)
			}
			found := false
			for issue := range res.Issues() {
				found = found || issue.Code() == c.code && strings.Contains(issue.Message(), c.phrase)
			}
			if !found {
				t.Errorf("no %s naming %q: %s", c.code, c.phrase, res)
			}
		})
	}
}

// A composition slot the importing schema refuses draws its refusal as one issue,
// beside one per child of another target.
func TestNewFromSnapshot_ARefusedSlotDrawsItsOwnIssues(t *testing.T) {
	t.Parallel()
	const built = "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> ITEMS (_:many) Item\n}\n\npart type Item {\n\tcode String primary\n}\n"
	for _, c := range []struct {
		name, bound string
		code        diag.Code
		// issues counts the import's issues, one when zero.
		issues int
	}{
		{
			name:  "a (many) slot the importing schema declares (one)",
			bound: strings.Replace(built, "(_:many)", "(_:one)", 1),
			code:  diag.E_DUPLICATE_COMPOSED_PK,
		},
		{
			name:  "a slot the importing schema does not declare",
			bound: strings.Replace(built, "\t*-> ITEMS (_:many) Item\n", "", 1),
			code:  diag.E_GRAPH_UNKNOWN_RELATION,
		},
		{
			name:  "a slot the importing schema declares as an association",
			bound: strings.Replace(built, "\t*-> ITEMS (_:many) Item\n", "\t--> ITEMS (_:many) Order\n", 1),
			code:  diag.E_GRAPH_UNKNOWN_RELATION,
		},
		{
			name:   "a (many) slot of another target",
			bound:  strings.Replace(built, "(_:many) Item\n", "(_:many) Other\n", 1) + "\npart type Other {\n\tcode String primary\n}\n",
			code:   diag.E_GRAPH_INVALID_COMPOSITION,
			issues: 2,
		},
		{
			name:  "a slot whose children's type the importing schema drops",
			bound: strings.ReplaceAll(built, "Item", "Other"),
			code:  diag.E_GRAPH_TYPE_NOT_FOUND,
		},
		{
			// The (one) slot holds two children, each of another type than its
			// target.
			name:   "a (one) slot of another target",
			bound:  strings.Replace(built, "(_:many) Item\n", "(_:one) Other\n", 1) + "\npart type Other {\n\tcode String primary\n}\n",
			code:   diag.E_GRAPH_INVALID_COMPOSITION,
			issues: 3,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := loadAs(t, built)
			g := graph.New(s)
			v := instance.NewValidator(s)
			vi, res := v.ValidateOne(t.Context(), "Order", instance.RawInstance{Properties: map[string]any{
				"id": "o1", "ITEMS": []any{map[string]any{"code": "a"}, map[string]any{"code": "b"}},
			}})
			if res.HasErrors() {
				t.Fatalf("validate: %s", res)
			}
			if res := g.Add(t.Context(), vi); res.HasErrors() {
				t.Fatalf("add: %s", res)
			}
			snap := g.Snapshot()
			bound := loadAs(t, c.bound)
			_, res = graph.NewFromSnapshot(bound, snap)
			want := max(c.issues, 1)
			if failures(res) != want || !res.HasCode(c.code) {
				t.Errorf("%d issues, want %d with a %s: %s", failures(res), want, c.code, res)
			}
		})
	}
}
