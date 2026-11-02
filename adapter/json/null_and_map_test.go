package json

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/location"
)

// A document that STATES a null states a key: `{"name": null}` and no "name"
// at all are different documents, and the parser is what keeps them apart —
// the validator answers both E_MISSING_REQUIRED for a required property. An
// assertion that the value reads nil holds for a missing key too, so this one
// asks for the key.
func TestParseObject_AnExplicitNullKeepsItsKey(t *testing.T) {
	t.Parallel()
	byType, result := New().ParseObject(t.Context(), location.NewSourceID("test://null.json"),
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

// A .ys document holds what no validator stores: the DSL has no map kind, and
// the validator stores no nil.
func personDocument(t *testing.T, name string) graph.Snapshot {
	t.Helper()
	return heldDocument(t, testSchemaSimple(t), "Person",
		`{"key":["p1"],"properties":{"id":"p1","name":`+name+`},"provenance":null}`)
}

// A stored nil is written as a JSON null, which this adapter's own parser reads
// back as the explicit key the test above pins.
func TestMarshalObject_AStoredNilIsWrittenAsNull(t *testing.T) {
	t.Parallel()
	data, err := New().MarshalObject(context.Background(), personDocument(t, `null`))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"name":null`) {
		t.Errorf("got %s, want the null stated", data)
	}
	byType, result := New().ParseObject(t.Context(), location.NewSourceID("test://back.json"), data)
	if !result.OK() {
		t.Fatalf("the writer's own output does not parse: %s", result)
	}
	if v, stated := byType["Person"][0].Properties["name"]; !stated || v != nil {
		t.Errorf("read back stated=%v value=%#v, want the key present and nil", stated, v)
	}
}

// A map renders as a JSON object rather than failing the export, as the writer
// renders any value its constraint cannot.
func TestMarshalObject_AMapValueIsWrittenAsAnObject(t *testing.T) {
	t.Parallel()
	data, err := New().MarshalObject(context.Background(), personDocument(t, `{"first":"Ann","n":2}`))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"name":{"first":"Ann","n":2}`) {
		t.Errorf("got %s, want the map as an object", data)
	}
}
