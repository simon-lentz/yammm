package yammm_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const (
	declaredSchemaName = "fleet_vehicles_with_a_name_past_the_cut"
	declaredTypeName   = "VehicleWithANameThatRunsPastTheCut"
	declaredPartName   = "SpareWheelWithANameThatRunsPastTheCut"
	declaredPath       = "/Users/someone/projects/fleet-data/schemas/"
)

const declaredSchema = `schema "` + declaredSchemaName + `"

type ` + declaredTypeName + ` {
	vin String primary
	year Integer
	--> SERVICED_BY (many) Garage
	*-> SPARE (many) Wheel
}

type Garage {
	gid String primary
}

part type Wheel {
	position String primary
}

part type ` + declaredPartName + ` {
	position String primary
}
`

// A message names a type the schema declares by its whole identity or tag,
// however long its schema path and name run, and a schema path or name the
// import closure holds whole: only what the data or the caller writes is cut,
// and a cut identity can read as the type it is compared with. An identity no
// schema resolves is cut by its parts, so its type name survives the cut.
func TestMessages_NameADeclaredIdentityWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, declaredSchema, declaredPath+"vehicles.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	other, res := schema.LoadString(ctx, "schema \"elsewhere\"\n\npart type Wheel {\n\tposition String primary\n}\n", declaredPath+"elsewhere.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	vehicle, _ := s.Type(declaredTypeName)
	wheel, _ := s.Type("Wheel")
	longPart, _ := s.Type(declaredPartName)
	strangerType, _ := other.Type("Wheel")
	part := func(typeName string, id schema.TypeID) *instance.ValidInstance {
		return instance.NewValidInstance(typeName, id, immutable.WrapKey([]any{"left"}),
			immutable.WrapProperties(map[string]any{"position": "left"}), nil, nil, nil)
	}
	root := func(vin string, composed map[string]immutable.Value) *instance.ValidInstance {
		return instance.NewValidInstance(declaredTypeName, vehicle.ID(), immutable.WrapKey([]any{vin}),
			immutable.WrapProperties(map[string]any{"vin": vin}), nil, composed, nil)
	}
	messages := func(r diag.Result) string {
		var b strings.Builder
		for issue := range r.Issues() {
			b.WriteString(issue.Message() + "\n")
		}
		return b.String()
	}
	wantWhole := func(where string, r diag.Result, want string) {
		t.Helper()
		if !strings.Contains(messages(r), want) {
			t.Errorf("%s: no message names %q whole: %s", where, want, r)
		}
	}

	g := graph.New(s)
	r := g.Add(ctx, root("v1", map[string]immutable.Value{"SPARE": immutable.Wrap([]any{part("Wheel", strangerType.ID())})}))
	wantWhole("graph.Add", r, wheel.ID().String())
	if strings.Contains(messages(r), strangerType.ID().String()) {
		t.Errorf("graph.Add: a message names the caller's identity %q whole: %s", strangerType.ID(), r)
	}
	wantWhole("graph.Add", g.Add(ctx, root("v2", map[string]immutable.Value{"SPARE": immutable.Wrap([]any{part(declaredPartName, longPart.ID())})})),
		`"`+declaredPartName+`"`)
	path := vehicle.ID().SchemaPath().String()
	wantWhole("graph.AddComposed", g.AddComposed(ctx, schema.NewTypeID(vehicle.ID().SchemaPath(), "Undeclared"), `["v1"]`, "SPARE", part("Wheel", wheel.ID())),
		path+":Undeclared")

	marshal := func(raws ...map[string]any) []byte {
		t.Helper()
		held := graph.New(s)
		v := instance.NewValidator(s)
		for _, props := range raws {
			typeName := declaredTypeName
			if _, ok := props["gid"]; ok {
				typeName = "Garage"
			}
			inst, r := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
			if inst == nil {
				t.Fatal(r)
			}
			held.Add(ctx, inst)
		}
		data, mr := snapshot.Marshal(ctx, held.Snapshot())
		if data == nil {
			t.Fatal(mr)
		}
		return data
	}
	edit := func(data []byte, old, replacement string) []byte {
		t.Helper()
		if !bytes.Contains(data, []byte(old)) {
			t.Fatalf("the document holds no %s: %s", old, data)
		}
		return bytes.ReplaceAll(data, []byte(old), []byte(replacement))
	}
	data := marshal(
		map[string]any{"gid": "g1"},
		map[string]any{"vin": "v1", "year": 2020, "serviced_by": []any{map[string]any{"_target_gid": "g1"}}},
		map[string]any{"vin": "v2"},
	)
	ref := declaredSchemaName + "#" + declaredTypeName
	for _, c := range []struct {
		old, replacement, want string
		opts                   []snapshot.LoadOption
	}{
		{old: `"SERVICED_BY"`, replacement: `"SERVICED_AT"`, want: "of " + ref + "["},
		{old: `"v2"`, replacement: `"v1"`, want: `"` + ref + `"`},
		{old: `"name":"Garage"`, replacement: `"name":"` + declaredTypeName + `"`, want: "carry one identity " + ref},
		{old: `"target_type":0`, replacement: `"target_type":1`, want: "targets " + ref},
		{old: `"year":2020`, replacement: `"year":"x"`, want: "revalidation of " + ref + "[", opts: []snapshot.LoadOption{snapshot.WithRevalidation(diag.Error)}},
		{old: `"name":"Garage"`, replacement: `"name":"MissingTypeWithANameRunningPastTheCut"`, want: "names " + declaredSchemaName + "#MissingType"},
	} {
		_, r := snapshot.Load(ctx, edit(data, c.old, c.replacement), s, c.opts...)
		wantWhole("snapshot.Load", r, c.want)
	}
	duplicated := marshal(map[string]any{"vin": "v1"}, map[string]any{"vin": "v1", "year": 2020})
	_, r = snapshot.Load(ctx, edit(duplicated, `,"conflict":{"type":0,"key":["v1"]}`, ""), s)
	wantWhole("snapshot.Load", r, "("+ref+"[")
	_, r = snapshot.Info(ctx, edit(data, `"v2"`, `"v1"`))
	wantWhole("snapshot.Info", r, `#"`+declaredTypeName[:32]+`"…`)
}
