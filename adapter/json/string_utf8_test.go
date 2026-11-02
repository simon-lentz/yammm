package json

import (
	"bytes"
	"context"
	"maps"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/snapparts"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/snappartstest"
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

// utf8JSONSnapshot reads a document holding props beside Holder h1's key, and label
// on its one LINK to Target t1.
func utf8JSONSnapshot(t *testing.T, props map[string]any, label any) graph.Snapshot {
	t.Helper()
	s, res := schema.LoadString(t.Context(), utf8JSONSchema, "utf8json.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	all := map[string]any{"id": "h1"}
	maps.Copy(all, props)
	return snappartstest.Loaded(t, s, snapparts.Parts{
		Instances: []snapparts.Instance{
			heldRoot(t, s, "Target", "t1", map[string]any{"key": "t1"}),
			heldRoot(t, s, "Holder", "h1", all),
		},
		Edges: []snapparts.Edge{heldEdge(t, s, "Holder", "h1", "LINK", "t1", map[string]any{"label": label})},
	})
}

// What MarshalObject writes ParseObject reads back as the bytes the snapshot
// holds, U+FFFD included.
func TestMarshalObject_AStringReadsBackAsTheBytesHeld(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"a�b", "é✓"} {
		snap := utf8JSONSnapshot(t, map[string]any{"text": text, "tags": []any{"ok", text}}, text)
		a := New()
		data, err := a.MarshalObject(context.Background(), snap)
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

// U+FFFD itself, and text spelling the escape U+FFFD has in JSON, are written as
// the text they are.
func TestMarshalObject_ValidTextIsWritten(t *testing.T) {
	t.Parallel()
	escapeText := "a" + string(rune(0x5c)) + "ufffdb"
	for _, c := range []struct {
		name string
		text string
		want string
	}{
		{"U+FFFD", "a�b", "a�b"},
		{"the escape's text", escapeText, "a" + string(rune(0x5c)) + string(rune(0x5c)) + "ufffdb"},
	} {
		data, err := New().MarshalObject(t.Context(), utf8JSONSnapshot(t, map[string]any{"text": c.text}, "ok"))
		if err != nil {
			t.Errorf("%s: MarshalObject: %v", c.name, err)
			continue
		}
		if !utf8.Valid(data) || !bytes.Contains(data, []byte(c.want)) {
			t.Errorf("%s: wrote %s; want valid UTF-8 holding %s", c.name, data, c.want)
		}
	}
}
