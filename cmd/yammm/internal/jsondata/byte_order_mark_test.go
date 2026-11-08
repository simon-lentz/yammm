package jsondata

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// TestParse_SkipsOneLeadingByteOrderMark pins that a data file starting
// with U+FEFF parses as the same document without it, and that a second mark is
// still a parse error.
func TestParse_SkipsOneLeadingByteOrderMark(t *testing.T) {
	t.Parallel()
	src := location.NewSourceID("data.json")
	parsed, result := Parse(context.Background(), src, []byte("\uFEFF{\"Test\": [{\"id\": \"a\"}]}"))
	if result.HasErrors() {
		t.Fatalf("issues: %s", result.String())
	}
	if len(parsed["Test"]) != 1 {
		t.Fatalf("parsed %d instances, want 1", len(parsed["Test"]))
	}
	if got := parsed["Test"][0].Properties["id"]; got != "a" {
		t.Errorf("id = %v, want %q", got, "a")
	}

	_, result = Parse(context.Background(), src, []byte("\uFEFF\uFEFF{\"Test\": []}"))
	if !result.HasErrors() {
		t.Error("a second mark must not parse")
	}
}
