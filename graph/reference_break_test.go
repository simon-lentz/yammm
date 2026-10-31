package graph_test

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

const referenceBreakSchema = `schema "refs"

type Company {
	id String primary
}

type Employee {
	id String primary
	--> WORKS_AT (_:one) Company
}

type Order {
	id String primary
	*-> TAGS (_:many) Tag
	*-> SEAL (_:one) Tag
	*-> LINES (_:many) Line
}

part type Line {
	note String
}

part type Tag {
	code String primary
}
`

// referenceParts builds parts over [referenceBreakSchema] with one Company, one
// Employee whose WORKS_AT names it, and one Order holding Tags t1 and t2 and a
// duplicate of t1. They rebuild clean.
type referenceParts struct {
	graph.SnapshotParts
	company, employee, order, tag, line schema.TypeID
}

func newReferenceParts(t *testing.T) (*schema.Schema, *referenceParts) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), referenceBreakSchema, "refs.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	p := &referenceParts{
		company: mustTypeID(t, s, "Company"), employee: mustTypeID(t, s, "Employee"),
		order: mustTypeID(t, s, "Order"), tag: mustTypeID(t, s, "Tag"), line: mustTypeID(t, s, "Line"),
	}
	p.Instances = []graph.InstanceParts{
		p.root(p.company, "c1", "c1"),
		p.root(p.employee, "e1", "e1"),
		p.ordered("o1", "o1", map[string][]graph.InstanceParts{"TAGS": {p.child("t1", "t1"), p.child("t2", "t2")}}),
	}
	p.Edges = []graph.EdgeParts{p.worksAt("e1", "c1")}
	p.Duplicates = []graph.DuplicateParts{p.tagDuplicate("o1", "TAGS", "t1", "t1")}
	return s, p
}

func (p *referenceParts) root(id schema.TypeID, key, property string) graph.InstanceParts {
	return graph.InstanceParts{
		TypeID: id, PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(map[string]any{"id": property}),
	}
}

func (p *referenceParts) ordered(key, property string, composed map[string][]graph.InstanceParts) graph.InstanceParts {
	o := p.root(p.order, key, property)
	o.Composed = composed
	return o
}

func (p *referenceParts) child(key, property string) graph.InstanceParts {
	return graph.InstanceParts{
		TypeID: p.tag, PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(map[string]any{"code": property}),
	}
}

func (p *referenceParts) worksAt(source, target string) graph.EdgeParts {
	return graph.EdgeParts{
		Relation: "WORKS_AT", SourceType: p.employee, SourceKey: immutable.WrapKey([]any{source}),
		TargetKey: immutable.WrapKey([]any{target}),
	}
}

func (p *referenceParts) tagDuplicate(parent, relation, key, property string) graph.DuplicateParts {
	return graph.DuplicateParts{
		Instance: p.child(key, property), ParentType: p.order,
		ParentKey: immutable.WrapKey([]any{parent}), Relation: relation,
	}
}

func TestRebuildSnapshot_TheReferencePartsAreClean(t *testing.T) {
	t.Parallel()
	s, p := newReferenceParts(t)
	snap, res := graph.RebuildSnapshot(s, p.SnapshotParts)
	if snap == nil || failures(res) != 0 {
		t.Fatalf("the parts are refused: %s", res)
	}
	dups := snap.Duplicates()
	if len(dups) != 1 || dups[0].Conflict() == nil || dups[0].Conflict().PrimaryKey().String() != `["t1"]` {
		t.Errorf("duplicates %v, want one whose conflict is the Tag at t1", dups)
	}
}

