package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"
	"github.com/simon-lentz/yammm/location"
)

func TestProvenance_TheZeroHandleAnswersAsTheRoot(t *testing.T) {
	var zero instance.Provenance
	if !zero.IsZero() {
		t.Error("the zero Provenance must report zero")
	}
	if got := zero.SourceName(); got != "" {
		t.Errorf("SourceName = %q, want empty", got)
	}
	if got := zero.Path(); got != path.Root {
		t.Errorf("Path = %q, want %q", got, path.Root)
	}
	if got := zero.Span(); !got.IsZero() {
		t.Errorf("Span = %v, want the zero span", got)
	}
}

// TestProvenance_KeepsWhatItWasGiven holds a record to its three parts as given:
// the path verbatim, including text the path methods would spell another way
// and the empty text, which is not the root.
func TestProvenance_KeepsWhatItWasGiven(t *testing.T) {
	span := location.Span{
		Source: sourcetest.ID("test://data.json"),
		Start:  location.Position{Line: 7, Column: 2, Offset: 40},
		End:    location.Position{Line: 7, Column: 9, Offset: 47},
	}
	for _, p := range []path.Path{path.Root.Key("Item"), `$["Item"]`, "$.Item[special=format]", ""} {
		prov := instance.NewProvenance("data.json", p, span)
		if prov.IsZero() {
			t.Fatalf("NewProvenance(%q) is zero", p)
		}
		if got := prov.Path(); got != p {
			t.Errorf("Path = %q, want %q", got, p)
		}
		if got := prov.SourceName(); got != "data.json" {
			t.Errorf("SourceName = %q, want data.json", got)
		}
		if got := prov.Span(); got != span {
			t.Errorf("Span = %v, want %v", got, span)
		}
	}
}
