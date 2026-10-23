package json

import (
	"context"
	"errors"
	"io"
	"maps"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const utf8JSONSchema = `schema "utf8json"

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

// jsonText is a named type over string, which the immutable layer holds as it
// arrived.
type jsonText string

// utf8JSONSnapshot builds Holder h1 through instance.NewValidInstance, which
// runs no validation, holding props beside its key and one LINK to Target t1
// carrying label.
func utf8JSONSnapshot(t *testing.T, props map[string]any, label any) *graph.Snapshot {
	t.Helper()
	s, res := schema.LoadString(t.Context(), utf8JSONSchema, "utf8json.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	targetT, _ := s.Type("Target")
	holderT, _ := s.Type("Holder")
	all := map[string]any{"id": "h1"}
	maps.Copy(all, props)
	g := graph.New(s)
	for _, inst := range []*instance.ValidInstance{
		instance.NewValidInstance("Target", targetT.ID(), immutable.WrapKey([]any{"t1"}),
			immutable.WrapProperties(map[string]any{"key": "t1"}), nil, nil, nil),
		instance.NewValidInstance("Holder", holderT.ID(), immutable.WrapKey([]any{"h1"}),
			immutable.WrapProperties(all),
			map[string]*instance.ValidEdgeData{"LINK": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{"t1"}), immutable.WrapProperties(map[string]any{"label": label})),
			})}, nil, nil),
	} {
		if r := g.Add(context.Background(), inst); r.HasErrors() {
			t.Fatalf("graph.Add: %s", r)
		}
	}
	return g.Snapshot()
}

// encoding/json writes each byte of an invalid UTF-8 sequence as U+FFFD, so a
// document holding such a string would read back as another value. A validated
// string is valid UTF-8, so only a bypass-built snapshot holds one. It is
// refused as the class wherever it stands, naming the site.
func TestWriters_AStringThatIsNotValidUTF8IsRefusedAsTheClass(t *testing.T) {
	t.Parallel()
	const bad = "a\xffb"
	for _, c := range []struct {
		name, mentions string
		props          map[string]any
		label          any
	}{
		{"a String property", `property "text"`, map[string]any{"text": bad}, "ok"},
		{"a named type over string", `property "text"`, map[string]any{"text": jsonText(bad)}, "ok"},
		{"a list element", `property "tags"`, map[string]any{"tags": []any{"ok", bad}}, "ok"},
		{"a value the constraint cannot render", `property "day"`, map[string]any{"day": bad}, "ok"},
		{"a map value", `property "text"`, map[string]any{"text": map[string]any{"x": bad}}, "ok"},
		{"a map key", `property "text"`, map[string]any{"text": map[string]any{bad: "x"}}, "ok"},
		{"an edge property", `edge property "label"`, nil, bad},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap := utf8JSONSnapshot(t, c.props, c.label)
			a := New()
			_, err := a.MarshalObject(context.Background(), snap)
			if !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("MarshalObject: %v does not match ErrUnrepresentable", err)
			}
			if err != nil && !strings.Contains(err.Error(), c.mentions) {
				t.Errorf("the refusal does not name %s: %v", c.mentions, err)
			}
			if err != nil && !utf8.ValidString(err.Error()) {
				t.Errorf("the refusal is not valid UTF-8: %q", err.Error())
			}
			if _, werr := a.WriteObject(context.Background(), io.Discard, snap); !errors.Is(werr, ErrUnrepresentable) {
				t.Errorf("WriteObject: %v does not match ErrUnrepresentable", werr)
			}
		})
	}
}

// What MarshalObject writes ParseObject reads back as the bytes the snapshot
// holds, U+FFFD included; a string it cannot write so is refused, never
// rewritten.
func TestMarshalObject_AStringReadsBackAsTheBytesHeld(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"a�b", "é✓", "a\xffb", "\xc3"} {
		snap := utf8JSONSnapshot(t, map[string]any{"text": text, "tags": []any{"ok", text}}, text)
		a := New()
		data, err := a.MarshalObject(context.Background(), snap)
		if errors.Is(err, ErrUnrepresentable) {
			continue
		}
		if err != nil {
			t.Fatalf("MarshalObject(%q): %v", text, err)
		}
		parsed, res := a.ParseObject(t.Context(), location.NewSourceID("utf8.json"), data)
		if res.HasErrors() || len(parsed["Holder"]) != 1 {
			t.Fatalf("ParseObject(%q): %s", text, res)
		}
		props := parsed["Holder"][0].Properties
		if got := props["text"]; got != text {
			t.Errorf("text read back as %q, want %q", got, text)
		}
		if got, _ := props["tags"].([]any); len(got) != 2 || got[1] != text {
			t.Errorf("tags read back as %q, want its second element %q", props["tags"], text)
		}
		link, _ := props["link"].([]any)
		if len(link) != 1 {
			t.Fatalf("link read back as %v", props["link"])
		}
		if got := link[0].(map[string]any)["label"]; got != text {
			t.Errorf("label read back as %q, want %q", got, text)
		}
	}
}
