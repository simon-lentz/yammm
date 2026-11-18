package cli

import (
	"reflect"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"
	"github.com/simon-lentz/yammm/location"
)

// TestRestampIndex_RewritesTheIndexAlone pins that the rewrite of an instance
// index keeps every other field of the issue, and returns an issue whose index
// is its element's own unchanged.
func TestRestampIndex_RewritesTheIndexAlone(t *testing.T) {
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
	restamp := restampIndex([]int{0, 2})
	if got, want := restamp(issue("1")), issue("2"); !reflect.DeepEqual(got, want) {
		t.Errorf("restamped %#v\nwant %#v", got, want)
	}
	if got, want := restamp(issue("0")), issue("0"); !reflect.DeepEqual(got, want) {
		t.Errorf("an unmoved index became %#v", got)
	}
}
