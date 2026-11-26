package cli

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// A snapshot read's issue with no span is placed in the file read; an issue
// that has a span keeps it, and an I/O failure, a cancellation and an internal
// fault are no fact of the file and stay unplaced. A path no identity names is
// named by its quoted spelling.
func TestPlaceIn_PlacesOnlyAFileFactWithNoSpan(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "x.ys")
	want, err := location.SourceIDFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	other, err := location.NewSourceID("other")
	if err != nil {
		t.Fatal(err)
	}
	spanned := location.Span{Source: other, Start: location.Position{Line: 3, Column: 1}, End: location.Position{Line: 3, Column: 2}}
	c := diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, "file fact").Build())
	c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, "spanned").WithSpan(spanned).Build())
	for _, code := range []diag.Code{diag.E_SNAPSHOT_IO, diag.E_CONTEXT_CANCELLED, diag.E_INTERNAL} {
		c.Collect(diag.NewIssue(diag.Error, code, code.String()).Build())
	}
	for iss := range PlaceIn(path, c.Result()).Issues() {
		switch iss.Message() {
		case "file fact":
			if !iss.HasSpan() || iss.Span().Source != want || iss.Span().Start != (location.Position{}) {
				t.Errorf("a file fact is placed at %v, want %s with no position", iss.Span(), want)
			}
		case "spanned":
			if iss.Span() != spanned {
				t.Errorf("a spanned issue moved to %v", iss.Span())
			}
		default:
			if iss.HasSpan() {
				t.Errorf("%s is placed at %v, want no span", iss.Code(), iss.Span())
			}
		}
	}

	unnamed := "bad\xff.ys"
	if _, err := location.SourceIDFromPath(unnamed); err == nil {
		t.Fatalf("%q has an identity; the case needs a path none names", unnamed)
	}
	c = diag.NewCollector(diag.NoLimit)
	c.Collect(diag.NewIssue(diag.Error, diag.E_SNAPSHOT_MALFORMED, "file fact").Build())
	for iss := range PlaceIn(unnamed, c.Result()).Issues() {
		if !iss.HasSpan() || iss.Span().Source.String() != strconv.Quote(unnamed) {
			t.Errorf("a path no identity names is placed at %v, want its quoted spelling", iss.Span())
		}
	}
}
