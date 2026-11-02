package yammm_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const keySchema = `schema "keys"

type T {
	id String primary
	f Float
	--> LINKS (many) U {
		w Integer
	}
	--> NEEDS (one) U
	--> MAYBE (_:one) U
	*-> PARTS (many) P
	*-> ONE (_:one) P
}

type U {
	id String primary
}

part type P {
	pid String primary
	*-> SUB (many) P
}

part type N {
	x String
}

abstract type A {
	id String primary
}
`

// longTypeName is a type name the cut shortens.
var longTypeName = "L" + strings.Repeat("Z", 100)

// NewFromSnapshot reads a snapshot of pairFrom under pairTo. Both load under
// one source name, so a type both declare has one identity in both.
var (
	pairFrom = `schema "pair"

type T {
	id String primary
	--> NEEDS (_:one) ` + longTypeName + `
	*-> PARTS (many) P
}

type ` + longTypeName + ` {
	id String primary
}

part type P {
	pid String primary
}

part type Q {
	pid String primary
}
`
	pairTo = `schema "pair"

type T {
	id String primary
	--> NEEDS (_:one) U
	*-> PARTS (many) Q
}

type U {
	id String primary
}

part type P {
	pid String primary
}

part type Q {
	pid String primary
}
`
)

// Every message the graph and snapshot paths write about a key, a relation or
// another name the data writes repeats its head alone, whichever rule draws
// it, and a short one is shown as the rule's form shows it.
func TestMessages_RepeatAKeyCutToItsHead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, keySchema, "keys.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	c := &echoChecker{t: t, judged: map[string]int{}}

	var paths []string
	result := func(where string, r diag.Result) {
		t.Helper()
		paths = append(paths, where)
		c.result(where, r)
	}
	fail := func(where string, err error) {
		t.Helper()
		paths = append(paths, where)
		c.err(where, err)
	}
	shows := func(where, got, want string) {
		t.Helper()
		if !strings.Contains(got, want) {
			t.Errorf("%s: the message does not show %s:\n%.600s", where, want, got)
		}
	}
	messages := func(r diag.Result) string {
		var b strings.Builder
		for issue := range r.Issues() {
			b.WriteString(issue.Message())
			b.WriteByte('\n')
		}
		return b.String()
	}
	// head is the cut form of a key over one longText component.
	head := `["` + strings.Repeat("Z", 30) + `… (4100 bytes)`

	valid := func(typeName string, props map[string]any) instance.ValidInstance {
		t.Helper()
		return instancetest.Valid(t, s, typeName, props)
	}
	needs := map[string]any{"_target_id": "u"}
	assembler := graph.NewBatchAssembler(ctx, s)
	for _, record := range []instance.ValidInstance{
		valid("U", map[string]any{"id": "u"}),
		valid("U", map[string]any{"id": "u2"}),
		valid("T", map[string]any{"id": longText, "needs": needs, "maybe": map[string]any{"_target_id": "u"}}),
	} {
		if err := assembler.AddValid(record); err != nil {
			t.Fatal(err)
		}
	}
	_, mergeErr := assembler.AddValidOrMerge(valid("T", map[string]any{"id": longText, "needs": needs, "maybe": map[string]any{"_target_id": "u2"}}))
	refused, isContextual := errors.AsType[*diag.ContextualError](mergeErr)
	if !isContextual {
		t.Fatalf("AddValidOrMerge a second (one) target: %v", mergeErr)
	}
	result("AddValidOrMerge a second (one) target", refused.Result)

	for _, key := range []string{"[" + longDigits + "e99999]", "null " + longText, `[] "` + longText + `"`} {
		_, err := graph.ParseKey(key)
		fail("ParseKey", err)
	}
	_, err := graph.ParseKey(`[1,`)
	fail("ParseKey a cut array", err)
	if err != nil {
		shows("ParseKey a cut array", err.Error(), `graph: parsing key "[1,": `)
	}

	from, res := schema.LoadString(ctx, pairFrom, "pair.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	into, res := schema.LoadString(ctx, pairTo, "pair.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	old := graph.New(from)
	if r := old.Add(ctx, instancetest.Valid(t, from, longTypeName, map[string]any{"id": "l"})); r.HasErrors() {
		t.Fatal(r)
	}
	if r := old.Add(ctx, instancetest.Valid(t, from, "T", map[string]any{
		"id": "t", "needs": map[string]any{"_target_id": "l"}, "parts": []any{map[string]any{"pid": longText}},
	})); r.HasErrors() {
		t.Fatal(r)
	}
	_, r := graph.NewFromSnapshot(into, old.Snapshot())
	result("NewFromSnapshot under another schema", r)

	empty, r := snapshot.Marshal(ctx, graph.New(s).Snapshot())
	if r.HasErrors() {
		t.Fatal(r)
	}
	header, _, ok := strings.Cut(strings.TrimPrefix(string(empty), `{"yammm_snapshot":`), `,"types":`)
	if !ok {
		t.Fatalf("Marshal wrote no types table after the header: %s", empty)
	}
	if i := strings.Index(header, `"integrity_hash":"`); i >= 0 {
		start := i + len(`"integrity_hash":"`)
		end := start + strings.IndexByte(header[start:], '"')
		header = header[:start] + header[end:]
	}
	headerWith := func(old, replacement string) string {
		t.Helper()
		if !strings.Contains(header, old) {
			t.Fatalf("the header holds no %s: %s", old, header)
		}
		return strings.Replace(header, old, replacement, 1)
	}
	const typesTable = `,"types":[{"schema":"keys","name":"P"},{"schema":"keys","name":"T"},{"schema":"keys","name":"U"}]`
	docWith := func(hdr, instances, duplicates, unresolved string) []byte {
		return []byte(`{"yammm_snapshot":` + hdr + typesTable + `,"instances":[` + instances +
			`],"diagnostics":{"duplicates":[` + duplicates + `],"unresolved":[` + unresolved + `]}}`)
	}
	doc := func(instances, duplicates, unresolved string) []byte {
		return docWith(header, instances, duplicates, unresolved)
	}
	Z, Z2 := longText, longText+"2"
	item := func(key, props, edges, composed string) string {
		out := `{"key":["` + key + `"],"properties":{` + props + `}`
		if edges != "" {
			out += `,"edges":{` + edges + `}`
		}
		if composed != "" {
			out += `,"composed":{` + composed + `}`
		}
		return out + `,"provenance":null}`
	}
	id := func(key string) string { return `"id":"` + key + `"` }
	child := func(row, key string) string {
		return `{"key":["` + key + `"],"type":` + row + `,"properties":{"pid":"` + key + `"},"provenance":null}`
	}
	needsU := `"NEEDS":[{"target_type":2,"target_key":["u"],"properties":{}}]`
	root := func(key string) string { return item(key, id(key), needsU, "") }
	ts := func(items ...string) string { return `{"type":1,"items":[` + strings.Join(items, ",") + `]}` }
	us := func(keys ...string) string {
		items := make([]string, len(keys))
		for i, key := range keys {
			items[i] = item(key, id(key), "", "")
		}
		return `{"type":2,"items":[` + strings.Join(items, ",") + `]}`
	}
	dup := func(row, key, instance, conflict, parent string) string {
		out := `{"type":` + row + `,"key":["` + key + `"],"instance":` + instance
		if conflict != "" {
			out += `,"conflict":` + conflict
		}
		return out + parent + `}`
	}
	conflict := func(row, key string) string { return `{"type":` + row + `,"key":["` + key + `"]}` }
	partInstance := func(key string) string {
		return `{"key":["` + key + `"],"properties":{"pid":"` + key + `"},"provenance":null}`
	}
	record := func(source, relation, targetKey, reason string) string {
		return `{"source_type":1,"source_key":["` + source + `"],"relation":"` + relation + `","target_type":2,"target_key":` +
			targetKey + `,"required":false,"reason":"` + reason + `"}`
	}
	load := func(where string, data []byte, opts ...snapshot.LoadOption) diag.Result {
		t.Helper()
		_, r := snapshot.Load(ctx, data, s, opts...)
		if r.Len() == 0 {
			t.Errorf("%s: Load reported nothing", where)
		}
		result(where, r)
		return r
	}

	if _, r := snapshot.Load(ctx, doc(ts(root("t"))+","+us("u"), "", ""), s); !r.OK() {
		t.Fatalf("the clean document draws issues:\n%s", r)
	}

	_, r = snapshot.HeaderOnlyRead(ctx, strings.NewReader(`"`+Z+`"`))
	result("HeaderOnlyRead a string document", r)
	_, r = snapshot.HeaderOnlyRead(ctx, strings.NewReader(`{"`+Z+`": 1}`))
	result("HeaderOnlyRead a long first key", r)
	_, r = snapshot.HeaderOnlyRead(ctx, strings.NewReader(`{"yammm_snapshot":`+header+`,"`+Z+`": }`))
	result("HeaderOnlyRead a key whose value does not decode", r)

	load("Load a string document", []byte(`"`+Z+`"`))
	load("Load a long feature", docWith(headerWith(`"features":[]`, `"features":["`+Z+`"]`), ts(root("t"))+","+us("u"), "", ""))
	feature := load("Load an unknown feature", docWith(headerWith(`"features":[]`, `"features":["nope"]`), ts(root("t"))+","+us("u"), "", ""))
	shows("Load an unknown feature", messages(feature), `unrecognized feature "nope"`)
	renamed := headerWith(`"schema_name":"keys"`, `"schema_name":"`+Z+`"`)
	renamed = strings.Replace(renamed, `"schema_hash":"sha256:`, `"schema_hash":"sha256:0`, 1)
	load("Load a long schema name", docWith(renamed, ts(root("t"))+","+us("u"), "", ""))
	load("Load a repeated member name", doc(ts(root("t"))+`,{"type":2,"items":[{"key":["u"],"properties":{"id":"u","`+Z+`":1,"`+Z+`":2},"provenance":null}]}`, "", ""))
	load("Load a provenance path that does not parse", doc(ts(`{"key":["t"],"properties":{"id":"t"},"edges":{`+needsU+`},"provenance":{"source_name":"x","path":"`+Z+`["}}`)+","+us("u"), "", ""))

	load("Load a root of another type row", doc(ts(`{"key":["`+Z+`"],"type":0,"properties":{"id":"`+Z+`"},"edges":{`+needsU+`},"provenance":null}`)+","+us("u"), "", ""))
	load("Load two roots at one key", doc(ts(root(Z), root(Z))+","+us("u"), "", ""))
	load("Load a child with no type row", doc(ts(item("t", id("t"), needsU, `"`+Z+`":[`+partInstance("c")+`]`))+","+us("u"), "", ""))
	load("Load an edge with no type row", doc(ts(item("t", id("t"), needsU+`,"`+Z+`":[{"target_key":["u"],"properties":{}}]`, ""))+","+us("u"), "", ""))
	load("Load an empty edge list under an undeclared relation", doc(ts(item(Z, id(Z), needsU+`,"`+Z+`":[]`, ""))+","+us("u"), "", ""))
	load("Load children under an undeclared composition", doc(ts(item("t", id("t"), needsU, `"`+Z+`":[`+child("0", "c")+`]`))+","+us("u"), "", ""))
	load("Load sibling children at one key", doc(ts(item(Z, id(Z), needsU, `"PARTS":[`+child("0", Z)+","+child("0", Z)+`]`))+","+us("u"), "", ""))
	load("Load a child of another type", doc(ts(item(Z, id(Z), needsU, `"PARTS":[{"key":["`+Z+`"],"type":2,"properties":{"id":"`+Z+`"},"provenance":null}]`))+","+us("u"), "", ""))
	load("Load a key that does not read back", doc(ts(`{"key":["`+Z+`",[]],"properties":{"id":"`+Z+`"},"edges":{`+needsU+`},"provenance":null}`)+","+us("u"), "", ""))
	load("Load a key its property disagrees with", doc(ts(item(Z2, id(Z), needsU, ""))+","+us("u"), "", ""))
	load("Load an undeclared property", doc(ts(item(Z, id(Z)+`,"`+Z2+`":1`, needsU, ""))+","+us("u"), "", ""))
	load("Load an undeclared edge property", doc(ts(item(Z, id(Z), needsU+`,"LINKS":[{"target_type":2,"target_key":["u"],"properties":{"w":1,"`+Z2+`":1}}]`, ""))+","+us("u"), "", ""))

	deep := child("0", Z)
	for range instance.MaxComposedDepth + 2 {
		deep = `{"key":["` + Z + `"],"type":0,"properties":{"pid":"` + Z + `"},"composed":{"SUB":[` + deep + `]},"provenance":null}`
	}
	load("Load a composed chain past the depth bound", doc(ts(item("t", id("t"), needsU, `"PARTS":[`+deep+`]`))+","+us("u"), "", ""))

	load("Load a duplicate of another type row", doc(ts(root(Z))+","+us("u"),
		dup("1", Z, `{"key":["`+Z+`"],"type":0,"properties":{"id":"`+Z+`"},"provenance":null}`, conflict("1", Z), ""), ""))
	load("Load a duplicate whose instance has another key", doc(ts(root(Z))+","+us("u"),
		dup("1", Z, item(Z2, id(Z2), "", ""), conflict("1", Z), ""), ""))
	load("Load a duplicate with children", doc(ts(root(Z))+","+us("u"),
		dup("1", Z, item(Z, id(Z), "", `"PARTS":[`+child("0", "c")+`]`), conflict("1", Z), ""), ""))
	load("Load a duplicate with edges", doc(ts(root(Z))+","+us("u"),
		dup("1", Z, item(Z, id(Z), needsU, ""), conflict("1", Z), ""), ""))
	load("Load a duplicate with no conflict", doc(ts(root(Z))+","+us("u"),
		dup("1", Z, item(Z, id(Z), "", ""), "", ""), ""))
	load("Load a duplicate of no root", doc(ts(root(Z))+","+us("u"),
		dup("1", Z, item(Z, id(Z), "", ""), conflict("1", Z2), ""), ""))
	load("Load a duplicate of another root", doc(ts(root(Z), root(Z2))+","+us("u"),
		dup("1", Z, item(Z, id(Z), "", ""), conflict("1", Z2), ""), ""))
	load("Load a duplicate stating a relation", doc(ts(root(Z))+","+us("u"),
		dup("0", "c", partInstance("c"), conflict("0", "c"), `,"relation":"`+Z+`"`), ""))

	load("Load a record of no root", doc(ts(root("t"))+","+us("u"), "", record(Z, "MAYBE", `["gone"]`, "target_missing")))
	load("Load an absent record under an optional association", doc(ts(root(Z))+","+us("u"), "", record(Z, "MAYBE", "null", "absent")))
	load("Load an absent record beside an edge", doc(ts(root(Z))+","+us("u"), "", record(Z, "NEEDS", "null", "absent")))
	load("Load a record naming a held target", doc(ts(root("t"))+","+us("u", Z), "", record("t", "MAYBE", `["`+Z+`"]`, "target_missing")))
	load("Load a root with no required record", doc(ts(item(Z, id(Z), "", ""))+","+us("u"), "", ""))
	load("Load an absent record with a target key", doc(ts(root("t"))+","+us("u"), "", record("t", "MAYBE", `["`+Z+`"]`, "absent")))
	load("Load an unresolved required association", doc(ts(item(Z, id(Z), "", ""))+","+us("u"), "", record(Z, "NEEDS", "null", "absent")),
		snapshot.WithRevalidation(diag.Warning))
	load("Load a target key that does not read back", doc(ts(item(Z, id(Z), `"NEEDS":[{"target_type":2,"target_key":[["`+Z2+`"]],"properties":{}}]`, ""))+","+us("u"), "", ""))
	load("Load a second (one) edge", doc(ts(item(Z, id(Z), `"NEEDS":[{"target_type":2,"target_key":["u"],"properties":{}},{"target_type":2,"target_key":["u2"],"properties":{}}]`, ""))+","+us("u", "u2"), "", ""))
	load("Load an edge to no root", doc(ts(item(Z, id(Z), `"NEEDS":[{"target_type":2,"target_key":["`+Z2+`"],"properties":{}}]`, ""))+","+us("u"), "", ""))
	revalidated := load("Load a value its constraint refuses", doc(ts(item(Z, id(Z)+`,"f":"x"`, needsU, ""))+","+us("u"), "", ""),
		snapshot.WithRevalidation(diag.Warning))
	shows("Load a value its constraint refuses", messages(revalidated), "revalidation of keys#T["+head+"]: ")

	stamped := strings.Replace(string(doc(ts(root("t"))+","+us("u"), "", "")), `{"yammm_snapshot":{`, `{"yammm_snapshot":{"created_at":"`+Z+`",`, 1)
	stamped = strings.Replace(stamped, `,"types":`, ` ,"types":`, 1)
	_, r = snapshot.UpdateMetadataOrReMarshal(ctx, []byte(stamped), map[string]string{"k": "v"}, s)
	result("UpdateMetadataOrReMarshal a long created_at", r)

	for _, where := range paths {
		if c.judged[where] == 0 {
			t.Errorf("%s wrote no message to judge", where)
		}
	}
}
