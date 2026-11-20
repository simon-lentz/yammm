package cli

import (
	"reflect"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"
	"github.com/simon-lentz/yammm/location"
)

// TestAnchorIssue_RewritesTheIndexAlone pins that the rewrite of an instance
// index keeps every other field of the issue, and returns an issue whose index
// is its element's own unchanged.
func TestAnchorIssue_RewritesTheIndexAlone(t *testing.T) {
	t.Parallel()
	source := sourcetest.ID("d.json")
	span := location.Span{Source: source, Start: location.Position{Line: 1, Column: 2}, End: location.Position{Line: 1, Column: 9}}
	issue := func(index string) diag.Issue {
		return diag.NewIssue(diag.Error, diag.E_MISSING_REQUIRED, "missing name").
			WithSpan(span).
			WithPath("d.json", "$.Person[2]").
			WithHint("add it").
			WithRelated(diag.RelatedInfo{Span: span, Message: "declared here"}).
			WithDetail(diag.DetailKeyTypeName, "Person").
			WithDetail(diag.DetailKeyInstanceIndex, index).
			Build()
	}
	restamp := anchorIssue(source, []int{0, 2})
	if got, want := restamp(issue("1")), issue("2"); !reflect.DeepEqual(got, want) {
		t.Errorf("restamped %#v\nwant %#v", got, want)
	}
	if got, want := restamp(issue("0")), issue("0"); !reflect.DeepEqual(got, want) {
		t.Errorf("an unmoved index became %#v", got)
	}
}

// TestAnchorIssue_PlacesATypeIssueAtTheSourceRoot pins that an issue naming no
// element keeps its code, message and details, and stands at the root of the
// file that states the type, with no span.
func TestAnchorIssue_PlacesATypeIssueAtTheSourceRoot(t *testing.T) {
	t.Parallel()
	source := sourcetest.ID("d.json")
	elsewhere := location.Span{Source: sourcetest.ID("e.json"), Start: location.Position{Line: 3, Column: 1}, End: location.Position{Line: 3, Column: 4}}
	issue := diag.NewIssue(diag.Error, diag.E_INSTANCE_TYPE_NOT_FOUND, "no type Nope").
		WithSpan(elsewhere).
		WithPath("e.json", "$.Nope[0]").
		WithDetail(diag.DetailKeyTypeName, "Nope").
		Build()
	want := diag.NewIssue(diag.Error, diag.E_INSTANCE_TYPE_NOT_FOUND, "no type Nope").
		WithPath(source.String(), "$").
		WithDetail(diag.DetailKeyTypeName, "Nope").
		Build()
	if got := anchorIssue(source, []int{0})(issue); !reflect.DeepEqual(got, want) {
		t.Errorf("anchored %#v\nwant %#v", got, want)
	}
}
