package csv

import (
	"bytes"
	"context"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// The CSV P2 corpus: write → parse → validate is the identity for a fully
// resolved graph. CSV has no nested lists and no compositions — both are
// documented limitations — so its schema declares neither.
const p2CSVSchema = `schema "p2csv"

type Target {
	on Date primary
}

type Station {
	code String primary
}

type Item {
	id String primary
	name String
	tags List<String>
	nums List<Float>
	--> REF (_) Target {
		since Timestamp["2006-01-02 15:04:05"]
	}
	--> MULTI (_:many) Station {
		w Integer
	}
}
`

func TestRoundTripP2_CSVWriteParseValidateIsTheIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(t.Context(), p2CSVSchema, "p2csv.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}

	raws := map[string][]map[string]any{
		"Target":  {{"on": "2026-08-21"}, {"on": "2026-08-22"}},
		"Station": {{"code": "sA"}, {"code": "s|B"}},
		"Item": {
			{
				"id": "i1", "name": "pipe|and|more",
				"tags": []any{"a|b", "c", `back\slash`},
				"nums": []any{1.5, -0.25},
				"ref":  map[string]any{"_target_on": "2026-08-21", "since": "2026-08-21 09:30:00"},
				"multi": []any{
					map[string]any{"_target_code": "sA", "w": int64(7)},
					map[string]any{"_target_code": "s|B"},
				},
			},
			{"id": "i2", "name": "quotes \"q\" and, commas"},
			{"id": "i3", "name": "unicode — ünïcødé ✓"},
		},
	}

	build := func(source map[string][]map[string]any) *graph.Snapshot {
		t.Helper()
		v := instance.NewValidator(s)
		g := graph.New(s)
		for _, typeName := range []string{"Target", "Station", "Item"} {
			for _, props := range source[typeName] {
				vi, vres := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
				if vres.HasErrors() {
					t.Fatalf("validate %s %v: %s", typeName, props["id"], vres.String())
				}
				if r := g.Add(ctx, vi); !r.OK() {
					t.Fatalf("add %s: %s", typeName, r.String())
				}
			}
		}
		return g.Snapshot()
	}

	// WithSchema gives the parser the closure it needs to coerce and
	// verify the Date FK components of REF.
	a := New(WithSchema(s))
	files1, err := a.MarshalSnapshot(ctx, build(raws))
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}

	// Parse every type back and re-validate.
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, typeName := range []string{"Target", "Station", "Item"} {
		typ, _ := s.Type(typeName)
		parsed, pres := a.ParseTyped(ctx, location.NewSourceID(typeName+".csv"), typeName, bytes.NewReader(files1[typeName]), typ)
		if pres.HasErrors() {
			t.Fatalf("ParseTyped rejected the writer's own %s output: %s\n%s", typeName, pres.String(), files1[typeName])
		}
		for i, raw := range parsed {
			// The writer emits every column; an empty property cell parses
			// as nil, which the validator drops for optional properties.
			vi, vres := v.ValidateOne(ctx, typeName, raw)
			if vres.HasErrors() {
				t.Fatalf("validator rejected re-parsed %s[%d]: %s\n%s", typeName, i, vres.String(), files1[typeName])
			}
			if r := g.Add(ctx, vi); !r.OK() {
				t.Fatalf("re-add %s[%d]: %s", typeName, i, r.String())
			}
		}
	}
	if cres := g.Check(ctx); !cres.OK() {
		t.Fatalf("re-built graph incomplete: %s", cres.String())
	}

	files2, err := a.MarshalSnapshot(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	for typeName, want := range files1 {
		if !bytes.Equal(want, files2[typeName]) {
			t.Fatalf("%s round trip is not the identity\nfirst:\n%s\nsecond:\n%s", typeName, want, files2[typeName])
		}
	}
}

// TestCSV_AbsentEdgeGroupMeansAbsent pins the absent-group rule: a row
// without an edge leaves every group column empty, and the parse side
// produces no edge field at all — never an explicit null, which the
// validator rejects.
func TestCSV_AbsentEdgeGroupMeansAbsent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(t.Context(), p2CSVSchema, "p2csv.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	typ, _ := s.Type("Item")

	in := "id,name,nums,tags,multi._target_code,multi.w,ref._target_on,ref.since\n" +
		"i1,solo,,,,,,\n"
	a := New(WithSchema(s))
	parsed, pres := a.ParseTyped(ctx, location.NewSourceID("item.csv"), "Item", bytes.NewReader([]byte(in)), typ)
	if pres.HasErrors() {
		t.Fatalf("ParseTyped: %s", pres.String())
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed %d rows, want 1", len(parsed))
	}
	props := parsed[0].Properties
	if _, has := props["ref"]; has {
		t.Fatalf("an all-empty group produced an edge field: %v", props)
	}
	if _, has := props["multi"]; has {
		t.Fatalf("an all-empty group produced an edge field: %v", props)
	}
	if _, vres := instance.NewValidator(s).ValidateOne(ctx, "Item", parsed[0]); vres.HasErrors() {
		t.Fatalf("validator rejected the absent-edge row: %s", vres.String())
	}
}
