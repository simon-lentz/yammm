package snapshot_test

import (
	"maps"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const utf8WireSchema = `schema "utf8wire"

type Target {
	key String primary
}

type Holder {
	id String primary
	text String
	tags List<String>
	day Date
	uid UUID
	--> LINK (_:many) Target {
		label String
	}
}
`

// wireText is a named type over string, which the immutable layer holds as it
// arrived.
type wireText string

// utf8WireSnapshot builds Holder h1 through instancetest, which runs no
// validation, holding props beside its key and one LINK to target carrying
// label. Target t1 is in the graph, so a LINK to any other key is unresolved.
func utf8WireSnapshot(t *testing.T, props map[string]any, target string, label any) (*schema.Schema, *graph.Snapshot) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), utf8WireSchema, "utf8wire.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	targetT, _ := s.Type("Target")
	holderT, _ := s.Type("Holder")
	all := map[string]any{"id": "h1"}
	maps.Copy(all, props)
	g := graph.New(s)
	for _, inst := range []*instance.ValidInstance{
		instancetest.VI("Target", instancetest.TypeID(targetT.ID()), instancetest.PK("t1"),
			instancetest.Props(map[string]any{"key": "t1"})),
		instancetest.VI("Holder", instancetest.TypeID(holderT.ID()), instancetest.PK("h1"),
			instancetest.Props(all),
			instancetest.Edges(map[string]*instance.ValidEdgeData{
				"LINK": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
					instance.NewValidEdgeTarget(immutable.WrapKey([]any{target}), immutable.WrapProperties(map[string]any{"label": label})),
				}),
			})),
	} {
		if r := g.Add(t.Context(), inst); r.HasErrors() {
			t.Fatalf("Add: %s", r)
		}
	}
	return s, g.Snapshot()
}

// The wire is JSON, whose encoder writes each byte of an invalid UTF-8
// sequence as U+FFFD: a document holding such a string would read back as
// another value. A validated string is valid UTF-8, so only a bypass-built
// snapshot holds one, and Marshal refuses it by name wherever it stands.
func TestMarshal_RefusesAStringThatIsNotValidUTF8ByName(t *testing.T) {
	t.Parallel()
	const bad = "a\xffb"
	holder := snapshot.TypeRef{Schema: "utf8wire", Name: "Holder"}.String()

	for _, c := range []struct {
		name     string
		props    map[string]any
		target   string
		label    any
		property string
		relation string
	}{
		{"a String property", map[string]any{"text": bad}, "t1", "ok", "text", ""},
		{"a named type over string", map[string]any{"text": wireText(bad)}, "t1", "ok", "text", ""},
		{"a list element", map[string]any{"tags": []any{"ok", bad}}, "t1", "ok", "tags", ""},
		{"a value the constraint cannot render", map[string]any{"day": bad}, "t1", "ok", "day", ""},
		{"a map value", map[string]any{"text": map[string]any{"x": bad}}, "t1", "ok", "text", ""},
		{"a map key", map[string]any{"text": map[string]any{bad: "x"}}, "t1", "ok", "text", ""},
		{"an edge property", nil, "t1", bad, "label", "LINK"},
		{"an unresolved record's property", nil, "t9", bad, "label", "LINK"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, snap := utf8WireSnapshot(t, c.props, c.target, c.label)
			data, res := snapshot.Marshal(t.Context(), snap)
			if data != nil {
				t.Errorf("Marshal wrote %d bytes for a string the wire cannot carry", len(data))
			}
			want := map[string]string{
				diag.DetailKeyTypeName:     holder,
				diag.DetailKeyPrimaryKey:   `["h1"]`,
				diag.DetailKeyPropertyName: c.property,
			}
			if c.relation != "" {
				want[diag.DetailKeyRelationName] = c.relation
			}
			requireUnwritable(t, res, want, `property "`+c.property+`"`, "a string that is not valid UTF-8")
		})
	}
}

// What Marshal writes Load reads back as the bytes the snapshot holds, U+FFFD
// included; a string it cannot write so is refused, never rewritten.
func TestMarshal_AStringReadsBackAsTheBytesHeld(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"a�b", "é✓", "a\xffb", "\xc3"} {
		s, snap := utf8WireSnapshot(t, map[string]any{"text": text, "tags": []any{"ok", text}}, "t1", text)
		data, res := snapshot.Marshal(t.Context(), snap)
		if !utf8.ValidString(text) {
			if data != nil || !res.HasCode(diag.E_SNAPSHOT_MALFORMED) {
				t.Errorf("Marshal(%q) wrote %d bytes, %s; want the refusal", text, len(data), res)
			}
			continue
		}
		if res.HasErrors() {
			t.Fatalf("Marshal(%q) refused valid text: %s", text, res)
		}
		loaded, lres := snapshot.Load(t.Context(), data, s)
		if loaded == nil {
			t.Fatalf("Load(%q): %s", text, lres)
		}
		holderT, _ := s.Type("Holder")
		inst, ok := loaded.InstanceByKey(holderT.ID(), `["h1"]`)
		if !ok {
			t.Fatalf("Load(%q): no Holder h1", text)
		}
		if got, _ := inst.Property("text"); got.Unwrap() != text {
			t.Errorf("text read back as %q, want %q", got.Unwrap(), text)
		}
		tags, _ := inst.Property("tags")
		if elems, ok := tags.Slice(); !ok || elems.Len() != 2 || elems.Get(1).Unwrap() != text {
			t.Errorf("tags read back as %v, want its second element %q", tags.Unwrap(), text)
		}
		edges := loaded.EdgesFrom(inst)
		if len(edges) != 1 {
			t.Fatalf("Load(%q): %d edges", text, len(edges))
		}
		if got, _ := edges[0].Properties().Get("label"); got.Unwrap() != text {
			t.Errorf("label read back as %q, want %q", got.Unwrap(), text)
		}
	}
}
