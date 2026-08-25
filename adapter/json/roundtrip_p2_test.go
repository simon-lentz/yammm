package json

import (
	"bytes"
	"maps"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// The P2 corpus: write → parse → validate is the identity for a fully
// resolved graph. Unresolved edges are deliberately not written; the last
// test pins that drop.
const p2Schema = `schema "p2"

type Target {
	on Date primary
}

type Station {
	code String primary
}

part type Part {
	pid String primary
}

type Item {
	id String primary
	name String
	tags List<String>
	nums List<Float>
	nested List<List<Integer>>
	--> REF (_) Target {
		since Timestamp["2006-01-02 15:04:05"]
	}
	--> MULTI (_:many) Station {
		w Integer
	}
	*-> PARTS (_:many) Part
	*-> LID (_) Part
}
`

func p2Source(t *testing.T) (*schema.Schema, location.SourceID) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), p2Schema, "p2.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	return s, location.NewSourceID("p2.json")
}

// p2Raws is the corpus: separator-bearing strings, quotes, CR/LF, unicode,
// empty strings, numeric FK components, nested lists, a formatted-Timestamp
// edge property, a zipped (many) association, a (one) composition, and an
// absent optional edge.
func p2Raws() map[string][]map[string]any {
	item := func(id, name string, extra map[string]any) map[string]any {
		m := map[string]any{"id": id, "name": name}
		maps.Copy(m, extra)
		return m
	}
	return map[string][]map[string]any{
		"Target":  {{"on": "2026-08-21"}, {"on": "2026-08-22"}},
		"Station": {{"code": "sA"}, {"code": "s|B"}},
		"Item": {
			item("i1", "pipe|and|more", map[string]any{
				"tags": []any{"a|b", "c"},
				"nums": []any{1.5, -0.25},
				"ref":  map[string]any{"_target_on": "2026-08-21", "since": "2026-08-21 09:30:00"},
				"multi": []any{
					map[string]any{"_target_code": "sA", "w": int64(7)},
					map[string]any{"_target_code": "s|B"},
				},
				"parts": []any{map[string]any{"pid": "p1"}, map[string]any{"pid": "p2"}},
				"lid":   []any{map[string]any{"pid": "L1"}},
			}),
			item("i2", "quotes \"q\" and, commas", map[string]any{
				"nested": []any{[]any{int64(1), int64(2)}, []any{int64(3)}},
			}),
			item("i3", "line\nbreak and \r\n crlf", nil),
			item("i4", "unicode — ünïcødé ✓", map[string]any{
				"ref": map[string]any{"_target_on": "2026-08-22"},
			}),
			item("i5", "", nil), // the empty string is a value, not null
		},
	}
}

func buildP2Snapshot(t *testing.T, s *schema.Schema, raws map[string][]map[string]any) *graph.Snapshot {
	t.Helper()
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, typeName := range []string{"Target", "Station", "Item"} {
		for _, props := range raws[typeName] {
			vi, res := v.ValidateOne(t.Context(), typeName, instance.RawInstance{Properties: props})
			if res.HasErrors() {
				t.Fatalf("validate %s %v: %s", typeName, props["id"], res.String())
			}
			if r := g.Add(t.Context(), vi); !r.OK() {
				t.Fatalf("add %s: %s", typeName, r.String())
			}
		}
	}
	return g.Snapshot()
}

func TestRoundTripP2_JSONWriteParseValidateIsTheIdentity(t *testing.T) {
	t.Parallel()
	s, source := p2Source(t)
	snap := buildP2Snapshot(t, s, p2Raws())

	a := New()
	doc1, err := a.MarshalObject(t.Context(), snap)
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}

	// The identity alone cannot see a value dropped on BOTH sides, so the
	// load-bearing payloads are pinned in the bytes first.
	for _, want := range []string{
		`"since":"2026-08-21 09:30:00"`,
		`"w":7`,
		`"_target_on":"2026-08-21"`,
		`"_target_code":"s|B"`,
	} {
		if !bytes.Contains(doc1, []byte(want)) {
			t.Fatalf("output missing %s:\n%s", want, doc1)
		}
	}

	parsed, res := a.ParseObject(t.Context(), source, doc1)
	if res.HasErrors() {
		t.Fatalf("ParseObject rejected the writer's own output: %s\n%s", res.String(), doc1)
	}

	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, typeName := range []string{"Target", "Station", "Item"} {
		for i, raw := range parsed[typeName] {
			vi, vres := v.ValidateOne(t.Context(), typeName, raw)
			if vres.HasErrors() {
				t.Fatalf("validator rejected re-parsed %s[%d]: %s\n%s", typeName, i, vres.String(), doc1)
			}
			if r := g.Add(t.Context(), vi); !r.OK() {
				t.Fatalf("re-add %s[%d]: %s", typeName, i, r.String())
			}
		}
	}
	if cres := g.Check(t.Context()); !cres.OK() {
		t.Fatalf("re-built graph incomplete: %s", cres.String())
	}

	doc2, err := a.MarshalObject(t.Context(), g.Snapshot())
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(doc1, doc2) {
		t.Fatalf("round trip is not the identity\nfirst:  %s\nsecond: %s", doc1, doc2)
	}
}

// TestMarshalObject_DropsUnresolvedEdgesDeliberately pins the scope of the
// identity claim: unresolved edges are not written — the .ys format is
// where they survive — so the claim holds for fully resolved graphs.
func TestMarshalObject_DropsUnresolvedEdgesDeliberately(t *testing.T) {
	t.Parallel()
	s, _ := p2Source(t)
	v := instance.NewValidator(s)
	g := graph.New(s)

	vi, res := v.ValidateOne(t.Context(), "Item", instance.RawInstance{Properties: map[string]any{
		"id":   "i9",
		"name": "dangling",
		"ref":  map[string]any{"_target_on": "2031-01-01"}, // no such Target
	}})
	if res.HasErrors() {
		t.Fatalf("validate: %s", res.String())
	}
	if r := g.Add(t.Context(), vi); !r.OK() {
		t.Fatalf("add: %s", r.String())
	}

	snap := g.Snapshot()
	if len(snap.Unresolved()) != 1 {
		t.Fatalf("fixture holds %d unresolved edges, want 1", len(snap.Unresolved()))
	}
	doc, err := New().MarshalObject(t.Context(), snap)
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}
	if bytes.Contains(doc, []byte(`"ref"`)) || bytes.Contains(doc, []byte("_target_on")) {
		t.Fatalf("an unresolved edge leaked into the output: %s", doc)
	}
}
