package yammm_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
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

// The import doors read a snapshot of pairFrom under pairTo. Both load under
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
	typeOf := func(s *schema.Schema, name string) schema.TypeID {
		t.Helper()
		typ, ok := s.Type(name)
		if !ok {
			t.Fatalf("schema %s declares no type %s", s.Name(), name)
		}
		return typ.ID()
	}
	tID, uID, pID, nID, aID := typeOf(s, "T"), typeOf(s, "U"), typeOf(s, "P"), typeOf(s, "N"), typeOf(s, "A")
	undeclared := schema.NewTypeID(tID.SchemaPath(), "Nope")
	c := &echoChecker{t: t, judged: map[string]int{}}

	var doors []string
	result := func(where string, r diag.Result) {
		t.Helper()
		doors = append(doors, where)
		c.result(where, r)
	}
	fail := func(where string, err error) {
		t.Helper()
		doors = append(doors, where)
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

	k := func(v ...any) immutable.Key { return immutable.WrapKey(v) }
	p := func(m map[string]any) immutable.Properties { return immutable.WrapProperties(m) }
	vi := func(name string, id schema.TypeID, key immutable.Key, props map[string]any,
		edges map[string]*instance.ValidEdgeData, composed map[string]immutable.Value,
	) *instance.ValidInstance {
		return instance.NewValidInstance(name, id, key, p(props), edges, composed, nil)
	}
	to := func(keys ...string) *instance.ValidEdgeData {
		targets := make([]instance.ValidEdgeTarget, len(keys))
		for i, key := range keys {
			targets[i] = instance.NewValidEdgeTarget(k(key), p(nil))
		}
		return instance.NewValidEdgeData(targets)
	}
	needs := map[string]*instance.ValidEdgeData{"NEEDS": to("u")}
	part := func(key string) *instance.ValidInstance {
		return vi("P", pID, k(key), map[string]any{"pid": key}, nil, nil)
	}
	parts := func(children ...*instance.ValidInstance) immutable.Value {
		list := make([]any, len(children))
		for i, child := range children {
			list[i] = child
		}
		return immutable.Wrap(list)
	}

	g := graph.New(s)
	if r := g.Add(ctx, vi("U", uID, k("u"), map[string]any{"id": "u"}, nil, nil)); r.HasErrors() {
		t.Fatal(r)
	}
	if r := g.Add(ctx, vi("U", uID, k("u2"), map[string]any{"id": "u2"}, nil, nil)); r.HasErrors() {
		t.Fatal(r)
	}
	result("Add an undeclared composition", g.Add(ctx, vi("T", tID, k("c1"), map[string]any{"id": "c1"}, needs,
		map[string]immutable.Value{longText: parts(part("x"))})))
	result("Add a composed child of a long type name", g.Add(ctx, vi("T", tID, k("c2"), map[string]any{"id": "c2"}, needs,
		map[string]immutable.Value{"PARTS": parts(vi(longText, pID, k("x"), map[string]any{"pid": "y"}, nil, nil))})))
	result("Add sibling children at one key", g.Add(ctx, vi("T", tID, k("c3"), map[string]any{"id": "c3"}, needs,
		map[string]immutable.Value{"PARTS": parts(part(longText), part(longText))})))
	result("Add an undeclared type", g.Add(ctx, vi(longText, undeclared, k("a"), map[string]any{}, nil, nil)))
	result("Add a keyless type", g.Add(ctx, vi(longText, nID, k("a"), map[string]any{"x": "a"}, nil, nil)))
	result("Add a part type", g.Add(ctx, vi(longText, pID, k("a"), map[string]any{"pid": "a"}, nil, nil)))
	result("Add an abstract type", g.Add(ctx, vi(longText, aID, k("a"), map[string]any{"id": "a"}, nil, nil)))
	result("Add a key its property disagrees with", g.Add(ctx, vi(longText, tID, k("a"), map[string]any{"id": "b"}, needs, nil)))
	result("Add a long key its property disagrees with", g.Add(ctx, vi("T", tID, k(longText), map[string]any{"id": longText + "x"}, needs, nil)))
	undeclaredProperty := g.Add(ctx, vi("T", tID, k("c4"), map[string]any{"id": "c4", "nope": 1}, needs, nil))
	result("Add an undeclared property", undeclaredProperty)
	shows("Add an undeclared property", messages(undeclaredProperty), `type "T" declares no property "nope"`)

	if r := g.Add(ctx, vi("T", tID, k("h"), map[string]any{"id": "h"}, needs, nil)); r.HasErrors() {
		t.Fatal(r)
	}
	result("AddComposed an undeclared relation", g.AddComposed(ctx, tID, `["h"]`, longText, part("x")))
	result("AddComposed a child of a long type name", g.AddComposed(ctx, tID, `["h"]`, "PARTS",
		vi(longText, pID, k("x"), map[string]any{"pid": "y"}, nil, nil)))
	if r := g.AddComposed(ctx, tID, `["h"]`, "PARTS", part(longText)); r.HasErrors() {
		t.Fatal(r)
	}
	result("AddComposed a repeated child key", g.AddComposed(ctx, tID, `["h"]`, "PARTS", part(longText)))
	mistyped := g.AddComposed(ctx, tID, `["h"]`, "PARTS", vi("Nope", undeclared, k("x"), map[string]any{"pid": "x"}, nil, nil))
	result("AddComposed a child of an undeclared type", mistyped)
	shows("AddComposed a child of an undeclared type", messages(mistyped), `child type "Nope" does not match relation target "P"`)

	if r := g.Add(ctx, vi("T", tID, k(longText), map[string]any{"id": longText},
		map[string]*instance.ValidEdgeData{"NEEDS": to("u"), "MAYBE": to("u")}, nil)); r.HasErrors() {
		t.Fatal(r)
	}
	_, merged := g.AddOrMerge(ctx, vi("T", tID, k(longText), map[string]any{"id": longText},
		map[string]*instance.ValidEdgeData{"NEEDS": to("u"), "MAYBE": to("u2")}, nil))
	result("AddOrMerge a second (one) target", merged)

	for _, key := range []string{"[" + longDigits + "e99999]", "null " + longText, `[] "` + longText + `"`} {
		_, err := graph.ParseKey(key)
		fail("ParseKey", err)
	}
	_, err := graph.ParseKey(`[1,`)
	fail("ParseKey a cut array", err)
	if err != nil {
		shows("ParseKey a cut array", err.Error(), `graph: parsing key "[1,": `)
	}

	ip := func(id schema.TypeID, key string, props map[string]any) graph.InstanceParts {
		return graph.InstanceParts{TypeID: id, PrimaryKey: k(key), Properties: p(props)}
	}
	tp := func(key string) graph.InstanceParts { return ip(tID, key, map[string]any{"id": key}) }
	u := ip(uID, "u", map[string]any{"id": "u"})
	needsEdge := func(source string) graph.EdgeParts {
		return graph.EdgeParts{Relation: "NEEDS", SourceType: tID, SourceKey: k(source), TargetKey: k("u")}
	}
	types := []schema.TypeID{tID, uID}
	rebuild := func(where string, parts graph.SnapshotParts) diag.Result {
		t.Helper()
		parts.Types = types
		_, r := graph.RebuildSnapshot(s, parts)
		result(where, r)
		return r
	}
	rebuild("RebuildSnapshot an edge from no root", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u},
		Edges:     []graph.EdgeParts{needsEdge(longText)},
	})
	rebuild("RebuildSnapshot an edge to no root", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u, tp("t")},
		Edges: []graph.EdgeParts{needsEdge("t"), {
			Relation: "LINKS", SourceType: tID, SourceKey: k("t"), TargetKey: k(longText),
			Properties: p(map[string]any{"w": 1}),
		}},
	})
	composedDuplicate := tp(longText)
	composedDuplicate.Composed = map[string][]graph.InstanceParts{"PARTS": {ip(pID, "x", map[string]any{"pid": "x"})}}
	rebuild("RebuildSnapshot a duplicate with children", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u, tp(longText)},
		Edges:     []graph.EdgeParts{needsEdge(longText)}, Duplicates: []graph.DuplicateParts{{Instance: composedDuplicate}},
	})
	rebuild("RebuildSnapshot a duplicate of no root", graph.SnapshotParts{
		Instances:  []graph.InstanceParts{u},
		Duplicates: []graph.DuplicateParts{{Instance: tp(longText)}},
	})
	rebuild("RebuildSnapshot a record of no root", graph.SnapshotParts{
		Instances:  []graph.InstanceParts{u},
		Unresolved: []graph.UnresolvedParts{{SourceType: tID, SourceKey: k(longText), Relation: "MAYBE", Reason: "target_missing", TargetKey: k("q")}},
	})
	unresolvable := rebuild("RebuildSnapshot an edge from an undeclared type", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u},
		Edges:     []graph.EdgeParts{{Relation: "NEEDS", SourceType: undeclared, SourceKey: k(longText), TargetKey: k("u")}},
	})
	shows("RebuildSnapshot an edge from an undeclared type", messages(unresolvable), "edge source position, key "+head)
	rebuild("RebuildSnapshot a duplicate under an undeclared relation", graph.SnapshotParts{
		Instances:  []graph.InstanceParts{u, tp("t")},
		Edges:      []graph.EdgeParts{needsEdge("t")},
		Duplicates: []graph.DuplicateParts{{Instance: ip(pID, "x", map[string]any{"pid": "x"}), ParentType: tID, ParentKey: k("t"), Relation: longText}},
	})
	wrong := tp(longText)
	wrong.Composed = map[string][]graph.InstanceParts{"PARTS": {ip(uID, "w", map[string]any{"id": "w"})}}
	rebuild("RebuildSnapshot a child of another type", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u, wrong},
		Edges:     []graph.EdgeParts{needsEdge(longText)},
	})
	siblings := tp(longText)
	siblings.Composed = map[string][]graph.InstanceParts{"PARTS": {ip(pID, longText, map[string]any{"pid": longText}), ip(pID, longText, map[string]any{"pid": longText})}}
	rebuild("RebuildSnapshot sibling children at one key", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u, siblings},
		Edges:     []graph.EdgeParts{needsEdge(longText)},
	})
	rebuild("RebuildSnapshot two roots at one key", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u, tp(longText), tp(longText)},
		Edges:     []graph.EdgeParts{needsEdge(longText)},
	})
	rebuild("RebuildSnapshot a key its property disagrees with", graph.SnapshotParts{Instances: []graph.InstanceParts{u, ip(tID, longText, map[string]any{"id": "other"})}})
	rebuild("RebuildSnapshot an undeclared property", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u, ip(tID, longText, map[string]any{"id": longText, longText + "p": 1})},
		Edges:     []graph.EdgeParts{needsEdge(longText)},
	})
	rebuild("RebuildSnapshot an undeclared edge property", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u, tp("t")},
		Edges:     []graph.EdgeParts{needsEdge("t"), {Relation: "LINKS", SourceType: tID, SourceKey: k("t"), TargetKey: k("u"), Properties: p(map[string]any{longText: 1})}},
	})
	rebuild("RebuildSnapshot an absent record under an optional association", graph.SnapshotParts{
		Instances:  []graph.InstanceParts{u, tp(longText)},
		Edges:      []graph.EdgeParts{needsEdge(longText)},
		Unresolved: []graph.UnresolvedParts{{SourceType: tID, SourceKey: k(longText), Relation: "MAYBE", Reason: "absent"}},
	})
	rebuild("RebuildSnapshot an absent record beside an edge", graph.SnapshotParts{
		Instances:  []graph.InstanceParts{u, tp(longText)},
		Edges:      []graph.EdgeParts{needsEdge(longText)},
		Unresolved: []graph.UnresolvedParts{{SourceType: tID, SourceKey: k(longText), Relation: "NEEDS", Reason: "absent"}},
	})
	rebuild("RebuildSnapshot a record naming a held target", graph.SnapshotParts{
		Instances:  []graph.InstanceParts{u, ip(uID, longText, map[string]any{"id": longText}), tp("t")},
		Edges:      []graph.EdgeParts{needsEdge("t")},
		Unresolved: []graph.UnresolvedParts{{SourceType: tID, SourceKey: k("t"), Relation: "MAYBE", Reason: "target_missing", TargetKey: k(longText)}},
	})
	rebuild("RebuildSnapshot a root with no required record", graph.SnapshotParts{Instances: []graph.InstanceParts{u, tp(longText)}})
	rebuild("RebuildSnapshot a second (one) edge", graph.SnapshotParts{
		Instances: []graph.InstanceParts{u, ip(uID, "u2", map[string]any{"id": "u2"}), tp(longText)},
		Edges:     []graph.EdgeParts{needsEdge(longText), {Relation: "NEEDS", SourceType: tID, SourceKey: k(longText), TargetKey: k("u2")}},
	})

	from, res := schema.LoadString(ctx, pairFrom, "pair.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	into, res := schema.LoadString(ctx, pairTo, "pair.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	fromT, fromL, fromP := typeOf(from, "T"), typeOf(from, longTypeName), typeOf(from, "P")
	old := graph.New(from)
	if r := old.Add(ctx, vi(longTypeName, fromL, k("l"), map[string]any{"id": "l"}, nil, nil)); r.HasErrors() {
		t.Fatal(r)
	}
	if r := old.Add(ctx, vi("T", fromT, k("t"), map[string]any{"id": "t"}, map[string]*instance.ValidEdgeData{"NEEDS": to("l")},
		map[string]immutable.Value{"PARTS": parts(vi("P", fromP, k(longText), map[string]any{"pid": longText}, nil, nil))})); r.HasErrors() {
		t.Fatal(r)
	}
	if r := old.AddComposed(ctx, fromT, `["t"]`, "PARTS", vi("P", fromP, k(longText), map[string]any{"pid": longText}, nil, nil)); !r.HasErrors() {
		t.Fatal("AddComposed accepted a repeated child key")
	}
	_, r := graph.NewFromSnapshot(into, old.Snapshot())
	result("NewFromSnapshot under another schema", r)

	unwritable := graph.New(s)
	unwritable.Add(ctx, vi("T", tID, k(longText), map[string]any{"id": longText, "f": math.NaN()}, needs, nil))
	_, r = snapshot.Marshal(ctx, unwritable.Snapshot())
	result("Marshal an unwritable value", r)
	chain := part(longText)
	for range 34 {
		chain = vi("P", pID, k(longText), map[string]any{"pid": longText}, nil, map[string]immutable.Value{"SUB": parts(chain)})
	}
	deep := graph.New(s)
	deep.Add(ctx, vi("U", uID, k("u"), map[string]any{"id": "u"}, nil, nil))
	deep.Add(ctx, vi("T", tID, k("d"), map[string]any{"id": "d"}, needs, map[string]immutable.Value{"PARTS": parts(chain)}))
	_, r = snapshot.Marshal(ctx, deep.Snapshot())
	result("Marshal a composed chain past the depth bound", r)

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
	under := func(parentKey, relation string) string {
		return `,"parent_type":1,"parent_key":["` + parentKey + `"],"relation":"` + relation + `"`
	}
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
	load("Load a duplicate relation with no parent", doc(ts(root(Z))+","+us("u"),
		dup("0", "c", partInstance("c"), conflict("0", "c"), `,"relation":"`+Z+`"`), ""))
	load("Load a duplicate of no parent", doc(ts(root("t"))+","+us("u"),
		dup("0", "c", partInstance("c"), conflict("0", "c"), under(Z, "PARTS")), ""))
	withChild := func(key string) string { return item(Z, id(Z), needsU, `"PARTS":[`+child("0", key)+`]`) }
	load("Load a duplicate stating no conflict key", doc(ts(withChild("c"))+","+us("u"),
		dup("0", Z, partInstance(Z), `{"type":0,"key":[]}`, under(Z, "PARTS")), ""))
	load("Load a duplicate of no child", doc(ts(withChild("c"))+","+us("u"),
		dup("0", Z2, partInstance(Z2), conflict("0", Z2), under(Z, "PARTS")), ""))
	load("Load a duplicate of another child", doc(ts(withChild(Z))+","+us("u"),
		dup("0", Z2, partInstance(Z2), conflict("0", Z), under(Z, "PARTS")), ""))
	load("Load a duplicate under an undeclared composition", doc(ts(root("t"))+","+us("u"),
		dup("0", "c", partInstance("c"), conflict("0", "c"), under("t", Z)), ""))

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

	for _, where := range doors {
		if c.judged[where] == 0 {
			t.Errorf("%s wrote no message to judge", where)
		}
	}
}
