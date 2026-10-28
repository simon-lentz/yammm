package snapshot_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The reader's structural pass reports one break once. Each row edits a
// document snapshot.Marshal wrote so that it breaks the facts the row names,
// and Load and Verify must report exactly the row's issues, whatever the
// revalidation mode. Info must too where the fact needs no schema.

const singleBreakSchema = `schema "sb"

type Company {
	id String primary
}

type Person {
	id String primary
	age Integer[0, 150]
	--> EMPLOYER (one) Company {
		since Integer[0, 3000]
	}
	--> MENTOR (_:one) Person
	--> CLIENTS (one:many) Company
	--> KNOWS (_:many) Person {
		note String
	}
}

type Order {
	id String primary
	*-> TAGS (_:many) Tag
	*-> LINES (_:many) Line
	*-> SEAL (_:one) Seal
	*-> BOX (_:one) Box
}

part type Tag {
	code String primary
}

part type Line {
	note String
}

part type Seal {
	code String primary
}

part type Box {
	n Integer
	*-> BOX (_:one) Box
}
`

// The types table of the fixture document, by row.
const (
	sbTypes = `"types":[{"schema":"sb","name":"Box"},{"schema":"sb","name":"Company"},{"schema":"sb","name":"Line"},{"schema":"sb","name":"Order"},{"schema":"sb","name":"Person"},{"schema":"sb","name":"Seal"},{"schema":"sb","name":"Tag"}]`

	sbFirstGroup = `"instances":[`
	sbLastGroup  = `}]}],"diagnostics"`
	sbC2         = `{"key":["c2"],"properties":{"id":"c2"},"provenance":null}`
	sbP1         = `{"key":["p1"],"properties":{"age":30,"id":"p1"},"edges":{`
	sbP2         = `{"key":["p2"],"properties":{"id":"p2"},"provenance":null}`
	sbP4         = `{"key":["p4"],"properties":{"id":"p4"},"provenance":null}`
	sbO1         = `{"key":["o1"],"properties":{"id":"o1"},"composed":{`
	sbClients    = `"CLIENTS":[{"target_type":1,"target_key":["c1"],"properties":{}},{"target_type":1,"target_key":["c2"],"properties":{}}]`
	sbEmployer   = `"EMPLOYER":[{"target_type":1,"target_key":["c1"],"properties":{"since":1999}}]`
	// sbEmployerTwice breaks the (one) association EMPLOYER of p1.
	sbEmployerTwice = `"EMPLOYER":[{"target_type":1,"target_key":["c1"],"properties":{"since":1999}},{"target_type":1,"target_key":["c2"],"properties":{"since":1999}}]`
	sbKnows         = `"KNOWS":[{"target_type":4,"target_key":["p2"],"properties":{"note":"n"}}]`
	sbMentor        = `"MENTOR":[{"target_type":4,"target_key":["p2"],"properties":{}}]`
	sbOuterBox      = `"BOX":[{"key":null,"type":0,"properties":{"n":1},"composed"`
	sbLastOrder     = `,"provenance":null}]},{"type":4,"items":[`
	sbInnerBox      = `{"key":null,"type":0,"properties":{"n":2},"provenance":null}`
	sbLines         = `"LINES":[{"key":null,"type":2,"properties":{"note":"a"},"provenance":null},{"key":null,"type":2,"properties":{"note":"b"},"provenance":null}],`
	sbSeal          = `"SEAL":[{"key":["s1"],"type":5,"properties":{"code":"s1"},"provenance":null}]`
	sbTagT1         = `{"key":["t1"],"type":6,"properties":{"code":"t1"},"provenance":null}`
	sbTagT2         = `{"key":["t2"],"type":6,"properties":{"code":"t2"},"provenance":null}`
	sbTags          = `"TAGS":[` + sbTagT1 + `,` + sbTagT2 + `]`

	sbDupInstance = `"instance":{"key":["c1"],"properties":{"id":"c1"},"provenance":null}`
	sbRootDup     = `{"type":1,"key":["c1"],` + sbDupInstance + `,"conflict":{"type":1,"key":["c1"]}}`
	sbSealDup     = `{"type":5,"key":["s2"],"instance":{"key":["s2"],"properties":{"code":"s2"},"provenance":null},"conflict":{"type":5,"key":["s1"]},"parent_type":3,"parent_key":["o1"],"relation":"SEAL"}`
	sbTagDup      = `{"type":6,"key":["t1"],"instance":{"key":["t1"],"properties":{"code":"t1"},"provenance":null},"conflict":{"type":6,"key":["t1"]},"parent_type":3,"parent_key":["o1"],"relation":"TAGS"}`
	sbDups        = `"duplicates":[`

	sbSealConflict = `"conflict":{"type":5,"key":["s1"]}`
	sbTagConflict  = `"conflict":{"type":6,"key":["t1"]}`

	sbEmpty      = `{"source_type":4,"source_key":["p2"],"relation":"CLIENTS","target_type":1,"target_key":null,"required":true,"reason":"empty"}`
	sbAbsent     = `{"source_type":4,"source_key":["p2"],"relation":"EMPLOYER","target_type":1,"target_key":null,"required":true,"reason":"absent"}`
	sbKnowsLost  = `{"source_type":4,"source_key":["p2"],"relation":"KNOWS","target_type":4,"target_key":["p8"],"required":false,"reason":"target_missing","properties":{"note":"m"}}`
	sbMentorLost = `{"source_type":4,"source_key":["p2"],"relation":"MENTOR","target_type":4,"target_key":["p9"],"required":false,"reason":"target_missing"}`
	sbUnresolved = `"unresolved":[`
)

var sbIntegrity = regexp.MustCompile(`"integrity_hash":"sha256:[0-9a-f]+"`)

// singleBreakDoc marshals a graph Graph.Add built and blanks its integrity
// hash, so an edited copy reads without one on every surface. It holds
// Companies c1 and c2; Person p1 with an edge under each of its four
// associations; Person p2 with an unresolved record under each; Order o1 with
// two Tags, two keyless Lines, a Seal and a Box in a Box; a root duplicate of
// c1, and a composed duplicate under BOX, under SEAL and under TAGS.
func singleBreakDoc(t *testing.T) (*schema.Schema, string) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), singleBreakSchema, "sb.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	add := func(typ string, props map[string]any) {
		vi, res := v.ValidateOne(t.Context(), typ, instance.RawInstance{Properties: props})
		if res.HasErrors() {
			t.Fatalf("validate %s: %s", typ, res)
		}
		g.Add(t.Context(), vi)
	}
	add("Company", map[string]any{"id": "c1"})
	add("Company", map[string]any{"id": "c2"})
	add("Company", map[string]any{"id": "c1"})
	add("Person", map[string]any{
		"id": "p1", "age": 30,
		"EMPLOYER": map[string]any{"_target_id": "c1", "since": 1999},
		"MENTOR":   map[string]any{"_target_id": "p2"},
		"CLIENTS":  []any{map[string]any{"_target_id": "c1"}, map[string]any{"_target_id": "c2"}},
		"KNOWS":    []any{map[string]any{"_target_id": "p2", "note": "n"}},
	})
	add("Person", map[string]any{
		"id": "p2", "CLIENTS": []any{},
		"MENTOR": map[string]any{"_target_id": "p9"},
		"KNOWS":  []any{map[string]any{"_target_id": "p8", "note": "m"}},
	})
	add("Order", map[string]any{
		"id":    "o1",
		"TAGS":  []any{map[string]any{"code": "t1"}, map[string]any{"code": "t2"}},
		"LINES": []any{map[string]any{"note": "a"}, map[string]any{"note": "b"}},
		"SEAL":  []any{map[string]any{"code": "s1"}},
		"BOX":   []any{map[string]any{"n": 1, "BOX": []any{map[string]any{"n": 2}}}},
	})
	id := func(name string) schema.TypeID {
		ty, ok := s.Type(name)
		if !ok {
			t.Fatalf("no type %s", name)
		}
		return ty.ID()
	}
	g.AddComposed(t.Context(), id("Order"), `["o1"]`, "TAGS",
		instancetest.VI("Tag", instancetest.TypeID(id("Tag")), instancetest.PK("t1"), instancetest.Props(map[string]any{"code": "t1"})))
	g.AddComposed(t.Context(), id("Order"), `["o1"]`, "BOX",
		instancetest.VI("Box", instancetest.TypeID(id("Box")), instancetest.NoKey(), instancetest.Props(map[string]any{"n": 3})))
	g.AddComposed(t.Context(), id("Order"), `["o1"]`, "SEAL",
		instancetest.VI("Seal", instancetest.TypeID(id("Seal")), instancetest.PK("s2"), instancetest.Props(map[string]any{"code": "s2"})))
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	doc := sbIntegrity.ReplaceAllString(string(data), `"integrity_hash":""`)
	if !strings.Contains(doc, sbTypes) {
		t.Fatalf("the fixture's types table moved: %s", doc)
	}
	return s, doc
}

// sbWant is one issue a row's document draws. Under onlyReval the issue is the
// revalidator's: no read without the option reports it, and it carries the
// option's severity.
type sbWant struct {
	code      diag.Code
	sev       diag.Severity
	phrase    string
	onlyReval bool
}

func sbError(code diag.Code, phrase string) sbWant {
	return sbWant{code: code, sev: diag.Error, phrase: phrase}
}

// sbRow is one edited document. Info reports the wants when schemaless is set:
// the fact needs no schema. required counts the
// unresolved records under a required association, each of which
// WithRevalidation reports as W_SNAPSHOT_UNRESOLVED_REQUIRED by its own rule;
// zero means the fixture's two. rebuild, where positive, is the number of
// issues graph.RebuildSnapshot reports for the document's parts.
type sbRow struct {
	name       string
	edits      [][2]string
	want       []sbWant
	schemaless bool
	required   int
	rebuild    int
}

func sbNestedBoxes(n int) string {
	var b strings.Builder
	for range n {
		b.WriteString(`{"key":null,"type":0,"properties":{"n":2},"composed":{"BOX":[`)
	}
	b.WriteString(sbInnerBox)
	for range n {
		b.WriteString(`]},"provenance":null}`)
	}
	return b.String()
}

// sbLost is a target_missing record of the root at source, by its types-table
// rows: Order's for o1 and Person's for any other source.
func sbLost(source, relation string, targetRow int, targetKey, props string) string {
	sourceRow := 4
	if source == "o1" {
		sourceRow = 3
	}
	return fmt.Sprintf(`{"source_type":%d,"source_key":[%q],"relation":%q,"target_type":%d,"target_key":%s,"required":false,"reason":"target_missing","properties":%s}`,
		sourceRow, source, relation, targetRow, targetKey, props)
}

func sbRecord(source, relation, reason string) string {
	target := 1
	if relation == "MENTOR" || relation == "KNOWS" {
		target = 4
	}
	return fmt.Sprintf(`{"source_type":4,"source_key":[%q],"relation":%q,"target_type":%d,"target_key":null,"required":true,"reason":%q}`,
		source, relation, target, reason)
}

