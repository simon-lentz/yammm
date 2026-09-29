package csv

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// errorCodes is every Error-or-worse code in res, sorted.
func errorCodes(res diag.Result) []string {
	var out []string
	for issue := range res.Issues() {
		if issue.Severity() >= diag.Error {
			out = append(out, issue.Code().String())
		}
	}
	slices.Sort(out)
	return out
}

// Each row holds the invariant the column-mapping rule states: the object a
// row makes is the object the JSON adapter makes from the equivalent document,
// and the validator answers the two alike. csvErrors is the parse's own
// Error codes beyond the JSON document's: E_CSV_COERCE for text that does not
// coerce, which JSON has no counterpart for.
func TestColumnMapping_EachNamedMemberParsesAsTheJSONEquivalent(t *testing.T) {
	t.Parallel()
	const (
		composition = `schema "c"
part type Address { street String
	zip Integer }
type Person { id String primary
	*-> ADDRESS (many) Address }
`
		property = `schema "p"
type Person { id String primary
	name String required }
`
		optional = `schema "o"
type Person { id String primary
	name String }
`
		relation = `schema "r"
type Company { company_id String primary }
type Person { id String primary
	age Integer
	--> WORKS_AT (_:one) Company { since Integer } }
`
		firm = `schema "f"
type Firm { code String primary }
type Person { id String primary
	--> HIRED_BY (_:one) Firm { w Integer } }
`
		composite = `schema "k"
type Company { cid String primary
	d Date primary }
type Person { id String primary
	--> WORKS_AT (_:one) Company { since Integer } }
`
	)
	fold := []instance.Option{}
	allow := []instance.Option{instance.WithAllowUnknownFields(true)}
	strict := []instance.Option{instance.WithStrictPropertyNames(true)}
	for _, c := range []struct {
		row, schema, csv, json string
		strict                 bool
		csvErrors              []string
	}{
		{
			row: "a composition group with a list separator is one object", schema: composition, csv: "id,address.street\np1,Main|Elm\n",
			json: `{"id":"p1","address":{"street":"Main|Elm"}}`,
		},
		{
			row: "a composition group with one segment is one object", schema: composition, csv: "id,address.street\np1,Main\n",
			json: `{"id":"p1","address":{"street":"Main"}}`,
		},
		{
			row: "a composition group keeps an Integer member as text", schema: composition, csv: "id,address.street,address.zip\np1,Main|Elm,12345|6\n",
			json: `{"id":"p1","address":{"street":"Main|Elm","zip":"12345|6"}}`,
		},
		{
			row: "a plain column beside a group of a property keeps the plain value", schema: property, csv: "id,name,name.first\np1,Ann,x\n",
			json: `{"id":"p1","name":{"first":"x"},"name":"Ann"}`,
		},
		{
			row: "an empty optional plain cell beside a group holds no value", schema: optional, csv: "id,name,name.first\np1,,x\n",
			json: `{"id":"p1","name":{"first":"x"}}`,
		},
		{
			row: "an exact plain key shadows a folded group", schema: relation,
			csv:  "id,works_at,Works_at._target_company_id,Works_at.since\np1,x,c1,soon\n",
			json: `{"id":"p1","works_at":"x","Works_at":{"_target_company_id":"c1","since":"soon"}}`,
		},
		{
			row: "a folded plain key and a folded group collide", schema: relation,
			csv:  "id,Works_At,WORKS_AT._target_company_id,WORKS_AT.since\np1,x,c1,soon\n",
			json: `{"id":"p1","Works_At":"x","WORKS_AT":{"_target_company_id":"c1","since":"soon"}}`,
		},
		{
			row: "a property's folded plain key and folded group collide", schema: relation,
			csv:  "id,AGE,Age.x\np1,abc,y\n",
			json: `{"id":"p1","AGE":"abc","Age":{"x":"y"}}`,
		},
		{
			row: "a column naming no member does not veto the group", schema: firm, csv: "id,hired_by._target_code,hired_by.note\np1,f1,a|b\n",
			json: `{"id":"p1","hired_by":{"_target_code":"f1","note":"a|b"}}`,
		},
		{
			row: "an exact key column is a key component under strict names", schema: composite, strict: true,
			csv:       "id,works_at._target_cid,works_at._target_d,works_at.since\np1,,notadate,2020\n",
			json:      `{"id":"p1","works_at":{"_target_cid":"","_target_d":"notadate","since":2020}}`,
			csvErrors: []string{"E_CSV_COERCE"},
		},
		{
			row: "a shadowed spelling does not decide the target count", schema: relation,
			csv:  "id,works_at._target_company_id,works_at.since,works_at.SINCE\np1,c1,5,5|6\n",
			json: `{"id":"p1","works_at":{"_target_company_id":"c1","since":5,"SINCE":"5|6"}}`,
		},
		{
			row: "a colliding pair does not decide the target count", schema: relation,
			csv:  "id,works_at._target_company_id,works_at.Since,works_at.SINCE\np1,c1,5,5|6\n",
			json: `{"id":"p1","works_at":{"_target_company_id":"c1","Since":"5","SINCE":"5|6"}}`,
		},
		{
			row: "a mis-cased key column is text under strict names", schema: relation, strict: true,
			csv:  "id,works_at._target_company_id,works_at._target_Company_id\np1,c1,a|b\n",
			json: `{"id":"p1","works_at":{"_target_company_id":"c1","_target_Company_id":"a|b"}}`,
		},
		{
			row: "a lone mis-cased key column is text under strict names", schema: relation, strict: true,
			csv:  "id,works_at._target_Company_id\np1,a|b\n",
			json: `{"id":"p1","works_at":{"_target_Company_id":"a|b"}}`,
		},
		{
			row: "a column taken whole at one target keeps its escapes", schema: firm,
			csv:  "id,hired_by._target_code,hired_by.note\np1,f1,a\\|b|c\n",
			json: `{"id":"p1","hired_by":{"_target_code":"f1","note":"a\\|b|c"}}`,
		},
		{
			row: "a group of a field naming nothing is one object", schema: optional, csv: "id,name,x.a,x.b\np1,n,1|2,1\n",
			json: `{"id":"p1","name":"n","x":{"a":"1|2","b":"1"}}`,
		},
	} {
		t.Run(c.row, func(t *testing.T) {
			t.Parallel()
			s, res := schema.LoadString(t.Context(), c.schema, "s.yammm")
			if res.HasErrors() {
				t.Fatalf("load schema: %s", res)
			}
			person, _ := s.Type("Person")
			byType, jres := jsonadapter.New().ParseObject(t.Context(), location.NewSourceID("p.json"),
				[]byte(`{"Person":[`+c.json+`]}`))
			fromJSON := byType["Person"][0]
			raws, pres := New(WithSchema(s), WithStrictPropertyNames(c.strict)).ParseTyped(t.Context(),
				location.NewSourceID("p.csv"), "Person", strings.NewReader(c.csv), person)
			if len(raws) != 1 {
				t.Fatalf("csv parse: %d instances, %s", len(raws), pres)
			}
			if !reflect.DeepEqual(raws[0].Properties, fromJSON.Properties) {
				t.Errorf("properties differ\ncsv:  %#v\njson: %#v", raws[0].Properties, fromJSON.Properties)
			}
			wantCodes := slices.Sorted(slices.Values(append(errorCodes(jres), c.csvErrors...)))
			if got := errorCodes(pres); !slices.Equal(got, wantCodes) {
				t.Errorf("parse Error codes = %q, want %q\ncsv:  %s\njson: %s", got, wantCodes, pres, jres)
			}
			modes := [][]instance.Option{fold, allow}
			if c.strict {
				modes = [][]instance.Option{strict, append(slices.Clone(strict), allow...)}
			}
			for i, opts := range modes {
				a, b := verdict(t, s, raws[0], opts...), verdict(t, s, fromJSON, opts...)
				if !slices.Equal(a, b) {
					t.Errorf("mode %d: the validator answers the two differently\ncsv:  %q\njson: %q", i, a, b)
				}
			}
		})
	}
}