// A reference no root resolves draws its own issue beside every other break. A
// reference to the address a refused root's key property states is that root's
// break, and draws none. A reference a check refused for another fact draws that
// fact alone.
func TestRebuildSnapshot_ReferencesDrawTheirIssueBesideAnotherBreak(t *testing.T) {
	t.Parallel()
	tagsOf := func(p *referenceParts, children ...graph.InstanceParts) {
		p.Instances[2] = p.ordered("o1", "o1", map[string][]graph.InstanceParts{"TAGS": children})
	}
	foreign := loadAs(t, "schema \"other\"\n\ntype Order {\n\tid String primary\n}\n\ntype Tag {\n\tcode String primary\n}\n")
	foreignOrder, foreignTag := mustTypeID(t, foreign, "Order"), mustTypeID(t, foreign, "Tag")
	withNope := func(p *referenceParts) map[string][]graph.InstanceParts {
		return map[string][]graph.InstanceParts{"NOPE": {p.child("t1", "t1")}}
	}
	for _, c := range []struct {
		name    string
		mutate  func(p *referenceParts)
		phrases []string
	}{
		{"an edge whose target is no root", func(p *referenceParts) {
			p.Edges[0] = p.worksAt("e1", "c7")
		}, []string{`edge target`}},
		{"an edge whose source is no root", func(p *referenceParts) {
			p.Edges[0] = p.worksAt("e7", "c1")
		}, []string{`edge source`}},
		{"an edge of no source and no target", func(p *referenceParts) {
			p.Edges[0] = p.worksAt("e7", "c7")
		}, []string{`edge source`}},
		{"an edge whose target is no root, beside a refused root", func(p *referenceParts) {
			p.Instances[0] = p.root(p.company, "c1", "cz")
			p.Edges[0] = p.worksAt("e1", "c7")
		}, []string{`disagrees with property "id"`, `edge target`}},
		{"an edge to no root beside an accepted one under a (one) association", func(p *referenceParts) {
			p.Edges = append(p.Edges, p.worksAt("e1", "c7"))
		}, []string{`edge target`}},
		{"an edge to the address a refused root's key property states", func(p *referenceParts) {
			p.Instances[0] = p.root(p.company, "cx", "c1")
		}, []string{`disagrees with property "id"`}},
		{"a key property no key can hold beside an edge to the root's stored key", func(p *referenceParts) {
			p.Instances[0].Properties = immutable.WrapProperties(map[string]any{"id": math.NaN()})
		}, []string{`disagrees with property "id"`}},
		{"a key property of a list no key can hold beside an edge to the root's stored key", func(p *referenceParts) {
			p.Instances[0].Properties = immutable.WrapProperties(map[string]any{"id": []any{math.NaN()}})
		}, []string{`disagrees with property "id"`}},
		{"a key property of a map no key can hold beside an edge to the root's stored key", func(p *referenceParts) {
			p.Instances[0].Properties = immutable.WrapProperties(map[string]any{"id": map[string]any{"n": math.Inf(1)}})
		}, []string{`disagrees with property "id"`}},
		{"a record whose source is no root", func(p *referenceParts) {
			p.Unresolved = []graph.UnresolvedParts{{
				SourceType: p.employee, SourceKey: immutable.WrapKey([]any{"e7"}), Relation: "WORKS_AT",
				TargetKey: immutable.WrapKey([]any{"c9"}), Reason: "target_missing",
			}}
		}, []string{`unresolved source`}},
		{"a record whose source is the address a refused root's key property states", func(p *referenceParts) {
			p.Instances[1] = p.root(p.employee, "ex", "e1")
			p.Edges = nil
			p.Unresolved = []graph.UnresolvedParts{{
				SourceType: p.employee, SourceKey: immutable.WrapKey([]any{"e1"}), Relation: "WORKS_AT",
				TargetKey: immutable.WrapKey([]any{"c9"}), Reason: "target_missing",
			}}
		}, []string{`disagrees with property "id"`}},
		{"a root duplicate of no root", func(p *referenceParts) {
			p.Duplicates = []graph.DuplicateParts{{Instance: p.root(p.company, "c7", "c7")}}
		}, []string{`no root is at its own type and key`}},
		{"a root duplicate at the address a refused root's key property states", func(p *referenceParts) {
			p.Instances[0] = p.root(p.company, "cx", "c1")
			p.Duplicates = []graph.DuplicateParts{{Instance: p.root(p.company, "c1", "c1")}}
		}, []string{`disagrees with property "id"`}},
		{"a root duplicate whose own key is refused", func(p *referenceParts) {
			p.Duplicates = []graph.DuplicateParts{{Instance: p.root(p.company, "c7", "cz")}}
		}, []string{`disagrees with property "id"`}},
		{"a duplicate holding composed children, beside a parent that is no root", func(p *referenceParts) {
			p.Duplicates[0] = p.tagDuplicate("o7", "TAGS", "t1", "t1")
			p.Duplicates[0].Instance.Composed = map[string][]graph.InstanceParts{"BOGUS": {{}}}
		}, []string{`has composed children`, `names parent`}},
		{"a composed duplicate under the address a refused root's key property states", func(p *referenceParts) {
			p.Instances[2].PrimaryKey = immutable.WrapKey([]any{"ox"})
		}, []string{`disagrees with property "id"`}},
		{"a composed duplicate under no composition of the address a refused root's key property states", func(p *referenceParts) {
			p.Instances[2].PrimaryKey = immutable.WrapKey([]any{"ox"})
			p.Duplicates[0] = p.tagDuplicate("o1", "NOPE", "t1", "t1")
		}, []string{`disagrees with property "id"`, `names "NOPE", which`}},
		{"a duplicate naming the slot a refused root's name refused, at its key property's address", func(p *referenceParts) {
			p.Instances[2] = p.ordered("ox", "o1", map[string][]graph.InstanceParts{"NOPE": {p.child("t1", "t1")}})
			p.Duplicates[0] = p.tagDuplicate("o1", "NOPE", "t1", "t1")
		}, []string{`disagrees with property "id"`, `under "NOPE"`}},
		{"a duplicate naming the slot a refused root's name refused, at an address a sound root holds", func(p *referenceParts) {
			p.Instances = append(p.Instances, p.ordered("o1", "oz", map[string][]graph.InstanceParts{"NOPE": {p.child("t1", "t1")}}))
			p.Duplicates[0] = p.tagDuplicate("o1", "NOPE", "t1", "t1")
		}, []string{`disagrees with property "id"`, `under "NOPE"`, `names "NOPE", which`}},
		{"a duplicate naming the slot its holder's name refused", func(p *referenceParts) {
			p.Instances[2] = p.ordered("o1", "o1", map[string][]graph.InstanceParts{"NOPE": {p.child("t1", "t1")}})
			p.Duplicates[0] = p.tagDuplicate("o1", "NOPE", "t1", "t1")
		}, []string{`under "NOPE"`}},
		{"a duplicate whose key a child's key property states", func(p *referenceParts) {
			tagsOf(p, p.child("tx", "t1"), p.child("t2", "t2"))
		}, []string{`disagrees with property "code"`}},
		{"a duplicate of the holder's child, beside a refused root at the holder's address", func(p *referenceParts) {
			p.Instances = append(p.Instances, p.root(p.order, "o1", "oz"))
		}, []string{`disagrees with property "id"`}},
		{"a duplicate whose key no child holds or states", func(p *referenceParts) {
			p.Duplicates[0] = p.tagDuplicate("o1", "TAGS", "t9", "t9")
		}, []string{`holds no child of the composition "TAGS" at its key`}},
		{"a duplicate whose own key is refused", func(p *referenceParts) {
			p.Duplicates[0] = p.tagDuplicate("o1", "TAGS", "t9", "tz")
		}, []string{`disagrees with property "code"`}},
		{"a duplicate whose key no child holds, under two siblings at one key", func(p *referenceParts) {
			tagsOf(p, p.child("t1", "t1"), p.child("t1", "t1"))
			p.Duplicates[0] = p.tagDuplicate("o1", "TAGS", "t9", "t9")
		}, []string{`holds children 0 and 1 at key`}},
		{"a duplicate at the key of a child of another type", func(p *referenceParts) {
			tagsOf(p, p.root(p.company, "t1", "t1"), p.child("t2", "t2"))
		}, []string{`child 0 is of type`}},
		{"two siblings at one key beside a child of another type", func(p *referenceParts) {
			tagsOf(p, p.child("t1", "t1"), p.root(p.company, "t3", "t3"), p.child("t1", "t1"))
		}, []string{`child 1 is of type`, `holds children 0 and 2 at key`}},
		{"a refused root holding a slot its type does not declare, and a duplicate at its stored key", func(p *referenceParts) {
			p.Instances[2] = p.ordered("ox", "o1", withNope(p))
			p.Duplicates[0] = p.tagDuplicate("ox", "NOPE", "t1", "t1")
		}, []string{`disagrees with property`, `holds composed children under "NOPE"`}},
		{"a sound root of a slot its type does not declare beside a refused root at its address", func(p *referenceParts) {
			p.Instances[2] = p.ordered("o1", "oz", nil)
			p.Instances = append(p.Instances, p.ordered("o1", "o1", withNope(p)))
			p.Duplicates[0] = p.tagDuplicate("o1", "NOPE", "t1", "t1")
		}, []string{`disagrees with property`, `holds composed children under "NOPE"`}},
		{"two sound roots at one address, the first holding a slot its type does not declare", func(p *referenceParts) {
			p.Instances[2] = p.ordered("o1", "o1", withNope(p))
			p.Instances = append(p.Instances, p.ordered("o1", "o1", nil))
			p.Duplicates[0] = p.tagDuplicate("o1", "NOPE", "t1", "t1")
		}, []string{`two instances`, `holds composed children under "NOPE"`}},
		{"two sound roots at one address, the second holding a slot its type does not declare", func(p *referenceParts) {
			p.Instances[2] = p.ordered("o1", "o1", nil)
			p.Instances = append(p.Instances, p.ordered("o1", "o1", withNope(p)))
			p.Duplicates[0] = p.tagDuplicate("o1", "NOPE", "t1", "t1")
		}, []string{`two instances`, `holds composed children under "NOPE"`}},
		{"a duplicate of a parent that is no root, under a slot its type does not declare", func(p *referenceParts) {
			p.Duplicates[0] = p.tagDuplicate("o7", "NOPE", "t1", "t1")
		}, []string{`which is not a root`}},
		{"an absent record of no root under an optional association", func(p *referenceParts) {
			p.Unresolved = []graph.UnresolvedParts{{SourceType: p.employee, SourceKey: immutable.WrapKey([]any{"e7"}), Relation: "WORKS_AT", Reason: "absent"}}
		}, []string{`unresolved source`}},
		{"a null key property, an edge to a null key", func(p *referenceParts) {
			p.Instances[0] = p.root(p.company, "cx", "c1")
			p.Instances[0].Properties = immutable.WrapProperties(map[string]any{"id": nil})
			p.Edges[0].TargetKey = immutable.WrapKey([]any{nil})
		}, []string{`is absent or null`, `edge target`}},
		{"a key of the wrong arity, its key property a list of no number, an edge to its key", func(p *referenceParts) {
			p.Instances[0] = p.root(p.company, "cx", "c1")
			p.Instances[0].PrimaryKey = immutable.WrapKey([]any{"cx", "extra"})
			p.Instances[0].Properties = immutable.WrapProperties(map[string]any{"id": []any{math.NaN()}})
			p.Edges[0].TargetKey = immutable.WrapKey([]any{"cx", "extra"})
		}, []string{`2-part target key`, `primary key has 2 components`}},
		{"a key property no document can hold, an edge from its text", func(p *referenceParts) {
			p.Instances[1] = p.root(p.employee, "ex", "\xff")
			p.Edges[0].SourceKey = immutable.WrapKey([]any{"\xff"})
		}, []string{`disagrees with property`, `edge source`}},
		{"a key property of no number renders as its value", func(p *referenceParts) {
			p.Instances[0].Properties = immutable.WrapProperties(map[string]any{"id": math.NaN()})
		}, []string{`vs [NaN]`}},
		{"a key property behind a pointer renders as the value it holds", func(p *referenceParts) {
			nan := math.NaN()
			p.Instances[0].Properties = immutable.WrapProperties(map[string]any{"id": &nan})
		}, []string{`vs [NaN]`}},
		{"a root of a keyless type at a key, a root duplicate of no key", func(p *referenceParts) {
			p.Instances = append(p.Instances, graph.InstanceParts{
				TypeID: p.line, PrimaryKey: immutable.WrapKey([]any{"x"}), Properties: immutable.WrapProperties(map[string]any{"note": "n"}),
			})
			p.Duplicates = append(p.Duplicates, graph.DuplicateParts{Instance: graph.InstanceParts{
				TypeID: p.line, Properties: immutable.WrapProperties(map[string]any{"note": "n"}),
			}})
		}, []string{`declares no primary key`}},
		{"a root and a root duplicate of a part type", func(p *referenceParts) {
			p.Instances = append(p.Instances, p.child("t1", "t1"))
			p.Duplicates[0] = graph.DuplicateParts{Instance: p.child("t1", "t1")}
		}, []string{`root instance`}},
		{"two roots of a part type at one key", func(p *referenceParts) {
			p.Instances = append(p.Instances, p.child("t1", "t1"), p.child("t1", "t1"))
		}, []string{`2 root instance`}},
		{"two root duplicates of a part type", func(p *referenceParts) {
			p.Duplicates[0] = graph.DuplicateParts{Instance: p.child("t1", "t1")}
			p.Duplicates = append(p.Duplicates, graph.DuplicateParts{Instance: p.child("t2", "t2")})
		}, []string{`of a type that is a part type`}},
		{"a root duplicate of a part type whose own key is refused", func(p *referenceParts) {
			p.Duplicates[0] = graph.DuplicateParts{Instance: p.child("tx", "t1")}
		}, []string{`disagrees with property`, `of a type that is a part type`}},
		{"a duplicate whose conflict only a refused root at its parent's address holds", func(p *referenceParts) {
			p.Instances = append([]graph.InstanceParts{p.ordered("o1", "oz", map[string][]graph.InstanceParts{"TAGS": {p.child("t9", "t9")}})}, p.Instances...)
			p.Duplicates[0] = p.tagDuplicate("o1", "TAGS", "t9", "t9")
		}, []string{`disagrees with property "id"`}},
		{"a duplicate whose parent type no schema resolves", func(p *referenceParts) {
			p.Duplicates[0].ParentType = foreignOrder
		}, []string{`unresolvable type identity`}},
		{"a duplicate of no parent type", func(p *referenceParts) {
			p.Duplicates[0].ParentType = schema.TypeID{}
		}, []string{`zero type identity`}},
		{"a duplicate whose type no schema resolves", func(p *referenceParts) {
			p.Duplicates[0].Instance.TypeID = foreignTag
		}, []string{`unresolvable type identity`}},
		{"a duplicate whose type no schema resolves, of a parent that is no root", func(p *referenceParts) {
			p.Duplicates[0] = p.tagDuplicate("o9", "TAGS", "t1", "t1")
			p.Duplicates[0].Instance.TypeID = foreignTag
		}, []string{`unresolvable type identity`, `which is not a root`}},
		{"a duplicate of no type, of a parent that is no root", func(p *referenceParts) {
			p.Duplicates[0] = p.tagDuplicate("o9", "TAGS", "t1", "t1")
			p.Duplicates[0].Instance.TypeID = schema.TypeID{}
		}, []string{`zero type identity`, `which is not a root`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, p := newReferenceParts(t)
			c.mutate(p)
			snap, res := graph.RebuildSnapshot(s, p.SnapshotParts)
			if snap != nil {
				t.Error("a refused rebuild returned a snapshot")
			}
			if failures(res) != len(c.phrases) {
				t.Errorf("%d issues, want %d: %s", failures(res), len(c.phrases), res)
			}
			for _, phrase := range c.phrases {
				if !strings.Contains(res.String(), phrase) {
					t.Errorf("no issue naming %q: %s", phrase, res)
				}
			}
		})
	}
}

