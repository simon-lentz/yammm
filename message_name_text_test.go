package yammm_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	csvadapter "github.com/simon-lentz/yammm/adapter/csv"
	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const nameSchema = `schema "names"

type T {
	id String primary
	--> LINKS (many) U {
		w Integer
	}
	--> NEEDS (one) U
	*-> PARTS (many) P
}

type U {
	id String primary
}

part type P {
	pid String primary
}
`

// Every message the data paths write about a name the data writes repeats its
// head alone, as a message about a value does: a member, a header, a type tag
// or a document key can run as long as a value can.
func TestMessages_RepeatANameCutToItsHead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, nameSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	typ, _ := s.Type("T")
	c := &echoChecker{t: t, judged: map[string]int{}}

	v := instance.NewValidator(s)
	for _, props := range []map[string]any{
		{"id": "a", longText: "x"},
		{"id": "a", "links": []any{map[string]any{"_target_id": "u", longText: 1}}},
	} {
		_, r := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: props})
		if r.OK() {
			t.Errorf("validator: %v was accepted", props)
		}
		c.result("validator", r)
	}
	_, r := v.ValidateOne(ctx, longText, instance.RawInstance{Properties: map[string]any{"id": "a"}})
	c.result("validator", r)

	g := graph.New(s)
	bypass := func(key string, props map[string]any, edges map[string]*instance.ValidEdgeData) *instance.ValidInstance {
		return instance.NewValidInstance("T", typ.ID(), immutable.WrapKey([]any{key}), immutable.WrapProperties(props), edges, nil, nil)
	}
	c.result("graph.Add", g.Add(ctx, bypass("h1", map[string]any{"id": "h1", longText: "x"}, nil)))
	c.result("graph.Add", g.Add(ctx, bypass("h2", map[string]any{"id": "h2"},
		map[string]*instance.ValidEdgeData{longText: instance.NewValidEdgeData(nil)})))
	c.result("graph.Add", g.Add(ctx, bypass("h3", map[string]any{"id": "h3"},
		map[string]*instance.ValidEdgeData{"LINKS": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{"u"}), immutable.WrapProperties(map[string]any{longText: 1})),
		})})))
	// The parent is held, so AddComposed reaches the relation and refuses it.
	if r := g.Add(ctx, bypass("h4", map[string]any{"id": "h4"}, nil)); r.HasErrors() {
		t.Fatalf("graph.Add h4: %s", r)
	}
	c.result("graph.AddComposed", g.AddComposed(ctx, typ.ID(), `["h4"]`, longText,
		instance.NewValidInstance("P", typ.ID(), immutable.WrapKey([]any{"c"}), immutable.WrapProperties(map[string]any{"pid": "c"}), nil, nil, nil)))

	_, r = graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{typ.ID()},
		Instances: []graph.InstanceParts{{
			TypeID: typ.ID(), PrimaryKey: immutable.WrapKey([]any{"r"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "r"}),
			Composed:   map[string][]graph.InstanceParts{longText: nil},
		}},
		Edges: []graph.EdgeParts{{Relation: longText, SourceType: typ.ID(), SourceKey: immutable.WrapKey([]any{"r"}), TargetKey: immutable.WrapKey([]any{"u"})}},
		Unresolved: []graph.UnresolvedParts{
			{SourceType: typ.ID(), SourceKey: immutable.WrapKey([]any{"r"}), Relation: "NEEDS", Reason: longText},
			{SourceType: typ.ID(), SourceKey: immutable.WrapKey([]any{"r"}), Relation: longText, Reason: "absent"},
		},
	})
	c.result("graph.RebuildSnapshot", r)

	source := location.MustNewSourceID("test://names.json")
	for _, doc := range []string{
		`{"T": [{"id": "a", "` + longText + `": 1, "` + longText + `": 2}]}`,
		`{"T": [], "` + longText + `": [], "` + longText + `": []}`,
		`{"` + longText + `!": []}`,
	} {
		_, r := jsonadapter.New().ParseObject(ctx, source, []byte(doc))
		c.result("json", r)
	}

	csvSource := location.MustNewSourceID("test://names.csv")
	_, r = csvadapter.New().ParseTyped(ctx, csvSource, "T", strings.NewReader("id,"+longText+","+longText+"\na,1,2\n"), typ)
	c.result("csv", r)
	_, r = csvadapter.New(csvadapter.WithTypeColumn("type")).ParseWithTypeColumn(ctx, csvSource,
		strings.NewReader("type,id\n"+longText+"!,a\n"), func(string) *schema.Type { return nil })
	c.result("csv", r)
	_, r = csvadapter.New(csvadapter.WithTypeColumn(longText)).ParseWithTypeColumn(ctx, csvSource,
		strings.NewReader("type,id\nT,a\n"), func(string) *schema.Type { return typ })
	c.result("csv", r)
	_, r = csvadapter.New(csvadapter.WithTypeColumn(longText)).ParseWithTypeColumn(ctx, csvSource,
		strings.NewReader(longText+",id\n!,a\n"), func(string) *schema.Type { return typ })
	c.result("csv", r)
	_, r = csvadapter.New(csvadapter.WithTypeColumn(longText+"\r\n")).ParseWithTypeColumn(ctx, csvSource,
		strings.NewReader("type,id\nT,a\n"), func(string) *schema.Type { return typ })
	c.result("csv", r)
	_, r = csvadapter.New().ParseTyped(ctx, csvSource, "T", strings.NewReader("id,"+longText+","+longText+".x\na,1,2\n"), typ)
	c.result("csv", r)

	held := graph.New(s)
	u, _ := s.Type("U")
	held.Add(ctx, instance.NewValidInstance("U", u.ID(), immutable.WrapKey([]any{"u"}), immutable.WrapProperties(map[string]any{"id": "u"}), nil, nil, nil))
	inst, vr := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{
		"id": "a", "links": []any{map[string]any{"_target_id": "u", "w": 1}},
	}})
	if inst == nil {
		t.Fatal(vr)
	}
	held.Add(ctx, inst)
	data, mr := snapshot.Marshal(ctx, held.Snapshot())
	if data == nil {
		t.Fatal(mr)
	}
	long := []byte(`"` + longText + `"`)
	for _, doc := range [][]byte{
		bytes.Replace(data, []byte(`"LINKS"`), long, 1),
		bytes.Replace(data, []byte(`"absent"`), long, 1),
		bytes.Replace(data, []byte(`"types"`), long, 1),
		append(bytes.TrimSuffix(bytes.TrimSpace(data), []byte("}")), []byte(`, `+string(long)+`: 1}`)...),
	} {
		_, r := snapshot.Load(ctx, doc, s)
		if r.OK() {
			t.Errorf("snapshot.Load accepted %.200s", doc)
		}
		c.result("snapshot.Load", r)
	}

	z := strings.Repeat("z", 40)
	fs, res := schema.LoadString(ctx, "schema \"folds\"\n\ntype F {\n\tid String primary\n\t"+z+" Integer\n\t--> "+strings.ToUpper(z)+
		"_R (many) G {\n\t\t"+z+" Integer\n\t}\n}\n\ntype G {\n\t"+z+" String primary\n}\n", "folds.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	upper, mixed := strings.ToUpper(z), strings.ToUpper(z[1:])+"z"
	fv := instance.NewValidator(fs)
	for _, props := range []map[string]any{
		{"id": "a", upper: 1, mixed: 2},
		{"id": "a", upper + "_R": []any{}, mixed + "_r": []any{}},
		{"id": "a", z + "_r": []any{map[string]any{"_target_" + upper: "g", "_target_" + mixed: "g"}}},
		{"id": "a", z + "_r": []any{map[string]any{"_target_" + z: "g", upper: 1, mixed: 2}}},
	} {
		_, r := fv.ValidateOne(ctx, "F", instance.RawInstance{Properties: props})
		if r.OK() {
			t.Errorf("validator: %v was accepted", props)
		}
		c.result("validator", r)
	}
	ft, _ := fs.Type("F")
	for _, doc := range []string{
		"id," + upper + "\na,x\n",
		"id," + upper + "_R._target_" + upper + "," + upper + "_R." + upper + "," + upper + "_R." + upper + "X\n" +
			"a,g1|g2,1|2|3,\nb,g1|g2,1|2,1|2|3\nc,g1,x,\n",
	} {
		_, r := csvadapter.New(csvadapter.WithSchema(fs)).ParseTyped(ctx, csvSource, "F", strings.NewReader(doc), ft)
		if r.OK() {
			t.Errorf("csv: %.200s was accepted", doc)
		}
		c.result("csv", r)
	}

	_, r = v.ValidateForComposition(ctx, "T", longText, []instance.RawInstance{{Properties: map[string]any{"pid": "c"}}})
	c.result("validator", r)
	_, err := instance.BuilderFor(s, longText)
	c.err("instance.SchemaBuilder", err)
	b, err := instance.BuilderFor(s, "T")
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.Property("id", "a").Property(longText, 1).Build()
	c.err("instance.SchemaBuilder", err)
	b, _ = instance.BuilderFor(s, "T")
	_, err = b.Property("id", "a").EdgeTo(longText, "u").Build()
	c.err("instance.SchemaBuilder", err)

	undeclared := schema.NewTypeID(typ.ID().SchemaPath(), longText)
	c.result("graph.AddComposed", g.AddComposed(ctx, undeclared, `["h4"]`, "PARTS",
		instance.NewValidInstance("P", typ.ID(), immutable.WrapKey([]any{"c"}), immutable.WrapProperties(map[string]any{"pid": "c"}), nil, nil, nil)))
	c.result("graph.Add", g.Add(ctx, bypass("h5", map[string]any{"id": "h5"}, nil)))
	c.result("graph.AddComposed", g.AddComposed(ctx, typ.ID(), `["h5"]`, "PARTS",
		instance.NewValidInstance("P", undeclared, immutable.WrapKey([]any{"c"}), immutable.WrapProperties(map[string]any{"pid": "c"}), nil, nil, nil)))
	_, r = graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{typ.ID()},
		Instances: []graph.InstanceParts{{
			TypeID: undeclared, PrimaryKey: immutable.WrapKey([]any{"r"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "r"}),
		}},
		Duplicates: []graph.DuplicateParts{{
			Instance: graph.InstanceParts{
				TypeID: typ.ID(), PrimaryKey: immutable.WrapKey([]any{"r"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "r"}),
			},
			ParentType: undeclared, ParentKey: immutable.WrapKey([]any{longText}),
		}},
	})
	c.result("graph.RebuildSnapshot", r)

	_, r = csvadapter.New(csvadapter.WithListSeparator(`\`+longText)).ParseTyped(ctx, csvSource, "T", strings.NewReader("id\na\n"), typ)
	c.result("csv", r)
	_, r = csvadapter.New(csvadapter.WithListSeparator(longText+"\r\n")).ParseTyped(ctx, csvSource, "T", strings.NewReader("id\na\n"), typ)
	c.result("csv", r)

	_, err = neo4j.CoerceParams(map[string]any{longText: "x"}, neo4j.ParamTypes{longText: schema.NewFloatConstraint()})
	c.err("neo4j.CoerceParams", err)
	_, err = neo4j.CoerceParams(map[string]any{"row": map[string]any{longText: "x"}}, neo4j.ParamTypes{"row." + longText: schema.NewFloatConstraint()})
	c.err("neo4j.CoerceParams", err)
	_, err = neo4j.CoerceParams(nil, neo4j.ParamTypes{longText: schema.NewListConstraint(nil)})
	c.err("neo4j.CoerceParams", err)

	_, mr = snapshot.Marshal(ctx, held.Snapshot(), snapshot.WithMetadata(map[string]string{longText + "\xff": "v"}))
	c.result("snapshot.Marshal", mr)
	_, mr = snapshot.Marshal(ctx, held.Snapshot(), snapshot.WithMetadata(map[string]string{longText: "\xff"}))
	c.result("snapshot.Marshal", mr)
	unknown := bytes.Replace(data, []byte(`"U"`), long, 1)
	_, r = snapshot.Load(ctx, unknown, s)
	c.result("snapshot.Load", r)
	_, r = snapshot.Info(ctx, bytes.Replace(unknown, []byte(`"T"`), long, 1))
	c.result("snapshot.Info", r)

	for _, where := range []string{
		"validator", "instance.SchemaBuilder", "graph.Add", "graph.AddComposed", "graph.RebuildSnapshot", "json", "csv",
		"neo4j.CoerceParams", "snapshot.Marshal", "snapshot.Load", "snapshot.Info",
	} {
		if c.judged[where] == 0 {
			t.Errorf("%s wrote no message to judge", where)
		}
	}
}
