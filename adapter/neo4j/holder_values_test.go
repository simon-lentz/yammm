package neo4j

import (
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
)

const holderSchema = `schema "holder"

type Target {
	key String primary
}

part type Note {
	body String
	marks List<String>
}

type Holder {
	id String primary
	text String
	kind Enum["ab", "cd"]
	code Pattern["^a.b$"]
	uid UUID
	at Timestamp
	tags List<String>
	nested List<List<String>>
	--> LINK (_:many) Target {
		label String
		mark String
		when Timestamp
	}
	*-> NOTES (_:many) Note
}
`

// holderValues is what one Holder's document states beside its key.
type holderValues struct {
	props map[string]any
	label any
	mark  any
	when  any
	note  map[string]any
}

// holderSnapshot reads a document of Holder h1, which no validator judged: h.props
// beside its key, one LINK to Target t1 carrying h.label, and one composed Note
// holding h.note.
func holderSnapshot(t *testing.T, h holderValues) (*Adapter, *GraphShape, graph.Snapshot) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), holderSchema, "holder.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	a := New()
	shape, sres := a.ShapeForSchema(t.Context(), s)
	if sres.HasErrors() {
		t.Fatalf("ShapeForSchema: %s", sres)
	}
	targetT, _ := s.Type("Target")
	holderT, _ := s.Type("Holder")
	noteT, _ := s.Type("Note")

	all := map[string]any{"id": "h1"}
	maps.Copy(all, h.props)
	label := h.label
	if label == nil {
		label = "ok"
	}
	note := h.note
	if note == nil {
		note = map[string]any{"body": "ok"}
	}
	snap := snappartstest.Loaded(t, s, snapparts.Parts{
		Types: []schema.TypeID{targetT.ID(), holderT.ID()},
		Instances: []snapparts.Instance{
			{TypeID: targetT.ID(), PrimaryKey: immutable.WrapKey([]any{"t1"}), Properties: immutable.WrapProperties(map[string]any{"key": "t1"})},
			{
				TypeID: holderT.ID(), PrimaryKey: immutable.WrapKey([]any{"h1"}), Properties: immutable.WrapProperties(all),
				Composed: map[string][]snapparts.Instance{"NOTES": {{TypeID: noteT.ID(), Properties: immutable.WrapProperties(note)}}},
			},
		},
		Edges: []snapparts.Edge{{
			Relation: "LINK", SourceType: holderT.ID(), SourceKey: immutable.WrapKey([]any{"h1"}), TargetKey: immutable.WrapKey([]any{"t1"}),
			Properties: immutable.WrapProperties(map[string]any{"label": label, "mark": h.mark, "when": h.when}),
		}},
	})
	return a, shape, snap
}

// deepList holds leaf inside depth lists.
func deepList(depth int, leaf any) any {
	for range depth {
		leaf = []any{leaf}
	}
	return leaf
}

// deepMap holds leaf inside depth string-keyed maps.
func deepMap(depth int, leaf any) any {
	for range depth {
		leaf = map[string]any{"k": leaf}
	}
	return leaf
}