// An edge a reference check refused leaves its source's (one) count, so a record
// beside it is beside no edge, as with an edge any other rule refused.
func TestRebuildSnapshot_AnEdgeOfNoRootIsNoEdgeBesideARecord(t *testing.T) {
	t.Parallel()
	s, parts := recordFactsParts(t)
	dropEdge(&parts, "WORKS_AT")
	emp := parts.Instances[1]
	parts.Edges = append(parts.Edges, graph.EdgeParts{
		Relation: "WORKS_AT", SourceType: emp.TypeID, SourceKey: emp.PrimaryKey, TargetKey: immutable.WrapKey([]any{"c7"}),
	})
	parts.Unresolved = append(parts.Unresolved, record(parts, "WORKS_AT", "absent", ""))
	_, res := graph.RebuildSnapshot(s, parts)
	if failures(res) != 1 || !strings.Contains(res.String(), "edge target") {
		t.Errorf("%d issues, want the edge's reference alone: %s", failures(res), res)
	}
}

// An identity no schema resolves draws one issue however many children hold it,
// and that issue names one child across runs.
func TestRebuildSnapshot_AnUnresolvableIdentityNamesOneChild(t *testing.T) {
	t.Parallel()
	ghost := mustTypeID(t, loadAs(t, "schema \"other\"\n\ntype Ghost {\n\tcode String primary\n}\n"), "Ghost")
	var first string
	for range 40 {
		s, p := newReferenceParts(t)
		a, b, c := p.child("a", "a"), p.child("b", "b"), p.child("c", "c")
		a.TypeID, b.TypeID, c.TypeID = ghost, ghost, ghost
		p.Instances[2] = p.ordered("o1", "o1", map[string][]graph.InstanceParts{"TAGS": {a}, "LINES": {b}, "SEAL": {c}})
		p.Duplicates = nil
		_, res := graph.RebuildSnapshot(s, p.SnapshotParts)
		if failures(res) != 1 {
			t.Fatalf("%d issues, want 1: %s", failures(res), res)
		}
		if first == "" {
			first = res.String()
		}
		if res.String() != first {
			t.Fatalf("two runs read %q and %q", first, res)
		}
	}
}

