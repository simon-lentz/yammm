package snapshot_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// A snapshot's structural facts — type identity, composition slot, stored key
// with its key properties, declared names and association shape — are one
// invariant held at every constructor of a snapshot: Graph.Add,
// RebuildSnapshot, the .ys decoder and the import. The class below builds one
// legal graph under revisionBase and offers it, under every revision of that
// schema, to five doors — Graph.Add, RebuildSnapshot, snapshot.Load,
// NewFromSnapshot and NewBatchAssemblerFromSnapshot — which must agree, but
// for the one fact Add's input cannot state (legalAtAdd).

const revisionBase = `schema "rev"

type Audit {
	id String primary
}

type Company {
	id String primary
	code String
	region String
}

part type Badge {
	code String primary
}

part type Note {
	code String primary
}

type Office {
	id String primary
}

type Person {
	id String primary
	nickname String
	--> WORKS_AT (_:one) Company {
		since Integer
	}
	--> MENTORS (_:many) Person
	*-> BADGES (many) Badge
}
`

// revision is one point of the class: each field moves one structural fact
// of revisionBase, and 0 keeps it.
type revision struct {
	audit int // 1: Audit is not declared (identity)
	// 1: Company's key is code (key moved); 2: id and region (arity); 3: vat,
	// which the data does not hold (key property absent)
	key      int
	badges   int // 1: BADGES targets Note (slot retargeted); 2: renamed TOKENS
	nickname int // 1: Person declares no nickname (undeclared property)
	since    int // 1: WORKS_AT declares no since (undeclared edge property)
	works    int // 1: WORKS_AT is not declared; 2: WORKS_AT targets Office
	mentors  int // 1: MENTORS is (one) and holds two records
	widened  bool
}

func (r revision) legal() bool {
	return r.audit == 0 && r.key == 0 && r.badges == 0 && r.nickname == 0 && r.since == 0 && r.works == 0 && r.mentors == 0
}

// legalAtAdd is legal() for Graph.Add, whose edge target names a key and no
// type: the relation's declared target decides it, so a retargeted relation is
// not a fact Add's input can state.
func (r revision) legalAtAdd() bool {
	moved := r
	if moved.works == 2 {
		moved.works = 0
	}
	return moved.legal()
}

func (r revision) String() string {
	return fmt.Sprintf("audit%d_key%d_badges%d_nickname%d_since%d_works%d_mentors%d_widened%v",
		r.audit, r.key, r.badges, r.nickname, r.since, r.works, r.mentors, r.widened)
}

func (r revision) source() string {
	var b strings.Builder
	b.WriteString("schema \"rev\"\n\n")
	if r.audit == 0 {
		b.WriteString("type Audit {\n\tid String primary\n}\n\n")
	}
	switch r.key {
	case 0:
		b.WriteString("type Company {\n\tid String primary\n\tcode String\n\tregion String\n}\n\n")
	case 1:
		b.WriteString("type Company {\n\tid String\n\tcode String primary\n\tregion String\n}\n\n")
	case 2:
		b.WriteString("type Company {\n\tid String primary\n\tcode String\n\tregion String primary\n}\n\n")
	case 3:
		b.WriteString("type Company {\n\tid String\n\tcode String\n\tregion String\n\tvat String primary\n}\n\n")
	}
	b.WriteString("part type Badge {\n\tcode String primary\n}\n\npart type Note {\n\tcode String primary\n}\n\n")
	b.WriteString("type Office {\n\tid String primary\n}\n\n")
	b.WriteString("type Person {\n\tid String primary\n")
	if r.nickname == 0 {
		b.WriteString("\tnickname String\n")
	}
	if r.widened {
		b.WriteString("\temail String\n")
	}
	if r.works != 1 {
		target := "Company"
		if r.works == 2 {
			target = "Office"
		}
		b.WriteString("\t--> WORKS_AT (_:one) " + target + " {\n")
		if r.since == 0 {
			b.WriteString("\t\tsince Integer\n")
		}
		if r.widened || r.since != 0 {
			b.WriteString("\t\tnote String\n")
		}
		b.WriteString("\t}\n")
	}
	if r.mentors == 0 {
		b.WriteString("\t--> MENTORS (_:many) Person\n")
	} else {
		b.WriteString("\t--> MENTORS (_:one) Person\n")
	}
	switch r.badges {
	case 0:
		b.WriteString("\t*-> BADGES (many) Badge\n")
	case 1:
		b.WriteString("\t*-> BADGES (many) Note\n")
	case 2:
		b.WriteString("\t*-> TOKENS (many) Badge\n")
	}
	b.WriteString("}\n")
	if r.widened {
		b.WriteString("\ntype Extra {\n\tid String primary\n}\n")
	}
	return b.String()
}

