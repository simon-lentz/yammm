package yammm_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
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

	held := graph.New(s)
	held.Add(ctx, instancetest.Valid(t, s, "U", map[string]any{"id": "u"}))
	inst, vr := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{
		"id": "a", "links": []any{map[string]any{"_target_id": "u", "w": 1}},
	}})
	if inst.IsZero() {
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

	_, err = neo4j.CoerceParams(map[string]any{longText: "x"}, neo4j.ParamTypes{longText: schema.NewFloatConstraint()})
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
		"validator", "instance.SchemaBuilder",
		"neo4j.CoerceParams", "snapshot.Marshal", "snapshot.Load", "snapshot.Info",
	} {
		if c.judged[where] == 0 {
			t.Errorf("%s wrote no message to judge", where)
		}
	}
}
