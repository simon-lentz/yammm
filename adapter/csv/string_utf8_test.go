package csv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
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

func loadUTF8CSVSchema(t *testing.T) *schema.Schema {
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
			if !pres.HasErrors() && inst != nil {
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

// utf8Bypass builds Holder h1 through instancetest, which runs no validation,
// holding props beside its key and one LINK to Target t1 carrying label.
func utf8Bypass(t *testing.T, s *schema.Schema, props map[string]any, label any) *graph.Snapshot {
	t.Helper()
	target, _ := s.Type("Target")
	holder, _ := s.Type("Holder")
	all := map[string]any{"id": "h1"}
	maps.Copy(all, props)
	g := graph.New(s)
	for _, inst := range []*instance.ValidInstance{
		instancetest.VI("Target", instancetest.TypeID(target.ID()), instancetest.PK("t1"),
			instancetest.Props(map[string]any{"key": "t1"})),
		instancetest.VI("Holder", instancetest.TypeID(holder.ID()), instancetest.PK("h1"),
			instancetest.Props(all),
			instancetest.Edges(map[string]*instance.ValidEdgeData{
				"LINK": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
					instance.NewValidEdgeTarget(immutable.WrapKey([]any{"t1"}), immutable.WrapProperties(map[string]any{"label": label})),
				}),
			})),
	} {
		if res := g.Add(context.Background(), inst); !res.OK() {
			t.Fatalf("add: %s", res)
		}
	}
	return g.Snapshot()
}

// A validated string is valid UTF-8, so only a bypass-built snapshot holds one
// that is not, and a document holding it would read back refused. Both writers
// refuse it as the class, wherever the string stands.
func TestWriters_AStringThatIsNotValidUTF8IsRefusedAsTheClass(t *testing.T) {
	t.Parallel()
	s := loadUTF8CSVSchema(t)
	for _, c := range []struct {
		name, mentions string
		props          map[string]any
		label          any
	}{
		{"a property", `property "text"`, map[string]any{"text": "a\xffb"}, "ok"},
		{"a list element", `property "tags": list element 1`, map[string]any{"tags": []any{"ok", "a\xffb"}}, "ok"},
		{"an edge property", `edge property "label"`, nil, "a\xffb"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			snap := utf8Bypass(t, s, c.props, c.label)
			a := New(WithSchema(s))
			if _, err := a.MarshalSnapshot(context.Background(), snap); !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("MarshalSnapshot: %v does not match ErrUnrepresentable", err)
			} else if !utf8.ValidString(err.Error()) {
				t.Errorf("the refusal is not valid UTF-8: %q", err.Error())
			} else if !strings.Contains(err.Error(), c.mentions) || !strings.Contains(err.Error(), "not valid UTF-8") {
				t.Errorf("the refusal does not name %s and the fault: %v", c.mentions, err)
			}
			err := a.WriteSnapshot(context.Background(), func(string) (io.Writer, error) { return io.Discard, nil }, snap)
			if !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("WriteSnapshot: %v does not match ErrUnrepresentable", err)
			}
		})
	}
}

// What the writer writes reads back as the bytes the snapshot holds, U+FFFD
// included; a string it cannot write so is refused, never rewritten.
func TestMarshalSnapshot_AStringReadsBackAsTheBytesHeld(t *testing.T) {
	t.Parallel()
	s := loadUTF8CSVSchema(t)
	holder, _ := s.Type("Holder")
	for _, text := range []string{"a�b", "é✓", "a\xffb", "\xc3"} {
		snap := utf8Bypass(t, s, map[string]any{"text": text, "tags": []any{"ok", text}}, text)
		a := New(WithSchema(s))
		files, err := a.MarshalSnapshot(context.Background(), snap)
		if !utf8.ValidString(text) {
			if !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("MarshalSnapshot(%q) = %v; want a refusal matching ErrUnrepresentable", text, err)
			}
			continue
		}
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

// A json.Number holding a byte that is no part of a character is refused as
// text that is not valid UTF-8, the fault it holds first.
func TestMarshalSnapshot_ANumberHoldingAnInvalidByteIsRefusedAsText(t *testing.T) {
	t.Parallel()
	s := loadUTF8CSVSchema(t)
	_, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), utf8Bypass(t, s, map[string]any{"text": json.Number("1\xff")}, "ok"))
	if !errors.Is(err, ErrUnrepresentable) || !strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("MarshalSnapshot = %v; want a refusal naming text that is not valid UTF-8", err)
	}
}
