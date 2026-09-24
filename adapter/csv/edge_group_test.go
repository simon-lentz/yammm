package csv

import (
	"reflect"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const edgeGroupSchema = `schema "g"
type Firm { code String primary }
type Office { cid String primary }
type P {
	id String primary
	--> AT (_:many) Firm { a Integer
		b Integer
		tag String required }
	--> HOUSED (_:one) Office { cid String required
		note String }
}
`

func parseEdgeGroup(t *testing.T, csvText string) (map[string]any, string) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), edgeGroupSchema, "g.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	p, _ := s.Type("P")
	raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("g.csv"), "P", strings.NewReader(csvText), p)
	if len(raws) != 1 {
		t.Fatalf("%d instances: %s", len(raws), pres)
	}
	return raws[0].Properties, pres.String()
}

// Where three deciding columns disagree, the refusal names the first two in
// header order.
func TestEdgeGroup_AClashNamesTheFirstPair(t *testing.T) {
	t.Parallel()
	props, res := parseEdgeGroup(t, "id,at._target_code,at.a,at.b\np1,f1|f2,5,1|2|3\n")
	if _, kept := props["at"]; kept || !strings.Contains(res, `"at._target_code" holds 2, "at.a" holds 1`) {
		t.Errorf("got %v and %s, want the group dropped and the first pair named", props, res)
	}
}

// An empty segment under a folded spelling places its member's empty value
// only where that spelling is the member's one: two folded spellings of one
// member are both absent, as a JSON object holding neither is.
func TestEdgeGroup_TwoEmptyFoldedSpellingsPlaceNothing(t *testing.T) {
	t.Parallel()
	props, res := parseEdgeGroup(t, "id,at._target_code,at.TAG,at.Tag\np1,f1,,\n")
	want := []any{map[string]any{"_target_code": "f1"}}
	if !reflect.DeepEqual(props["at"], want) {
		t.Errorf("at = %#v, want %#v (%s)", props["at"], want, res)
	}
}

// A key component and an edge property of one name are two members: each
// empty folded spelling places its own member's empty value.
func TestEdgeGroup_AKeyAndAnEdgePropertyOfOneNameAreTwoMembers(t *testing.T) {
	t.Parallel()
	props, res := parseEdgeGroup(t, "id,housed._TARGET_CID,housed.CID,housed.note\np1,,,x\n")
	want := map[string]any{"_TARGET_CID": "", "CID": "", "note": "x"}
	if !reflect.DeepEqual(props["housed"], want) {
		t.Errorf("housed = %#v, want %#v (%s)", props["housed"], want, res)
	}
}
