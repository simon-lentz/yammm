package csv

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

const nameResolutionSchema = `schema "names"

type Company {
	company_id String primary
}

type Person {
	id String primary
	name String required
	age Integer
	nick String
	rank Integer
	startYear Integer
	--> WORKS_AT (_:one) Company {
		since Integer
		endYear Integer
		desk Integer
	}
	--> KNOWS (_:many) Company {
		weight Integer
		tag String required
	}
}
`

// verdict is the validator's answer for one instance under one mode: each
// diagnostic's code and message, sorted, with no position, since the CSV and
// JSON documents place one field at different lines and columns.
func verdict(t *testing.T, s *schema.Schema, raw instance.RawInstance, opts ...instance.Option) []string {
	t.Helper()
	raw.Provenance = nil
	_, res := instance.NewValidator(s, opts...).ValidateOne(context.Background(), "Person", raw)
	var out []string
	for issue := range res.Issues() {
		out = append(out, fmt.Sprintf("%s %s", issue.Code(), issue.Message()))
	}
	slices.Sort(out)
	return out
}

// A folding parser — the default, as the validator's default is — resolves a
// header name by the rule the validator applies to a JSON object's keys: exact
// first, then the ASCII fold, keys kept as the header spells them, over the keys
// each row holds. So a row parses as its JSON equivalent does, property for
// property, and a folding validator answers the two alike, with unknown fields
// refused or allowed. A row's JSON equivalent omits every empty cell the parser
// skips. The JSON adapter is the other implementation of the contract; each
// case runs with and without WithSchema.
func TestColumnMapping_AFoldingParserParsesAsTheJSONEquivalent(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), nameResolutionSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	person, _ := s.Type("Person")

	for _, c := range []struct {
		name, csv, json string
	}{
		{
			"a mis-cased property, coerced and emptied by its member",
			"id,Name,Age\np1,,42\n",
			`{"id":"p1","Name":"","Age":42}`,
		},
		{
			"a mis-cased optional property left empty",
			"id,name,NICK\np1,Ann,\n",
			`{"id":"p1","name":"Ann","NICK":null}`,
		},
		{
			"an exact column shadows a mis-cased one",
			"id,name,NAME\np1,Ann,Bo\n",
			`{"id":"p1","name":"Ann","NAME":"Bo"}`,
		},
		{
			"two mis-cased columns collide",
			"id,Name,NAME\np1,Ann,Bo\n",
			`{"id":"p1","Name":"Ann","NAME":"Bo"}`,
		},
		{
			"a mis-cased association, key component and edge property",
			"id,name,WORKS_AT._TARGET_COMPANY_ID,WORKS_AT.Since\np1,Ann,c1,2020\n",
			`{"id":"p1","name":"Ann","WORKS_AT":{"_TARGET_COMPANY_ID":"c1","Since":2020}}`,
		},
		{
			"an exact association spelling shadows a mis-cased one",
			"id,name,works_at._target_company_id,WORKS_AT.since\np1,Ann,c1,2020\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"c1"},"WORKS_AT":{"since":"2020"}}`,
		},
		{
			"two mis-cased association spellings collide",
			"id,name,WORKS_AT._target_company_id,Works_At.since\np1,Ann,c1,2020\n",
			`{"id":"p1","name":"Ann","WORKS_AT":{"_target_company_id":"c1"},"Works_At":{"since":"2020"}}`,
		},
		{
			"a mis-cased edge property shadowed by an exact one",
			"id,name,works_at._target_company_id,works_at.since,works_at.SINCE\np1,Ann,c1,2020,2021\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"c1","since":2020,"SINCE":"2021"}}`,
		},
		{
			"a header name is matched as written, never trimmed",
			"id,name, age\np1,Ann,42\n",
			`{"id":"p1","name":"Ann"," age":"42"}`,
		},
		{
			"a folded pair with one cell empty: the filled one resolves",
			"id,name,Age,AGE\np1,Ann,42,\n",
			`{"id":"p1","name":"Ann","Age":42}`,
		},
		{
			"a folded pair both filled collides",
			"id,name,Age,AGE\np1,Ann,7,8\n",
			`{"id":"p1","name":"Ann","Age":"7","AGE":"8"}`,
		},
		{
			"an exact column claims its member, and a folded one beside it is passed on",
			"id,name,age,AGE\np1,Ann,7,8\n",
			`{"id":"p1","name":"Ann","age":7,"AGE":"8"}`,
		},
		{
			"a name holding a non-ASCII letter folds to nothing",
			"id,name,RAN\u212a\np1,Ann,3\n",
			`{"id":"p1","name":"Ann","RAN\u212a":"3"}`,
		},
		{
			"a mis-cased column folds onto a camel-case member",
			"id,name,STARTYEAR,works_at._target_company_id,works_at.ENDYEAR\np1,Ann,1999,c1,2021\n",
			`{"id":"p1","name":"Ann","STARTYEAR":1999,"works_at":{"_target_company_id":"c1","ENDYEAR":2021}}`,
		},
		{
			"the exact association spelling is absent in this row, so the mis-cased one resolves",
			"id,name,works_at._target_company_id,works_at.since,WORKS_AT._target_company_id,WORKS_AT.since\np1,Ann,,,c1,2020\n",
			`{"id":"p1","name":"Ann","WORKS_AT":{"_target_company_id":"c1","since":2020}}`,
		},
		{
			"two mis-cased association spellings, one group empty: the filled one resolves",
			"id,name,WORKS_AT._target_company_id,WORKS_AT.since,Works_At._target_company_id\np1,Ann,c1,2020,\n",
			`{"id":"p1","name":"Ann","WORKS_AT":{"_target_company_id":"c1","since":2020}}`,
		},
		{
			"a passed association group skips its empty cell",
			"id,name,WORKS_AT._target_company_id,WORKS_AT.since,Works_At._target_company_id,Works_At.since\np1,Ann,c1,,c2,2020\n",
			`{"id":"p1","name":"Ann","WORKS_AT":{"_target_company_id":"c1"},"Works_At":{"_target_company_id":"c2","since":"2020"}}`,
		},
		{
			"the exact edge property is absent on the target, so the mis-cased one resolves",
			"id,name,works_at._target_company_id,works_at.since,works_at.SINCE\np1,Ann,c1,,2020\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"c1","SINCE":2020}}`,
		},
		{
			"a folded edge-property pair with one cell empty: the filled one resolves",
			"id,name,works_at._target_company_id,works_at.Since,works_at.SINCE\np1,Ann,c1,2020,\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"c1","Since":2020}}`,
		},
		{
			"each target of a (many) association decides its own claims",
			"id,name,knows._target_company_id,knows.weight,knows.WEIGHT\np1,Ann,c1|c2,5|,|6\n",
			`{"id":"p1","name":"Ann","knows":[{"_target_company_id":"c1","weight":5},{"_target_company_id":"c2","WEIGHT":6}]}`,
		},
		{
			"an exact column claims its member with an empty cell too",
			"id,name,age,AGE\np1,Ann,,8\n",
			`{"id":"p1","name":"Ann","age":null,"AGE":"8"}`,
		},
		{
			"an exact edge property with an empty value claims it on the target",
			"id,name,knows._target_company_id,knows.tag,knows.TAG\np1,Ann,c1,,x\n",
			`{"id":"p1","name":"Ann","knows":[{"_target_company_id":"c1","tag":"","TAG":"x"}]}`,
		},
		{
			"an exact association spelling claims ahead of a mis-cased one, whatever its values",
			"id,name,works_at._target_company_id,works_at.since,WORKS_AT.since\np1,Ann,c1,2020,2021\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"c1","since":2020},"WORKS_AT":{"since":"2021"}}`,
		},
		{
			"a folded association spelling carries a suffix it cannot place as text",
			"id,name,WORKS_AT._target_company_id,WORKS_AT.bogus\np1,Ann,c1,x\n",
			`{"id":"p1","name":"Ann","WORKS_AT":{"_target_company_id":"c1","bogus":"x"}}`,
		},
		{
			"a lone mis-cased edge column left empty is absent, as its member is",
			"id,name,works_at._target_company_id,works_at.SINCE\np1,Ann,c1,\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"c1"}}`,
		},
		{
			"a name holding a non-ASCII letter folds to nothing at an association spelling",
			"id,name,WOR\u212aS_AT._target_company_id\np1,Ann,c1\n",
			`{"id":"p1","name":"Ann","WOR\u212aS_AT":{"_target_company_id":"c1"}}`,
		},
		{
			"a name holding a non-ASCII letter folds to nothing at a suffix",
			"id,name,works_at._target_company_id,works_at.DES\u212a\np1,Ann,c1,4\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"c1","DES\u212a":"4"}}`,
		},
		{
			"an exact spelling whose only value names no member still claims its field",
			"id,name,works_at.bogus,WORKS_AT._target_company_id,WORKS_AT.since\np1,Ann,x,c1,2020\n",
			`{"id":"p1","name":"Ann","works_at":{"bogus":"x"},"WORKS_AT":{"_target_company_id":"c1","since":"2020"}}`,
		},
		{
			"a dotted field naming no association is carried as an object",
			"id,name,nosuch.a,nosuch.b\np1,Ann,1,2\n",
			`{"id":"p1","name":"Ann","nosuch":{"a":"1","b":"2"}}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			byType, jres := jsonadapter.New().ParseObject(t.Context(), location.NewSourceID("p.json"),
				[]byte(`{"Person":[`+c.json+`]}`))
			if jres.HasErrors() {
				t.Fatalf("json parse: %s", jres.String())
			}
			fromJSON := byType["Person"][0]
			for adapter, a := range map[string]*Adapter{"WithSchema": New(WithSchema(s)), "without WithSchema": New()} {
				raws, pres := a.ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person",
					strings.NewReader(c.csv), person)
				if pres.HasErrors() || len(raws) != 1 {
					t.Fatalf("%s: csv parse: %d instances, %s", adapter, len(raws), pres.String())
				}
				if !reflect.DeepEqual(raws[0].Properties, fromJSON.Properties) {
					t.Errorf("%s: properties differ\ncsv:  %#v\njson: %#v", adapter, raws[0].Properties, fromJSON.Properties)
				}
				for _, mode := range []struct {
					name string
					opts []instance.Option
				}{
					{"fold", nil},
					{"fold, unknown fields allowed", []instance.Option{instance.WithAllowUnknownFields(true)}},
				} {
					fromCSV, fromJSONVerdict := verdict(t, s, raws[0], mode.opts...), verdict(t, s, fromJSON, mode.opts...)
					if !slices.Equal(fromCSV, fromJSONVerdict) {
						t.Errorf("%s, %s mode: the validator answers the two differently\ncsv:  %q\njson: %q",
							adapter, mode.name, fromCSV, fromJSONVerdict)
					}
				}
			}
		})
	}
}

// Each row type of a multi-type file resolves the header by its own members.
func TestColumnMapping_EachRowTypeResolvesTheHeaderItself(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), nameResolutionSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	in := "kind,id,company_id,AGE\nCompany,,C1,\nPerson,p1,,7\n"
	byType, pres := New(WithTypeColumn("kind"), WithSchema(s)).ParseWithTypeColumn(t.Context(),
		location.NewSourceID("mixed.csv"), strings.NewReader(in), func(name string) *schema.Type {
			typ, _ := s.Type(name)
			return typ
		})
	if pres.HasErrors() {
		t.Fatalf("parse: %s", pres.String())
	}
	if got := byType["Person"][0].Properties["AGE"]; got != int64(7) {
		t.Errorf("Person AGE = %#v, want the Integer 7 its folded member coerces", got)
	}
	if got, has := byType["Company"][0].Properties["AGE"]; has {
		t.Errorf("Company holds AGE = %#v; an empty column it does not declare is skipped", got)
	}
	if got := byType["Company"][0].Properties["company_id"]; got != "C1" {
		t.Errorf("Company company_id = %#v", got)
	}
}

// An exact spelling claims its association only when its group writes an
// object: a group whose columns disagree on the target count writes none, so
// the mis-cased spelling is the key the validator sees and resolves to the
// association. The parse reports the fault in the exact group; the validator
// adds none of its own.
func TestColumnMapping_AGroupThatWritesNothingClaimsNothing(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), nameResolutionSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	person, _ := s.Type("Person")
	for _, c := range []struct{ name, csv, fault string }{
		{
			"a target-count disagreement", "id,name,works_at._target_company_id,works_at.since,WORKS_AT._target_company_id,WORKS_AT.since\np1,Ann,a|b,1,c1,2020\n",
			`association "works_at" columns disagree on target count`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person",
				strings.NewReader(c.csv), person)
			if pres.Len() != 1 {
				t.Fatalf("want the one fault in the exact group, got %s", pres)
			}
			if _, ok := issueContaining(pres, c.fault); !ok {
				t.Errorf("no diagnostic containing %q: %s", c.fault, pres)
			}
			want := map[string]any{"_target_company_id": "c1", "since": int64(2020)}
			if got := raws[0].Properties["WORKS_AT"]; !mapEqual(got, want) {
				t.Errorf("WORKS_AT = %#v, want %#v", got, want)
			}
			if _, has := raws[0].Properties["works_at"]; has {
				t.Errorf("the exact group wrote %#v", raws[0].Properties["works_at"])
			}
			if v := verdict(t, s, raws[0]); len(v) != 0 {
				t.Errorf("the validator reports %q", v)
			}
		})
	}
}

// A key component resolves by the same claim rule, which only WithSchema can
// apply, since it needs the target's keys: an exact key column claims its
// member, an empty one included, since a String key's empty value is "".
func TestColumnMapping_AKeyComponentClaimsByTheRule(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), nameResolutionSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	person, _ := s.Type("Person")
	for _, c := range []struct{ name, csv, json string }{
		{
			"an exact key column claims ahead of a mis-cased one",
			"id,name,works_at._target_company_id,works_at._TARGET_COMPANY_ID\np1,Ann,c1,c2\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"c1","_TARGET_COMPANY_ID":"c2"}}`,
		},
		{
			"an empty exact key column claims its member too",
			"id,name,works_at._target_company_id,works_at._TARGET_COMPANY_ID\np1,Ann,,c2\n",
			`{"id":"p1","name":"Ann","works_at":{"_target_company_id":"","_TARGET_COMPANY_ID":"c2"}}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			raws, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person",
				strings.NewReader(c.csv), person)
			if pres.HasErrors() {
				t.Fatalf("csv parse: %s", pres)
			}
			byType, jres := jsonadapter.New().ParseObject(t.Context(), location.NewSourceID("p.json"),
				[]byte(`{"Person":[`+c.json+`]}`))
			if jres.HasErrors() {
				t.Fatalf("json parse: %s", jres)
			}
			if got, want := raws[0].Properties, byType["Person"][0].Properties; !reflect.DeepEqual(got, want) {
				t.Errorf("properties differ\ncsv:  %#v\njson: %#v", got, want)
			}
		})
	}
}

// A strict parser, given the validator's WithStrictPropertyNames(true), matches
// every name exactly, so a mis-cased name is the strict validator's unknown
// field: its value is carried as written and never coerced, and its empty cell
// is skipped, as the JSON object that omits the key is. Exact names resolve as
// they do under the fold.
func TestColumnMapping_AStrictParserParsesAsTheJSONEquivalent(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), nameResolutionSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	person, _ := s.Type("Person")
	for _, c := range []struct{ name, csv, json string }{
		{"an empty mis-cased column is skipped", "id,name,NICK\np1,Ann,\n", `{"id":"p1","name":"Ann"}`},
		{"a filled mis-cased column is carried as written", "id,name,Age\np1,Ann,abc\n", `{"id":"p1","name":"Ann","Age":"abc"}`},
		{
			"exact names resolve and coerce", "id,name,age,works_at._target_company_id,works_at.since\np1,Ann,7,c1,2020\n",
			`{"id":"p1","name":"Ann","age":7,"works_at":{"_target_company_id":"c1","since":2020}}`,
		},
		{
			"a mis-cased association spelling is carried as text", "id,name,WORKS_AT._target_company_id,WORKS_AT.since\np1,Ann,c1,2020\n",
			`{"id":"p1","name":"Ann","WORKS_AT":{"_target_company_id":"c1","since":"2020"}}`,
		},
		{
			"a mis-cased suffix is carried as text", "id,name,works_at._TARGET_COMPANY_ID,works_at.SINCE\np1,Ann,c1,2020\n",
			`{"id":"p1","name":"Ann","works_at":{"_TARGET_COMPANY_ID":"c1","SINCE":"2020"}}`,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			byType, jres := jsonadapter.New().ParseObject(t.Context(), location.NewSourceID("p.json"),
				[]byte(`{"Person":[`+c.json+`]}`))
			if jres.HasErrors() {
				t.Fatalf("json parse: %s", jres.String())
			}
			fromJSON := byType["Person"][0]
			for adapter, a := range map[string]*Adapter{
				"WithSchema":         New(WithSchema(s), WithStrictPropertyNames(true)),
				"without WithSchema": New(WithStrictPropertyNames(true)),
			} {
				raws, pres := a.ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person",
					strings.NewReader(c.csv), person)
				if pres.HasErrors() || len(raws) != 1 {
					t.Fatalf("%s: csv parse: %d instances, %s", adapter, len(raws), pres.String())
				}
				if !reflect.DeepEqual(raws[0].Properties, fromJSON.Properties) {
					t.Errorf("%s: properties differ\ncsv:  %#v\njson: %#v", adapter, raws[0].Properties, fromJSON.Properties)
				}
				for _, mode := range []struct {
					name string
					opts []instance.Option
				}{
					{"strict", []instance.Option{instance.WithStrictPropertyNames(true)}},
					{"strict, unknown fields allowed", []instance.Option{
						instance.WithStrictPropertyNames(true), instance.WithAllowUnknownFields(true),
					}},
				} {
					fromCSV, fromJSONVerdict := verdict(t, s, raws[0], mode.opts...), verdict(t, s, fromJSON, mode.opts...)
					if !slices.Equal(fromCSV, fromJSONVerdict) {
						t.Errorf("%s, %s mode: the validator answers the two differently\ncsv:  %q\njson: %q",
							adapter, mode.name, fromCSV, fromJSONVerdict)
					}
				}
			}
		})
	}
}

// A value that does not coerce is reported alike whether its column names its
// member exactly or by fold: a folding parser serves a folding validator, which
// reads both names. The text is kept for the validator either way.
func TestColumnMapping_AFoldedValueThatDoesNotCoerceIsReportedAsAnExactOneIs(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), nameResolutionSchema, "names.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	person, _ := s.Type("Person")
	for _, c := range []struct{ csv, want string }{
		{"id,name,age\np1,Ann,abc\n", `column "age": cannot parse "abc" as Integer`},
		{"id,name,Age\np1,Ann,abc\n", `column "Age": cannot parse "abc" as Integer`},
		{"id,name,works_at._target_company_id,works_at.SINCE\np1,Ann,c1,soon\n", `column "works_at".SINCE: cannot parse "soon" as Integer`},
		{"id,name,WORKS_AT._target_company_id,WORKS_AT.since\np1,Ann,c1,soon\n", `column "WORKS_AT".since: cannot parse "soon" as Integer`},
	} {
		_, pres := New(WithSchema(s)).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person",
			strings.NewReader(c.csv), person)
		if issue, ok := issueContaining(pres, c.want); !ok || issue.Code() != E_CSV_COERCE {
			t.Errorf("%q: want E_CSV_COERCE containing %q, got %s", c.csv, c.want, pres)
		}
	}
}
