package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// Graph.Add derives an association's records from the data: an edge per
// resolved target, target_missing per target no root holds, and an absent or
// empty record alone, only under a required association. RebuildSnapshot holds
// its parts to the same facts, so every constructor yields a snapshot Add could.
const recordFactsSchema = `schema "facts"

type Company {
	id String primary
}

type Employee {
	id String primary
	--> WORKS_AT (one) Company
	--> KNOWS (_:many) Company
	--> AUDITS (one:many) Company
	--> ADVISES (_:one) Company
}

type Order {
	id String primary
	*-> LINES (many) Line
	*-> MAIN (_:one) Tag
}

part type Line {
	note String
}

part type Tag {
	code String primary
}
`

func recordFactsParts(t *testing.T) (*schema.Schema, graph.SnapshotParts) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), recordFactsSchema, "facts.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	compID := mustTypeID(t, s, "Company")
	empID := mustTypeID(t, s, "Employee")
	c1 := immutable.WrapKey([]any{"c1"})
	e1 := immutable.WrapKey([]any{"e1"})
	return s, graph.SnapshotParts{
		Types: []schema.TypeID{compID, empID},
		Instances: []graph.InstanceParts{
			{TypeID: compID, PrimaryKey: c1, Properties: immutable.WrapProperties(map[string]any{"id": "c1"})},
			{TypeID: empID, PrimaryKey: e1, Properties: immutable.WrapProperties(map[string]any{"id": "e1"})},
		},
		Edges: []graph.EdgeParts{
			{Relation: "WORKS_AT", SourceType: empID, SourceKey: e1, TargetKey: c1},
			{Relation: "AUDITS", SourceType: empID, SourceKey: e1, TargetKey: c1},
		},
	}
}

func dropEdge(parts *graph.SnapshotParts, relation string) {
	kept := parts.Edges[:0]
	for _, e := range parts.Edges {
		if e.Relation != relation {
			kept = append(kept, e)
		}
	}
	parts.Edges = kept
}

func record(parts graph.SnapshotParts, relation, reason, target string) graph.UnresolvedParts {
	emp := parts.Instances[1]
	u := graph.UnresolvedParts{SourceType: emp.TypeID, SourceKey: emp.PrimaryKey, Relation: relation, Reason: reason}
	if target != "" {
		u.TargetKey = immutable.WrapKey([]any{target})
	}
	return u
}

func TestRebuildSnapshot_HoldsRecordsToTheFactsAddDerivesThemBy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		mutate func(p *graph.SnapshotParts)
		want   string
	}{
		{"a required association with no record", func(p *graph.SnapshotParts) {
			dropEdge(p, "WORKS_AT")
		}, `holds no edge and no record under the required association "WORKS_AT"`},
		{"an absent record under an optional association", func(p *graph.SnapshotParts) {
			p.Unresolved = append(p.Unresolved, record(*p, "KNOWS", "absent", ""))
		}, `states reason "absent" under "KNOWS"`},
		{"an empty record beside an edge", func(p *graph.SnapshotParts) {
			p.Unresolved = append(p.Unresolved, record(*p, "AUDITS", "empty", ""))
		}, `holds an empty record beside another record`},
		{"two absent records in one association", func(p *graph.SnapshotParts) {
			dropEdge(p, "AUDITS")
			p.Unresolved = append(p.Unresolved, record(*p, "AUDITS", "absent", ""), record(*p, "AUDITS", "absent", ""))
		}, `holds an absent record beside another record`},
		{"a target_missing record naming a target a root holds", func(p *graph.SnapshotParts) {
			p.Unresolved = append(p.Unresolved, record(*p, "KNOWS", "target_missing", "c1"))
		}, `names target`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, parts := recordFactsParts(t)
			c.mutate(&parts)
			snap, res := graph.RebuildSnapshot(s, parts)
			if snap != nil {
				t.Error("a snapshot Add could not make was built")
			}
			requireFatalNaming(t, res, c.want)
		})
	}
}

