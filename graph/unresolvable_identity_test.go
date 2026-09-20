package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A type identity the schema cannot resolve.
//
// RebuildSnapshot's contract is that every identity its parts carry names a
// type of the bound schema. The zero identity was refused at every position and
// a FOREIGN one was not, because the root-type rule returned "no rule" for an
// identity it could not resolve and named the identity check as the one that
// would report it, which only tested the zero value. A snapshot could then hold
// instances of a type its schema does not know: Snapshot.Types reports only the
// types table, so the group left no trace, and a writer asked to key output by
// that type had no name to give.

// unresolvableSchema is a two-type closure; the identity below belongs to
// neither and to no schema this test loads.
const unresolvableSchema = `schema "entry"

type Company {
	company_id String primary
}

type Employee {
	employee_id String primary
	--> WORKS_AT (_:one) Company
}
`

func unresolvableFixture(t *testing.T) (*schema.Schema, schema.TypeID) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), unresolvableSchema, "entry.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s, schema.NewTypeID(location.MustNewSourceID("test://absent.yammm"), "Ghost")
}

func TestRebuildSnapshot_RefusesAnUnresolvableInstanceGroup(t *testing.T) {
	t.Parallel()
	s, ghost := unresolvableFixture(t)
	key := immutable.WrapKey([]any{"g1"})

	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			ghost: {{
				TypeName: "Ghost", TypeID: ghost, PrimaryKey: key,
				Properties: immutable.WrapProperties(map[string]any{"gid": "g1"}),
			}},
		},
	})

	if res.Err() == nil {
		t.Fatal("a group keyed by an identity the schema cannot resolve was accepted")
	}
	if msg := res.String(); !strings.Contains(msg, "unresolvable type identity") {
		t.Errorf("refusal does not name the rule: %s", msg)
	}
}

// The identity reaches the writers through an edge as well as through a group,
// and the edge endpoint is the position that carried it to adapter/json's
// unresolvable-target arm.
func TestRebuildSnapshot_RefusesAnUnresolvableEdgeEndpoint(t *testing.T) {
	t.Parallel()
	s, ghost := unresolvableFixture(t)
	empT, _ := s.Type("Employee")
	empKey := immutable.WrapKey([]any{"e1"})

	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{empT.ID()},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			empT.ID(): {{
				TypeName: "Employee", TypeID: empT.ID(), PrimaryKey: empKey,
				Properties: immutable.WrapProperties(map[string]any{"employee_id": "e1"}),
			}},
		},
		Edges: []graph.EdgeParts{{
			Relation: "WORKS_AT", SourceType: empT.ID(), SourceKey: empKey,
			TargetType: ghost, TargetKey: immutable.WrapKey([]any{"g1"}),
			Properties: immutable.WrapProperties(nil),
		}},
	})

	if res.Err() == nil {
		t.Fatal("an edge naming a target type the schema cannot resolve was accepted")
	}
	// The whole phrase, because an edge whose target instance is merely absent
	// is refused for a different reason and would satisfy a looser match.
	if msg := res.String(); !strings.Contains(msg, "unresolvable type identity at edge target position") {
		t.Errorf("refusal does not name the rule and the position: %s", msg)
	}
}

// A composed child's type need not be a root type, so the root-type rule never
// judged it; the identity rule does, at every depth.
func TestRebuildSnapshot_RefusesAnUnresolvableComposedChild(t *testing.T) {
	t.Parallel()
	s, ghost := unresolvableFixture(t)
	empT, _ := s.Type("Employee")
	empKey := immutable.WrapKey([]any{"e1"})

	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{empT.ID()},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			empT.ID(): {{
				TypeName: "Employee", TypeID: empT.ID(), PrimaryKey: empKey,
				Properties: immutable.WrapProperties(map[string]any{"employee_id": "e1"}),
				Composed: map[string][]graph.InstanceParts{
					"PIECES": {{
						TypeName: "Ghost", TypeID: ghost,
						PrimaryKey: immutable.WrapKey([]any{"g1"}),
						Properties: immutable.WrapProperties(map[string]any{"gid": "g1"}),
					}},
				},
			}},
		},
	})

	if res.Err() == nil {
		t.Fatal("a composed child of a type the schema cannot resolve was accepted")
	}
	if msg := res.String(); !strings.Contains(msg, "composed child") {
		t.Errorf("refusal does not name the position: %s", msg)
	}
}

// The rule judges identities, not names: a resolvable identity is accepted at
// every one of those positions, so the guard is not refusing everything.
func TestRebuildSnapshot_AcceptsResolvableIdentitiesAtEveryPosition(t *testing.T) {
	t.Parallel()
	s, _ := unresolvableFixture(t)
	compT, _ := s.Type("Company")
	empT, _ := s.Type("Employee")
	compKey := immutable.WrapKey([]any{"c1"})
	empKey := immutable.WrapKey([]any{"e1"})

	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{compT.ID(), empT.ID()},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			compT.ID(): {{
				TypeName: "Company", TypeID: compT.ID(), PrimaryKey: compKey,
				Properties: immutable.WrapProperties(map[string]any{"company_id": "c1"}),
			}},
			empT.ID(): {{
				TypeName: "Employee", TypeID: empT.ID(), PrimaryKey: empKey,
				Properties: immutable.WrapProperties(map[string]any{"employee_id": "e1"}),
			}},
		},
		Edges: []graph.EdgeParts{{
			Relation: "WORKS_AT", SourceType: empT.ID(), SourceKey: empKey,
			TargetType: compT.ID(), TargetKey: compKey,
			Properties: immutable.WrapProperties(nil),
		}},
	})

	if err := res.Err(); err != nil {
		t.Errorf("a snapshot of resolvable identities was refused: %v", err)
	}
}
