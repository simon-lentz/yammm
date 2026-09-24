package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
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

// requireIdentityRefusal asserts result holds the identity rule's refusal
// naming want, raised as the rule's zero-identity refusal is: Fatal E_INTERNAL.
// The message alone would leave a refusal demoted to an Error, or moved to
// another code, green.
func requireIdentityRefusal(t *testing.T, result diag.Result, want string) {
	t.Helper()
	for issue := range result.Issues() {
		if !strings.Contains(issue.Message(), want) {
			continue
		}
		if issue.Severity() != diag.Fatal || issue.Code() != diag.E_INTERNAL {
			t.Errorf("refusal is %v %s, want Fatal E_INTERNAL: %s", issue.Severity(), issue.Code(), issue.Message())
		}
		return
	}
	t.Errorf("no refusal names %q: %s", want, result)
}

func unresolvableFixture(t *testing.T) (*schema.Schema, schema.TypeID) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), unresolvableSchema, "entry.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s, schema.NewTypeID(location.MustNewSourceID("test://absent.yammm"), "Ghost")
}

func TestRebuildSnapshot_RefusesAnUnresolvableRootInstance(t *testing.T) {
	t.Parallel()
	s, ghost := unresolvableFixture(t)
	key := immutable.WrapKey([]any{"g1"})

	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{},
		Instances: []graph.InstanceParts{
			{
				TypeID: ghost, PrimaryKey: key,
				Properties: immutable.WrapProperties(map[string]any{"gid": "g1"}),
			},
		},
	})

	if res.Err() == nil {
		t.Fatal("a root instance of an identity the schema cannot resolve was accepted")
	}
	requireIdentityRefusal(t, res, "unresolvable type identity "+ghost.String()+` at instance position, key ["g1"]`)
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
		Instances: []graph.InstanceParts{
			{
				TypeID: empT.ID(), PrimaryKey: empKey,
				Properties: immutable.WrapProperties(map[string]any{"employee_id": "e1"}),
				Composed: map[string][]graph.InstanceParts{
					"PIECES": {{
						TypeID:     ghost,
						PrimaryKey: immutable.WrapKey([]any{"g1"}),
						Properties: immutable.WrapProperties(map[string]any{"gid": "g1"}),
					}},
				},
			},
		},
	})

	if res.Err() == nil {
		t.Fatal("a composed child of a type the schema cannot resolve was accepted")
	}
	requireIdentityRefusal(t, res, "unresolvable type identity "+ghost.String()+" at composed child position")
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
		Instances: []graph.InstanceParts{
			{
				TypeID: compT.ID(), PrimaryKey: compKey,
				Properties: immutable.WrapProperties(map[string]any{"company_id": "c1"}),
			},
			{
				TypeID: empT.ID(), PrimaryKey: empKey,
				Properties: immutable.WrapProperties(map[string]any{"employee_id": "e1"}),
			},
		},
		Edges: []graph.EdgeParts{{
			Relation: "WORKS_AT", SourceType: empT.ID(), SourceKey: empKey,
			TargetKey:  compKey,
			Properties: immutable.WrapProperties(nil),
		}},
	})

	if err := res.Err(); err != nil {
		t.Errorf("a snapshot of resolvable identities was refused: %v", err)
	}
}

// A duplicate record and an unresolved record carry identities too: a
// duplicate's instance and parent, an unresolved record's source and an edge's
// source. Each case asserts the identity rule's own phrase and its position,
// because a record whose position holds no conflict or no instance is refused
// for another reason and would satisfy a looser match.
func TestRebuildSnapshot_RefusesAnUnresolvableIdentityInARecord(t *testing.T) {
	t.Parallel()
	s, ghost := unresolvableFixture(t)
	empT, _ := s.Type("Employee")
	empKey := immutable.WrapKey([]any{"e1"})
	ghostKey := immutable.WrapKey([]any{"g1"})
	emp := graph.InstanceParts{
		TypeID: empT.ID(), PrimaryKey: empKey,
		Properties: immutable.WrapProperties(map[string]any{"employee_id": "e1"}),
	}

	for _, c := range []struct {
		position string
		parts    func(p *graph.SnapshotParts)
	}{
		{"unresolved source", func(p *graph.SnapshotParts) {
			p.Unresolved = []graph.UnresolvedParts{{
				SourceType: ghost, SourceKey: ghostKey, Relation: "WORKS_AT",
				TargetKey: empKey, Reason: "target_missing",
			}}
		}},
		{"edge source", func(p *graph.SnapshotParts) {
			p.Edges = []graph.EdgeParts{{
				Relation: "WORKS_AT", SourceType: ghost, SourceKey: ghostKey,
				TargetKey:  empKey,
				Properties: immutable.WrapProperties(nil),
			}}
		}},
		{"instance", func(p *graph.SnapshotParts) {
			stray := emp
			stray.TypeID = ghost
			p.Instances = append(p.Instances, stray)
		}},
		{"duplicate instance", func(p *graph.SnapshotParts) {
			stray := emp
			stray.TypeID = ghost
			p.Duplicates = []graph.DuplicateParts{{
				Instance: stray,
			}}
		}},
		{"duplicate parent", func(p *graph.SnapshotParts) {
			p.Duplicates = []graph.DuplicateParts{{
				Instance:   emp,
				ParentType: ghost, ParentKey: ghostKey, Relation: "PIECES",
			}}
		}},
	} {
		t.Run(c.position, func(t *testing.T) {
			t.Parallel()
			parts := graph.SnapshotParts{
				Types: []schema.TypeID{empT.ID()},
				Instances: []graph.InstanceParts{
					emp,
				},
			}
			c.parts(&parts)
			_, res := graph.RebuildSnapshot(s, parts)
			requireIdentityRefusal(t, res, "unresolvable type identity "+ghost.String()+" at "+c.position+" position")
		})
	}
}

// A types entry the schema cannot resolve draws the identity rule's refusal and
// no other. The addressability rule judges the types table too, and would say
// the type is "reachable only through an intermediate import" — false of a type
// that is in no import at all — so it leaves an unresolvable identity to this
// rule, as the root-type rule does.
func TestRebuildSnapshot_RefusesAnUnresolvableTypesEntry(t *testing.T) {
	t.Parallel()
	s, ghost := unresolvableFixture(t)

	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{Types: []schema.TypeID{ghost}})
	requireIdentityRefusal(t, res, "unresolvable type identity "+ghost.String()+" at types entry 0")
	if n := len(slicesOf(res)); n != 1 {
		t.Errorf("want the one refusal, got %d: %s", n, res)
	}
}

func slicesOf(result diag.Result) []diag.Issue {
	var out []diag.Issue
	for issue := range result.Issues() {
		out = append(out, issue)
	}
	return out
}

// TestRebuildSnapshot_NilSchemaPanics holds RebuildSnapshot to the rule New,
// NewBatchAssembler and NewBatchAssemblerFromSnapshot apply: every identity of
// a snapshot names a type of its schema, and a nil schema names none.
func TestRebuildSnapshot_NilSchemaPanics(t *testing.T) {
	t.Parallel()
	_, ghost := unresolvableFixture(t)
	defer func() {
		if r := recover(); r != "graph.RebuildSnapshot: nil Schema" {
			t.Errorf("recovered %v, want the nil-schema panic", r)
		}
	}()
	graph.RebuildSnapshot(nil, graph.SnapshotParts{Types: []schema.TypeID{ghost}})
}
