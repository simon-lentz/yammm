package json

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/snapparts"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
)

// kinSchema declares every relation on an ABSTRACT parent, so each one reaches
// Employee by inheritance: a (one) association, a (many) association and a
// composition. A writer that looked a relation up among a type's own
// declarations would find none of them.
const kinSchema = `schema "kin"

type Company {
	company_id String primary
	region String primary
}

part type Badge {
	code String primary
}

abstract type Staff {
	staff_id String primary
	--> WORKS_AT (_:one) Company
	--> ADVISES (_:many) Company
	*-> BADGES (many) Badge
}

type Employee extends Staff {
	grade String
}
`

type kinIDs struct{ company, badge, employee schema.TypeID }

// kinSnapshot reads a document of one Employee and two Companies, after shape adds
// edges and composed children.
func kinSnapshot(t *testing.T, shape func(p *snapparts.Parts, emp *snapparts.Instance, ids kinIDs)) graph.Snapshot {
	t.Helper()
	s, res := schema.LoadString(t.Context(), kinSchema, "kin.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	companyT, _ := s.Type("Company")
	badgeT, _ := s.Type("Badge")
	employeeT, _ := s.Type("Employee")
	ids := kinIDs{companyT.ID(), badgeT.ID(), employeeT.ID()}

	company := func(id string, key ...any) snapparts.Instance {
		return snapparts.Instance{
			TypeID: ids.company, PrimaryKey: immutable.WrapKey(key),
			Properties: immutable.WrapProperties(map[string]any{"company_id": id, "region": "eu"}),
		}
	}
	emp := snapparts.Instance{
		TypeID: ids.employee, PrimaryKey: immutable.WrapKey([]any{"e1"}),
		Properties: immutable.WrapProperties(map[string]any{"staff_id": "e1"}),
	}
	parts := snapparts.Parts{
		Types: []schema.TypeID{ids.company, ids.employee},
		Instances: []snapparts.Instance{
			company("c1", "c1", "eu"),
			company("c2", "c2", "eu"),
		},
	}
	shape(&parts, &emp, ids)
	parts.Instances = append(parts.Instances, emp)

	return snappartstest.Loaded(t, s, parts)
}

func kinEdge(ids kinIDs, rel string, key ...any) snapparts.Edge {
	return snapparts.Edge{
		Relation: rel, SourceType: ids.employee, SourceKey: immutable.WrapKey([]any{"e1"}),
		TargetKey: immutable.WrapKey(key), Properties: immutable.WrapProperties(nil),
	}
}

func kinBadge(ids kinIDs, code string) snapparts.Instance {
	return snapparts.Instance{
		TypeID: ids.badge, PrimaryKey: immutable.WrapKey([]any{code}),
		Properties: immutable.WrapProperties(map[string]any{"code": code}),
	}
}

// Every relation here is inherited. Each is written under its field name, in
// the shape its multiplicity takes.
func TestMarshalObject_InheritedRelationsAreWritten(t *testing.T) {
	t.Parallel()
	snap := kinSnapshot(t, func(p *snapparts.Parts, emp *snapparts.Instance, ids kinIDs) {
		p.Edges = []snapparts.Edge{
			kinEdge(ids, "WORKS_AT", "c1", "eu"),
			kinEdge(ids, "ADVISES", "c1", "eu"),
			kinEdge(ids, "ADVISES", "c2", "eu"),
		}
		emp.Composed = map[string][]snapparts.Instance{"BADGES": {kinBadge(ids, "b1")}}
	})
	data, err := New().MarshalObject(context.Background(), snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"works_at":{"_target_company_id":"c1","_target_region":"eu"}`,
		`"advises":[{"_target_company_id":"c1","_target_region":"eu"},{"_target_company_id":"c2","_target_region":"eu"}]`,
		`"badges":[{"code":"b1"}]`,
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("want %s in:\n%s", want, data)
		}
	}
}
