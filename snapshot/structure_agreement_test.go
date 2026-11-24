package snapshot_test

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// A snapshot's structural facts — type identity, composition slot, stored key
// with its key properties, declared names and association shape — are held by
// the .ys decoder. The class below builds one legal graph under revisionBase and
// offers it, under every revision of that schema, to snapshot.Load.

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

func loadRevisionSchema(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, err := schema.LoadString(t.Context(), src, "rev.yammm")
	if err != nil {
		t.Fatalf("load revision: %s\n%s", err, src)
	}
	return s
}

// revisionGraph is a legal graph under revisionBase holding each fact at every
// position that stores it: a root of every type, a composed child, an edge
// with a property, an unresolved record with a property, a duplicate, and a
// (many) association holding an edge and an unresolved record of one source.
func revisionGraph(t *testing.T, s schema.Schema) graph.Snapshot {
	t.Helper()
	ctx := t.Context()
	v := instance.NewValidator(s)
	g := graph.New(s)
	add := func(typeName string, props map[string]any, wantOK bool) {
		vi, err := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
		if err != nil {
			t.Fatalf("validate %s: %s", typeName, err)
		}
		if err := g.Add(ctx, vi); (err == nil) != wantOK {
			t.Fatalf("add %s: %s", typeName, err)
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
	if len(slices.Collect(snap.Edges())) != 2 || len(slices.Collect(snap.Unresolved())) != 2 || len(slices.Collect(snap.Duplicates())) != 1 {
		t.Fatalf("fixture: %d edges, %d unresolved, %d duplicates", len(slices.Collect(snap.Edges())), len(slices.Collect(snap.Unresolved())), len(slices.Collect(snap.Duplicates())))
	}
	return snap
}

var schemaHashField = regexp.MustCompile(`"schema_hash":"[^"]*"`)

// loadClaimingSchema marshals snap and loads it against s as a writer that claims s
// would have written it: the header names s's hash, and the integrity hash
// is not checked. A refusal then comes from the document's contents.
func loadClaimingSchema(t *testing.T, s schema.Schema, snap graph.Snapshot) diag.Result {
	t.Helper()
	data, err := snapshot.Marshal(t.Context(), snap)
	if err != nil {
		t.Fatalf("marshal: %s", err)
	}
	if n := len(schemaHashField.FindAllIndex(data, -1)); n != 1 {
		t.Fatalf("schema_hash appears %d times", n)
	}
	data = schemaHashField.ReplaceAll(data, fmt.Appendf(nil, `"schema_hash":%q`, schema.StructuralHash(s)))
	_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(t.Context(), data, s, snapshot.WithIntegrityCheck(false), snapshot.WithWarnings(warn))
	})
	if res.HasCode(diag.E_SNAPSHOT_INCOMPATIBLE_SCHEMA) || res.HasCode(diag.E_SNAPSHOT_INTEGRITY_MISMATCH) {
		t.Fatalf("Load refused the header, not the contents: %s", res)
	}
	return res
}

// wantRules lists the issue each moved fact must draw at the decoder behind
// snapshot.Load: a snapshot code, or a graph code where the graph shares the rule.
func wantRules(r revision) [][2]string {
	var want [][2]string
	// A types-table row the schema does not declare stops the decoder before
	// the body, so the body's rules are judged only where every row resolves.
	load := func(code diag.Code, phrase string) {
		if r.audit == 0 {
			want = append(want, [2]string{code.String(), phrase})
		}
	}
	switch r.key {
	case 1:
		load(diag.E_SNAPSHOT_MALFORMED, `component 0 disagrees with property "code"`)
	case 2:
		load(diag.E_SNAPSHOT_MALFORMED, "has 1 components; the type declares 2")
	case 3:
		load(diag.E_SNAPSHOT_MALFORMED, `key property "vat": it is absent or null`)
	}
	if r.key == 2 && r.works == 0 {
		load(diag.E_SNAPSHOT_MALFORMED, "carries a 1-part target key; the target declares 2")
	}
	switch r.badges {
	case 1:
		load(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the composition declares as")
	case 2:
		load(diag.E_SNAPSHOT_INVALID_COMPOSED, "which the type does not declare as a composition")
	}
	if r.nickname == 1 {
		load(diag.E_SNAPSHOT_MALFORMED, `holds property "nickname", which its type does not declare`)
	}
	if r.since == 1 && r.works != 1 {
		load(diag.E_SNAPSHOT_MALFORMED, `holds edge property "since", which the association does not declare`)
	}
	switch r.works {
	case 1:
		load(diag.E_GRAPH_UNKNOWN_RELATION, `under "WORKS_AT", which the type does not declare as an association`)
	case 2:
		load(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the association declares as")
	}
	if r.mentors == 1 {
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

func TestStructuralFacts_TheReaderJudgesEveryRevision(t *testing.T) {
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
			res := loadClaimingSchema(t, loadRevisionSchema(t, r.source()), snap)
			if r.legal() == (diagtest.Err(res) != nil) {
				t.Errorf("legal=%v, refused=%v: %s", r.legal(), diagtest.Err(res) != nil, res)
			}
			for _, w := range wantRules(r) {
				if !hasRule(res, w[0], w[1]) {
					t.Errorf("no %s naming %q: %s", w[0], w[1], res)
				}
			}
		})
	}
	t.Logf("%d legal revisions, %d illegal", accepted, refused)
}
