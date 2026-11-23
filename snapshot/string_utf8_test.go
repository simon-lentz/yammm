package snapshot_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
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

// utf8WireSnapshot holds Holder h1 with text in its text and in the last element of
// its tags, and one LINK to Target t1 carrying text as its label.
func utf8WireSnapshot(t *testing.T, text string) (schema.Schema, graph.Snapshot) {
	t.Helper()
	s, err := schema.LoadString(t.Context(), utf8WireSchema, "utf8wire.yammm")
	if err != nil {
		t.Fatalf("load schema: %s", err)
	}
	holder := instancetest.Valid(t, s, "Holder", map[string]any{
		"id": "h1", "text": text, "tags": []any{"ok", text},
		"link": []any{map[string]any{"_target_key": "t1", "label": text}},
	})
	target := instancetest.Valid(t, s, "Target", map[string]any{"key": "t1"})
	return s, snapshottest.BuildSnapshot(t, s, target, holder)
}

// What Marshal writes Load reads back as the bytes the snapshot holds, U+FFFD
// and a character past the Basic Multilingual Plane included.
func TestMarshal_AStringReadsBackAsTheBytesHeld(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"a\uFFFDb", "\u00e9\u2713", "\U0001F600"} {
		s, snap := utf8WireSnapshot(t, text)
		data, err := snapshot.Marshal(t.Context(), snap)
		if err != nil {
			t.Fatalf("Marshal(%q) refused valid text: %s", text, err)
		}
		loaded, readErr := snapshot.Unmarshal(t.Context(), data, s)
		if loaded.IsZero() {
			t.Fatalf("Load(%q): %s", text, readErr)
		}
		holderT, _ := s.Type("Holder")
		inst, ok := loaded.InstanceByKey(holderT.ID(), `["h1"]`)
		if !ok {
			t.Fatalf("Load(%q): no Holder h1", text)
		}
		if got, _ := inst.Property("text"); got.Any() != text {
			t.Errorf("text read back as %q, want %q", got.Any(), text)
		}
		tags, _ := inst.Property("tags")
		if elems, ok := tags.AsSlice(); !ok || elems.Len() != 2 || elems.At(1).Any() != text {
			t.Errorf("tags read back as %v, want its second element %q", tags.Any(), text)
		}
		edges := slices.Collect(loaded.EdgesFrom(inst))
		if len(edges) != 1 {
			t.Fatalf("Load(%q): %d edges", text, len(edges))
		}
		if got, _ := edges[0].Properties().Get("label"); got.Any() != text {
			t.Errorf("label read back as %q, want %q", got.Any(), text)
		}
	}
}
