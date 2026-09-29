package csv

import (
	"slices"
	"strings"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A group the validator reads as a shadowed key is text, however its cells
// count: splitting it is a decision about the association, which the key does
// not claim. So its disagreeing counts are no fault, and the validator reports
// the key as unknown, as it reports the JSON object holding that text.
func TestColumnMapping_AShadowedGroupIsNotSplit(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), nameResolutionSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	person, _ := s.Type("Person")
	const csvText = "id,name,works_at,WORKS_AT._target_company_id,WORKS_AT.since\np1,Ann,x,c1|c2,5\n"
	const doc = `{"id":"p1","name":"Ann","works_at":"x","WORKS_AT":{"_target_company_id":"c1|c2","since":"5"}}`
	byType, _ := jsonadapter.New().ParseObject(t.Context(), location.NewSourceID("p.json"), []byte(`{"Person":[`+doc+`]}`))
	raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person", strings.NewReader(csvText), person)
	if pres.HasErrors() {
		t.Errorf("parse: %s", pres)
	}
	if a, b := verdict(t, s, raws[0]), verdict(t, s, byType["Person"][0]); !slices.Equal(a, b) {
		t.Errorf("the validator answers the two differently\ncsv:  %q\njson: %q", a, b)
	}
}