const stampedSchema = `schema "stamped"

type Run {
	at Timestamp primary
	--> NEXT (_:many) Run
	*-> TAGS (_:many) Stamp
}

part type Stamp {
	at Timestamp primary
}
`

// An address compares under the schema's canonical form: a child's stored key, its
// key property and a duplicate's key are one address however each is spelled.
func TestRebuildSnapshot_AnAddressComparesCanonically(t *testing.T) {
	t.Parallel()
	const (
		zulu   = "2024-01-01T00:00:00Z"
		offset = "2024-01-01T00:00:00+00:00"
		later  = "2030-01-01T00:00:00Z"
	)
	s := loadAs(t, stampedSchema)
	run, stamp := mustTypeID(t, s, "Run"), mustTypeID(t, s, "Stamp")
	child := func(key, at string) graph.InstanceParts {
		return graph.InstanceParts{TypeID: stamp, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{"at": at})}
	}
	root := func(key, at string, children ...graph.InstanceParts) graph.InstanceParts {
		i := graph.InstanceParts{TypeID: run, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{"at": at})}
		if len(children) > 0 {
			i.Composed = map[string][]graph.InstanceParts{"TAGS": children}
		}
		return i
	}
	duplicate := func(key string) graph.DuplicateParts {
		return graph.DuplicateParts{Instance: child(key, key), ParentType: run, ParentKey: immutable.WrapKey([]any{zulu}), Relation: "TAGS"}
	}
	for _, c := range []struct {
		name    string
		parts   graph.SnapshotParts
		phrases []string
	}{
		{"a duplicate of a sound child, spelled another way", graph.SnapshotParts{
			Instances:  []graph.InstanceParts{root(zulu, zulu, child(offset, offset))},
			Duplicates: []graph.DuplicateParts{duplicate(zulu)},
		}, nil},
		{"a duplicate of a refused child's key property, spelled another way", graph.SnapshotParts{
			Instances:  []graph.InstanceParts{root(zulu, zulu, child(later, zulu))},
			Duplicates: []graph.DuplicateParts{duplicate(offset)},
		}, []string{`disagrees with property`}},
		{"a duplicate of a refused child's stored key, spelled another way", graph.SnapshotParts{
			Instances:  []graph.InstanceParts{root(zulu, zulu, child(offset, later))},
			Duplicates: []graph.DuplicateParts{duplicate(zulu)},
		}, []string{`disagrees with property`}},
		{"a duplicate of a refused child's stored key, the duplicate spelled another way", graph.SnapshotParts{
			Instances:  []graph.InstanceParts{root(zulu, zulu, child(zulu, later))},
			Duplicates: []graph.DuplicateParts{duplicate(offset)},
		}, []string{`disagrees with property`}},
		{"a refusal names the canonical key", graph.SnapshotParts{
			Instances:  []graph.InstanceParts{root(zulu, zulu, child(zulu, zulu))},
			Duplicates: []graph.DuplicateParts{duplicate("2025-01-01T00:00:00+00:00")},
		}, []string{`[["2025-01-01T00:00:00Z"]], holds no child`}},
		{"an edge to a refused root's key property, spelled another way", graph.SnapshotParts{
			Instances: []graph.InstanceParts{root(later, offset), root("2031-01-01T00:00:00Z", "2031-01-01T00:00:00Z")},
			Edges:     []graph.EdgeParts{{Relation: "NEXT", SourceType: run, SourceKey: immutable.WrapKey([]any{"2031-01-01T00:00:00Z"}), TargetKey: immutable.WrapKey([]any{zulu})}},
		}, []string{`disagrees with property`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, res := graph.RebuildSnapshot(s, c.parts)
			if failures(res) != len(c.phrases) {
				t.Errorf("%d issues, want %d: %s", failures(res), len(c.phrases), res)
			}
			for _, phrase := range c.phrases {
				if !strings.Contains(res.String(), phrase) {
					t.Errorf("no issue naming %q: %s", phrase, res)
				}
			}
		})
	}
}

