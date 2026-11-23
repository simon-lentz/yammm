package jsondata

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"
)

// TestParse_SkipsOneLeadingByteOrderMark pins that a data file starting
// with U+FEFF parses as the same document without it, and that a second mark is
// still a parse error.
func TestParse_SkipsOneLeadingByteOrderMark(t *testing.T) {
	t.Parallel()
	src := sourcetest.ID("data.json")
	parsed, result := Parse(context.Background(), src, []byte("\uFEFF{\"Test\": [{\"id\": \"a\"}]}"))
	if diagtest.Err(result) != nil {
		t.Fatalf("issues: %s", result.String())
	}
	if len(parsed["Test"].Elements) != 1 {
		t.Fatalf("parsed %d instances, want 1", len(parsed["Test"].Elements))
	}
	if got := parsed["Test"].Elements[0].Properties["id"]; got != "a" {
		t.Errorf("id = %v, want %q", got, "a")
	}

	_, result = Parse(context.Background(), src, []byte("\uFEFF\uFEFF{\"Test\": []}"))
	if diagtest.Err(result) == nil {
		t.Error("a second mark must not parse")
	}
}