// sbRows lists one row per issue-collecting site of the structural pass, then
// the documents in which two rules once claimed one break.
func sbRows() []sbRow {
	malformed := func(phrase string) []sbWant { return []sbWant{sbError(diag.E_SNAPSHOT_MALFORMED, phrase)} }
	one := func(code diag.Code, phrase string) []sbWant { return []sbWant{sbError(code, phrase)} }
	revalued := func(code diag.Code, phrase string) []sbWant {
		return []sbWant{{code: code, phrase: phrase, onlyReval: true}}
	}
	edit := func(from, to string) [][2]string { return [][2]string{{from, to}} }
	inP2 := func(edges string) [][2]string {
		return edit(sbP2, `{"key":["p2"],"properties":{"id":"p2"},"edges":{`+edges+`},"provenance":null}`)
	}
	p3 := `{"key":["p3"],"properties":{"id":"p3"},"edges":{"CLIENTS":[{"target_type":1,"target_key":["c1"],"properties":{}}],"EMPLOYER":[{"target_type":1,"target_key":["c1"],"properties":{}}]},"provenance":null}`
	lineDup := func(relation string, parentType int, parentKey string) string {
		return fmt.Sprintf(`{"type":2,"key":null,"instance":{"key":null,"properties":{"note":"a"},"provenance":null},"conflict":{"type":2,"key":null},"parent_type":%d,"parent_key":[%q],"relation":%q},`,
			parentType, parentKey, relation)
	}
	return []sbRow{
		// The instances section's groups.
		{
			name: "group with no row", edits: edit(sbFirstGroup, sbFirstGroup+`{"type":null,"items":[]},`),
			want: malformed("instances entry 0 carries no types-table row"), schemaless: true,
		},
		{
			name: "group row out of range", edits: edit(sbFirstGroup, sbFirstGroup+`{"type":99,"items":[]},`),
			want: malformed("instances entry 0 references types table row 99"), schemaless: true,
		},
		{
			name: "two groups of one row", edits: edit(sbLastGroup, `}]},{"type":1,"items":[]}],"diagnostics"`),
			want: malformed("both reference types-table row 1"), schemaless: true,
		},
		{
			name: "group of a part type", edits: edit(sbLastGroup, `}]},{"type":6,"items":[]}],"diagnostics"`),
			want: one(diag.E_SNAPSHOT_INVALID_ROOT, "is a part type"),
		},
		{
			name: "root of a part type", edits: edit(sbLastGroup, `}]},{"type":6,"items":[{"key":["t7"],"properties":{"code":"t7"},"provenance":null}]}],"diagnostics"`),
			want: one(diag.E_SNAPSHOT_INVALID_ROOT, "is a part type"), rebuild: 1,
		},

		// One instance.
		{
			name: "nesting past the depth limit", edits: edit(sbInnerBox, sbNestedBoxes(instance.MaxComposedDepth-1)),
			want: one(diag.E_SNAPSHOT_DEPTH_EXCEEDED, "exceeds limit"), schemaless: true,
		},
		{
			name: "root stating another row", edits: edit(sbP2, `{"key":["p2"],"type":1,"properties":{"id":"p2"},"provenance":null}`),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "declares type row 1"), schemaless: true,
		},
		{
			name: "composed child with edges", edits: edit(sbTagT2, `{"key":["t2"],"type":6,"properties":{"code":"t2"},"edges":{"X":[{"target_type":1,"target_key":["c1"],"properties":{}}]},"provenance":null}`),
			want: one(diag.E_SNAPSHOT_INVALID_COMPOSED, "composed children must not carry edges"), schemaless: true,
		},
		{
			name: "unparseable provenance path", edits: edit(sbP2, `{"key":["p2"],"properties":{"id":"p2"},"provenance":{"source_name":"f","path":""}}`),
			want: []sbWant{{code: diag.W_SNAPSHOT_PATH_FALLBACK, sev: diag.Warning, phrase: "could not be parsed"}}, schemaless: true,
		},
		{
			name: "stored key component not a scalar", edits: edit(sbP1, `{"key":[["p1"]],"properties":{"age":30,"id":"p1"},"edges":{`),
			want: malformed("whose component 0 is not a scalar"), schemaless: true,
		},
		{
			name: "stored key disagrees with its property", edits: edit(sbP1, `{"key":["px"],"properties":{"age":30,"id":"p1"},"edges":{`),
			want: malformed(`disagrees with property "id"`), rebuild: 1,
		},
		{
			name: "stored key of another arity", edits: edit(sbP1, `{"key":["p1","x"],"properties":{"age":30,"id":"p1"},"edges":{`),
			want: malformed("has 2 components"), rebuild: 1,
		},
		{
			name: "undeclared property", edits: edit(sbP2, `{"key":["p2"],"properties":{"ghost":1,"id":"p2"},"provenance":null}`),
			want: malformed(`holds property "ghost"`), rebuild: 1,
		},
		{
			name: "undeclared property on a composed child", edits: edit(sbTagT2, `{"key":["t2"],"type":6,"properties":{"code":"t2","ghost":1},"provenance":null}`),
			want: malformed(`holds property "ghost"`), rebuild: 1,
		},
		{
			name: "two roots at one address", edits: edit(sbC2, sbC2+`,`+sbC2),
			want: one(diag.E_DUPLICATE_PK, `duplicate primary key ["c2"]`), schemaless: true, rebuild: 1,
		},
		{
			name: "value outside its bounds", edits: edit(`"age":30`, `"age":500`),
			want: []sbWant{{code: diag.E_CONSTRAINT_FAIL, phrase: "revalidation of sb#Person", onlyReval: true}},
		},

		// One edge.
		{
			name: "undeclared edge property", edits: edit(sbKnows, `"KNOWS":[{"target_type":4,"target_key":["p2"],"properties":{"ghost":1,"note":"n"}}]`),
			want: malformed(`holds edge property "ghost"`), rebuild: 1,
		},
		{
			name: "edge with no target row", edits: edit(sbMentor, `"MENTOR":[{"target_type":null,"target_key":["p2"],"properties":{}}]`),
			want: malformed("carries no types-table row"), schemaless: true,
		},
		{
			name: "edge target row out of range under a required association", edits: edit(sbEmployer, `"EMPLOYER":[{"target_type":99,"target_key":["c1"],"properties":{"since":1999}}]`),
			want: malformed("references types table row 99"), schemaless: true,
		},
		{
			name: "edge target key component not a scalar", edits: edit(sbKnows, `"KNOWS":[{"target_type":4,"target_key":[["p2"]],"properties":{"note":"n"}}]`),
			want: malformed("whose component 0 is not a scalar"), schemaless: true,
		},
		{
			name: "edge under an undeclared relation", edits: edit(sbMentor, sbMentor+`,"XA":[{"target_type":1,"target_key":["c1"],"properties":{}}]`),
			want: one(diag.E_GRAPH_UNKNOWN_RELATION, `under "XA"`), rebuild: 1,
		},
		{
			name: "edge to another type", edits: edit(sbMentor, `"MENTOR":[{"target_type":1,"target_key":["c1"],"properties":{}}]`),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the association declares as sb#Person"),
		},
		{
			name: "edge to another type whose target key has another arity", edits: edit(sbMentor, `"MENTOR":[{"target_type":1,"target_key":["c1","x"],"properties":{}}]`),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the association declares as sb#Person"),
		},
		{
			name: "edge to another type whose target key holds no scalar", edits: edit(sbMentor, `"MENTOR":[{"target_type":1,"target_key":[["c1"]],"properties":{}}]`),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the association declares as sb#Person"),
		},
		{
			name: "record of another target type whose target key has another arity", edits: edit(sbMentorLost, strings.Replace(strings.Replace(sbMentorLost, `"target_type":4`, `"target_type":1`, 1), `["p9"]`, `["p9","x"]`, 1)),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the association declares as sb#Person"),
		},
		{
			name: "edge target key of another arity", edits: edit(sbMentor, `"MENTOR":[{"target_type":4,"target_key":["p2","x"],"properties":{}}]`),
			want: malformed("carries a 2-part target key"), rebuild: 1,
		},
		{
			name: "edge to no root", edits: edit(sbMentor, `"MENTOR":[{"target_type":4,"target_key":["p7"],"properties":{}}]`),
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, `references sb#Person[["p7"]] which does not exist`), schemaless: true, rebuild: 1,
		},
		{
			name: "two edges under a (one) association", edits: edit(sbMentor, `"MENTOR":[{"target_type":4,"target_key":["p2"],"properties":{}},{"target_type":4,"target_key":["p1"],"properties":{}}]`),
			want: one(diag.E_GRAPH_CARDINALITY, `(one) association "MENTOR"`), rebuild: 1,
		},

		// One composition slot.
		{
			name: "children under an undeclared composition", edits: edit(sbSeal, `"NOPE":[{"key":["t9"],"type":6,"properties":{"code":"t9"},"provenance":null}],`+sbSeal),
			want: one(diag.E_SNAPSHOT_INVALID_COMPOSED, "does not declare as a composition"), rebuild: 1,
		},
		{
			name: "two children under a (one) composition", edits: [][2]string{
				{sbSeal, `"SEAL":[{"key":["s1"],"type":5,"properties":{"code":"s1"},"provenance":null},{"key":["s3"],"type":5,"properties":{"code":"s3"},"provenance":null}]`},
				{sbSealDup + `,`, ``},
			},
			want: one(diag.E_DUPLICATE_COMPOSED_PK, "(one) cardinality violated"), rebuild: 1,
		},
		{
			name: "two (many) siblings at one key", edits: edit(sbTagT2, sbTagT1),
			want: one(diag.E_DUPLICATE_COMPOSED_PK, "holds children 0 and 1 at key"), rebuild: 1,
		},
		{
			name: "composed child with no row", edits: edit(sbTagT2, `{"key":["t2"],"type":null,"properties":{"code":"t2"},"provenance":null}`),
			want: malformed("composed child under sb#Order.TAGS carries no types-table row"), schemaless: true,
		},
		{
			name: "composed child of another type", edits: edit(sbTagT2, `{"key":["t2"],"type":5,"properties":{"code":"t2"},"provenance":null}`),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the composition declares as sb#Tag"), rebuild: 1,
		},

		// One duplicate record.
		{
			name: "duplicate with no row", edits: edit(sbRootDup, `{"type":null,"key":["c1"],`+sbDupInstance+`,"conflict":{"type":1,"key":["c1"]}}`),
			want: malformed("duplicate record 1 carries no types-table row"), schemaless: true,
		},
		{
			name: "duplicate instance stating another row", edits: edit(sbDupInstance, `"instance":{"key":["c1"],"type":4,"properties":{"id":"c1"},"provenance":null}`),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "but its record states row 1"), schemaless: true,
		},
		{
			name: "duplicate stating another key than its instance", edits: edit(`{"type":1,"key":["c1"],"instance"`, `{"type":1,"key":["cx"],"instance"`),
			want: malformed("states key"), schemaless: true,
		},
		{
			name: "duplicate instance with an unparseable provenance path", edits: edit(sbDupInstance, `"instance":{"key":["c1"],"properties":{"id":"c1"},"provenance":{"source_name":"f","path":""}}`),
			want: []sbWant{{code: diag.W_SNAPSHOT_PATH_FALLBACK, sev: diag.Warning, phrase: "could not be parsed"}}, schemaless: true,
		},
		{
			name: "duplicate instance with an undeclared property", edits: edit(sbDupInstance, `"instance":{"key":["c1"],"properties":{"ghost":1,"id":"c1"},"provenance":null}`),
			want: malformed(`holds property "ghost"`), rebuild: 1,
		},
		{
			name: "duplicate instance whose key disagrees with its property", edits: edit(sbDupInstance, `"instance":{"key":["c1"],"properties":{"id":"cz"},"provenance":null}`),
			want: malformed(`disagrees with property "id"`), rebuild: 1,
		},
		{
			name: "root duplicate of a part type", edits: edit(sbDups, sbDups+`{"type":6,"key":["t1"],"instance":{"key":["t1"],"properties":{"code":"t1"},"provenance":null},"conflict":{"type":6,"key":["t1"]}},`),
			want: one(diag.E_SNAPSHOT_INVALID_ROOT, "duplicate record 0 denotes sb#Tag"),
		},
		{
			name: "duplicate instance with composed children", edits: edit(sbDupInstance, `"instance":{"key":["c1"],"properties":{"id":"c1"},"composed":{"X":[]},"provenance":null}`),
			want: one(diag.E_SNAPSHOT_COMPOSED_ON_DUPLICATE, "must not have composed children"), schemaless: true,
		},
		{
			name: "duplicate instance with edges", edits: edit(sbDupInstance, `"instance":{"key":["c1"],"properties":{"id":"c1"},"edges":{"X":[]},"provenance":null}`),
			want: one(diag.E_SNAPSHOT_EDGES_ON_DUPLICATE, "must not have edges"), schemaless: true,
		},
		{
			name: "duplicate with no conflict block", edits: edit(sbDupInstance+`,"conflict":{"type":1,"key":["c1"]}`, sbDupInstance+`,"conflict":null`),
			want: malformed("carries no conflict block"), schemaless: true,
		},
		{
			name: "duplicate conflict row out of range", edits: edit(sbDupInstance+`,"conflict":{"type":1,"key":["c1"]}`, sbDupInstance+`,"conflict":{"type":99,"key":["c1"]}`),
			want: malformed("duplicate record 1 conflict references types table row 99"), schemaless: true,
		},
		{
			name: "root duplicate with parent coordinates", edits: edit(sbDupInstance+`,"conflict":{"type":1,"key":["c1"]}`, sbDupInstance+`,"conflict":{"type":1,"key":["c1"]},"parent_key":["o1"]`),
			want: malformed("states no relation but carries parent coordinates"), schemaless: true,
		},
		{
			name: "root duplicate whose conflict is another root", edits: edit(sbDupInstance+`,"conflict":{"type":1,"key":["c1"]}`, sbDupInstance+`,"conflict":{"type":1,"key":["c2"]}`),
			want: malformed("a root duplicate's conflict is the root at its own type and key"), schemaless: true,
		},
		{
			name: "composed duplicate with no parent row", edits: edit(`"conflict":{"type":6,"key":["t1"]},"parent_type":3,`, `"conflict":{"type":6,"key":["t1"]},`),
			want: malformed("with no parent coordinates"), schemaless: true,
		},
		{
			name: "composed duplicate parent row out of range", edits: edit(`"conflict":{"type":6,"key":["t1"]},"parent_type":3,`, `"conflict":{"type":6,"key":["t1"]},"parent_type":99,`),
			want: malformed("duplicate record 3 parent references types table row 99"), schemaless: true,
		},
		{
			name: "composed duplicate under no root", edits: edit(`"parent_key":["o1"],"relation":"TAGS"`, `"parent_key":["o9"],"relation":"TAGS"`),
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, "does not resolve to a root instance"), schemaless: true,
		},
		{
			name: "composed duplicate with no child at its key", edits: edit(sbTagDup, strings.ReplaceAll(sbTagDup, `"t1"`, `"t9"`)),
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, "references non-existent instance"), schemaless: true, rebuild: 1,
		},
		{
			name: "composed duplicate of another type than its composition", edits: edit(`{"type":5,"key":["s2"],"instance"`, `{"type":6,"key":["s2"],"instance"`),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, `which the composition "SEAL" declares as sb#Seal`), rebuild: 1,
		},
		{
			name: "composed duplicate of another type whose stated conflict is its slot's", edits: edit(sbSealDup, strings.Replace(sbSealDup, `{"type":5,"key":["s2"]`, `{"type":6,"key":["s2"]`, 1)),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, `which the composition "SEAL" declares as sb#Seal`), rebuild: 1,
		},
		{
			name: "composed duplicate of another type whose stated conflict is not in its slot", edits: edit(sbSealDup, strings.Replace(strings.Replace(sbSealDup, `{"type":5,"key":["s2"]`, `{"type":6,"key":["s2"]`, 1), sbSealConflict, `"conflict":{"type":5,"key":["s9"]}`, 1)),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, `which the composition "SEAL" declares as sb#Seal`), rebuild: 1,
		},
		{
			name: "composed duplicate of a keyless part whose stated conflict is another type", edits: edit(`"conflict":{"type":0,"key":null},"parent_type":3,"parent_key":["o1"],"relation":"BOX"`, `"conflict":{"type":3,"key":null},"parent_type":3,"parent_key":["o1"],"relation":"BOX"`),
			// RebuildSnapshot derives a duplicate's conflict and reads no stated one.
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, "references non-existent instance"), schemaless: true,
		},
		{
			name: "composed duplicate with no row", edits: edit(sbTagDup, strings.Replace(sbTagDup, `{"type":6,`, `{"type":null,`, 1)),
			want: malformed("duplicate record"), rebuild: 1,
		},
		{
			name: "composed duplicate with no row whose stated conflict is a sibling at another key", edits: edit(sbTagDup, strings.Replace(strings.Replace(sbTagDup, `{"type":6,`, `{"type":null,`, 1), sbTagConflict, `"conflict":{"type":6,"key":["t2"]}`, 1)),
			want: malformed("duplicate record"), rebuild: 1,
		},
		{
			name: "composed duplicate whose conflict is a sibling at another key", edits: edit(`"conflict":{"type":6,"key":["t1"]}`, `"conflict":{"type":6,"key":["t2"]}`),
			want: malformed("its conflict is the child at its own key"),
		},
		{
			name: "keyed (many) duplicate with an empty conflict key", edits: edit(sbTagConflict, `"conflict":{"type":6,"key":[]}`),
			want: malformed("states no conflict key; under the (many) composition"),
		},
		{
			name: "keyed (many) duplicate with a null conflict key", edits: edit(sbTagConflict, `"conflict":{"type":6,"key":null}`),
			want: malformed("states no conflict key; under the (many) composition"),
		},
		{
			name: "keyed (many) duplicate with an empty conflict key beside a sole sibling", edits: [][2]string{
				{sbTags, `"TAGS":[` + sbTagT1 + `]`},
				{sbTagConflict, `"conflict":{"type":6,"key":[]}`},
			},
			want: malformed("states no conflict key; under the (many) composition"),
		},
		{
			name: "keyed (many) duplicate with an empty conflict key of another type", edits: edit(sbTagConflict, `"conflict":{"type":5,"key":[]}`),
			want: malformed("states no conflict key; under the (many) composition"),
		},
		// The document graph.RebuildSnapshot and snapshot.Marshal of v0.21.0 wrote
		// for a keyed child with no key and its duplicate: two breaks, and the
		// null conflict key is no third.
		{
			name: "keyed (many) duplicate and its sole sibling with no keys and a null conflict key", edits: [][2]string{
				{sbTags, `"TAGS":[{"key":[],"type":6,"properties":{"code":"t1"},"provenance":null}]`},
				{sbTagDup, `{"type":6,"key":[],"instance":{"key":[],"properties":{"code":"t1"},"provenance":null},"conflict":{"type":6,"key":null},"parent_type":3,"parent_key":["o1"],"relation":"TAGS"}`},
			},
			want: []sbWant{
				sbError(diag.E_SNAPSHOT_MALFORMED, "which is empty"),
				sbError(diag.E_SNAPSHOT_MALFORMED, "which is empty"),
			}, rebuild: 2,
		},
		{name: "keyed (one) duplicate with a null conflict key", edits: edit(sbSealConflict, `"conflict":{"type":5,"key":null}`)},
		{name: "keyed (one) duplicate with an empty conflict key", edits: edit(sbSealConflict, `"conflict":{"type":5,"key":[]}`)},
		{
			name: "keyed (one) duplicate with a null conflict key of another type", edits: edit(sbSealConflict, `"conflict":{"type":6,"key":null}`),
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, "references non-existent instance"), schemaless: true,
		},

		// One unresolved record.
		{
			name: "record with an undocumented reason", edits: edit(sbMentorLost, strings.Replace(sbMentorLost, "target_missing", "bogus", 1)),
			want: malformed(`states reason "bogus"`), schemaless: true, rebuild: 1,
		},
		{
			name: "absent record with a target key", edits: edit(sbAbsent, strings.Replace(sbAbsent, `"target_key":null`, `"target_key":["c1"]`, 1)),
			want: malformed("but carries a target key"), schemaless: true, rebuild: 1,
		},
		{
			name: "absent record with edge properties", edits: edit(sbAbsent, strings.Replace(sbAbsent, `"reason":"absent"`, `"reason":"absent","properties":{"since":1}`, 1)),
			want: malformed("but carries edge properties"), schemaless: true, rebuild: 1,
		},
		{
			name: "record with no source row", edits: edit(sbKnowsLost, strings.Replace(sbKnowsLost, `"source_type":4`, `"source_type":null`, 1)),
			want: malformed("unresolved record 2 source carries no types-table row"), schemaless: true,
		},
		{
			name: "record with an undeclared edge property", edits: edit(sbKnowsLost, strings.Replace(sbKnowsLost, `{"note":"m"}`, `{"ghost":1,"note":"m"}`, 1)),
			want: malformed(`holds edge property "ghost"`), rebuild: 1,
		},
		{
			name: "record under an undeclared relation", edits: edit(sbMentorLost, strings.Replace(sbMentorLost, "MENTOR", "NOPE", 1)),
			want: one(diag.E_GRAPH_UNKNOWN_RELATION, `under "NOPE"`), rebuild: 1,
		},
		{
			name: "record of another target type", edits: edit(sbMentorLost, strings.Replace(sbMentorLost, `"target_type":4`, `"target_type":1`, 1)),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the association declares as sb#Person"),
		},
		{
			name: "record target key of another arity", edits: edit(sbMentorLost, strings.Replace(sbMentorLost, `["p9"]`, `["p9","x"]`, 1)),
			want: malformed("carries a 2-part target key"), rebuild: 1,
		},
		{
			name: "record target key component not a scalar", edits: edit(sbMentorLost, strings.Replace(sbMentorLost, `["p9"]`, `[["p9"]]`, 1)),
			want: malformed("whose component 0 is not a scalar"), schemaless: true, rebuild: 1,
		},
		{
			name: "record sourced at no root", edits: edit(sbMentorLost, strings.Replace(sbMentorLost, `["p2"]`, `["p7"]`, 1)),
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, "unresolved edge source"), schemaless: true,
		},
		{
			name: "record target row out of range", edits: edit(sbMentorLost, strings.Replace(sbMentorLost, `"target_type":4`, `"target_type":99`, 1)),
			want: malformed("unresolved record 3 target references types table row 99"), schemaless: true,
		},

		// One unresolved record's values, which the revalidator judges as an
		// edge's. A record a check refused feeds it nothing.
		{
			name: "record edge property outside its bounds", edits: edit(sbAbsent, sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"since":9999}`)),
			want: revalued(diag.E_CONSTRAINT_FAIL, `revalidation of sb#Person[["p2"]]: edge property "since"`),
		},
		{
			name: "record edge property of another kind under a (many) association", edits: edit(sbKnowsLost, strings.Replace(sbKnowsLost, `{"note":"m"}`, `{"note":7}`, 1)),
			want: revalued(diag.E_TYPE_MISMATCH, `revalidation of sb#Person[["p2"]]: edge property "note"`),
		},
		{
			name: "record edge property of another kind beside an edge", edits: edit(sbUnresolved, sbUnresolved+sbLost("p1", "KNOWS", 4, `["p8"]`, `{"note":7}`)+`,`),
			want: revalued(diag.E_TYPE_MISMATCH, `revalidation of sb#Person[["p1"]]: edge property "note"`),
		},
		{name: "record edge property within its bounds", edits: edit(sbAbsent, sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"since":3000}`))},
		{
			name: "record edge property outside its bounds beside an undeclared one", edits: edit(sbAbsent, sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"ghost":1,"since":9999}`)),
			want: malformed(`holds edge property "ghost"`), rebuild: 1,
		},
		{
			name: "record of another arity with an edge property outside its bounds", edits: edit(sbAbsent, sbLost("p2", "EMPLOYER", 1, `["c9","x"]`, `{"since":9999}`)),
			want: malformed("carries a 2-part target key"), rebuild: 1,
		},
		{
			name: "record with no target key and an edge property outside its bounds", edits: edit(sbAbsent, sbLost("p2", "EMPLOYER", 1, `[]`, `{"since":9999}`)),
			want: malformed("carries a 0-part target key"), rebuild: 1,
		},
		{
			name: "record under an undeclared relation with an edge property", edits: edit(sbUnresolved, sbUnresolved+sbLost("p2", "NOPE", 1, `["c9"]`, `{"since":9999}`)+`,`),
			want: one(diag.E_GRAPH_UNKNOWN_RELATION, `under "NOPE"`), rebuild: 1,
		},
		{
			name: "record under a composition with an edge property", edits: edit(sbUnresolved, sbUnresolved+sbLost("o1", "TAGS", 6, `["t9"]`, `{"since":9999}`)+`,`),
			want: one(diag.E_GRAPH_UNKNOWN_RELATION, `under "TAGS"`), rebuild: 1,
		},
		{
			name: "record of another target type with an edge property of another kind", edits: edit(sbUnresolved, sbUnresolved+sbLost("p2", "KNOWS", 1, `["c9"]`, `{"note":7}`)+`,`),
			want: one(diag.E_SNAPSHOT_TYPE_MISMATCH, "which the association declares as sb#Person"),
		},
		{
			name: "record with an undocumented reason and an edge property outside its bounds", edits: edit(sbAbsent, strings.Replace(sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"since":9999}`), "target_missing", "bogus", 1)),
			want: malformed(`states reason "bogus"`), schemaless: true, rebuild: 1,
		},
		{
			name: "record outside its bounds beside an edge under a (one) association", edits: edit(sbUnresolved, sbUnresolved+sbLost("p1", "EMPLOYER", 1, `["c9"]`, `{"since":9999}`)+`,`),
			want: one(diag.E_GRAPH_CARDINALITY, `(one) association "EMPLOYER"`), required: 3, rebuild: 1,
		},
		{
			name: "record outside its bounds of a root the walk refused", edits: [][2]string{
				{sbP2, `{"key":["p2"],"properties":{"ghost":1,"id":"p2"},"provenance":null}`},
				{sbAbsent, sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"since":9999}`)},
			},
			want: malformed(`holds property "ghost"`), rebuild: 1,
		},
		{
			name: "two records outside their bounds under a (one) association", edits: edit(sbAbsent, sbLost("p2", "EMPLOYER", 1, `["c8"]`, `{"since":8888}`)+`,`+sbLost("p2", "EMPLOYER", 1, `["c9"]`, `{"since":9999}`)),
			want: []sbWant{
				sbError(diag.E_GRAPH_CARDINALITY, `(one) association "EMPLOYER"`),
				{code: diag.E_CONSTRAINT_FAIL, phrase: "integer 8888 exceeds", onlyReval: true},
			}, required: 3, rebuild: 1,
		},

		// The records together.
		{
			name: "absent record under an optional association", edits: edit(sbMentorLost, strings.Replace(strings.Replace(sbMentorLost, "target_missing", "absent", 1), `["p9"]`, `null`, 1)),
			want: malformed("which is optional"), rebuild: 1,
		},
		{
			name: "empty record twice under a (many) association", edits: edit(sbEmpty, sbEmpty+`,`+sbEmpty),
			want: malformed("holds an empty record beside another record"), required: 3, rebuild: 1,
		},
		{
			name: "target_missing record naming a root", edits: edit(sbMentorLost, strings.Replace(sbMentorLost, `["p9"]`, `["p1"]`, 1)),
			want: malformed("which a root holds"), rebuild: 1,
		},
		{
			name: "root with no record under a required association", edits: edit(sbAbsent+`,`, ``),
			want: malformed(`holds no edge and no record under the required association "EMPLOYER"`), required: 1, rebuild: 1,
		},

		// One break that two rules claimed.
		{
			name: "refused edge whose target is no root", edits: edit(sbMentor, sbMentor+`,"XA":[{"target_type":1,"target_key":["gone"],"properties":{}}]`),
			want: one(diag.E_GRAPH_UNKNOWN_RELATION, `under "XA"`), rebuild: 1,
		},
		{
			name: "edge target key of another arity holding a non-scalar", edits: edit(sbMentor, `"MENTOR":[{"target_type":4,"target_key":[["x"],"y"],"properties":{}}]`),
			want: malformed("carries a 2-part target key"), rebuild: 1,
		},
		{
			name: "absent record with a non-scalar target key", edits: edit(sbAbsent, strings.Replace(sbAbsent, `"target_key":null`, `"target_key":[[1]]`, 1)),
			want: malformed("but carries a target key"), schemaless: true, rebuild: 1,
		},
		{
			name: "record under an undeclared relation with a non-scalar target key", edits: edit(sbMentorLost, strings.Replace(strings.Replace(sbMentorLost, "MENTOR", "NOPE", 1), `["p9"]`, `[["p9"]]`, 1)),
			want: one(diag.E_GRAPH_UNKNOWN_RELATION, `under "NOPE"`),
		},
		{
			name: "two children of a (one) composition at one key", edits: [][2]string{
				{sbSeal, `"SEAL":[{"key":["s1"],"type":5,"properties":{"code":"s1"},"provenance":null},{"key":["s1"],"type":5,"properties":{"code":"s1"},"provenance":null}]`},
				{sbSealDup + `,`, ``},
			},
			want: one(diag.E_DUPLICATE_COMPOSED_PK, "(one) cardinality violated"), rebuild: 1,
		},
		{
			name: "two children of a (one) composition beside its duplicate", edits: edit(sbSeal, `"SEAL":[{"key":["s1"],"type":5,"properties":{"code":"s1"},"provenance":null},{"key":["s3"],"type":5,"properties":{"code":"s3"},"provenance":null}]`),
			want: one(diag.E_DUPLICATE_COMPOSED_PK, "(one) cardinality violated"), rebuild: 1,
		},
		{
			name: "composed duplicate under a keyless (many) slot of two", edits: edit(sbDups, sbDups+lineDup("LINES", 3, "o1")),
			want: malformed("where no child conflicts"), rebuild: 1,
		},
		{
			name: "composed duplicate under a keyless (many) slot of none", edits: [][2]string{{sbLines, ``}, {sbDups, sbDups + lineDup("LINES", 3, "o1")}},
			want: malformed("where no child conflicts"), rebuild: 1,
		},
		{
			name: "composed duplicate under an undeclared relation", edits: edit(sbDups, sbDups+lineDup("NOPE", 3, "o1")),
			want: one(diag.E_SNAPSHOT_INVALID_COMPOSED, "does not declare as a composition"), rebuild: 1,
		},
		{
			name: "composed duplicate under an association", edits: edit(sbDups, sbDups+lineDup("KNOWS", 4, "p1")),
			want: one(diag.E_SNAPSHOT_INVALID_COMPOSED, "does not declare as a composition"), rebuild: 1,
		},
		{
			name: "root duplicate whose conflict is no root", edits: edit(sbDupInstance+`,"conflict":{"type":1,"key":["c1"]}`, sbDupInstance+`,"conflict":{"type":1,"key":["zz"]}`),
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, "duplicate conflict"), schemaless: true,
		},
		{
			name: "root duplicate whose conflict key is empty", edits: edit(sbDupInstance+`,"conflict":{"type":1,"key":["c1"]}`, sbDupInstance+`,"conflict":{"type":1,"key":[]}`),
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, "duplicate conflict"), schemaless: true,
		},
		{
			name: "root duplicate whose conflict key has another arity", edits: edit(sbDupInstance+`,"conflict":{"type":1,"key":["c1"]}`, sbDupInstance+`,"conflict":{"type":1,"key":["c1","x"]}`),
			want: one(diag.E_SNAPSHOT_DANGLING_REFERENCE, "duplicate conflict"), schemaless: true,
		},
		{
			name: "absent record beside an edge under a (one) association", edits: edit(sbUnresolved, sbUnresolved+sbRecord("p1", "EMPLOYER", "absent")+`,`),
			want: malformed("holds an absent record beside another record"), required: 3, rebuild: 1,
		},
		{
			name: "absent record twice under a (one) association", edits: edit(sbAbsent, sbAbsent+`,`+sbAbsent),
			want: malformed("holds an absent record beside another record"), required: 3, rebuild: 1,
		},
		{
			name: "two roots at one address each with a (one) edge", edits: edit(sbP2, sbP2+`,`+p3+`,`+p3),
			want: one(diag.E_DUPLICATE_PK, `duplicate primary key ["p3"]`), schemaless: true, rebuild: 1,
		},
		{
			name: "record with an undocumented reason and no target key", edits: edit(sbAbsent, strings.Replace(sbAbsent, `"absent"`, `"bogus"`, 1)),
			want: malformed(`states reason "bogus"`), schemaless: true, rebuild: 1,
		},
		{
			name: "(many) sibling whose key is its neighbour's and not its property's", edits: edit(sbTagT2, `{"key":["t1"],"type":6,"properties":{"code":"t2"},"provenance":null}`),
			want: malformed(`disagrees with property "code"`), rebuild: 1,
		},

		{
			name: "two children of a keyless (one) composition beside its duplicate", edits: edit(sbOuterBox, `"BOX":[{"key":null,"type":0,"properties":{"n":7},"provenance":null},`+sbOuterBox[len(`"BOX":[`):]),
			want: one(diag.E_DUPLICATE_COMPOSED_PK, "(one) cardinality violated"), rebuild: 1,
		},
		{
			name: "two roots at one address, the second holding a child", edits: edit(sbLastOrder, `,"provenance":null},{"key":["o1"],"properties":{"id":"o1"},"composed":{"BOX":[{"key":null,"type":0,"properties":{"n":9},"provenance":null}]},"provenance":null}]},{"type":4,"items":[`),
			want: one(diag.E_DUPLICATE_PK, `duplicate primary key ["o1"]`), schemaless: true, rebuild: 1,
		},
		{
			name: "two roots at one address, the second holding an edge the first has an absent record for", edits: edit(sbP2, sbP2+`,{"key":["p2"],"properties":{"id":"p2"},"edges":{"EMPLOYER":[{"target_type":1,"target_key":["c1"],"properties":{}}]},"provenance":null}`),
			want: one(diag.E_DUPLICATE_PK, `duplicate primary key ["p2"]`), schemaless: true, rebuild: 1,
		},
		{
			name: "refused record that names a root", edits: edit(sbKnowsLost, strings.Replace(strings.Replace(sbKnowsLost, `{"note":"m"}`, `{"ghost":1,"note":"m"}`, 1), `["p8"]`, `["p1"]`, 1)),
			want: malformed(`holds edge property "ghost"`), rebuild: 1,
		},

		{
			name: "(many) sibling at the key of one whose key is not its property's", edits: [][2]string{
				{sbTagT1, `{"key":["t2"],"type":6,"properties":{"code":"t1"},"provenance":null}`},
				{`,` + sbTagDup, ``},
			},
			want: malformed(`disagrees with property "code"`), rebuild: 1,
		},
		{
			name: "record with an undocumented reason beside an edge under a (one) association", edits: edit(sbUnresolved, sbUnresolved+`{"source_type":4,"source_key":["p1"],"relation":"EMPLOYER","target_type":1,"target_key":["c9"],"required":true,"reason":"bogus"},`),
			want: malformed(`states reason "bogus"`), schemaless: true, required: 3, rebuild: 1,
		},

		// Two breaks, each reported once.
		{
			name: "two (many) siblings at the key of one whose key is not its property's", edits: [][2]string{
				{sbTagT1, `{"key":["t2"],"type":6,"properties":{"code":"t1"},"provenance":null},` + sbTagT2},
				{`,` + sbTagDup, ``},
			},
			want: []sbWant{
				sbError(diag.E_SNAPSHOT_MALFORMED, `disagrees with property "code"`),
				sbError(diag.E_DUPLICATE_COMPOSED_PK, "holds children 1 and 2 at key"),
			}, rebuild: 2,
		},
		{
			name: "two roots at one address with no record under their required associations", edits: edit(sbP2, sbP2+`,`+sbP4+`,`+sbP4),
			want: []sbWant{
				sbError(diag.E_DUPLICATE_PK, `duplicate primary key ["p4"]`),
				sbError(diag.E_SNAPSHOT_MALFORMED, `holds no edge and no record under the required association "CLIENTS"`),
				sbError(diag.E_SNAPSHOT_MALFORMED, `holds no edge and no record under the required association "EMPLOYER"`),
			}, rebuild: 3,
		},
		{
			name: "two roots with empty keys", edits: edit(sbC2, sbC2+`,{"key":[],"properties":{"id":"c8"},"provenance":null},{"key":[],"properties":{"id":"c9"},"provenance":null}`),
			want: []sbWant{
				sbError(diag.E_SNAPSHOT_MALFORMED, "which is empty"),
				sbError(diag.E_SNAPSHOT_MALFORMED, "which is empty"),
			}, rebuild: 2,
		},
		{
			name: "two (many) siblings with empty keys", edits: [][2]string{
				{sbTags, `"TAGS":[{"key":[],"type":6,"properties":{"code":"t1"},"provenance":null},{"key":[],"type":6,"properties":{"code":"t2"},"provenance":null}]`},
				{`,` + sbTagDup, ``},
			},
			want: []sbWant{
				sbError(diag.E_SNAPSHOT_MALFORMED, "which is empty"),
				sbError(diag.E_SNAPSHOT_MALFORMED, "which is empty"),
			}, rebuild: 2,
		},
		{
			// The refused edge is held, so the required association is not
			// empty, and it is no record beside the empty one.
			name: "edge with an unresolvable target row beside an empty record", edits: inP2(`"CLIENTS":[{"target_type":99,"target_key":["c1"],"properties":{}}]`),
			want: malformed("references types table row 99"),
		},
		{
			name: "refused edge beside an accepted one under a (one) association", edits: edit(sbMentor, `"MENTOR":[{"target_type":4,"target_key":["p2"],"properties":{}},{"target_type":4,"target_key":["p1","x"],"properties":{}}]`),
			want: malformed("carries a 2-part target key"), rebuild: 1,
		},
	}
}