// Each record Add does make is accepted, so the facts refuse only what Add
// would never record.
func TestRebuildSnapshot_AcceptsEachRecordAddMakes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		mutate func(p *graph.SnapshotParts)
	}{
		{"edges alone", func(*graph.SnapshotParts) {}},
		{"an absent record under a required (one)", func(p *graph.SnapshotParts) {
			dropEdge(p, "WORKS_AT")
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "absent", ""))
		}},
		{"an empty record under a required (many)", func(p *graph.SnapshotParts) {
			dropEdge(p, "AUDITS")
			p.Unresolved = append(p.Unresolved, record(*p, "AUDITS", "empty", ""))
		}},
		{"a target_missing record beside an edge", func(p *graph.SnapshotParts) {
			p.Unresolved = append(p.Unresolved, record(*p, "AUDITS", "target_missing", "c9"))
		}},
		{"a target_missing record alone under a required association", func(p *graph.SnapshotParts) {
			dropEdge(p, "WORKS_AT")
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "target_missing", "c9"))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, parts := recordFactsParts(t)
			c.mutate(&parts)
			if _, res := graph.RebuildSnapshot(s, parts); res.Err() != nil {
				t.Errorf("a record Add makes was refused: %s", res)
			}
		})
	}
}

// Add never records a duplicate in a keyless (many) slot: a keyless child has
// no key to collide at, so every child is attached.
func TestRebuildSnapshot_RefusesADuplicateInAKeylessManySlot(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), recordFactsSchema, "facts.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	orderID := mustTypeID(t, s, "Order")
	lineID := mustTypeID(t, s, "Line")
	o1 := immutable.WrapKey([]any{"o1"})
	line := graph.InstanceParts{TypeID: lineID, Properties: immutable.WrapProperties(map[string]any{"note": "a"})}

	snap, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{orderID},
		Instances: []graph.InstanceParts{{
			TypeID: orderID, PrimaryKey: o1,
			Properties: immutable.WrapProperties(map[string]any{"id": "o1"}),
			Composed:   map[string][]graph.InstanceParts{"LINES": {line}},
		}},
		Duplicates: []graph.DuplicateParts{{Instance: line, ParentType: orderID, ParentKey: o1, Relation: "LINES"}},
	})
	if snap != nil {
		t.Error("a duplicate in a keyless (many) slot was accepted")
	}
	requireFatalNaming(t, result, `is under the keyless (many) composition "LINES", where no child conflicts`)
}

// An absent record beside another of its slot is one break, reported once.
func TestRebuildSnapshot_ReportsAStandingAloneBreakOncePerSlot(t *testing.T) {
	t.Parallel()
	s, parts := recordFactsParts(t)
	dropEdge(&parts, "AUDITS")
	parts.Unresolved = append(parts.Unresolved,
		record(parts, "AUDITS", "absent", ""), record(parts, "AUDITS", "absent", ""), record(parts, "AUDITS", "absent", ""))
	_, res := graph.RebuildSnapshot(s, parts)
	n := 0
	for issue := range res.Issues() {
		if strings.Contains(issue.Message(), "beside another record") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("the slot's break was reported %d times, want once: %s", n, res)
	}
}

// An absent or empty record beside another record is one break, and a (one)
// association whose other records still number more than one is a second: the
// fact refuses the record alone, so the records beside it are counted.
func TestRebuildSnapshot_ARecordBesideAnOverflowIsTwoBreaks(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		reasons  []string
		edges    int
		lost     bool
		overflow string
	}{
		{name: "an absent record beside two edges", reasons: []string{"absent"}, edges: 2, overflow: "holds 2 records"},
		{name: "an empty record beside an edge and a target_missing record", reasons: []string{"empty"}, edges: 1, lost: true, overflow: "holds 2 records"},
		{name: "two absent records beside two edges", reasons: []string{"absent", "absent"}, edges: 2, overflow: "holds 2 records"},
		{name: "two absent records beside an edge", reasons: []string{"absent", "absent"}, edges: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, parts := recordFactsParts(t)
			compID := mustTypeID(t, s, "Company")
			c2 := immutable.WrapKey([]any{"c2"})
			parts.Instances = append(parts.Instances, graph.InstanceParts{
				TypeID: compID, PrimaryKey: c2, Properties: immutable.WrapProperties(map[string]any{"id": "c2"}),
			})
			if c.edges == 2 {
				worksAt := parts.Edges[0]
				worksAt.TargetKey = c2
				parts.Edges = append(parts.Edges, worksAt)
			}
			if c.lost {
				parts.Unresolved = append(parts.Unresolved, record(parts, "WORKS_AT", "target_missing", "c9"))
			}
			for _, reason := range c.reasons {
				parts.Unresolved = append(parts.Unresolved, record(parts, "WORKS_AT", reason, ""))
			}
			snap, res := graph.RebuildSnapshot(s, parts)
			if snap != nil {
				t.Error("a snapshot Add could not make was built")
			}
			beside, overflow := 0, 0
			for issue := range res.Issues() {
				switch {
				case issue.Severity() == diag.Fatal && issue.Code() == diag.E_INTERNAL && strings.Contains(issue.Message(), "beside another record"):
					beside++
				case issue.Code() == diag.E_GRAPH_CARDINALITY && c.overflow != "" && strings.Contains(issue.Message(), c.overflow):
					overflow++
				default:
					t.Errorf("an issue no break draws: %v", issue)
				}
			}
			want := 0
			if c.overflow != "" {
				want = 1
			}
			if beside != 1 || overflow != want {
				t.Errorf("%d record breaks and %d overflows, want 1 and %d: %s", beside, overflow, want, res)
			}
		})
	}
}

