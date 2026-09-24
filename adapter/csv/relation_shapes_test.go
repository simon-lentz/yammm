package csv

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// kinSchema declares every relation on an ABSTRACT parent, so each one reaches
// Employee by inheritance, and gives Employee a composition so an edge can be
// filed under a relation name of the wrong kind.
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

// kinSnapshot rebuilds one Employee and two Companies, with the edges given.
// graph.RebuildSnapshot reconstructs a document and does not validate one.
func kinSnapshot(t *testing.T, edges func(employee, company schema.TypeID) []graph.EdgeParts) (*graph.Snapshot, *schema.Schema) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), kinSchema, "kin.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	companyT, _ := s.Type("Company")
	employeeT, _ := s.Type("Employee")
	company := func(id string, key ...any) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID: companyT.ID(), PrimaryKey: immutable.WrapKey(key),
			Properties: immutable.WrapProperties(map[string]any{"company_id": id, "region": "eu"}),
		}
	}
	snap, r := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{companyT.ID(), employeeT.ID()},
		Instances: []graph.InstanceParts{
			company("c1", "c1", "eu"),
			company("c2", "c2", "eu"),
			{
				TypeID: employeeT.ID(), PrimaryKey: immutable.WrapKey([]any{"e1"}),
				Properties: immutable.WrapProperties(map[string]any{"staff_id": "e1"}),
			},
		},
		Edges: edges(employeeT.ID(), companyT.ID()),
	})
	if err := r.Err(); err != nil {
		t.Fatalf("RebuildSnapshot refused the parts, so the case has no subject: %v", err)
	}
	return snap, s
}

func kinEdge(from, to schema.TypeID, rel string, key ...any) graph.EdgeParts {
	return graph.EdgeParts{
		Relation: rel, SourceType: from, SourceKey: immutable.WrapKey([]any{"e1"}),
		TargetType: to, TargetKey: immutable.WrapKey(key), Properties: immutable.WrapProperties(nil),
	}
}

// Every association here is inherited, and both are written, a (many) zipped on
// the list separator.
func TestMarshalSnapshot_InheritedAssociationsOfBothMultiplicitiesAreWritten(t *testing.T) {
	t.Parallel()
	snap, s := kinSnapshot(t, func(e, c schema.TypeID) []graph.EdgeParts {
		return []graph.EdgeParts{
			kinEdge(e, c, "WORKS_AT", "c1", "eu"),
			kinEdge(e, c, "ADVISES", "c1", "eu"), kinEdge(e, c, "ADVISES", "c2", "eu"),
		}
	})
	files, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = "grade,staff_id,advises._target_company_id,advises._target_region,works_at._target_company_id,works_at._target_region\n" +
		",e1,c1|c2,eu|eu,c1,eu\n"
	if got := string(files["Employee"]); got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}