// revisions generates the class: every combination of the seven moves, and
// the legal revision widened by declarations the base graph does not use.
func revisions() []revision {
	var out []revision
	for audit := range 2 {
		for key := range 4 {
			for badges := range 3 {
				for nickname := range 2 {
					for since := range 2 {
						for works := range 3 {
							for mentors := range 2 {
								out = append(out, revision{
									audit: audit, key: key, badges: badges, nickname: nickname,
									since: since, works: works, mentors: mentors,
								})
							}
						}
					}
				}
			}
		}
	}
	return append(out, revision{widened: true})
}

func loadRevisionSchema(t *testing.T, src string) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "rev.yammm")
	if res.HasErrors() {
		t.Fatalf("load revision: %s\n%s", res, src)
	}
	return s
}

// revisionGraph is a legal graph under revisionBase holding each fact at every
// position that stores it: a root of every type, a composed child, an edge
// with a property, an unresolved record with a property, a duplicate, and a
// (many) association holding an edge and an unresolved record of one source.
func revisionGraph(t *testing.T, s *schema.Schema) *graph.Snapshot {
	t.Helper()
	ctx := t.Context()
	v := instance.NewValidator(s)
	g := graph.New(s)
	add := func(typeName string, props map[string]any, wantOK bool) {
		vi, vr := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
		if vr.HasErrors() {
			t.Fatalf("validate %s: %s", typeName, vr)
		}
		if r := g.Add(ctx, vi); r.OK() != wantOK {
			t.Fatalf("add %s: %s", typeName, r)
		}
	}
	add("Audit", map[string]any{"id": "a1"}, true)
	add("Company", map[string]any{"id": "c1", "code": "k1", "region": "eu"}, true)
	add("Office", map[string]any{"id": "o1"}, true)
	add("Person", map[string]any{
		"id": "p1", "nickname": "x",
		"works_at": map[string]any{"_target_id": "c1", "since": 2020},
		"mentors":  []any{map[string]any{"_target_id": "p2"}, map[string]any{"_target_id": "p9"}},
		"badges":   []any{map[string]any{"code": "b1"}},
	}, true)
	add("Person", map[string]any{
		"id": "p2", "nickname": "y",
		"works_at": map[string]any{"_target_id": "c9", "since": 2021},
	}, true)
	add("Person", map[string]any{"id": "p1", "nickname": "z"}, false)
	snap := g.Snapshot()
	if len(snap.Edges()) != 2 || len(snap.Unresolved()) != 2 || len(snap.Duplicates()) != 1 {
		t.Fatalf("fixture: %d edges, %d unresolved, %d duplicates", len(snap.Edges()), len(snap.Unresolved()), len(snap.Duplicates()))
	}
	return snap
}

// addDoor offers every root of snap to Graph.Add under s, as instances that
// carry what snap holds, and reports every refusal.
func addDoor(t *testing.T, s *schema.Schema, snap *graph.Snapshot) diag.Result {
	t.Helper()
	g := graph.New(s)
	c := diag.NewCollector(0)
	for inst := range snap.AllInstances() {
		c.Merge(g.Add(t.Context(), validInstanceOf(t, snap, inst)))
	}
	return c.Result()
}