// A slot the importing schema makes (many) and a parent it keys canonically: the
// import looks the parent up by its canonical key, so the record is judged.
func TestNewFromSnapshot_ADuplicateOfACanonicalParentIsJudged(t *testing.T) {
	t.Parallel()
	const item = "\npart type Item {\n\tcode String primary\n}\n"
	built := loadAs(t, "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> ITEMS (_:one) Item\n}\n"+item)
	bound := loadAs(t, "schema \"entry\"\n\ntype Order {\n\tid Timestamp primary\n\t*-> ITEMS (_:many) Item\n}\n"+item)
	const offset = "2024-01-01T00:00:00+00:00"
	g := graph.New(built)
	vi, res := instance.NewValidator(built).ValidateOne(t.Context(), "Order", instance.RawInstance{
		Properties: map[string]any{"id": offset, "ITEMS": []any{map[string]any{"code": "a"}}},
	})
	if res.HasErrors() {
		t.Fatal(res)
	}
	if r := g.Add(t.Context(), vi); r.HasErrors() {
		t.Fatal(r)
	}
	g.AddComposed(t.Context(), mustTypeID(t, built, "Order"), graph.FormatKey(offset), "ITEMS", instancetest.VI("Item",
		instancetest.TypeID(mustTypeID(t, built, "Item")), instancetest.PK("b"), instancetest.Props(map[string]any{"code": "b"})))
	if n := len(g.Snapshot().Duplicates()); n != 1 {
		t.Fatalf("%d duplicates, want 1", n)
	}
	_, imported := graph.NewFromSnapshot(bound, g.Snapshot())
	if failures(imported) != 1 || !strings.Contains(imported.String(), `holds no child of the composition "ITEMS" at its key`) {
		t.Errorf("%d issues, want the record's alone: %s", failures(imported), imported)
	}
}

