package jsondata

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// A document that STATES a null states a key: `{"name": null}` and no "name"
// at all are different documents, and the parser is what keeps them apart —
// the validator answers both E_MISSING_REQUIRED for a required property. An
// assertion that the value reads nil holds for a missing key too, so this one
// asks for the key.
func TestParse_AnExplicitNullKeepsItsKey(t *testing.T) {
	t.Parallel()
	byType, result := Parse(t.Context(), location.NewSourceID("test://null.json"),
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