func validInstanceOf(t *testing.T, snap *graph.Snapshot, inst *graph.Instance) *instance.ValidInstance {
	t.Helper()
	targets := map[string][]instance.ValidEdgeTarget{}
	for _, e := range snap.EdgesFrom(inst) {
		targets[e.Relation()] = append(targets[e.Relation()], instance.NewValidEdgeTarget(e.Target().PrimaryKey(), e.Properties()))
	}
	for _, u := range snap.Unresolved() {
		if u.Source.TypeID() != inst.TypeID() || u.Source.PrimaryKey().String() != inst.PrimaryKey().String() {
			continue
		}
		vals, err := graph.ParseKey(u.TargetKey)
		if err != nil {
			t.Fatalf("parse %s: %v", u.TargetKey, err)
		}
		targets[u.Relation] = append(targets[u.Relation], instance.NewValidEdgeTarget(immutable.WrapKey(vals), u.Properties()))
	}
	var edges map[string]*instance.ValidEdgeData
	for rel, ts := range targets {
		if edges == nil {
			edges = map[string]*instance.ValidEdgeData{}
		}
		edges[rel] = instance.NewValidEdgeData(ts)
	}
	var composed map[string]immutable.Value
	for _, rel := range inst.ComposedRelations() {
		if composed == nil {
			composed = map[string]immutable.Value{}
		}
		var children []*instance.ValidInstance
		for _, child := range inst.Composed(rel) {
			children = append(children, validInstanceOf(t, snap, child))
		}
		composed[rel] = immutable.Wrap(children)
	}
	return instance.NewValidInstance(inst.TypeName(), inst.TypeID(), inst.PrimaryKey(), inst.Properties(), edges, composed, nil)
}

var schemaHashField = regexp.MustCompile(`"schema_hash":"[^"]*"`)

// loadDoor marshals snap and loads it against s as a writer that claims s
// would have written it: the header names s's hash, and the integrity hash
// is not checked. A refusal then comes from the document's contents.
func loadDoor(t *testing.T, s *schema.Schema, snap *graph.Snapshot) diag.Result {
	t.Helper()
	data, mres := snapshot.Marshal(t.Context(), snap)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	if n := len(schemaHashField.FindAllIndex(data, -1)); n != 1 {
		t.Fatalf("schema_hash appears %d times", n)
	}
	data = schemaHashField.ReplaceAll(data, fmt.Appendf(nil, `"schema_hash":%q`, schema.StructuralHash(s)))
	_, res := snapshot.Load(t.Context(), data, s, snapshot.WithIntegrityCheck(false))
	if res.HasCode(diag.E_SNAPSHOT_INCOMPATIBLE_SCHEMA) || res.HasCode(diag.E_SNAPSHOT_INTEGRITY_MISMATCH) {
		t.Fatalf("the load door refused the header, not the contents: %s", res)
	}
	return res
}

