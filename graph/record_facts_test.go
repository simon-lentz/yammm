package graph_test

import (
	"slices"
	"testing"

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
	s, err := schema.LoadString(t.Context(), recordFactsSchema, "facts.yammm")
	if err != nil {
		t.Fatalf("load schema: %s", err)
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
