package yammm_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const (
	declaredSchemaName = "fleet_vehicles_with_a_name_past_the_cut"
	declaredTypeName   = "VehicleWithANameThatRunsPastTheCut"
	declaredPartName   = "SpareWheelWithANameThatRunsPastTheCut"
	declaredRoot       = "embedded://fleet/Users/someone/projects/fleet-data/schemas"
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
	s, res := schema.LoadSourcesWithEntry(ctx, map[string][]byte{"vehicles.yammm": []byte(declaredSchema)},
		"vehicles.yammm", "", schema.WithSourcesOnly(true), schema.WithSyntheticRoot(declaredRoot))
	if res.HasErrors() {
		t.Fatal(res)
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
			if inst.IsZero() {
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
	_, r := snapshot.Load(ctx, edit(duplicated, `,"conflict":{"type":0,"key":["v1"]}`, ""), s)
	wantWhole("snapshot.Load", r, "("+ref+"[")
	_, r = snapshot.Info(ctx, edit(data, `"v2"`, `"v1"`))
	wantWhole("snapshot.Info", r, `#"`+declaredTypeName[:32]+`"…`)
}