// A document may hold a number no driver number holds and a list or a map nested
// past the depth this adapter writes. The call that would send one returns an error
// under the value's own sentinel and the class, naming the site, and the other call
// is not at fault.
func TestBatchQueries_ADocumentValueNoDriverHoldsIsNeverSent(t *testing.T) {
	t.Parallel()
	const huge, wide = json.Number("1e400"), json.Number("1180591620717411303424")
	past := maxFormDepth + 1
	for _, c := range []struct {
		name, refusedBy, mentions string
		holds                     holderValues
		sentinel                  error
	}{
		{"a number at a property", "nodes", `property "text"`, holderValues{props: map[string]any{"text": huge}}, errUnheldNumber},
		{"a number in a list", "nodes", `property "tags"`, holderValues{props: map[string]any{"tags": []any{"ok", huge}}}, errUnheldNumber},
		{"a number in a map", "nodes", `property "text"`, holderValues{props: map[string]any{"text": map[string]any{"k": huge}}}, errUnheldNumber},
		{"an integer literal the reader left as text, deep in a list", "nodes", `property "text"`, holderValues{props: map[string]any{"text": deepList(70, wide)}}, errUnheldNumber},
		{"a number at a part's property", "nodes", `property "body"`, holderValues{note: map[string]any{"body": huge}}, errUnheldNumber},
		{"a number at an edge property", "edges", `property "label"`, holderValues{label: huge}, errUnheldNumber},
		{"a list nested past the depth", "nodes", `property "text"`, holderValues{props: map[string]any{"text": deepList(past, "x")}}, errNoForm},
		{"a map nested past the depth", "nodes", `property "text"`, holderValues{props: map[string]any{"text": deepMap(past, "x")}}, errNoForm},
		{"a part's list nested past the depth", "nodes", `property "body"`, holderValues{note: map[string]any{"body": deepList(past, "x")}}, errNoForm},
		{"an edge's map nested past the depth", "edges", `property "label"`, holderValues{label: deepMap(past, "x")}, errNoForm},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a, shape, snap := holderSnapshot(t, c.holds)
			_, nerr := a.BatchNodeQueries(t.Context(), snap, shape)
			_, eerr := a.BatchEdgeQueries(t.Context(), snap, shape)
			refusal, other := nerr, eerr
			if c.refusedBy == "edges" {
				refusal, other = eerr, nerr
			}
			if other != nil {
				t.Errorf("the call that sends no such value failed: %v", other)
			}
			if !errors.Is(refusal, c.sentinel) || !errors.Is(refusal, ErrUnrepresentable) {
				t.Fatalf("the %s call = %v; want %v and ErrUnrepresentable", c.refusedBy, refusal, c.sentinel)
			}
			if !strings.Contains(refusal.Error(), c.mentions) {
				t.Errorf("the refusal does not name %s: %v", c.mentions, refusal)
			}
		})
	}
}

// sentProp returns the value the node queries send for property name, or nil.
func sentProp(nodes []*BatchNodeQuery, name string) any {
	for _, q := range nodes {
		rows, _ := q.Params["rows"].([]map[string]any)
		for _, row := range rows {
			props, _ := row["props"].(map[string]any)
			if v, ok := props[name]; ok {
				return v
			}
		}
	}
	return nil
}

// Each value is sent in its form, at a node and an edge alike: a Timestamp's
// text spelled at another offset as the instant, and a UUID's text in the spelling
// the document holds.
func TestBatchQueries_EachValueIsSentInItsForm(t *testing.T) {
	t.Parallel()
	const spelled = "2024-01-02T04:04:05+01:00"
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	const id = "12345678-1234-1234-1234-123456789abc"
	a, shape, snap := holderSnapshot(t, holderValues{
		props: map[string]any{"at": spelled, "uid": id, "text": "x", "tags": []any{"a"}},
		label: "y",
		when:  spelled,
	})
	nodes, err := a.BatchNodeQueries(t.Context(), snap, shape)
	if err != nil {
		t.Fatalf("BatchNodeQueries: %v", err)
	}
	edges, err := a.BatchEdgeQueries(t.Context(), snap, shape)
	if err != nil {
		t.Fatalf("BatchEdgeQueries: %v", err)
	}
	sent := map[string]any{}
	for _, name := range []string{"uid", "text", "tags"} {
		sent[name] = sentProp(nodes, name)
	}
	var sentAt, sentWhen any
	sentAt = sentProp(nodes, "at")
	for _, q := range edges {
		for _, row := range q.Params["rows"].([]map[string]any) {
			if rel, ok := row["rel_props"].(map[string]any); ok {
				sent["label"], sentWhen = rel["label"], rel["when"]
			}
		}
	}
	want := map[string]any{"uid": id, "text": "x", "tags": []string{"a"}, "label": "y"}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("sent %#v, want %#v", sent, want)
	}
	for name, v := range map[string]any{"at": sentAt, "when": sentWhen} {
		if got, ok := v.(time.Time); !ok || !got.Equal(at) {
			t.Errorf("%s sent as %#v, want the instant %v", name, v, at)
		}
	}
}

