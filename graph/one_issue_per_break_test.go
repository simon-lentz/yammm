package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// The structural rules report one break once: a unit a rule refused feeds no
// later rule. The snapshot package's single-break table holds RebuildSnapshot
// to it over every shape a document can state; the rows here are the shapes
// only parts can state, and the import's.

func failures(res diag.Result) int {
	n := 0
	for issue := range res.Issues() {
		if issue.Severity().IsFailure() {
			n++
		}
	}
	return n
}

func TestRebuildSnapshot_OneBreakDrawsOneIssue(t *testing.T) {
	t.Parallel()
	company := func(p graph.SnapshotParts, key, id string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID: p.Instances[0].TypeID, PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": id}),
		}
	}
	tag := func(s *schema.Schema) graph.InstanceParts {
		ty, _ := s.Type("Tag")
		return graph.InstanceParts{
			TypeID: ty.ID(), PrimaryKey: immutable.WrapKey([]any{"t1"}),
			Properties: immutable.WrapProperties(map[string]any{"code": "t1"}),
		}
	}
	foreign := mustTypeID(t, loadAs(t, "schema \"other\"\n\ntype Employee {\n\tid String primary\n}\n"), "Employee")
	// An edge or a record whose source type does not resolve could stand under
	// any root at its key, so the root's required association it names is not
	// empty.
	unsourced := func(p *graph.SnapshotParts, source schema.TypeID) {
		dropEdge(p, "WORKS_AT")
		p.Unresolved = append(p.Unresolved, graph.UnresolvedParts{
			SourceType: source, SourceKey: immutable.WrapKey([]any{"e1"}), Relation: "WORKS_AT",
			TargetKey: immutable.WrapKey([]any{"c9"}), Reason: "target_missing",
		})
	}
	for _, c := range []struct {
		name   string
		mutate func(s *schema.Schema, p *graph.SnapshotParts)
		want   int
		phrase string
	}{
		{"a required association's one record, whose source identity is zero", func(_ *schema.Schema, p *graph.SnapshotParts) {
			unsourced(p, schema.TypeID{})
		}, 1, "zero type identity at unresolved source"},
		{"a required association's one record, whose source identity another schema declares", func(_ *schema.Schema, p *graph.SnapshotParts) {
			unsourced(p, foreign)
		}, 1, "unresolvable type identity"},
		{"a required association's one edge, whose source identity is zero", func(_ *schema.Schema, p *graph.SnapshotParts) {
			for i := range p.Edges {
				if p.Edges[i].Relation == "WORKS_AT" {
					p.Edges[i].SourceType = schema.TypeID{}
				}
			}
		}, 1, "zero type identity at edge source"},
		{"a required association's one edge, whose source identity another schema declares", func(_ *schema.Schema, p *graph.SnapshotParts) {
			for i := range p.Edges {
				if p.Edges[i].Relation == "WORKS_AT" {
					p.Edges[i].SourceType = foreign
				}
			}
		}, 1, "unresolvable type identity"},
		// Such a record stands under no other relation and at no other key.
		{"a record whose source identity is zero, at another key than a required association's root", func(_ *schema.Schema, p *graph.SnapshotParts) {
			unsourced(p, schema.TypeID{})
			p.Unresolved[len(p.Unresolved)-1].SourceKey = immutable.WrapKey([]any{"e9"})
		}, 2, `under the required association "WORKS_AT"`},
		{"a record whose source identity is zero, under another relation than a required association's", func(_ *schema.Schema, p *graph.SnapshotParts) {
			unsourced(p, schema.TypeID{})
			p.Unresolved[len(p.Unresolved)-1].Relation = "KNOWS"
		}, 2, `under the required association "WORKS_AT"`},
		{"an absent record under an undeclared relation", func(_ *schema.Schema, p *graph.SnapshotParts) {
			p.Unresolved = append(p.Unresolved, record(*p, "NOPE", "absent", ""))
		}, 1, `under "NOPE", which its type does not declare as an association`},
		{"an absent record whose source identity is zero", func(_ *schema.Schema, p *graph.SnapshotParts) {
			p.Unresolved = append(p.Unresolved, graph.UnresolvedParts{
				SourceKey: immutable.WrapKey([]any{"e1"}), Relation: "WORKS_AT", Reason: "absent",
			})
		}, 1, "zero type identity at unresolved source"},
		{"an empty record under a composition's name", func(s *schema.Schema, p *graph.SnapshotParts) {
			ty, _ := s.Type("Order")
			order := graph.InstanceParts{
				TypeID: ty.ID(), PrimaryKey: immutable.WrapKey([]any{"o1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "o1"}),
			}
			p.Instances = append(p.Instances, order)
			p.Unresolved = append(p.Unresolved, graph.UnresolvedParts{
				SourceType: order.TypeID, SourceKey: order.PrimaryKey, Relation: "LINES", Reason: "empty",
			})
		}, 1, `under "LINES", which its type does not declare as an association`},
		{"a root of a part type the types list", func(s *schema.Schema, p *graph.SnapshotParts) {
			p.Types = append(p.Types, tag(s).TypeID)
			p.Instances = append(p.Instances, tag(s))
		}, 1, "types entry 2 denotes"},
		{"a root of a part type the types do not list", func(s *schema.Schema, p *graph.SnapshotParts) {
			p.Instances = append(p.Instances, tag(s))
		}, 1, "1 root instance(s) of type"},
		{"two roots of a keyless part type the types list", func(s *schema.Schema, p *graph.SnapshotParts) {
			ty, _ := s.Type("Line")
			line := graph.InstanceParts{TypeID: ty.ID(), Properties: immutable.WrapProperties(map[string]any{"note": "x"})}
			p.Types = append(p.Types, ty.ID())
			p.Instances = append(p.Instances, line, line)
		}, 1, "types entry 2 denotes"},
		// A root whose key the key rule refuses is no duplicate, in either order.
		// The parts do not say which of two roots at one address an edge there
		// is the edge of, so no rule judges the address's (one) count.
		{"a root whose key is another root's and not its property's, after that root", func(_ *schema.Schema, p *graph.SnapshotParts) {
			p.Instances = append(p.Instances, company(*p, "c1", "zz"))
		}, 1, `disagrees with property "id"`},
		{"a root whose key is another root's and not its property's, before that root", func(_ *schema.Schema, p *graph.SnapshotParts) {
			p.Instances = append([]graph.InstanceParts{company(*p, "c1", "zz")}, p.Instances...)
		}, 1, `disagrees with property "id"`},
		{"a root whose key is another root's and not its property's, beside that root's (one) association of two edges", func(_ *schema.Schema, p *graph.SnapshotParts) {
			emp := p.Instances[1]
			p.Instances = append(p.Instances, graph.InstanceParts{
				TypeID: emp.TypeID, PrimaryKey: emp.PrimaryKey, Properties: immutable.WrapProperties(map[string]any{"id": "e9"}),
			})
			p.Edges = append(p.Edges, graph.EdgeParts{
				Relation: "WORKS_AT", SourceType: emp.TypeID, SourceKey: emp.PrimaryKey, TargetKey: immutable.WrapKey([]any{"c7"}),
			})
		}, 1, `disagrees with property "id"`},
		{"two roots at one address", func(_ *schema.Schema, p *graph.SnapshotParts) {
			p.Instances = append(p.Instances, company(*p, "c1", "c1"))
		}, 1, "two instances of type"},
		// The refused edge is held, so the required association is not empty,
		// and it is no record beside the absent one.
		{"an edge holding an undeclared property beside an absent record", func(_ *schema.Schema, p *graph.SnapshotParts) {
			for i := range p.Edges {
				if p.Edges[i].Relation == "WORKS_AT" {
					p.Edges[i].Properties = immutable.WrapProperties(map[string]any{"bogus": 1})
				}
			}
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "absent", ""))
		}, 1, `edge property "bogus"`},
		// A root whose key the key rule refused is judged by no record fact.
		{"a root whose key is refused, with an edge and an absent record beside it", func(_ *schema.Schema, p *graph.SnapshotParts) {
			p.Instances[1].Properties = immutable.WrapProperties(map[string]any{"id": "ex"})
			p.Unresolved = append(p.Unresolved, record(*p, "WORKS_AT", "absent", ""))
		}, 1, `disagrees with property "id"`},
		{"a (one) association of two edges, one to no root", func(_ *schema.Schema, p *graph.SnapshotParts) {
			emp := p.Instances[1]
			p.Edges = append(p.Edges, graph.EdgeParts{
				Relation: "WORKS_AT", SourceType: emp.TypeID, SourceKey: emp.PrimaryKey, TargetKey: immutable.WrapKey([]any{"c7"}),
			})
		}, 1, `(one) association "WORKS_AT"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s, parts := recordFactsParts(t)
			c.mutate(s, &parts)
			snap, res := graph.RebuildSnapshot(s, parts)
			if snap != nil {
				t.Error("a refused rebuild returned a snapshot")
			}
			if n := failures(res); n != c.want {
				t.Errorf("%d issues, want %d: %s", n, c.want, res)
			}
			if !strings.Contains(res.String(), c.phrase) {
				t.Errorf("no issue naming %q: %s", c.phrase, res)
			}
		})
	}
}

func loadAs(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "entry.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

// snapshotOf validates each raw instance of typeName under s and adds it.
func snapshotOf(t *testing.T, s *schema.Schema, typeName string, raws ...map[string]any) *graph.Snapshot {
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
			if _, bres := graph.NewBatchAssemblerFromSnapshot(t.Context(), bound, snap); failures(bres) != failures(res) {
				t.Errorf("the seeded assembler reports %d issues, the import %d: %s", failures(bres), failures(res), bres)
			}
		})
	}
}

// A composition slot the importing schema refuses holds no occupant a
// duplicate's conflict is resolved against: the slot's refusal is the one
// issue, and the duplicate under it draws none of its own.
func TestNewFromSnapshot_ADuplicateUnderARefusedSlotResolvesNoConflict(t *testing.T) {
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
			// The (one) slot holds two children, each of another type than its
			// target, and the duplicate is a third break of its own.
			name:   "a (one) slot of another target, its duplicate of the old one",
			bound:  strings.Replace(built, "(_:many) Item\n", "(_:one) Other\n", 1) + "\npart type Other {\n\tcode String primary\n}\n",
			code:   diag.E_GRAPH_INVALID_COMPOSITION,
			issues: 4,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := loadAs(t, built)
			order, _ := s.Type("Order")
			item, _ := s.Type("Item")
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
			g.AddComposed(t.Context(), order.ID(), `["o1"]`, "ITEMS",
				instancetest.VI("Item", instancetest.TypeID(item.ID()), instancetest.PK("a"), instancetest.Props(map[string]any{"code": "a"})))
			snap := g.Snapshot()
			if len(snap.Duplicates()) != 1 {
				t.Fatalf("the snapshot holds %d duplicates, want 1", len(snap.Duplicates()))
			}
			bound := loadAs(t, c.bound)
			_, res = graph.NewFromSnapshot(bound, snap)
			want := max(c.issues, 1)
			if failures(res) != want || !res.HasCode(c.code) {
				t.Errorf("%d issues, want %d with a %s: %s", failures(res), want, c.code, res)
			}
			if _, bres := graph.NewBatchAssemblerFromSnapshot(t.Context(), bound, snap); failures(bres) != failures(res) {
				t.Errorf("the seeded assembler reports %d issues, the import %d: %s", failures(bres), failures(res), bres)
			}
		})
	}
}

// Edges whose source no root stands at count toward no (one) association: each
// addresses an instance the parts do not hold, which RebuildSnapshot refuses
// as Fatal E_INTERNAL, one issue for each.
func TestRebuildSnapshot_EdgesOfNoRootCountTowardNoAssociation(t *testing.T) {
	t.Parallel()
	s, parts := recordFactsParts(t)
	emp := parts.Instances[1]
	for _, c := range []string{"c1", "c7"} {
		parts.Edges = append(parts.Edges, graph.EdgeParts{
			Relation: "WORKS_AT", SourceType: emp.TypeID, SourceKey: immutable.WrapKey([]any{"e9"}), TargetKey: immutable.WrapKey([]any{c}),
		})
	}
	_, res := graph.RebuildSnapshot(s, parts)
	if res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("edges of no root were counted toward a (one) association: %s", res)
	}
	if failures(res) != 2 || !res.HasCode(diag.E_INTERNAL) {
		t.Errorf("%s, want one Fatal E_INTERNAL for each edge of no root", res)
	}
}

// A root duplicate's conflict is the root at its own type and key, so parts
// that give it a parent state a field no rule reads: RebuildSnapshot refuses
// them, and a root duplicate that names none rebuilds.
func TestRebuildSnapshot_ARootDuplicateNamesNoParent(t *testing.T) {
	t.Parallel()
	s := loadAs(t, "schema \"entry\"\n\ntype Company {\n\tid String primary\n}\n")
	company, _ := s.Type("Company")
	c1 := graph.InstanceParts{
		TypeID: company.ID(), PrimaryKey: immutable.WrapKey([]any{"c1"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "c1"}),
	}
	for name, c := range map[string]struct {
		parent schema.TypeID
		key    immutable.Key
		want   int
	}{
		"no parent":       {want: 0},
		"a parent type":   {parent: company.ID(), want: 1},
		"a parent key":    {key: immutable.WrapKey([]any{"c1"}), want: 1},
		"a parent at all": {parent: company.ID(), key: immutable.WrapKey([]any{"c1"}), want: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
				Types: []schema.TypeID{company.ID()}, Instances: []graph.InstanceParts{c1},
				Duplicates: []graph.DuplicateParts{{Instance: c1, ParentType: c.parent, ParentKey: c.key}},
			})
			named := res.HasCode(diag.E_INTERNAL) && strings.Contains(res.String(), "is a root duplicate, and names the parent")
			if failures(res) != c.want || c.want > 0 && !named {
				t.Errorf("%d issues, want %d: %s", failures(res), c.want, res)
			}
		})
	}
}

// A composed duplicate's conflict is resolved only in a slot a sound root holds
// alone, at the import as at the reader. Under a keyless (many) slot a held
// parent's duplicate has no conflict, which is its one break; a parent whose
// key the importing schema refuses, or an address two roots stand at, is the
// break instead, and the duplicate under it draws nothing more.
func TestNewFromSnapshot_ADuplicateResolvesNoConflictAtAnUnheldParent(t *testing.T) {
	t.Parallel()
	const keylessItem = "\npart type Item {\n\tcode String\n}\n"
	for _, c := range []struct {
		name, built, bound string
		parents            []string
		code               diag.Code
	}{
		{
			name:    "a held parent, the slot keyless under the importing schema",
			built:   "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> ITEMS (_:many) Item\n}\n",
			bound:   "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> ITEMS (_:many) Item\n}\n",
			parents: []string{"o1"},
			code:    diag.E_GRAPH_INVALID_COMPOSITION,
		},
		{
			name:    "a parent whose key the importing schema refuses",
			built:   "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> ITEMS (_:many) Item\n}\n",
			bound:   "schema \"entry\"\n\ntype Order {\n\tid String primary\n\tn String primary\n\t*-> ITEMS (_:many) Item\n}\n",
			parents: []string{"o1"},
			code:    diag.E_GRAPH_INVALID_PK,
		},
		{
			name:    "two parents the importing schema keys as one",
			built:   "schema \"entry\"\n\ntype Order {\n\tid String primary\n\t*-> ITEMS (_:many) Item\n}\n",
			bound:   "schema \"entry\"\n\ntype Order {\n\tid Timestamp primary\n\t*-> ITEMS (_:many) Item\n}\n",
			parents: []string{"2024-01-01T00:00:00Z", "2024-01-01T00:00:00+00:00"},
			code:    diag.E_DUPLICATE_PK,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			s := loadAs(t, c.built+"\npart type Item {\n\tcode String primary\n}\n")
			order, _ := s.Type("Order")
			item, _ := s.Type("Item")
			g := graph.New(s)
			v := instance.NewValidator(s)
			for _, id := range c.parents {
				vi, res := v.ValidateOne(t.Context(), "Order", instance.RawInstance{Properties: map[string]any{
					"id": id, "ITEMS": []any{map[string]any{"code": "a"}},
				}})
				if res.HasErrors() {
					t.Fatalf("validate: %s", res)
				}
				if res := g.Add(t.Context(), vi); res.HasErrors() {
					t.Fatalf("add: %s", res)
				}
			}
			g.AddComposed(t.Context(), order.ID(), graph.FormatKey(c.parents[0]), "ITEMS",
				instancetest.VI("Item", instancetest.TypeID(item.ID()), instancetest.PK("a"), instancetest.Props(map[string]any{"code": "a"})))
			if len(g.Snapshot().Duplicates()) != 1 {
				t.Fatalf("the snapshot holds %d duplicates, want 1", len(g.Snapshot().Duplicates()))
			}
			_, res := graph.NewFromSnapshot(loadAs(t, c.bound+keylessItem), g.Snapshot())
			if failures(res) != 1 || !res.HasCode(c.code) {
				t.Errorf("%d issues, want one %s: %s", failures(res), c.code, res)
			}
		})
	}
}