// A keyless type declares no key property, so a root of it states no address an
// edge's empty key could name.
func TestRebuildSnapshot_ARootOfAKeylessTypeStatesNoAddress(t *testing.T) {
	t.Parallel()
	s := loadAs(t, "schema \"own\"\n\ntype Order {\n\tid String primary\n}\n\nabstract type Line {\n\tnote String\n\t--> OWNER (_:one) Order\n}\n")
	order, line := mustTypeID(t, s, "Order"), mustTypeID(t, s, "Line")
	_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Instances: []graph.InstanceParts{
			{TypeID: order, PrimaryKey: immutable.WrapKey([]any{"o1"}), Properties: immutable.WrapProperties(map[string]any{"id": "o1"})},
			{TypeID: line, PrimaryKey: immutable.WrapKey([]any{"x"}), Properties: immutable.WrapProperties(map[string]any{"note": "n"})},
		},
		Edges: []graph.EdgeParts{{SourceType: line, SourceKey: immutable.WrapKey(nil), Relation: "OWNER", TargetKey: immutable.WrapKey([]any{"o1"})}},
	})
	if !strings.Contains(res.String(), "edge source") {
		t.Errorf("RebuildSnapshot = %s, want the edge's source refused", res)
	}
}

// Every root asks whether a record of an unresolvable source could stand at it, so
// the records are read once per relation, never once per root.
func TestRebuildSnapshot_RecordsOfNoSourceCostLinearly(t *testing.T) {
	s := loadAs(t, "schema \"own\"\n\ntype Run {\n\tat Timestamp primary\n\t--> OWNER (one) Run\n}\n")
	run := mustTypeID(t, s, "Run")
	foreign := mustTypeID(t, loadAs(t, "schema \"other\"\n\ntype Ghost {\n\tid String primary\n}\n"), "Ghost")
	allocations := func(k int) float64 {
		parts := graph.SnapshotParts{Instances: make([]graph.InstanceParts, k), Unresolved: make([]graph.UnresolvedParts, k)}
		for i := range k {
			at := time.Unix(int64(i), 0).UTC().Format(time.RFC3339)
			parts.Instances[i] = graph.InstanceParts{
				TypeID: run, PrimaryKey: immutable.WrapKey([]any{at}), Properties: immutable.WrapProperties(map[string]any{"at": at}),
			}
			parts.Unresolved[i] = graph.UnresolvedParts{
				SourceType: foreign, SourceKey: immutable.WrapKey([]any{time.Unix(int64(i+k), 0).UTC().Format(time.RFC3339)}), Relation: "OWNER", Reason: "absent",
			}
		}
		return testing.AllocsPerRun(1, func() {
			if _, res := graph.RebuildSnapshot(s, parts); res.OK() {
				t.Fatal("the parts are accepted")
			}
		})
	}
	if small, large := allocations(100), allocations(400); large > 6*small {
		t.Errorf("%.0f allocations at 100 roots and %.0f at 400, want growth near 4 times", small, large)
	}
}