// Of several properties holding a number no driver number holds the error names the
// first in sorted order, on every run, as the package's other value refusals do.
func TestBatchQueries_TheRefusalNamesTheFirstPropertyInSortedOrder(t *testing.T) {
	t.Parallel()
	const huge = json.Number("1e400")
	for range 30 {
		a, shape, snap := holderSnapshot(t, holderValues{props: map[string]any{"text": huge, "code": huge, "uid": huge}, label: huge, mark: huge})
		if _, err := a.BatchNodeQueries(t.Context(), snap, shape); err == nil || !strings.Contains(err.Error(), `property "code"`) {
			t.Fatalf("BatchNodeQueries: %v, want the refusal of property \"code\"", err)
		}
		if _, err := a.BatchEdgeQueries(t.Context(), snap, shape); err == nil || !strings.Contains(err.Error(), `property "label"`) {
			t.Fatalf("BatchEdgeQueries: %v, want the refusal of property \"label\"", err)
		}
	}
}

// Valid text is sent as the bytes the snapshot holds, U+FFFD itself included.
func TestBatchQueries_ValidTextIsSentAsHeld(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"a�b", "é✓", "", "\x00"} {
		a, shape, snap := holderSnapshot(t, holderValues{
			props: map[string]any{"text": text, "tags": []any{"ok", text}},
			label: text,
			note:  map[string]any{"body": text, "marks": []any{text}},
		})
		nodes, err := a.BatchNodeQueries(t.Context(), snap, shape)
		if err != nil {
			t.Fatalf("BatchNodeQueries(%q): %v", text, err)
		}
		edges, err := a.BatchEdgeQueries(t.Context(), snap, shape)
		if err != nil {
			t.Fatalf("BatchEdgeQueries(%q): %v", text, err)
		}

		sent := map[string]any{}
		for _, q := range nodes {
			for _, row := range q.Params["rows"].([]map[string]any) {
				props, _ := row["props"].(map[string]any)
				for _, name := range []string{"text", "tags", "body", "marks"} {
					if v, ok := props[name]; ok {
						sent[name] = v
					}
				}
			}
		}
		for _, q := range edges {
			for _, row := range q.Params["rows"].([]map[string]any) {
				if rel, ok := row["rel_props"].(map[string]any); ok {
					sent["label"] = rel["label"]
				}
			}
		}
		want := map[string]any{
			"text": text, "tags": []string{"ok", text},
			"body": text, "marks": []string{text}, "label": text,
		}
		if !reflect.DeepEqual(sent, want) {
			t.Errorf("sent %#v, want %#v", sent, want)
		}
	}
}

// A key is sent from the instance's properties, so a key no driver number holds is
// refused too, beside the missing and the null keys the same call reports.
func TestExtractKeyProps_AKeyNoDriverNumberHoldsIsRefusedBesideTheOthers(t *testing.T) {
	t.Parallel()
	shape := &nodeShape{label: "Holder", primaryKeys: []string{"id", "region", "code"}}
	_, err := extractKeyProps(immutable.WrapProperties(map[string]any{"id": json.Number("1e400"), "code": nil}), shape)
	if !errors.Is(err, errUnheldNumber) {
		t.Fatalf("extractKeyProps = %v; want errUnheldNumber", err)
	}
	for _, want := range []string{`primary key "id"`, "missing required primary key(s): [region]", "nil primary key(s): [code]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not hold %q: %v", want, err)
		}
	}
	if keys, err := extractKeyProps(immutable.WrapProperties(map[string]any{"id": "a�b", "region": "é", "code": "c"}), shape); err != nil || keys["id"] != "a�b" {
		t.Errorf("valid keys: %v, %v", keys, err)
	}
}
