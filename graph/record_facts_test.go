package graph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

// Graph.Add derives an association's records from the data: an edge per resolved
// target, target_missing per target no root holds, and an absent or empty record
// alone, only under a required association.
const recordFactsSchema = `schema "facts"

type Company {
	id String primary
}

type Employee {
	id String primary
	--> WORKS_AT (one) Company
	--> AUDITS (one:many) Company
}
`

func recordFactsParts(t *testing.T) (schema.Schema, snapparts.Parts) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), recordFactsSchema, "facts.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	compID := mustTypeID(t, s, "Company")
	empID := mustTypeID(t, s, "Employee")
	c1 := immutable.WrapKey([]any{"c1"})
	e1 := immutable.WrapKey([]any{"e1"})
	return s, snapparts.Parts{
		Types: []schema.TypeID{compID, empID},
		Instances: []snapparts.Instance{
			{TypeID: compID, PrimaryKey: c1, Properties: immutable.WrapProperties(map[string]any{"id": "c1"})},
			{TypeID: empID, PrimaryKey: e1, Properties: immutable.WrapProperties(map[string]any{"id": "e1"})},
		},
		Edges: []snapparts.Edge{
			{Relation: "WORKS_AT", SourceType: empID, SourceKey: e1, TargetKey: c1},
			{Relation: "AUDITS", SourceType: empID, SourceKey: e1, TargetKey: c1},
		},
	}
}

func dropEdge(parts *snapparts.Parts, relation string) {
	kept := parts.Edges[:0]
	for _, e := range parts.Edges {
		if e.Relation != relation {
			kept = append(kept, e)
		}
	}
	parts.Edges = kept
}

func record(parts snapparts.Parts, relation, reason, target string) snapparts.Unresolved {
	emp := parts.Instances[1]
	u := snapparts.Unresolved{SourceType: emp.TypeID, SourceKey: emp.PrimaryKey, Relation: relation, Reason: reason}
	if target != "" {
		u.TargetKey = immutable.WrapKey([]any{target})
	}
	return u
}

// A rebuild holds each record Add makes as the parts state it.
func TestRebuild_HoldsEachRecordAddMakes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name                string
		mutate              func(p *snapparts.Parts)
		wantEdges, wantRecs int
	}{
		{"edges alone", func(*snapparts.Parts) {}, 2, 0},
		{"an absent record under a required (one)", func(p *snapparts.Parts) {
			dropEdge(p, "WORKS_AT")
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "absent", ""))
		}, 1, 1},
		{"an empty record under a required (many)", func(p *snapparts.Parts) {
			dropEdge(p, "AUDITS")
			p.Unresolved = append(p.Unresolved, record(*p, "AUDITS", "empty", ""))
		}, 1, 1},
		{"a target_missing record beside an edge", func(p *snapparts.Parts) {
			p.Unresolved = append(p.Unresolved, record(*p, "AUDITS", "target_missing", "c9"))
		}, 2, 1},
		{"a target_missing record alone under a required association", func(p *snapparts.Parts) {
			dropEdge(p, "WORKS_AT")
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "target_missing", "c9"))
		}, 1, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, parts := recordFactsParts(t)
			c.mutate(&parts)
			snap := rebuild(s, parts)
			if got := len(slices.Collect(snap.Edges())); got != c.wantEdges {
				t.Errorf("%d edges, want %d", got, c.wantEdges)
			}
			if got := len(slices.Collect(snap.Unresolved())); got != c.wantRecs {
				t.Errorf("%d unresolved records, want %d", got, c.wantRecs)
			}
		})
	}
}

// An edge may spell either endpoint any way its UUID constraint accepts, and still
// addresses the root at that UUID.
func TestRebuild_AnEdgeResolvesItsEndpointsByCanonicalKeys(t *testing.T) {
	t.Parallel()
	s := loadAs(t, `schema "ids"

type Company {
	id UUID primary
}

type Employee {
	id UUID primary
	--> WORKS_AT (one) Company
}
`)
	const company, employee = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", "b1ffcd88-8d1a-4ef8-bb6d-6bb9bd380a22"
	compID, empID := mustTypeID(t, s, "Company"), mustTypeID(t, s, "Employee")
	upper := func(k string) immutable.Key { return immutable.WrapKey([]any{strings.ToUpper(k)}) }
	snap := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{compID, empID},
		Instances: []snapparts.Instance{
			{TypeID: compID, PrimaryKey: immutable.WrapKey([]any{company}), Properties: immutable.WrapProperties(map[string]any{"id": company})},
			{TypeID: empID, PrimaryKey: immutable.WrapKey([]any{employee}), Properties: immutable.WrapProperties(map[string]any{"id": employee})},
		},
		Edges: []snapparts.Edge{{Relation: "WORKS_AT", SourceType: empID, SourceKey: upper(employee), TargetKey: upper(company)}},
	})
	edges := slices.Collect(snap.Edges())
	if len(edges) != 1 {
		t.Fatalf("%d edges, want 1", len(edges))
	}
	if got, want := edges[0].Source().PrimaryKey().String(), graph.FormatKey(employee); got != want {
		t.Errorf("source = %s, want %s", got, want)
	}
	if got, want := edges[0].Target().PrimaryKey().String(), graph.FormatKey(company); got != want {
		t.Errorf("target = %s, want %s", got, want)
	}
}