type sbMode struct {
	name  string
	reval bool
	sev   diag.Severity
	opts  []snapshot.LoadOption
}

func sbModes() []sbMode {
	return []sbMode{
		{name: "plain", opts: []snapshot.LoadOption{snapshot.WithIssueLimit(0)}},
		{name: "revalidate at Warning", reval: true, sev: diag.Warning, opts: []snapshot.LoadOption{snapshot.WithIssueLimit(0), snapshot.WithRevalidation(diag.Warning)}},
		{name: "revalidate at Error", reval: true, sev: diag.Error, opts: []snapshot.LoadOption{snapshot.WithIssueLimit(0), snapshot.WithRevalidation(diag.Error)}},
	}
}

// sbCheck fails unless res holds exactly the wanted issues, and under
// revalidation beside them one W_SNAPSHOT_UNRESOLVED_REQUIRED per required
// record at the option's severity.
func sbCheck(t *testing.T, res diag.Result, want []sbWant, mode sbMode, required int) {
	t.Helper()
	var got []diag.Issue
	byRule := 0
	for issue := range res.Issues() {
		if mode.reval && issue.Code() == diag.W_SNAPSHOT_UNRESOLVED_REQUIRED && issue.Severity() == mode.sev {
			byRule++
			continue
		}
		got = append(got, issue)
	}
	if mode.reval && byRule != required {
		t.Errorf("%d W_SNAPSHOT_UNRESOLVED_REQUIRED issues, want %d:\n%s", byRule, required, res)
	}
	matched := make([]bool, len(got))
	wanted := 0
	for _, w := range want {
		sev := w.sev
		if w.onlyReval {
			if !mode.reval {
				continue
			}
			sev = mode.sev
		}
		wanted++
		found := false
		for i, issue := range got {
			if !matched[i] && issue.Code() == w.code && issue.Severity() == sev && strings.Contains(issue.Message(), w.phrase) {
				matched[i], found = true, true
				break
			}
		}
		if !found {
			t.Errorf("no %s %s naming %q:\n%s", sev, w.code, w.phrase, res)
		}
	}
	if len(got) != wanted {
		t.Errorf("%d issues, want %d:\n%s", len(got), wanted, res)
	}
}