// A root with no composed child orders no slots, so the walk adds no allocation
// per leaf for a rule it never reads.
func TestRebuildSnapshot_ALeafRootOrdersNoSlots(t *testing.T) {
	s := loadAs(t, "schema \"leaf\"\n\ntype Leaf {\n\tid String primary\n}\n")
	leaf := mustTypeID(t, s, "Leaf")
	allocations := func(k int) float64 {
		parts := graph.SnapshotParts{Types: []schema.TypeID{leaf}, Instances: make([]graph.InstanceParts, k)}
		for i := range k {
			id := fmt.Sprintf("l%d", i)
			parts.Instances[i] = graph.InstanceParts{
				TypeID: leaf, PrimaryKey: immutable.WrapKey([]any{id}), Properties: immutable.WrapProperties(map[string]any{"id": id}),
			}
		}
		return testing.AllocsPerRun(5, func() {
			if _, res := graph.RebuildSnapshot(s, parts); !res.OK() {
				t.Fatalf("the parts are refused: %s", res)
			}
		})
	}
	const small, large = 400, 1600
	got := (allocations(large) - allocations(small)) / (large - small)
	if got > 6 {
		t.Errorf("%.2f allocations per leaf, want at most 6", got)
	}
}

// The duplicates of one slot read its children once, so their cost grows with the
// slot, never with its square. A refused child is read as a sound one is.
func TestRebuildSnapshot_DuplicatesOfOneSlotCostLinearly(t *testing.T) {
	const timestamped = `schema "stamped"

type Order {
	id Timestamp primary
	*-> EVENTS (_:many) Event
}

part type Event {
	at Timestamp primary
}
`
	s, res := schema.LoadString(t.Context(), timestamped, "stamped.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	order, event := mustTypeID(t, s, "Order"), mustTypeID(t, s, "Event")
	orderAt := func(i int) string {
		return time.Unix(int64(1_000_000+i), 0).UTC().Format(time.RFC3339)
	}
	allocations := func(k int, refused bool) float64 {
		children := make([]graph.InstanceParts, k)
		parts := graph.SnapshotParts{Duplicates: make([]graph.DuplicateParts, k)}
		for i := range k {
			at := time.Unix(int64(i), 0).UTC().Format(time.RFC3339)
			stored := at
			if refused {
				stored = time.Unix(int64(i+k), 0).UTC().Format(time.RFC3339)
			}
			children[i] = graph.InstanceParts{
				TypeID: event, PrimaryKey: immutable.WrapKey([]any{stored}),
				Properties: immutable.WrapProperties(map[string]any{"at": at}),
			}
			parts.Duplicates[i] = graph.DuplicateParts{
				Instance: graph.InstanceParts{
					TypeID: event, PrimaryKey: immutable.WrapKey([]any{at}),
					Properties: immutable.WrapProperties(map[string]any{"at": at}),
				},
				ParentType: order, ParentKey: immutable.WrapKey([]any{orderAt(0)}), Relation: "EVENTS",
			}
		}
		parts.Instances = []graph.InstanceParts{{
			TypeID: order, PrimaryKey: immutable.WrapKey([]any{orderAt(0)}),
			Properties: immutable.WrapProperties(map[string]any{"id": orderAt(0)}),
			Composed:   map[string][]graph.InstanceParts{"EVENTS": children},
		}}
		for i := range k {
			id := orderAt(i + 1)
			parts.Instances = append(parts.Instances, graph.InstanceParts{
				TypeID: order, PrimaryKey: immutable.WrapKey([]any{id}),
				Properties: immutable.WrapProperties(map[string]any{"id": id}),
			})
		}
		return testing.AllocsPerRun(1, func() {
			if _, res := graph.RebuildSnapshot(s, parts); res.OK() == refused {
				t.Fatalf("refused %v, want %v: %s", !res.OK(), refused, res)
			}
		})
	}
	for _, refused := range []bool{false, true} {
		if small, large := allocations(100, refused), allocations(400, refused); large > 6*small {
			t.Errorf("refused children %v: %.0f allocations at 100 duplicates and %.0f at 400, want growth near 4 times", refused, small, large)
		}
	}
}