// wantRules lists the issue each moved fact must draw at RebuildSnapshot, at
// NewFromSnapshot, and at the decoder behind snapshot.Load. The import reports
// Add's code where RebuildSnapshot, whose parts break the rule only through a
// broken caller, reports E_INTERNAL; the decoder reports a snapshot code, or
// Add's where the decoder shares it.
func wantRules(r revision) map[string][][2]string {
	want := map[string][][2]string{}
	both := func(code diag.Code, phrase string) {
		want["RebuildSnapshot"] = append(want["RebuildSnapshot"], [2]string{code.String(), phrase})
		want["NewFromSnapshot"] = append(want["NewFromSnapshot"], [2]string{code.String(), phrase})
	}
	split := func(rebuild, imported diag.Code, phrase string) {
		want["RebuildSnapshot"] = append(want["RebuildSnapshot"], [2]string{rebuild.String(), phrase})
		want["NewFromSnapshot"] = append(want["NewFromSnapshot"], [2]string{imported.String(), phrase})
	}
	// A types-table row the schema does not declare stops the decoder before
	// the body, so the body's rules are judged only where every row resolves.
	load := func(code diag.Code, phrase string) {
		if r.audit == 0 {
			want["snapshot.Load"] = append(want["snapshot.Load"], [2]string{code.String(), phrase})
		}
	}
	if r.audit == 1 {
		split(diag.E_INTERNAL, diag.E_GRAPH_TYPE_NOT_FOUND, "unresolvable type identity")
	}
	switch r.key {
	case 1:
		both(diag.E_GRAPH_INVALID_PK, "of type Company")
		load(diag.E_SNAPSHOT_MALFORMED, `component 0 disagrees with property "code"`)
	case 2:
		both(diag.E_GRAPH_INVALID_PK, "of type Company")
		load(diag.E_SNAPSHOT_MALFORMED, "has 1 components; the type declares 2")
	case 3:
		both(diag.E_GRAPH_INVALID_PK, `primary key property "vat" is absent or null`)
		load(diag.E_SNAPSHOT_MALFORMED, `key property "vat": it is absent or null`)
	}
	if r.key == 2 && r.works == 0 {
		both(diag.E_GRAPH_INVALID_PK, "carries a 1-part target key; the target declares 2")
		load(diag.E_SNAPSHOT_MALFORMED, "carries a 1-part target key; the target declares 2")
	}
	switch r.badges {
	case 1:
		both(diag.E_GRAPH_INVALID_COMPOSITION, "the composition declares")
		load(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the composition declares as")
	case 2:
		both(diag.E_GRAPH_UNKNOWN_RELATION, "does not declare as a composition")
		load(diag.E_SNAPSHOT_INVALID_COMPOSED, "which the type does not declare as a composition")
	}
	if r.nickname == 1 {
		split(diag.E_INTERNAL, diag.E_UNKNOWN_FIELD, `holds property "nickname"`)
		load(diag.E_SNAPSHOT_MALFORMED, `holds property "nickname", which its type does not declare`)
	}
	if r.since == 1 && r.works != 1 {
		split(diag.E_INTERNAL, diag.E_UNKNOWN_EDGE_FIELD, `holds edge property "since"`)
		load(diag.E_SNAPSHOT_MALFORMED, `holds edge property "since", which the association does not declare`)
	}
	switch r.works {
	case 1:
		both(diag.E_GRAPH_UNKNOWN_RELATION, `under "WORKS_AT", which its type does not declare as an association`)
		load(diag.E_GRAPH_UNKNOWN_RELATION, `under "WORKS_AT", which the type does not declare as an association`)
	case 2:
		both(diag.E_GRAPH_UNKNOWN_RELATION, "the association declares string://rev.yammm:Office")
		load(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the association declares as")
	}
	if r.mentors == 1 {
		both(diag.E_GRAPH_CARDINALITY, `(one) association "MENTORS" of Person[["p1"]] holds 2 records`)
		load(diag.E_GRAPH_CARDINALITY, `(one) association "MENTORS"`)
	}
	return want
}

func hasRule(res diag.Result, code, phrase string) bool {
	for issue := range res.Issues() {
		if issue.Code().String() == code && strings.Contains(issue.Message(), phrase) {
			return true
		}
	}
	return false
}

func TestStructuralFacts_EveryConstructorAgreesAcrossEveryRevision(t *testing.T) {
	t.Parallel()
	base := loadRevisionSchema(t, revisionBase)
	snap := revisionGraph(t, base)
	var accepted, refused int
	for _, r := range revisions() {
		if r.legal() {
			accepted++
		} else {
			refused++
		}
		t.Run(r.String(), func(t *testing.T) {
			t.Parallel()
			s2 := loadRevisionSchema(t, r.source())
			outcomes := map[string]diag.Result{
				"Graph.Add":       addDoor(t, s2, snap),
				"snapshot.Load":   loadDoor(t, s2, snap),
				"RebuildSnapshot": rebuildDoor(t, s2, snap),
			}
			g, ires := importInto(s2, snap)
			outcomes["NewFromSnapshot"] = ires
			ba, sres := seedInto(t.Context(), s2, snap)
			outcomes["NewBatchAssemblerFromSnapshot"] = sres
			for door, res := range outcomes {
				legal := r.legal()
				if door == "Graph.Add" {
					legal = r.legalAtAdd()
				}
				if legal == res.HasErrors() {
					t.Errorf("%s: legal=%v, refused=%v: %s", door, legal, res.HasErrors(), res)
				}
			}
			if !r.legal() && (g != nil || ba != nil) {
				t.Errorf("a refused import returned a graph (%v) or an assembler (%v)", g != nil, ba != nil)
			}
			for door, rules := range wantRules(r) {
				for _, w := range rules {
					if !hasRule(outcomes[door], w[0], w[1]) {
						t.Errorf("%s: no %s naming %q: %s", door, w[0], w[1], outcomes[door])
					}
				}
			}
		})
	}
	t.Logf("%d legal revisions, %d illegal", accepted, refused)
}

func rebuildDoor(t *testing.T, s *schema.Schema, snap *graph.Snapshot) diag.Result {
	t.Helper()
	_, res := graph.RebuildSnapshot(s, partsOf(t, snap))
	return res
}

// The import door as S2's probe found it: a snapshot built under one schema
// and imported into another with the same source id.
func TestNewFromSnapshot_RefusesWhatTheImportingSchemaBreaks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src1, src2 string
		add              map[string]map[string]any
		order            []string
		code             diag.Code
	}{
		{"key property moves", `schema "d"
type Company {
	id String primary
	code String
}
type Person {
	id String primary
	--> WORKS_AT (_:one) Company
}`, `schema "d"
type Company {
	id String
	code String primary
}
type Person {
	id String primary
	--> WORKS_AT (_:one) Company
}`, map[string]map[string]any{
			"Company": {"id": "c1", "code": "k1"},
			"Person":  {"id": "p1", "works_at": map[string]any{"_target_id": "c1"}},
		}, []string{"Company", "Person"}, diag.E_GRAPH_INVALID_PK},
		{"key arity grows", `schema "d"
type Company {
	id String primary
	region String
}
type Person {
	id String primary
	--> WORKS_AT (_:one) Company
}`, `schema "d"
type Company {
	id String primary
	region String primary
}
type Person {
	id String primary
	--> WORKS_AT (_:one) Company
}`, map[string]map[string]any{
			"Company": {"id": "c1", "region": "eu"},
			"Person":  {"id": "p1", "works_at": map[string]any{"_target_id": "c1"}},
		}, []string{"Company", "Person"}, diag.E_GRAPH_INVALID_PK},
		{"composition retargets", `schema "d"
part type Badge {
	code String primary
}
part type Note {
	code String primary
}
type Employee {
	staff_id String primary
	*-> BADGES (many) Badge
}`, `schema "d"
part type Badge {
	code String primary
}
part type Note {
	code String primary
}
type Employee {
	staff_id String primary
	*-> BADGES (many) Note
}`, map[string]map[string]any{
			"Employee": {"staff_id": "e1", "badges": []any{map[string]any{"code": "b1"}}},
		}, []string{"Employee"}, diag.E_GRAPH_INVALID_COMPOSITION},
		{"composition renamed away", `schema "d"
part type Badge {
	code String primary
}
type Employee {
	staff_id String primary
	*-> BADGES (many) Badge
}`, `schema "d"
part type Badge {
	code String primary
}
type Employee {
	staff_id String primary
	*-> TOKENS (many) Badge
}`, map[string]map[string]any{
			"Employee": {"staff_id": "e1", "badges": []any{map[string]any{"code": "b1"}}},
		}, []string{"Employee"}, diag.E_GRAPH_UNKNOWN_RELATION},
		{
			"a revision drops a property", "schema \"rev\"\n\ntype P {\n\tid String primary\n\tnickname String\n}\n",
			"schema \"rev\"\n\ntype P {\n\tid String primary\n}\n",
			map[string]map[string]any{"P": {"id": "p1", "nickname": "x"}},
			[]string{"P"},
			diag.E_UNKNOWN_FIELD,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			s1, r1 := schema.LoadString(ctx, tc.src1, "door.yammm")
			s2, r2 := schema.LoadString(ctx, tc.src2, "door.yammm")
			if r1.HasErrors() || r2.HasErrors() {
				t.Fatal(r1, r2)
			}
			v := instance.NewValidator(s1)
			g := graph.New(s1)
			for _, tn := range tc.order {
				vi, vr := v.ValidateOne(ctx, tn, instance.RawInstance{Properties: tc.add[tn]})
				if !vr.OK() {
					t.Fatalf("validate %s: %s", tn, vr)
				}
				if r := g.Add(ctx, vi); !r.OK() {
					t.Fatalf("add %s: %s", tn, r)
				}
			}
			imported, res := importInto(s2, g.Snapshot())
			if imported != nil || !res.HasCode(tc.code) {
				t.Errorf("NewFromSnapshot = (graph %v, %s), want a nil graph and %s", imported != nil, res, tc.code)
			}
			ba, bres := seedInto(ctx, s2, g.Snapshot())
			if ba != nil || !bres.HasCode(tc.code) {
				t.Errorf("NewBatchAssemblerFromSnapshot = (assembler %v, %s), want nil and %s", ba != nil, bres, tc.code)
			}
			// The same schema loaded twice is another pointer, so the import
			// walks the snapshot, and a snapshot that holds finds nothing.
			again, ra := schema.LoadString(ctx, tc.src1, "door.yammm")
			if ra.HasErrors() {
				t.Fatal(ra)
			}
			if imported, res := importInto(again, g.Snapshot()); imported == nil || res.HasErrors() {
				t.Errorf("an equal schema loaded twice was refused: %s", res)
			}
		})
	}
}