func sbFailures(res diag.Result) int {
	n := 0
	for issue := range res.Issues() {
		if issue.Severity().IsFailure() {
			n++
		}
	}
	return n
}

func TestRead_TheFixtureIsClean(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for _, mode := range sbModes() {
		sbCheck(t, snapshot.Verify(t.Context(), []byte(doc), s, mode.opts...), nil, mode, 2)
		_, res := snapshot.Load(t.Context(), []byte(doc), s, mode.opts...)
		sbCheck(t, res, nil, mode, 2)
	}
	_, res := snapshot.Info(t.Context(), []byte(doc))
	sbCheck(t, res, nil, sbMode{}, 0)
	if res := snapshot.RebuildDocument([]byte(doc), s); !res.OK() {
		t.Errorf("RebuildSnapshot over the fixture's parts: %s", res)
	}
}

func TestRead_OneBreakDrawsOneIssue(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for _, row := range sbRows() {
		edited := doc
		for _, e := range row.edits {
			edited = editOnce(t, edited, e[0], e[1])
		}
		required := row.required
		if required == 0 {
			required = 2
		}
		for _, mode := range sbModes() {
			t.Run(row.name+"/Verify/"+mode.name, func(t *testing.T) {
				t.Parallel()
				sbCheck(t, snapshot.Verify(t.Context(), []byte(edited), s, mode.opts...), row.want, mode, required)
			})
			t.Run(row.name+"/Load/"+mode.name, func(t *testing.T) {
				t.Parallel()
				_, res := snapshot.Load(t.Context(), []byte(edited), s, mode.opts...)
				sbCheck(t, res, row.want, mode, required)
			})
		}
		t.Run(row.name+"/Info", func(t *testing.T) {
			t.Parallel()
			_, res := snapshot.Info(t.Context(), []byte(edited))
			if row.schemaless {
				sbCheck(t, res, row.want, sbMode{}, 0)
				return
			}
			// Without a schema Info reads the break as another fact or as none,
			// and reports it once at most all the same.
			if n := sbFailures(res); n > len(row.want) {
				t.Errorf("%d issues for %d breaks:\n%s", n, len(row.want), res)
			}
		})
		if row.rebuild > 0 {
			t.Run(row.name+"/RebuildSnapshot", func(t *testing.T) {
				t.Parallel()
				if n := sbFailures(snapshot.RebuildDocument([]byte(edited), s)); n != row.rebuild {
					t.Errorf("RebuildSnapshot reports %d issues over the document's parts, want %d:\n%s",
						n, row.rebuild, snapshot.RebuildDocument([]byte(edited), s))
				}
				if row.rebuild == 1 {
					if n := sbFailures(snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))); n != 1 {
						t.Errorf("Verify reports %d issues where RebuildSnapshot reports 1", n)
					}
				}
			})
		}
	}
}

