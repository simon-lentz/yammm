package csv

import (
	"bytes"
	"context"
	"maps"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const utf8CSVSchema = `schema "utf8"

type Target {
	key String primary
}

type Holder {
	id String primary
	text String
	code Pattern["^a.b$"]
	kind Enum["ab", "cd"]
	uid UUID
	day Date
	at Timestamp
	tags List<String>
	--> LINK (_:many) Target {
		label String
	}
}
`

func loadUTF8CSVSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), utf8CSVSchema, "utf8.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s
}

// A cell is the bytes the file holds, so a CSV file is the one document that
// delivers a string that is not valid UTF-8 to the validator. No position a
// cell fills yields a validated instance: the adapter or the validator refuses
// the row, in a diagnostic that is itself valid UTF-8.
func TestParseTyped_ACellThatIsNotValidUTF8YieldsNoValidInstance(t *testing.T) {
	t.Parallel()
	s := loadUTF8CSVSchema(t)
	holder, _ := s.Type("Holder")
	v := instance.NewValidator(s)

	for _, c := range []struct {
		name, header, row string
	}{
		{"a String property", "id,text", "h1,a\xffb"},
		{"a Pattern property its pattern matches", "id,code", "h1,a\xffb"},
		{"an Enum property", "id,kind", "h1,a\xff"},
		{"a UUID property", "id,uid", "h1,6ba7b810-9dad-11d1-80b4-00c04fd430c\xff"},
		{"a Date property", "id,day", "h1,2026-01-0\xff"},
		{"a Timestamp property", "id,at", "h1,2026-01-02T03:04:05\xff"},
		{"a List<String> element", "id,tags", "h1,ok|a\xffb"},
		{"a primary key component", "id,text", "a\xffb,ok"},
		{"a foreign key component", "id,link._target_key", "h1,a\xffb"},
		{"an edge property", "id,link._target_key,link.label", "h1,t1,a\xffb"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("utf8.csv"), "Holder",
				strings.NewReader(c.header+"\n"+c.row+"\n"), holder)
			if len(raws) != 1 {
				t.Fatalf("ParseTyped returned %d rows, want 1: %s", len(raws), pres)
			}
			inst, vres := v.ValidateOne(t.Context(), "Holder", raws[0])
			if !pres.HasErrors() && !inst.IsZero() {
				t.Errorf("a cell that is not valid UTF-8 was parsed and validated")
			}
			if !pres.HasErrors() && !vres.HasErrors() {
				t.Errorf("neither the parse nor the validation reported the cell")
			}
			for _, res := range []diag.Result{pres, vres} {
				for issue := range res.Issues() {
					if !utf8.ValidString(issue.Message()) {
						t.Errorf("a message is not valid UTF-8: %q", issue.Message())
					}
				}
			}
		})
	}
}

// heldUTF8 reads a document holding props beside Holder h1's key, and label on its
// one LINK to Target t1.
func heldUTF8(t *testing.T, s schema.Schema, props map[string]any, label any) graph.Snapshot {
	t.Helper()
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

// What the writer writes reads back as the bytes the snapshot holds, U+FFFD
// included.
func TestMarshalSnapshot_AStringReadsBackAsTheBytesHeld(t *testing.T) {
	t.Parallel()
	s := loadUTF8CSVSchema(t)
	holder, _ := s.Type("Holder")
	for _, text := range []string{"a�b", "é✓"} {
		snap := heldUTF8(t, s, map[string]any{"text": text, "tags": []any{"ok", text}}, text)
		a := New(WithSchema(s))
		files, err := a.MarshalSnapshot(context.Background(), snap)
		if err != nil {
			t.Fatalf("MarshalSnapshot(%q) refused valid text: %v", text, err)
		}
		raws, res := a.ParseTyped(t.Context(), location.NewSourceID("utf8.csv"), "Holder", bytes.NewReader(files["Holder"]), holder)
		if res.HasErrors() || len(raws) != 1 {
			t.Fatalf("ParseTyped(%q): %d rows: %s", text, len(raws), res)
		}
		props := raws[0].Properties
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