// A record a fact refuses is no (one) count and no record beside an absent
// one, so it draws its own issue alone, and the records beside it are judged
// without it.
func TestRebuildSnapshot_ARecordAFactRefusesIsNoCount(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name   string
		mutate func(p *graph.SnapshotParts, c2 immutable.Key)
		want   []string
	}{
		{"an absent record under an optional association beside an edge", func(p *graph.SnapshotParts, _ immutable.Key) {
			p.Edges = append(p.Edges, advises(*p, p.Instances[0].PrimaryKey))
			p.Unresolved = append(p.Unresolved, record(*p, "ADVISES", "absent", ""))
		}, []string{"which is optional"}},
		{"an absent record under an optional association beside two edges", func(p *graph.SnapshotParts, c2 immutable.Key) {
			p.Edges = append(p.Edges, advises(*p, p.Instances[0].PrimaryKey), advises(*p, c2))
			p.Unresolved = append(p.Unresolved, record(*p, "ADVISES", "absent", ""))
		}, []string{"which is optional", `(one) association "ADVISES" of Employee[["e1"]] holds 2 records`}},
		{"two absent records under an optional association", func(p *graph.SnapshotParts, _ immutable.Key) {
			p.Unresolved = append(p.Unresolved, record(*p, "ADVISES", "absent", ""), record(*p, "ADVISES", "absent", ""))
		}, []string{"which is optional", "which is optional"}},
		{"a target_missing record naming a root beside an edge", func(p *graph.SnapshotParts, _ immutable.Key) {
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "target_missing", "c2"))
		}, []string{"which a root holds"}},
		{"an absent record beside a target_missing record naming a root", func(p *graph.SnapshotParts, _ immutable.Key) {
			dropEdge(p, "WORKS_AT")
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "absent", ""), record(*p, "WORKS_AT", "target_missing", "c2"))
		}, []string{"which a root holds"}},
		{"a refused absent record beside an absent record and two edges", func(p *graph.SnapshotParts, c2 immutable.Key) {
			worksAt := p.Edges[0]
			worksAt.TargetKey = c2
			p.Edges = append(p.Edges, worksAt)
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "absent", "c1"), record(*p, "WORKS_AT", "absent", ""))
		}, []string{"carries a target key or edge properties", "beside another record", `(one) association "WORKS_AT" of Employee[["e1"]] holds 2 records`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, parts := recordFactsParts(t)
			c2 := immutable.WrapKey([]any{"c2"})
			parts.Instances = append(parts.Instances, graph.InstanceParts{
				TypeID: mustTypeID(t, s, "Company"), PrimaryKey: c2, Properties: immutable.WrapProperties(map[string]any{"id": "c2"}),
			})
			c.mutate(&parts, c2)
			snap, res := graph.RebuildSnapshot(s, parts)
			if snap != nil {
				t.Error("a snapshot Add could not make was built")
			}
			matched := make([]bool, len(c.want))
			n := 0
			for issue := range res.Issues() {
				n++
				found := false
				for i, phrase := range c.want {
					if !matched[i] && strings.Contains(issue.Message(), phrase) {
						matched[i], found = true, true
						break
					}
				}
				if !found {
					t.Errorf("an issue no break draws: %v", issue)
				}
			}
			if n != len(c.want) {
				t.Errorf("%d issues, want %d naming %q: %s", n, len(c.want), c.want, res)
			}
		})
	}
}