// A duplicate under a name that is no composition names its slot in the
// details the same fact carries on an instance.
func TestRead_ADuplicateUnderNoCompositionNamesItsSlot(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	edited := editOnce(t, doc, `"parent_key":["o1"],"relation":"TAGS"`, `"parent_key":["o1"],"relation":"NOPE"`)
	res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
	if res.Len() != 1 {
		t.Fatalf("%d issues, want 1: %s", res.Len(), res)
	}
	for issue := range res.Issues() {
		got := map[string]string{}
		for _, d := range issue.Details() {
			got[d.Key] = d.Value
		}
		if issue.Code() != diag.E_SNAPSHOT_INVALID_COMPOSED || got[diag.DetailKeyTypeName] != "sb#Order" || got[diag.DetailKeyRelationName] != "NOPE" {
			t.Errorf("issue %s with details %v, want %s naming sb#Order and NOPE", issue.Code(), got, diag.E_SNAPSHOT_INVALID_COMPOSED)
		}
	}
}

// A duplicate's own type is a fact apart from its slot's children: under a
// slot the walk refused for holding two children, a duplicate of another type
// than the composition's draws its own issue, and resolves no conflict. Under
// a slot the walk refused for its name, the name is the one break.
func TestRead_ADuplicateUnderARefusedSlotKeepsItsOwnPosition(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	twoSeals := strings.Replace(sbSeal, `,"provenance":null}]`, `,"provenance":null},{"key":["s3"],"type":5,"properties":{"code":"s3"},"provenance":null}]`, 1)
	for name, c := range map[string]struct {
		edits [][2]string
		want  []diag.Code
	}{
		"a Tag recorded under a (one) slot of two Seals": {
			[][2]string{{sbSeal, twoSeals}, {sbSealDup, strings.Replace(sbSealDup, `{"type":5,"key":["s2"]`, `{"type":6,"key":["s2"]`, 1)}},
			[]diag.Code{diag.E_DUPLICATE_COMPOSED_PK, diag.E_SNAPSHOT_TYPE_MISMATCH},
		},
		"a Seal recorded under a (one) slot of two Seals": {
			[][2]string{{sbSeal, twoSeals}},
			[]diag.Code{diag.E_DUPLICATE_COMPOSED_PK},
		},
		"a Seal recorded under the association its children sit in": {
			[][2]string{
				{sbP1, strings.Replace(sbP1, `"edges":{`, `"composed":{"KNOWS":[{"key":["s9"],"type":5,"properties":{"code":"s9"},"provenance":null}]},"edges":{`, 1)},
				{`"parent_type":3,"parent_key":["o1"],"relation":"SEAL"`, `"parent_type":4,"parent_key":["p1"],"relation":"KNOWS"`},
			},
			[]diag.Code{diag.E_SNAPSHOT_INVALID_COMPOSED},
		},
		"a Seal recorded under the undeclared slot its children sit in": {
			[][2]string{{sbSeal, strings.Replace(sbSeal, `"SEAL"`, `"NOPE"`, 1)}, {`"relation":"SEAL"`, `"relation":"NOPE"`}},
			[]diag.Code{diag.E_SNAPSHOT_INVALID_COMPOSED},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range c.edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
			if sbFailures(res) != len(c.want) {
				t.Errorf("%d issues, want %v:\n%s", sbFailures(res), c.want, res)
			}
			for _, code := range c.want {
				if !hasCode(res, code) {
					t.Errorf("no %s:\n%s", code, res)
				}
			}
		})
	}
}

