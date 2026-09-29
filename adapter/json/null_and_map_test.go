package json

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
)

// A document that STATES a null states a key: `{"name": null}` and no "name"
// at all are different documents, and the parser is what keeps them apart —
// the validator answers both E_MISSING_REQUIRED for a required property. An
// assertion that the value reads nil holds for a missing key too, so this one
// asks for the key.
func TestParseObject_AnExplicitNullKeepsItsKey(t *testing.T) {
	t.Parallel()
	byType, result := New().ParseObject(t.Context(), location.MustNewSourceID("test://null.json"),
		[]byte(`{"Person":[{"id":"p1","name":null,"tags":[null]}]}`))
	if !result.OK() {
		t.Fatalf("parse: %s", result)
	}
	props := byType["Person"][0].Properties
	if v, stated := props["name"]; !stated || v != nil {
		t.Errorf(`"name": stated=%v value=%#v, want the key present and nil`, stated, v)
	}
	if tags, _ := props["tags"].([]any); len(tags) != 1 || tags[0] != nil {
		t.Errorf(`"tags" = %#v, want one nil element`, props["tags"])
	}
}

// bypassPerson builds a Person through instance.NewValidInstance, which runs no
// validation. The DSL has no map kind and the validator stores no nil, so a
// stored nil or a map-valued property reaches the writer only this way or
// through graph.RebuildSnapshot, which a .ys document reaches.
func bypassPerson(t *testing.T, props map[string]any) *graph.Snapshot {
	t.Helper()
	s := testSchemaSimple(t)
	personT, _ := s.Type("Person")
	g := graph.New(s)
	inst := instance.NewValidInstance("Person", personT.ID(), immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(props), nil, nil, nil)
	if r := g.Add(context.Background(), inst); r.HasErrors() {
		t.Fatalf("graph.Add: %s", r)
	}
	return g.Snapshot()
}

// A stored nil is written as a JSON null, which this adapter's own parser reads
// back as the explicit key the test above pins.
func TestMarshalObject_AStoredNilIsWrittenAsNull(t *testing.T) {
	t.Parallel()
	data, err := New().MarshalObject(context.Background(), bypassPerson(t, map[string]any{"id": "p1", "name": nil}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"name":null`) {
		t.Errorf("got %s, want the null stated", data)
	}
	byType, result := New().ParseObject(t.Context(), location.MustNewSourceID("test://back.json"), data)
	if !result.OK() {
		t.Fatalf("the writer's own output does not parse: %s", result)
	}
	if v, stated := byType["Person"][0].Properties["name"]; !stated || v != nil {
		t.Errorf("read back stated=%v value=%#v, want the key present and nil", stated, v)
	}
}

// No schema kind yields a map, so only a bypass-built value reaches the map
// arm; it renders as a JSON object rather than failing the export, as the
// writer renders any value its constraint cannot.
func TestMarshalObject_AMapValueIsWrittenAsAnObject(t *testing.T) {
	t.Parallel()
	data, err := New().MarshalObject(context.Background(),
		bypassPerson(t, map[string]any{"id": "p1", "name": map[string]any{"first": "Ann", "n": int64(2)}}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"name":{"first":"Ann","n":2}`) {
		t.Errorf("got %s, want the map as an object", data)
	}
}