// advises is an ADVISES edge from the parts' Employee to the Company at target.
func advises(parts graph.SnapshotParts, target immutable.Key) graph.EdgeParts {
	emp := parts.Instances[1]
	return graph.EdgeParts{Relation: "ADVISES", SourceType: emp.TypeID, SourceKey: emp.PrimaryKey, TargetKey: target}
}

// A composed duplicate is judged against its parent's slot, so each way the
// slot cannot hold its conflict is refused rather than read: a parent that is
// no root, a relation the parent does not declare, and an empty (one) slot.
func TestRebuildSnapshot_RefusesAComposedDuplicateWhoseSlotHoldsNoConflict(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), recordFactsSchema, "facts.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	orderID := mustTypeID(t, s, "Order")
	tagID := mustTypeID(t, s, "Tag")
	o1 := immutable.WrapKey([]any{"o1"})
	tag := graph.InstanceParts{
		TypeID: tagID, PrimaryKey: immutable.WrapKey([]any{"t1"}),
		Properties: immutable.WrapProperties(map[string]any{"code": "t1"}),
	}
	for _, c := range []struct {
		name, parentKey, relation, want string
	}{
		{"a parent that is no root", "o9", "MAIN", `names parent`},
		{"a relation the parent does not declare", "o1", "NOPE", `names "NOPE", which`},
		{"an empty (one) slot", "o1", "MAIN", `is under the (one) composition "MAIN", which holds 0 children`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
				Types: []schema.TypeID{orderID},
				Instances: []graph.InstanceParts{{
					TypeID: orderID, PrimaryKey: o1,
					Properties: immutable.WrapProperties(map[string]any{"id": "o1"}),
				}},
				Duplicates: []graph.DuplicateParts{{
					Instance: tag, ParentType: orderID, ParentKey: immutable.WrapKey([]any{c.parentKey}), Relation: c.relation,
				}},
			})
			if snap != nil {
				t.Error("a duplicate whose slot holds no conflict was accepted")
			}
			requireFatalNaming(t, res, c.want)
		})
	}
}

// The facts compare keys in canonical form: an edge's source key and a
// target_missing record's target key may spell a UUID any way its constraint
// accepts, and still address the root at that UUID.
func TestRebuildSnapshot_RecordFactsCompareCanonicalKeys(t *testing.T) {
	t.Parallel()
	const uuidSchema = `schema "ids"

type Company {
	id UUID primary
}

type Employee {
	id UUID primary
	--> WORKS_AT (one) Company
	--> KNOWS (_:many) Company
}
`
	s, res := schema.LoadString(t.Context(), uuidSchema, "ids.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	const company, employee = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11", "b1ffcd88-8d1a-4ef8-bb6d-6bb9bd380a22"
	compID, empID := mustTypeID(t, s, "Company"), mustTypeID(t, s, "Employee")
	upper := func(k string) immutable.Key { return immutable.WrapKey([]any{strings.ToUpper(k)}) }
	parts := func() graph.SnapshotParts {
		return graph.SnapshotParts{
			Types: []schema.TypeID{compID, empID},
			Instances: []graph.InstanceParts{
				{TypeID: compID, PrimaryKey: immutable.WrapKey([]any{company}), Properties: immutable.WrapProperties(map[string]any{"id": company})},
				{TypeID: empID, PrimaryKey: immutable.WrapKey([]any{employee}), Properties: immutable.WrapProperties(map[string]any{"id": employee})},
			},
			Edges: []graph.EdgeParts{{Relation: "WORKS_AT", SourceType: empID, SourceKey: upper(employee), TargetKey: upper(company)}},
		}
	}
	if _, res := graph.RebuildSnapshot(s, parts()); res.Err() != nil {
		t.Errorf("an edge spelling its source key in upper case left the root uncovered: %s", res)
	}
	p := parts()
	p.Unresolved = []graph.UnresolvedParts{{SourceType: empID, SourceKey: upper(employee), Relation: "KNOWS", TargetKey: upper(company), Reason: "target_missing"}}
	_, res = graph.RebuildSnapshot(s, p)
	requireFatalNaming(t, res, "which a root holds")
}
