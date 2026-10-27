package neo4j

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const utf8Neo4jSchema = `schema "utf8neo"

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

// neo4jText is a named type over string, which the immutable layer holds as it
// arrived and the driver sends as a string.
type neo4jText string

type innerText struct{ S string }

// embedsText promotes innerText's field, as the driver's struct mapping reads it.
type embedsText struct{ innerText }

// utf8Holder is what one bypass-built Holder holds beside its key.
type utf8Holder struct {
	props map[string]any
	label any
	mark  any
	when  any
	note  map[string]any
}

// utf8Neo4jSnapshot builds Holder h1 through instance.NewValidInstance, which
// runs no validation: h.props beside its key, one LINK to Target t1 carrying
// h.label, and one composed Note holding h.note.
func utf8Neo4jSnapshot(t *testing.T, h utf8Holder) (*Adapter, *GraphShape, *graph.Snapshot) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), utf8Neo4jSchema, "utf8neo.yammm")
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
	g := graph.New(s)
	for _, inst := range []*instance.ValidInstance{
		instance.NewValidInstance("Target", targetT.ID(), immutable.WrapKey([]any{"t1"}),
			immutable.WrapProperties(map[string]any{"key": "t1"}), nil, nil, nil),
		instance.NewValidInstance("Holder", holderT.ID(), immutable.WrapKey([]any{"h1"}),
			immutable.WrapProperties(all),
			map[string]*instance.ValidEdgeData{"LINK": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{"t1"}), immutable.WrapProperties(map[string]any{"label": label, "mark": h.mark, "when": h.when})),
			})},
			map[string]immutable.Value{"NOTES": immutable.Wrap([]any{
				instance.NewValidInstance("Note", noteT.ID(), immutable.Key{}, immutable.WrapProperties(note), nil, nil, nil),
			})}, nil),
	} {
		if r := g.Add(context.Background(), inst); r.HasErrors() {
			t.Fatalf("graph.Add: %s", r)
		}
	}
	return a, shape, g.Snapshot()
}

// invalidStrings returns where a string that is not valid UTF-8 stands in v:
// a string of any type, a map's key, and what a slice, an array, a map, a
// pointer or a struct's field holds, at any depth.
func invalidStrings(v reflect.Value, at string) []string {
	var found []string
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			found = append(found, invalidStrings(v.Elem(), at)...)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			found = append(found, invalidStrings(v.Field(i), at+"."+v.Type().Field(i).Name)...)
		}
	case reflect.String:
		if !utf8.ValidString(v.String()) {
			found = append(found, at)
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			found = append(found, invalidStrings(v.Index(i), fmt.Sprintf("%s[%d]", at, i))...)
		}
	case reflect.Map:
		for iter := v.MapRange(); iter.Next(); {
			key := fmt.Sprintf("%s[%q]", at, iter.Key().Interface())
			found = append(found, invalidStrings(iter.Key(), key+" (the key)")...)
			found = append(found, invalidStrings(iter.Value(), key)...)
		}
	}
	return found
}

// The server stores U+FFFD for each invalid sequence of a string it is sent,
// with no error, so a string that is not valid UTF-8 would be stored as
// another value. A validated string is valid UTF-8, so only a bypass-built
// snapshot holds one. Wherever it stands, the call that would send it returns
// an error naming the site, and no query that either call returns holds one.
func TestBatchQueries_AStringThatIsNotValidUTF8IsNeverSent(t *testing.T) {
	t.Parallel()
	const bad = "a\xffb"
	for _, c := range []struct {
		name, refusedBy, mentions string
		holds                     utf8Holder
	}{
		{"a String property", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": bad}}},
		{"a named type over string", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": neo4jText(bad)}}},
		{"an Enum property", "nodes", `property "kind"`, utf8Holder{props: map[string]any{"kind": bad}}},
		{"a Pattern property", "nodes", `property "code"`, utf8Holder{props: map[string]any{"code": bad}}},
		{"a UUID property", "nodes", `property "uid"`, utf8Holder{props: map[string]any{"uid": bad}}},
		{"a List<String> element", "nodes", `property "tags"`, utf8Holder{props: map[string]any{"tags": []any{"ok", bad}}}},
		{"a List<String> element in a []string", "nodes", `property "tags"`, utf8Holder{props: map[string]any{"tags": []string{"ok", bad}}}},
		{"a map's value", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": map[string]any{"x": bad}}}},
		{"a map's key", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": map[string]any{bad: "x"}}}},
		{"a key of a map keyed by a named string", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": map[neo4jText]int{neo4jText(bad): 1}}}},
		{"a composed part's property", "nodes", `property "body"`, utf8Holder{note: map[string]any{"body": bad}}},
		{"a composed part's list element", "nodes", `property "marks"`, utf8Holder{note: map[string]any{"body": "ok", "marks": []any{bad}}}},
		{"an edge property", "edges", `property "label"`, utf8Holder{label: bad}},
		{"an edge property in a named type", "edges", `property "label"`, utf8Holder{label: neo4jText(bad)}},
		{"a pointer to it", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": new(bad)}}},
		{"a pointer to a pointer to it", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": new(new(bad))}}},
		{"a pointer to a named type over it", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": new(neo4jText(bad))}}},
		{"one value of a map holding several", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": map[string]any{"a": "ok", "b": "ok", "c": bad, "d": "ok"}}}},
		{"a composed part's pointer", "nodes", `property "body"`, utf8Holder{note: map[string]any{"body": new(bad)}}},
		{"an edge property's pointer", "edges", `property "label"`, utf8Holder{label: new(bad)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a, shape, snap := utf8Neo4jSnapshot(t, c.holds)
			nodes, nerr := a.BatchNodeQueries(t.Context(), snap, shape)
			edges, eerr := a.BatchEdgeQueries(t.Context(), snap, shape)

			for i, q := range nodes {
				for _, at := range invalidStrings(reflect.ValueOf(q.Params), fmt.Sprintf("node query %d", i)) {
					t.Errorf("a string that is not valid UTF-8 would be sent at %s", at)
				}
			}
			for i, q := range edges {
				for _, at := range invalidStrings(reflect.ValueOf(q.Params), fmt.Sprintf("edge query %d", i)) {
					t.Errorf("a string that is not valid UTF-8 would be sent at %s", at)
				}
			}

			refusal, other := nerr, eerr
			if c.refusedBy == "edges" {
				refusal, other = eerr, nerr
			}
			if other != nil {
				t.Errorf("the call that sends no such string failed: %v", other)
			}
			if refusal == nil {
				t.Fatalf("the %s call returned no error", c.refusedBy)
			}
			if !strings.Contains(refusal.Error(), c.mentions) || !strings.Contains(refusal.Error(), "not valid UTF-8") {
				t.Errorf("the refusal does not name %s and the fault: %v", c.mentions, refusal)
			}
			if !utf8.ValidString(refusal.Error()) {
				t.Errorf("the refusal is not valid UTF-8: %q", refusal.Error())
			}
		})
	}
}

// A time is sent only at a Timestamp or a Date, which re-express a zone the
// host cannot resolve as its offset, so a zone name that is not valid UTF-8 is
// never sent. The graph stores a time it renders as text, so the time is held
// behind a pointer, which it keeps as given.
func TestBatchQueries_ATimesZoneNameThatIsNotValidUTF8IsNeverSent(t *testing.T) {
	t.Parallel()
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone("Z\xffZ", 3600))
	a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{props: map[string]any{"at": &at}})
	nodes, err := a.BatchNodeQueries(t.Context(), snap, shape)
	if err != nil {
		t.Fatalf("BatchNodeQueries: %v", err)
	}
	for i, q := range nodes {
		for _, where := range invalidStrings(reflect.ValueOf(q.Params), fmt.Sprintf("node query %d", i)) {
			t.Errorf("a string that is not valid UTF-8 would be sent at %s", where)
		}
	}
	sent, ok := sentProp(nodes, "at").(time.Time)
	if !ok || !sent.Equal(at) {
		t.Errorf("sent %#v, want the instant %v", sent, at)
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

// A time or a UUID is sent only where its constraint renders it, as every other
// writer of this module writes it; a UUID at a UUID property is sent as its text.
func TestBatchQueries_ATimeOrAUUIDItsConstraintDoesNotRenderIsNeverSent(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	at := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name, refusedBy, mentions string
		holds                     utf8Holder
	}{
		{"a time at a String", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": at}}},
		{"a uuid at a String", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": id}}},
		{"a time inside a map", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": map[string]any{"k": at}}}},
		{"a time at an edge's String", "edges", `property "label"`, utf8Holder{label: at}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a, shape, snap := utf8Neo4jSnapshot(t, c.holds)
			_, nerr := a.BatchNodeQueries(t.Context(), snap, shape)
			_, eerr := a.BatchEdgeQueries(t.Context(), snap, shape)
			refusal := nerr
			if c.refusedBy == "edges" {
				refusal = eerr
			}
			if refusal == nil || !strings.Contains(refusal.Error(), c.mentions) || !strings.Contains(refusal.Error(), "a time or a UUID its constraint does not render") {
				t.Errorf("the %s call = %v; want a refusal naming %s as a time or a UUID no constraint renders", c.refusedBy, refusal, c.mentions)
			}
		})
	}
	a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{props: map[string]any{"uid": &id}})
	nodes, err := a.BatchNodeQueries(t.Context(), snap, shape)
	if err != nil {
		t.Fatalf("a pointer to a uuid at a UUID: %v", err)
	}
	if sent := sentProp(nodes, "uid"); sent != id.String() {
		t.Errorf("a pointer to a uuid at a UUID was sent as %#v, want its text %q", sent, id.String())
	}
}

// Each value is sent in its one form, at a node and an edge alike: a pointer
// as the value it points to, a named string as a string, and a uuid.UUID at a
// UUID as its text. The graph stores a time or a UUID it renders as text, so
// each is held behind a pointer, which it keeps as given.
func TestBatchQueries_EachValueIsSentInItsForm(t *testing.T) {
	t.Parallel()
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{
		props: map[string]any{"at": &at, "uid": &id, "text": neo4jText("x"), "tags": []any{neo4jText("a")}},
		label: neo4jText("y"),
		when:  &at,
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
	for _, name := range []string{"at", "uid", "text", "tags"} {
		sent[name] = sentProp(nodes, name)
	}
	for _, q := range edges {
		for _, row := range q.Params["rows"].([]map[string]any) {
			if rel, ok := row["rel_props"].(map[string]any); ok {
				sent["label"], sent["when"] = rel["label"], rel["when"]
			}
		}
	}
	want := map[string]any{"at": at, "uid": id.String(), "text": "x", "tags": []string{"a"}, "label": "y", "when": at}
	if !reflect.DeepEqual(sent, want) {
		t.Errorf("sent %#v, want %#v", sent, want)
	}
}

// Of several properties holding such a string the error names the first in
// sorted order, on every run, as the package's other value refusals do.
func TestBatchQueries_TheRefusalNamesTheFirstPropertyInSortedOrder(t *testing.T) {
	t.Parallel()
	const bad = "a\xffb"
	for range 30 {
		a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{props: map[string]any{"text": bad, "code": bad, "uid": bad}, label: bad, mark: bad})
		if _, err := a.BatchNodeQueries(t.Context(), snap, shape); err == nil || !strings.Contains(err.Error(), `property "code"`) {
			t.Fatalf("BatchNodeQueries: %v, want the refusal of property \"code\"", err)
		}
		if _, err := a.BatchEdgeQueries(t.Context(), snap, shape); err == nil || !strings.Contains(err.Error(), `property "label"`) {
			t.Fatalf("BatchEdgeQueries: %v, want the refusal of property \"label\"", err)
		}
	}
}

// Valid text is sent as the bytes the snapshot holds, U+FFFD itself included:
// the rule refuses bytes that are no character, never the character a decoder
// writes for them.
func TestBatchQueries_ValidTextIsSentAsHeld(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"a�b", "é✓", "", "\x00"} {
		a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{
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

// A key is sent from the instance's properties, so the key rule holds there
// too, beside the missing and the null keys the same call reports.
func TestExtractKeyProps_AKeyThatIsNotValidUTF8IsRefused(t *testing.T) {
	t.Parallel()
	shape := &NodeShape{Label: "Holder", PrimaryKeys: []string{"id", "region", "code"}}
	_, err := extractKeyProps(immutable.WrapProperties(map[string]any{"id": "a\xffb", "code": nil}), shape)
	if err == nil {
		t.Fatal("extractKeyProps returned no error")
	}
	for _, want := range []string{`primary key "id"`, "not valid UTF-8", "missing required primary key(s): [region]", "nil primary key(s): [code]"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not hold %q: %v", want, err)
		}
	}
	if !utf8.ValidString(err.Error()) {
		t.Errorf("the error is not valid UTF-8: %q", err.Error())
	}
	if keys, err := extractKeyProps(immutable.WrapProperties(map[string]any{"id": "a�b", "region": "é", "code": "c"}), shape); err != nil || keys["id"] != "a�b" {
		t.Errorf("valid keys: %v, %v", keys, err)
	}
}

// A nil inside a value holds no string, so the value is sent: the rule refuses
// bytes that are no character, never an absent value.
func TestBatchQueries_ANilInsideAValueIsSent(t *testing.T) {
	t.Parallel()
	a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{props: map[string]any{
		"text": map[string]any{"x": nil},
		"tags": [2]any{"ok", nil},
	}})
	if _, err := a.BatchNodeQueries(t.Context(), snap, shape); err != nil && strings.Contains(err.Error(), "not valid UTF-8") {
		t.Errorf("a nil inside a value was refused as text: %v", err)
	}
}

// A value that is no scalar, list or string-keyed map is no form any writer of
// this module writes, so the call that would send it returns an error naming
// the site, whatever text it holds, and no query holds it.
func TestBatchQueries_AValueOfNoFormIsNeverSent(t *testing.T) {
	t.Parallel()
	const bad = "a\xffb"
	for _, c := range []struct {
		name, refusedBy, mentions string
		holds                     utf8Holder
	}{
		{"an array's element", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": [2]string{"ok", bad}}}},
		{"a value of a map keyed by an integer", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": map[int]string{1: bad}}}},
		{"a pointer to a list holding it", "nodes", `property "tags"`, utf8Holder{props: map[string]any{"tags": new([]string{"ok", bad})}}},
		{"a struct's exported field", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": struct{ S string }{bad}}}},
		{"a field promoted through an embedded struct", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": embedsText{innerText{bad}}}}},
		{"a struct holding valid text", "nodes", `property "text"`, utf8Holder{props: map[string]any{"text": struct{ S string }{"ok"}}}},
		{"an edge property's struct", "edges", `property "label"`, utf8Holder{label: struct{ S string }{"ok"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			a, shape, snap := utf8Neo4jSnapshot(t, c.holds)
			_, nerr := a.BatchNodeQueries(t.Context(), snap, shape)
			_, eerr := a.BatchEdgeQueries(t.Context(), snap, shape)
			refusal := nerr
			if c.refusedBy == "edges" {
				refusal = eerr
			}
			if refusal == nil || !strings.Contains(refusal.Error(), c.mentions) || !strings.Contains(refusal.Error(), "no scalar, list or string-keyed map") {
				t.Errorf("the %s call = %v; want a refusal naming %s as a value of no form", c.refusedBy, refusal, c.mentions)
			}
		})
	}
}
