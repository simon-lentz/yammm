package jsondata

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// A string value reads each byte of an invalid UTF-8 sequence as U+FFFD, as a
// member name does, at a property and inside a list.
func TestParse_AStringValueReadsInvalidUTF8AsTheReplacement(t *testing.T) {
	t.Parallel()
	doc := []byte("{\"Test\": [{\"id\": \"a\xff\xfe\", \"tags\": [\"b\xff\"]}]}")
	parsed, result := Parse(context.Background(), location.NewSourceID("data.json"), doc)
	if result.HasErrors() || len(parsed["Test"]) != 1 {
		t.Fatalf("parsed %d instances, issues: %s", len(parsed["Test"]), result.String())
	}
	props := parsed["Test"][0].Properties
	if got := props["id"]; got != "a��" {
		t.Errorf("id = %q, want %q", got, "a��")
	}
	tags, ok := props["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "b�" {
		t.Errorf("tags = %#v, want [%q]", props["tags"], "b�")
	}
}