// An unresolved record a check refused names no reference to resolve, as a
// refused edge names none: a refused record whose source no root stands at is
// one break, as RebuildSnapshot reports it.
func TestRead_ARefusedRecordNamesNoSource(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for name, record := range map[string]string{
		"under an undeclared association":  sbLost("p7", "NOPE", 4, `["p8"]`, `{}`),
		"with an undeclared edge property": sbLost("p7", "KNOWS", 4, `["p8"]`, `{"bogus":1}`),
		"with a target key of two parts":   sbLost("p7", "KNOWS", 4, `["p8","x"]`, `{"note":"m"}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := []byte(editOnce(t, doc, sbUnresolved, sbUnresolved+record+","))
			res := snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(0))
			if sbFailures(res) != 1 || hasCode(res, diag.E_SNAPSHOT_DANGLING_REFERENCE) {
				t.Errorf("%d issues, want the record's one refusal and no dangling source:\n%s", sbFailures(res), res)
			}
			if n := sbFailures(snapshot.RebuildDocument(edited, s)); n != 1 {
				t.Errorf("RebuildSnapshot reports %d issues, want 1", n)
			}
		})
	}
}

// A capped collector keeps the same issue on every read of one document.
func TestRead_ACappedReadKeepsTheSameIssues(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for name, edits := range map[string][][2]string{
		"undeclared relations": {{sbMentor, sbMentor + `,"XA":[{"target_type":1,"target_key":["c1"],"properties":{}}],"XB":[{"target_type":1,"target_key":["c1"],"properties":{}}],"XC":[{"target_type":1,"target_key":["c1"],"properties":{}}],"XD":[{"target_type":1,"target_key":["c1"],"properties":{}}]`}},
		"edges to no root": {
			{sbMentor, `"MENTOR":[{"target_type":4,"target_key":["p7"],"properties":{}}]`},
			{sbKnows, `"KNOWS":[{"target_type":4,"target_key":["p6"],"properties":{"note":"n"}}]`},
			{sbEmployer, `"EMPLOYER":[{"target_type":1,"target_key":["c7"],"properties":{"since":1999}}]`},
		},
		"undeclared compositions": {{sbSeal, `"NA":[],"NB":[],"NC":[],"ND":[],` + sbSeal}},
		"two (one) associations over": {
			{sbMentor, `"MENTOR":[{"target_type":4,"target_key":["p2"],"properties":{}},{"target_type":4,"target_key":["p1"],"properties":{}}]`},
			{sbEmployer, `"EMPLOYER":[{"target_type":1,"target_key":["c1"],"properties":{"since":1999}},{"target_type":1,"target_key":["c2"],"properties":{}}]`},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			read := func() string {
				return snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(1)).String()
			}
			first := read()
			if !strings.Contains(first, "E_") {
				t.Fatalf("the document drew no issue: %s", first)
			}
			for i := range 200 {
				if got := read(); got != first {
					t.Fatalf("read %d kept another issue:\n%s\nthe first read kept:\n%s", i+1, got, first)
				}
			}
		})
	}
}

// A reference to an address a refused unit states is judged no further: a root
// whose stored key is not its property's states its property's address, a
// group entry the reader refused states each of its roots' addresses, and a
// composed child states its property's key in its slot. The refusal is the
// break's one issue. A reference no refused unit states still dangles.
func TestRead_AReferenceARefusedUnitStatesIsJudgedNoFurther(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	const (
		sbC1              = `{"key":["c1"],"properties":{"id":"c1"},"provenance":null}`
		sbC3              = `{"key":["c3"],"properties":{"id":"c3"},"provenance":null}`
		sbCX              = `{"key":["cx"],"properties":{"id":"c2"},"provenance":null}`
		sbPX              = `{"key":["px"],"properties":{"id":"p2"},"provenance":null}`
		sbRootDupConflict = `"conflict":{"type":1,"key":["c1"]}`
	)
	sbNullOrder := [2]string{`{"type":3,"items":[` + sbO1, `{"type":null,"items":[` + sbO1}
	sbRepeatOrder := [2]string{`{"type":3,"items":[` + sbO1, `{"type":3,"items":[]},{"type":3,"items":[` + sbO1}
	sbOrderOX := [2]string{sbO1, `{"key":["ox"],"properties":{"id":"o1"},"composed":{`}
	sbTagNoKey := [2]string{sbTagConflict, `"conflict":{"type":6,"key":[]}`}
	const (
		sbSealS1 = `{"key":["s1"],"type":5,"properties":{"code":"s1"},"provenance":null}`
		sbSealS3 = `{"key":["s3"],"type":5,"properties":{"code":"s3"},"provenance":null}`
	)
	for name, c := range map[string]struct {
		edits [][2]string
		want  int
	}{
		"an edge to a root whose key is not its property's":                   {[][2]string{{sbC2, sbCX}}, 1},
		"records whose source's key is not its property's":                    {[][2]string{{sbP2, `{"key":["px"],"properties":{"id":"p2"},"provenance":null}`}}, 1},
		"duplicates under a root in a group with no row":                      {[][2]string{{`{"type":3,"items":[` + sbO1, `{"type":null,"items":[` + sbO1}}, 1},
		"an edge to a root in a group that repeats a row":                     {[][2]string{{sbC1 + `,` + sbC2 + `]}`, sbC1 + `]},{"type":1,"items":[` + sbC2 + `]}`}}, 1},
		"a conflict naming a child whose key is not its property":             {[][2]string{{sbSeal, strings.Replace(sbSeal, `"key":["s1"]`, `"key":["sx"]`, 1)}}, 1},
		"an edge to the key property of a root in a group that repeats a row": {[][2]string{{sbC1 + `,` + sbC2 + `]}`, sbC1 + `]},{"type":1,"items":[` + sbCX + `]}`}}, 1},
		"an edge to the key property of a root in a group with no row":        {[][2]string{{sbC1 + `,` + sbC2 + `]}`, sbC1 + `]},{"type":null,"items":[` + sbCX + `]}`}}, 1},
		"a root duplicate's conflict a refused unit states, not its own key": {[][2]string{
			{sbC2, sbCX}, {sbRootDupConflict, `"conflict":{"type":1,"key":["c2"]}`},
		}, 2},
		"a root duplicate of a refused root whose conflict is its own key": {[][2]string{
			{sbC2, sbCX}, {sbRootDup, `{"type":1,"key":["c2"],"instance":{"key":["c2"],"properties":{"id":"c2"},"provenance":null},"conflict":{"type":1,"key":["c2"]}}`},
		}, 1},
		"a composed duplicate naming no composition under a stated parent": {[][2]string{sbNullOrder, {`"relation":"SEAL"`, `"relation":"NOPE"`}}, 2},
		"a record under a stated source naming a target a root holds": {[][2]string{
			{sbP2, sbPX}, {`"target_key":["p8"]`, `"target_key":["p1"]`},
		}, 2},
		"an absent record under an optional association of a stated source": {[][2]string{
			{sbP2, sbPX}, {`"target_key":["p9"],"required":false,"reason":"target_missing"}`, `"target_key":null,"required":false,"reason":"absent"}`},
		}, 2},
		"a refused child under a refused parent": {[][2]string{
			{sbO1, `{"key":["ox"],"properties":{"id":"o1"},"composed":{`},
			{sbSeal, strings.Replace(sbSeal, `"key":["s1"]`, `"key":["sx"]`, 1)},
		}, 2},
		"an edge to a null key beside a refused root with no key property": {[][2]string{
			{sbC2, `{"key":["cx"],"properties":{},"provenance":null}`},
			{sbClients, strings.Replace(sbClients, `"target_key":["c2"]`, `"target_key":[null]`, 1)},
		}, 2},
		"an edge to another address beside a group with no row": {[][2]string{
			sbNullOrder, {sbClients, strings.Replace(sbClients, `"target_key":["c2"]`, `"target_key":["c9"]`, 1)},
		}, 2},
		"every root of a group with no row":        {[][2]string{{`{"type":1,"items":[` + sbC1, `{"type":null,"items":[` + sbC1}}, 1},
		"every root of a group that repeats a row": {[][2]string{{`{"type":1,"items":[` + sbC1 + `,` + sbC2 + `]}`, `{"type":1,"items":[` + sbC3 + `]},{"type":1,"items":[` + sbC1 + `,` + sbC2 + `]}`}}, 1},
		"a conflict naming another child beside a stated one": {[][2]string{
			{sbSeal, strings.Replace(sbSeal, `"key":["s1"]`, `"key":["sx"]`, 1)},
			{sbSealConflict, `"conflict":{"type":5,"key":["s9"]}`},
		}, 2},
		"a keyed duplicate with no conflict key under a parent in a group that repeats a row": {[][2]string{sbRepeatOrder, sbTagNoKey}, 2},
		"a keyed duplicate with no conflict key under a parent in a group with no row":        {[][2]string{sbNullOrder, sbTagNoKey}, 2},
		"a keyed duplicate with no conflict key under a stated parent":                        {[][2]string{sbOrderOX, sbTagNoKey}, 2},
		"a keyed duplicate with no conflict key under a refused parent's stored key": {[][2]string{
			sbOrderOX, sbTagNoKey, {`"parent_key":["o1"],"relation":"TAGS"}`, `"parent_key":["ox"],"relation":"TAGS"}`},
		}, 2},
		"a duplicate under a refused parent's slot that its own composition does not name": {[][2]string{
			sbOrderOX, {`"SEAL":[{"key":["s1"]`, `"NOPE":[{"key":["s1"]`}, {`"relation":"SEAL"}`, `"relation":"NOPE"}`},
		}, 2},
		"a duplicate under a refused parent's stored key in a slot its composition does not name": {[][2]string{
			sbOrderOX,
			{`"SEAL":[{"key":["s1"]`, `"NOPE":[{"key":["s1"]`},
			{`"parent_key":["o1"],"relation":"SEAL"}`, `"parent_key":["ox"],"relation":"NOPE"}`},
		}, 2},
		"a duplicate under the sound root at a refused root's stated address keeps its dangling conflict": {[][2]string{
			{sbLastOrder, `,"provenance":null},{"key":["ox"],"properties":{"id":"o1"},"composed":{"SEAL":[` + sbSealS1 + `,` + sbSealS3 + `]},"provenance":null}]},{"type":4,"items":[`},
			{sbSealConflict, `"conflict":{"type":5,"key":["s9"]}`},
		}, 3},
		"a duplicate under the sound root at a refused root's stated address keeps its slot's name": {[][2]string{
			{sbLastOrder, `,"provenance":null},{"key":["ox"],"properties":{"id":"o1"},"composed":{"NOPE":[` + sbSealS1 + `]},"provenance":null}]},{"type":4,"items":[`},
			{`"relation":"SEAL"}`, `"relation":"NOPE"}`},
		}, 3},
		"a duplicate under a sound root at an address a refused root shares keeps its slot's name": {[][2]string{
			{sbLastOrder, `,"provenance":null},{"key":["o1"],"properties":{"id":"o6"},"composed":{"NOPE":[` + sbSealS1 + `]},"provenance":null}]},{"type":4,"items":[`},
			{`"relation":"SEAL"}`, `"relation":"NOPE"}`},
		}, 3},
		"a duplicate under two sound roots at a refused root's stated address keeps its slot's name": {[][2]string{
			{sbLastOrder, `,"provenance":null},{"key":["o1"],"properties":{"id":"o1"},"provenance":null},{"key":["ox"],"properties":{"id":"o1"},"composed":{"NOPE":[` + sbSealS1 + `]},"provenance":null}]},{"type":4,"items":[`},
			{`"relation":"SEAL"}`, `"relation":"NOPE"}`},
		}, 4},
		"a (one) association of the sound root at a refused root's stated address keeps its count": {[][2]string{
			{sbP2, sbP2 + `,{"key":["px"],"properties":{"id":"p1"},"composed":{"EMPLOYER":[` + sbInnerBox + `]},"provenance":null}`},
			{sbEmployer, sbEmployerTwice},
		}, 3},
		"a (one) association of a sound root at an address a refused root shares keeps its count": {[][2]string{
			{sbP2, sbP2 + `,{"key":["p1"],"properties":{"id":"pz"},"composed":{"EMPLOYER":[` + sbInnerBox + `]},"provenance":null}`},
			{sbEmployer, sbEmployerTwice},
		}, 3},
		"an edge to an address no refused unit states": {[][2]string{
			{sbC2, sbCX},
			{sbClients, strings.Replace(sbClients, `"target_key":["c2"]`, `"target_key":["c9"]`, 1)},
		}, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range c.edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
			if n := sbFailures(res); n != c.want {
				t.Errorf("%d issues, want %d:\n%s", n, c.want, res)
			}
		})
	}
}

// A composed slot and an association may share a name: the walk's refusal of
// the slot leaves the association's (one) count judged.
func TestRead_ARefusedSlotLeavesTheAssociationOfItsNameCounted(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	edited := editOnce(t, doc, sbEmployer, sbEmployerTwice)
	edited = editOnce(t, edited, sbP1, `{"key":["p1"],"properties":{"age":30,"id":"p1"},"composed":{"EMPLOYER":[`+sbInnerBox+`]},"edges":{`)
	res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
	if n := sbFailures(res); n != 2 {
		t.Errorf("%d issues, want 2 (the slot, the count):\n%s", n, res)
	}
	if !res.HasCode(diag.E_GRAPH_CARDINALITY) {
		t.Errorf("no %s:\n%s", diag.E_GRAPH_CARDINALITY, res)
	}
}

// A refused unit states its addresses by their canonical keys, so a reference
// that spells the same instant another way is judged no further too.
func TestRead_ARefusedUnitStatesItsCanonicalAddress(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "ts"

type Ev {
	at Timestamp primary
}

type Watcher {
	id String primary
	--> SEES (_:many) Ev
}
`, "ts.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, raw := range []struct {
		typ   string
		props map[string]any
	}{
		{"Ev", map[string]any{"at": "2020-01-01T00:00:00Z"}},
		{"Ev", map[string]any{"at": "2021-01-01T00:00:00Z"}},
		{"Watcher", map[string]any{"id": "w1", "SEES": []any{
			map[string]any{"_target_at": "2020-01-01T00:00:00Z"},
			map[string]any{"_target_at": "2021-01-01T00:00:00Z"},
		}}},
	} {
		vi, res := v.ValidateOne(t.Context(), raw.typ, instance.RawInstance{Properties: raw.props})
		if res.HasErrors() {
			t.Fatalf("validate %s: %s", raw.typ, res)
		}
		if res := g.Add(t.Context(), vi); res.HasErrors() {
			t.Fatalf("add %s: %s", raw.typ, res)
		}
	}
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	doc := sbIntegrity.ReplaceAllString(string(data), `"integrity_hash":""`)
	const (
		ev20 = `{"key":["2020-01-01T00:00:00Z"],"properties":{"at":"2020-01-01T00:00:00Z"},"provenance":null}`
		ev21 = `{"key":["2021-01-01T00:00:00Z"],"properties":{"at":"2021-01-01T00:00:00Z"},"provenance":null}`
		// A refused group's root with no key property states its stored key alone.
		ev20b = `{"key":["2020-01-01T00:00:00+00:00"],"properties":{},"provenance":null}`
	)
	group := fmt.Sprintf(`{"type":%d,"items":[`, tableRow(t, doc, "Ev"))
	for name, edit := range map[string][2]string{
		"a root whose property spells its key another way": {ev20, `{"key":["1999-01-01T00:00:00Z"],"properties":{"at":"2020-01-01T00:00:00+00:00"},"provenance":null}`},
		"a root of a group that repeats a row":             {group + ev20 + `,` + ev21 + `]}`, group + ev21 + `]},` + group + ev20b + `]}`},
		"a root of a group with no row":                    {group + ev20 + `,` + ev21 + `]}`, group + ev21 + `]},{"type":null,"items":[` + ev20b + `]}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := snapshot.Verify(t.Context(), []byte(editOnce(t, doc, edit[0], edit[1])), s, snapshot.WithIssueLimit(0))
			if n := sbFailures(res); n != 1 {
				t.Errorf("%d issues, want 1:\n%s", n, res)
			}
		})
	}
}

// A duplicate whose stored key the reader refuses has no address, so no
// conflict is resolved for it: the refusal is its one issue, as RebuildSnapshot
// reports it once.
func TestRead_ADuplicateWhoseKeyIsRefusedResolvesNoConflict(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for name, c := range map[string]struct{ from, to string }{
		"a root duplicate whose key has another arity": {
			sbRootDup,
			`{"type":1,"key":["c1","x"],"instance":{"key":["c1","x"],"properties":{"id":"c1"},"provenance":null},"conflict":{"type":1,"key":["c1"]}}`,
		},
		"a root duplicate whose key is no scalar": {
			sbRootDup,
			`{"type":1,"key":[["c1"]],"instance":{"key":[["c1"]],"properties":{"id":"c1"},"provenance":null},"conflict":{"type":1,"key":["c1"]}}`,
		},
		"a composed duplicate whose key has another arity": {sbTagDup, strings.ReplaceAll(sbTagDup, `"key":["t1"],"instance":{"key":["t1"]`, `"key":["t1","x"],"instance":{"key":["t1","x"]`)},
		"a composed duplicate whose key is empty":          {sbTagDup, strings.ReplaceAll(sbTagDup, `"key":["t1"],"instance":{"key":["t1"]`, `"key":[],"instance":{"key":[]`)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := []byte(editOnce(t, doc, c.from, c.to))
			res := snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(0))
			if n := sbFailures(res); n != 1 || !strings.Contains(res.String(), "stores key") {
				t.Errorf("%d issues, want the key's one refusal:\n%s", n, res)
			}
			if n := sbFailures(snapshot.RebuildDocument(edited, s)); n != 1 {
				t.Errorf("RebuildSnapshot reports %d issues, want 1", n)
			}
		})
	}
}

// Two target_missing records under a (many) association are two records the
// association may hold: neither is counted toward a (one) association's
// overflow.
func TestRead_RecordsUnderAManyAssociationAreNoOverflow(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	edited := []byte(editOnce(t, doc, sbKnowsLost, sbKnowsLost+`,`+sbLost("p2", "KNOWS", 4, `["p7"]`, `{"note":"z"}`)))
	if res := snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(0)); sbFailures(res) != 0 {
		t.Errorf("two records under a (many) association are refused:\n%s", res)
	}
}

// Children past the composed depth limit each draw E_SNAPSHOT_DEPTH_EXCEEDED,
// and no later check compares their keys: two children at one key there are
// no duplicate.
func TestRead_ChildrenPastTheDepthLimitCompareNoKeys(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "dk"

type R {
	id String primary
	*-> K (_:many) K
}

part type K {
	code String primary
	*-> K (_:many) K
}
`, "dk.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	vi, res := instance.NewValidator(s).ValidateOne(t.Context(), "R", instance.RawInstance{Properties: map[string]any{
		"id": "r1", "K": []any{map[string]any{"code": "a"}},
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	g := graph.New(s)
	if r := g.Add(t.Context(), vi); r.HasErrors() {
		t.Fatalf("add: %s", r)
	}
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	doc := sbIntegrity.ReplaceAllString(string(data), `"integrity_hash":""`)
	const leaf = `{"key":["a"],"type":0,"properties":{"code":"a"},"provenance":null}`
	var b strings.Builder
	for range instance.MaxComposedDepth {
		b.WriteString(`{"key":["a"],"type":0,"properties":{"code":"a"},"composed":{"K":[`)
	}
	b.WriteString(leaf + "," + leaf)
	for range instance.MaxComposedDepth {
		b.WriteString(`]},"provenance":null}`)
	}
	res = snapshot.Verify(t.Context(), []byte(editOnce(t, doc, leaf, b.String())), s, snapshot.WithIssueLimit(0))
	if res.Len() == 0 {
		t.Fatal("a tree past the depth limit is accepted")
	}
	for issue := range res.Issues() {
		if issue.Code() != diag.E_SNAPSHOT_DEPTH_EXCEEDED {
			t.Errorf("%s beside the depth refusals: %s", issue.Code(), res)
		}
	}
}

// Only a root holds composition slots: a keyed child's own children fill no
// slot a duplicate is resolved in, so a duplicate parented at a root of the
// child's type finds that root's children alone.
func TestRead_OnlyARootHoldsSlots(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "dk"

type R {
	id String primary
	*-> K (_:many) K
}

part type K {
	code String primary
	*-> K (_:many) K
}
`, "dk.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	vi, res := instance.NewValidator(s).ValidateOne(t.Context(), "R", instance.RawInstance{Properties: map[string]any{
		"id": "r1", "K": []any{map[string]any{"code": "a", "K": []any{map[string]any{"code": "b"}}}},
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res)
	}
	g := graph.New(s)
	if r := g.Add(t.Context(), vi); r.HasErrors() {
		t.Fatalf("add: %s", r)
	}
	data, res := snapshot.Marshal(t.Context(), g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	doc := sbIntegrity.ReplaceAllString(string(data), `"integrity_hash":""`)
	// A root of the part type at the child's key, holding another child, and a
	// duplicate parented at that root whose conflict is the nested child's key.
	doc = editOnce(t, doc, `"provenance":null}]}],"diagnostics":{"duplicates":[]`,
		`"provenance":null}]},{"type":0,"items":[{"key":["a"],"properties":{"code":"a"},"composed":{"K":[`+
			`{"key":["c"],"type":0,"properties":{"code":"c"},"provenance":null}]},"provenance":null}]}],"diagnostics":{"duplicates":[`+
			`{"type":0,"key":["b"],"instance":{"key":["b"],"properties":{"code":"b"},"provenance":null},"conflict":{"type":0,"key":["b"]},`+
			`"parent_type":0,"parent_key":["a"],"relation":"K"}]`)
	res = snapshot.Verify(t.Context(), []byte(doc), s, snapshot.WithIssueLimit(0))
	if res.Len() != 2 || !hasCode(res, diag.E_SNAPSHOT_INVALID_ROOT) || !hasCode(res, diag.E_SNAPSHOT_DANGLING_REFERENCE) {
		t.Errorf("%s, want the root's E_SNAPSHOT_INVALID_ROOT and the duplicate's dangling conflict", res)
	}
}

// A root whose key property no longer gives its stored key still stands at
// that address: every reference to it resolves, and the key's refusal is the
// edit's one issue, as RebuildSnapshot reports it. A root whose stored key was
// edited stands at another address, so a reference to the old one dangles at
// the reader; RebuildSnapshot, which resolves no reference before its gate,
// reports the key alone.
func TestRead_ARootWhoseKeyIsRefusedStillStandsAtItsAddress(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	for name, c := range map[string]struct {
		from, to string
		reader   int
	}{
		"a referenced Company's key property":         {sbC2, strings.Replace(sbC2, `{"id":"c2"}`, `{"id":"cx"}`, 1), 1},
		"a Person with edges and records":             {sbP2, strings.Replace(sbP2, `{"id":"p2"}`, `{"id":"px"}`, 1), 1},
		"a Person with no key property":               {sbP2, strings.Replace(sbP2, `{"id":"p2"}`, `{}`, 1), 1},
		"an Order with composed duplicates":           {sbO1, strings.Replace(sbO1, `{"id":"o1"}`, `{"id":"ox"}`, 1), 1},
		"a Person's stored key, its references stale": {sbP2, strings.Replace(sbP2, `["p2"]`, `["pz"]`, 1), -1},
		// p9 is the target of p2's MENTOR record; a root there whose key is refused
		// is no root graph.Add holds.
		"a root at a record's target whose key is refused": {sbLastOrder, sbLastOrder + `{"key":["p9"],"properties":{"id":"px"},"provenance":null},`, 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := []byte(editOnce(t, doc, c.from, c.to))
			res := snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(0))
			if c.reader > 0 && sbFailures(res) != c.reader {
				t.Errorf("%d issues, want %d:\n%s", sbFailures(res), c.reader, res)
			}
			if !strings.Contains(res.String(), "stores key") {
				t.Errorf("no key refusal:\n%s", res)
			}
			if n := sbFailures(snapshot.RebuildDocument(edited, s)); n != 1 {
				t.Errorf("RebuildSnapshot reports %d issues, want 1", n)
			}
		})
	}
}

// At an address two roots stand at, an unresolved record names the address, not
// a root: it counts toward no (one) association, and no absent or empty record
// there is judged beside another. A composed duplicate parented there resolves
// no conflict. The required-association fact is judged, over every edge the
// roots there hold. A root whose key a check refused holds its records toward
// no (one) association, as it holds its edges. Each document is one break.
func TestRead_AnAddressNoSoundRootHoldsAloneJudgesNoRecord(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	p4 := func(id, edges string) string {
		if edges != "" {
			edges = `,"edges":{` + edges + `}`
		}
		return `{"key":["p4"],"properties":{"id":"` + id + `"}` + edges + `,"provenance":null}`
	}
	records := func(rs ...string) [2]string {
		return [2]string{sbUnresolved, sbUnresolved + strings.Join(rs, ",") + ","}
	}
	roots := func(rs ...string) [2]string { return [2]string{sbP2, sbP2 + "," + strings.Join(rs, ",")} }
	mentorLost := func(target string) string {
		return sbLost("p4", "MENTOR", 4, `["`+target+`"]`, `{}`)
	}
	for name, c := range map[string]struct {
		edits [][2]string
		want  diag.Code
	}{
		"a refused root with a MENTOR edge and a MENTOR record": {
			[][2]string{roots(p4("pz", sbMentor)), records(mentorLost("p9"))}, diag.E_SNAPSHOT_MALFORMED,
		},
		"a refused root with two MENTOR records": {
			[][2]string{roots(p4("pz", "")), records(mentorLost("p9"), mentorLost("p8"))}, diag.E_SNAPSHOT_MALFORMED,
		},
		"two roots at one address, the first with an EMPLOYER edge, and an absent EMPLOYER record": {
			[][2]string{roots(p4("p4", sbEmployer), p4("p4", "")), records(sbRecord("p4", "EMPLOYER", "absent"), sbRecord("p4", "CLIENTS", "empty"))}, diag.E_DUPLICATE_PK,
		},
		"two roots at one address, the second with the EMPLOYER edge": {
			[][2]string{roots(p4("p4", ""), p4("p4", sbEmployer)), records(sbRecord("p4", "CLIENTS", "empty"))}, diag.E_DUPLICATE_PK,
		},
		"a root with a MENTOR edge, a refused root at its address and a MENTOR record": {
			[][2]string{roots(p4("p4", sbMentor), p4("pz", "")), records(mentorLost("p9"), sbRecord("p4", "CLIENTS", "empty"), sbRecord("p4", "EMPLOYER", "absent"))}, diag.E_SNAPSHOT_MALFORMED,
		},
		"a refused root, a root at its address with a MENTOR edge and a MENTOR record": {
			[][2]string{roots(p4("pz", ""), p4("p4", sbMentor)), records(mentorLost("p9"), sbRecord("p4", "CLIENTS", "empty"), sbRecord("p4", "EMPLOYER", "absent"))}, diag.E_SNAPSHOT_MALFORMED,
		},
		"two Orders at one address, the composed duplicates the second's": {
			[][2]string{{sbO1, `{"key":["o1"],"properties":{"id":"o1"},"provenance":null},` + sbO1}}, diag.E_DUPLICATE_PK,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := doc
			for _, e := range c.edits {
				edited = editOnce(t, edited, e[0], e[1])
			}
			res := snapshot.Verify(t.Context(), []byte(edited), s, snapshot.WithIssueLimit(0))
			if sbFailures(res) != 1 || !hasCode(res, c.want) {
				t.Errorf("%d issues, want one %s:\n%s", sbFailures(res), c.want, res)
			}
			if n := sbFailures(snapshot.RebuildDocument([]byte(edited), s)); n != 1 {
				t.Errorf("RebuildSnapshot reports %d issues, want 1", n)
			}
		})
	}
}

// The reader nests each edge in its root, so at an address two roots share
// each root's own (one) of two edges is a second break it reports, whichever
// root holds the edges; RebuildSnapshot, whose parts cannot say which root an
// edge there is the edge of, reports the duplicate alone.
func TestRead_ASharedAddressKeepsEachRootsOwnOverflow(t *testing.T) {
	t.Parallel()
	s, doc := singleBreakDoc(t)
	two := `"MENTOR":[{"target_type":4,"target_key":["p2"],"properties":{}},{"target_type":4,"target_key":["p1"],"properties":{}}]`
	twin := `{"key":["p4"],"properties":{"id":"p4"},"edges":{` + two + `},"provenance":null}`
	for name, roots := range map[string]string{
		"the first root":  twin + "," + sbP4,
		"the second root": sbP4 + "," + twin,
		"both roots":      twin + "," + twin,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			edited := []byte(editOnce(t, editOnce(t, doc, sbP2, sbP2+","+roots), sbUnresolved,
				sbUnresolved+sbRecord("p4", "CLIENTS", "empty")+","+sbRecord("p4", "EMPLOYER", "absent")+","))
			overflows := strings.Count(roots, two)
			res := snapshot.Verify(t.Context(), edited, s, snapshot.WithIssueLimit(0))
			cardinality := 0
			for issue := range res.Issues() {
				if issue.Code() == diag.E_GRAPH_CARDINALITY {
					cardinality++
				}
			}
			if sbFailures(res) != 1+overflows || !hasCode(res, diag.E_DUPLICATE_PK) || cardinality != overflows {
				t.Errorf("%d issues, want the duplicate and %d (one) overflow:\n%s", sbFailures(res), overflows, res)
			}
			if n := sbFailures(snapshot.RebuildDocument(edited, s)); n != 1 {
				t.Errorf("RebuildSnapshot reports %d issues, want 1", n)
			}
		})
	}
}
