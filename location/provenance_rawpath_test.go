package location

import (
	"testing"

	"github.com/simon-lentz/yammm/location/path"
)

func TestProvenance_WithRawPathReturnsACopyThatKeepsTheSpan(t *testing.T) {
	span := Span{Start: Position{Line: 7, Column: 2, Byte: 40}, End: Position{Line: 7, Column: 9, Byte: 47}}
	a := NewProvenance("f", path.Root().Key("k"), span)
	if a.HasRawPath() {
		t.Error("a Provenance built with a path holds no raw path")
	}
	b := a.WithRawPath("$.raw")
	if a.HasRawPath() || a.RawPath() != "" {
		t.Errorf("WithRawPath moved its receiver: %q", a.RawPath())
	}
	if !b.HasRawPath() || b.RawPath() != "$.raw" {
		t.Errorf("the copy holds %v, %q", b.HasRawPath(), b.RawPath())
	}
	if b.Span() != span || b.SourceName() != "f" {
		t.Errorf("the copy holds span %v, source %q", b.Span(), b.SourceName())
	}
}
