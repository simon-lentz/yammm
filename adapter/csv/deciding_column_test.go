package csv

import (
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

// Edge properties of each kind: an optional String, an Integer and a Date hold
// nothing where their segment is empty, and a required String holds "". Site
// has a String and a Date key component, and Extra a property that is no key.
const decidingSchema = `schema "deciding"
type Firm { code String primary }
type Site { sid String primary
	d Date primary }
type Extra { id String primary
	code String }
type Person {
	id String primary
	--> KNOWS (_:many) Firm { tag String
		weight Integer
		day Date }
	--> HIRED_BY (_:one) Firm { tag String }
	--> RATES (_:many) Firm { label String required }
	--> BOOKED (_:many) Site
	--> REF (_:one) Extra
}
`

// decidingWant is what one row states under one mode: the members its JSON
// document holds beside the id, the parse's own Error codes, which that
// document draws none of, and the validator's codes for the document.
type decidingWant struct {
	members   string
	csvErrors []string
	verdict   []string
}

const (
	parseError  = "E_ADAPTER_PARSE"
	unknownEdge = "E_UNKNOWN_EDGE_FIELD"
)

func clash() decidingWant { return decidingWant{csvErrors: []string{parseError}} }

func holds(members string, verdict ...string) decidingWant {
	return decidingWant{members: members, verdict: verdict}
}

// A column that claims its member on a row decides that row's target count,
// whatever the header's other spellings of the member are: an empty column
// beside it changes nothing. Each row is held against the JSON document it
// states, under folding and strict names, with and without WithSchema, through
// both entry points. A nil noSchema or strictNoSchema is the mode before it.
func TestEdgeGroup_AClaimingColumnDecidesTheTargetCount(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), decidingSchema, "deciding.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	person, _ := s.Type("Person")

	const (
		knowsTag    = "knows._target_code,knows.tag,knows.TAG"
		knowsTags   = "knows._target_code,knows.TAG,knows.Tag"
		knowsWeight = "knows._target_code,knows.weight,knows.WEIGHT"
		knowsDay    = "knows._target_code,knows.day,knows.DAY"
		hiredTag    = "hired_by._target_code,hired_by.tag,hired_by.TAG"
		keyPair     = "knows._target_code,knows._TARGET_CODE"
		bookedKeys  = "booked._target_sid,booked._target_d,booked._TARGET_D"
		noMember    = "knows._target_code,knows.zzz"

		twoTagged = `"knows":[{"_target_code":"f1","TAG":"a"},{"_target_code":"f2","TAG":"b"}]`
		twoBare   = `"knows":[{"_target_code":"f1"},{"_target_code":"f2"}]`
		missingFK = "E_MISSING_FK_TARGET"
		partialFK = "E_PARTIAL_COMPOSITE_FK"
		collision = "E_CASE_FOLD_COLLISION"
		required  = "E_MISSING_REQUIRED"
		edgeShape = "E_EDGE_SHAPE_MISMATCH"
	)
	p := func(w decidingWant) *decidingWant { return &w }

	for _, row := range []struct {
		name, header, record string
		fold                 decidingWant
		noSchema             *decidingWant
		strict               decidingWant
		strictNoSchema       *decidingWant
	}{
		// An optional String: the exact column empty, the folded one filled.
		{
			name: "exact empty, folded holds two segments for one target", header: knowsTag, record: "f1,,a|b",
			fold:   clash(),
			strict: holds(`"knows":[{"_target_code":"f1","TAG":"a|b"}]`, unknownEdge),
		},
		{
			name: "exact empty, folded holds two segments for two targets", header: knowsTag, record: "f1|f2,,a|b",
			fold:   holds(twoTagged),
			strict: holds(twoTagged, unknownEdge, unknownEdge),
		},
		{
			name: "exact empty, folded holds three segments for two targets", header: knowsTag, record: "f1|f2,,a|b|c",
			fold:   clash(),
			strict: decidingWant{members: twoBare, csvErrors: []string{parseError}},
		},
		{
			name: "exact empty, folded holds one segment", header: knowsTag, record: "f1,,a",
			fold:   holds(`"knows":[{"_target_code":"f1","TAG":"a"}]`),
			strict: holds(`"knows":[{"_target_code":"f1","TAG":"a"}]`, unknownEdge),
		},
		{
			name: "exact empty, folded holds one segment with an escaped separator", header: knowsTag, record: `f1,,a\|b`,
			fold:   holds(`"knows":[{"_target_code":"f1","TAG":"a|b"}]`),
			strict: holds(`"knows":[{"_target_code":"f1","TAG":"a|b"}]`, unknownEdge),
		},
		{
			name: "exact empty, folded holds an escaped separator and a bare one", header: knowsTag, record: `f1,,a\|b|c`,
			fold:   clash(),
			strict: holds(`"knows":[{"_target_code":"f1","TAG":"a\\|b|c"}]`, unknownEdge),
		},
		// The reverse, and both filled.
		{
			name: "exact holds two segments for one target, folded empty", header: knowsTag, record: "f1,a|b,",
			fold: clash(), strict: clash(),
		},
		{
			name: "exact holds two segments for two targets, folded empty", header: knowsTag, record: "f1|f2,a|b,",
			fold:   holds(`"knows":[{"_target_code":"f1","tag":"a"},{"_target_code":"f2","tag":"b"}]`),
			strict: holds(`"knows":[{"_target_code":"f1","tag":"a"},{"_target_code":"f2","tag":"b"}]`),
		},
		{
			name: "exact filled shadows a folded column of two segments", header: knowsTag, record: "f1,x,a|b",
			fold:   holds(`"knows":[{"_target_code":"f1","tag":"x","TAG":"a|b"}]`, unknownEdge),
			strict: holds(`"knows":[{"_target_code":"f1","tag":"x","TAG":"a|b"}]`, unknownEdge),
		},
		{
			name: "exact empty on the second target alone, folded filled on both", header: knowsTag, record: "f1|f2,x|,a|b",
			fold:   holds(`"knows":[{"_target_code":"f1","tag":"x","TAG":"a"},{"_target_code":"f2","TAG":"b"}]`, unknownEdge),
			strict: holds(`"knows":[{"_target_code":"f1","tag":"x","TAG":"a"},{"_target_code":"f2","TAG":"b"}]`, unknownEdge, unknownEdge),
		},
		// Two folded spellings and no exact one.
		{
			name: "two folded, one holds two segments for one target", header: knowsTags, record: "f1,a|b,",
			fold:   clash(),
			strict: holds(`"knows":[{"_target_code":"f1","TAG":"a|b"}]`, unknownEdge),
		},
		{
			name: "two folded, one holds two segments for two targets", header: knowsTags, record: "f1|f2,a|b,",
			fold:   holds(twoTagged),
			strict: holds(twoTagged, unknownEdge, unknownEdge),
		},
		{
			name: "two folded, both filled", header: knowsTags, record: "f1,a|b,c",
			fold:   holds(`"knows":[{"_target_code":"f1","TAG":"a|b","Tag":"c"}]`, collision),
			strict: holds(`"knows":[{"_target_code":"f1","TAG":"a|b","Tag":"c"}]`, unknownEdge, unknownEdge),
		},
		{
			name: "two folded, each filled on its own target", header: knowsTags, record: "f1|f2,a|,|b",
			fold:   holds(`"knows":[{"_target_code":"f1","TAG":"a"},{"_target_code":"f2","Tag":"b"}]`),
			strict: holds(`"knows":[{"_target_code":"f1","TAG":"a"},{"_target_code":"f2","Tag":"b"}]`, unknownEdge, unknownEdge),
		},
		// An Integer and a Date, which have no empty value.
		{
			name: "Integer: exact empty, folded holds two segments for one target", header: knowsWeight, record: "f1,,5|6",
			fold:   clash(),
			strict: holds(`"knows":[{"_target_code":"f1","WEIGHT":"5|6"}]`, unknownEdge),
		},
		{
			name: "Integer: exact empty, folded holds two segments for two targets", header: knowsWeight, record: "f1|f2,,5|6",
			fold:   holds(`"knows":[{"_target_code":"f1","WEIGHT":5},{"_target_code":"f2","WEIGHT":6}]`),
			strict: holds(`"knows":[{"_target_code":"f1","WEIGHT":"5"},{"_target_code":"f2","WEIGHT":"6"}]`, unknownEdge, unknownEdge),
		},
		{
			name: "Integer: the folded column alone is filled", header: knowsWeight, record: ",,1|2",
			fold:           holds(`"knows":[{"_target_code":"","WEIGHT":1},{"_target_code":"","WEIGHT":2}]`),
			noSchema:       p(holds(`"knows":[{"WEIGHT":1},{"WEIGHT":2}]`, missingFK, missingFK)),
			strict:         holds(`"knows":[{"_target_code":"","WEIGHT":"1|2"}]`, unknownEdge),
			strictNoSchema: p(holds(`"knows":[{"WEIGHT":"1|2"}]`, missingFK, unknownEdge)),
		},
		{
			name: "Integer: two folded, one holds three segments for two targets", header: "knows._target_code,knows.Weight,knows.WEIGHT", record: "f1|f2,,1|2|3",
			fold:   clash(),
			strict: decidingWant{members: twoBare, csvErrors: []string{parseError}},
		},
		{
			name: "Date: exact empty, folded holds two segments for one target", header: knowsDay, record: "f1,,2020-01-01|2020-01-02",
			fold:   clash(),
			strict: holds(`"knows":[{"_target_code":"f1","DAY":"2020-01-01|2020-01-02"}]`, unknownEdge),
		},
		{
			name: "Date: exact empty, folded holds two segments for two targets", header: knowsDay, record: "f1|f2,,2020-01-01|2020-01-02",
			fold:   holds(`"knows":[{"_target_code":"f1","DAY":"2020-01-01"},{"_target_code":"f2","DAY":"2020-01-02"}]`),
			strict: holds(`"knows":[{"_target_code":"f1","DAY":"2020-01-01"},{"_target_code":"f2","DAY":"2020-01-02"}]`, unknownEdge, unknownEdge),
		},
		// A required String, whose empty exact column holds "" and so claims.
		{
			name: "required String: exact empty shadows the folded column", header: "rates._target_code,rates.label,rates.LABEL", record: "f1,,a|b",
			fold:   holds(`"rates":[{"_target_code":"f1","label":"","LABEL":"a|b"}]`, unknownEdge),
			strict: holds(`"rates":[{"_target_code":"f1","label":"","LABEL":"a|b"}]`, unknownEdge),
		},
		{
			name: "required String: two folded, one holds two segments for one target", header: "rates._target_code,rates.LABEL,rates.Label", record: "f1,a|b,",
			fold:   clash(),
			strict: holds(`"rates":[{"_target_code":"f1","LABEL":"a|b"}]`, required, unknownEdge),
		},
		{
			name: "required String: two folded, one holds two segments for two targets", header: "rates._target_code,rates.LABEL,rates.Label", record: "f1|f2,a|b,",
			fold:   holds(`"rates":[{"_target_code":"f1","LABEL":"a"},{"_target_code":"f2","LABEL":"b"}]`),
			strict: holds(`"rates":[{"_target_code":"f1","LABEL":"a"},{"_target_code":"f2","LABEL":"b"}]`, required, required, unknownEdge, unknownEdge),
		},
		// A (one) association.
		{
			name: "(one): exact empty, folded holds two segments for one target", header: hiredTag, record: "f1,,a|b",
			fold:   clash(),
			strict: holds(`"hired_by":{"_target_code":"f1","TAG":"a|b"}`, unknownEdge),
		},
		{
			name: "(one): exact empty, folded holds one segment", header: hiredTag, record: "f1,,a",
			fold:   holds(`"hired_by":{"_target_code":"f1","TAG":"a"}`),
			strict: holds(`"hired_by":{"_target_code":"f1","TAG":"a"}`, unknownEdge),
		},
		{
			name: "(one): two folded, one holds two segments for one target", header: "hired_by._target_code,hired_by.TAG,hired_by.Tag", record: "f1,a|b,",
			fold:   clash(),
			strict: holds(`"hired_by":{"_target_code":"f1","TAG":"a|b"}`, unknownEdge),
		},
		{
			name: "(one): exact empty, folded holds two segments for two targets", header: hiredTag, record: "f1|f2,,a|b",
			fold:   holds(`"hired_by":[{"_target_code":"f1","TAG":"a"},{"_target_code":"f2","TAG":"b"}]`, edgeShape),
			strict: holds(`"hired_by":[{"_target_code":"f1","TAG":"a"},{"_target_code":"f2","TAG":"b"}]`, edgeShape),
		},
		// A key component.
		{
			name: "String key: two folded, one holds two segments", header: "knows._Target_code,knows._TARGET_CODE", record: "c1|c2,",
			fold:   holds(`"knows":[{"_Target_code":"c1"},{"_Target_code":"c2"}]`),
			strict: holds(`"knows":[{"_Target_code":"c1|c2"}]`, missingFK, unknownEdge),
		},
		{
			name: "String key: exact empty holds the empty key, folded holds two segments", header: keyPair, record: ",f1|f2",
			fold:           holds(`"knows":[{"_target_code":"","_TARGET_CODE":"f1|f2"}]`, unknownEdge),
			noSchema:       p(holds(`"knows":[{"_TARGET_CODE":"f1"},{"_TARGET_CODE":"f2"}]`)),
			strict:         holds(`"knows":[{"_target_code":"","_TARGET_CODE":"f1|f2"}]`, unknownEdge),
			strictNoSchema: p(holds(`"knows":[{"_TARGET_CODE":"f1|f2"}]`, missingFK, unknownEdge)),
		},
		{
			name: "String key: exact filled, folded holds two segments", header: keyPair, record: "f1,a|b",
			fold:     holds(`"knows":[{"_target_code":"f1","_TARGET_CODE":"a|b"}]`, unknownEdge),
			noSchema: p(clash()),
			strict:   holds(`"knows":[{"_target_code":"f1","_TARGET_CODE":"a|b"}]`, unknownEdge),
		},
		{
			name: "Date key: exact empty, folded alone holds two segments", header: bookedKeys, record: ",,2020-01-01|2020-01-02",
			fold:           holds(`"booked":[{"_target_sid":"","_TARGET_D":"2020-01-01"},{"_target_sid":"","_TARGET_D":"2020-01-02"}]`),
			noSchema:       p(holds(`"booked":[{"_TARGET_D":"2020-01-01"},{"_TARGET_D":"2020-01-02"}]`, partialFK, partialFK)),
			strict:         holds(`"booked":[{"_target_sid":"","_TARGET_D":"2020-01-01|2020-01-02"}]`, partialFK, unknownEdge),
			strictNoSchema: p(holds(`"booked":[{"_TARGET_D":"2020-01-01|2020-01-02"}]`, missingFK, unknownEdge)),
		},
		{
			name: "Date key: exact empty, folded holds two segments for two targets", header: bookedKeys, record: "s1|s2,,2020-01-01|2020-01-02",
			fold:   holds(`"booked":[{"_target_sid":"s1","_TARGET_D":"2020-01-01"},{"_target_sid":"s2","_TARGET_D":"2020-01-02"}]`),
			strict: holds(`"booked":[{"_target_sid":"s1","_TARGET_D":"2020-01-01"},{"_target_sid":"s2","_TARGET_D":"2020-01-02"}]`, partialFK, partialFK, unknownEdge, unknownEdge),
		},
		{
			name: "Date key: exact empty, folded holds two segments for one target", header: bookedKeys, record: "s1,,2020-01-01|2020-01-02",
			fold:   clash(),
			strict: holds(`"booked":[{"_target_sid":"s1","_TARGET_D":"2020-01-01|2020-01-02"}]`, partialFK, unknownEdge),
		},
		// A column naming no member decides nothing.
		{
			name: "no member: two segments for one target are taken whole", header: noMember, record: "f1,x|y",
			fold:   holds(`"knows":[{"_target_code":"f1","zzz":"x|y"}]`, unknownEdge),
			strict: holds(`"knows":[{"_target_code":"f1","zzz":"x|y"}]`, unknownEdge),
		},
		{
			name: "no member: one segment with an escaped separator is unescaped", header: noMember, record: `f1,x\|y`,
			fold:   holds(`"knows":[{"_target_code":"f1","zzz":"x|y"}]`, unknownEdge),
			strict: holds(`"knows":[{"_target_code":"f1","zzz":"x|y"}]`, unknownEdge),
		},
		{
			name: "no member: an escaped separator and a bare one are taken whole", header: noMember, record: `f1,x\|y|z`,
			fold:   holds(`"knows":[{"_target_code":"f1","zzz":"x\\|y|z"}]`, unknownEdge),
			strict: holds(`"knows":[{"_target_code":"f1","zzz":"x\\|y|z"}]`, unknownEdge),
		},
		{
			name: "no member: a target property that is no key", header: "ref._target_id,ref._target_code", record: "x1,p|q",
			fold:           holds(`"ref":{"_target_id":"x1","_target_code":"p|q"}`, unknownEdge),
			noSchema:       p(clash()),
			strict:         holds(`"ref":{"_target_id":"x1","_target_code":"p|q"}`, unknownEdge),
			strictNoSchema: p(clash()),
		},
	} {
		for _, mode := range []struct {
			strict, withSchema bool
		}{{false, true}, {false, false}, {true, true}, {true, false}} {
			want := row.fold
			switch {
			case mode.strict && !mode.withSchema && row.strictNoSchema != nil:
				want = *row.strictNoSchema
			case mode.strict:
				want = row.strict
			case !mode.withSchema && row.noSchema != nil:
				want = *row.noSchema
			}
			for _, typeColumn := range []bool{false, true} {
				name := fmt.Sprintf("%s/strict=%v/schema=%v/typeColumn=%v", row.name, mode.strict, mode.withSchema, typeColumn)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					doc := `{"id":"p1"}`
					if want.members != "" {
						doc = `{"id":"p1",` + want.members + `}`
					}
					byType, jres := jsonadapter.New().ParseObject(t.Context(), location.NewSourceID("p.json"), []byte(`{"Person":[`+doc+`]}`))
					if jres.HasErrors() || len(byType["Person"]) != 1 {
						t.Fatalf("the row's document does not parse: %s", jres)
					}
					fromJSON := byType["Person"][0]

					opts := []Option{WithStrictPropertyNames(mode.strict)}
					if mode.withSchema {
						opts = append(opts, WithSchema(s))
					}
					var raws []instance.RawInstance
					var pres interface {
						String() string
					}
					var codes []string
					if typeColumn {
						in := "T,id," + row.header + "\nPerson,p1," + row.record + "\n"
						byRowType, r := New(append(opts, WithTypeColumn("T"))...).ParseWithTypeColumn(t.Context(),
							location.NewSourceID("p.csv"), strings.NewReader(in), func(string) *schema.Type { return person })
						raws, pres, codes = byRowType["Person"], r, errorCodes(r)
					} else {
						in := "id," + row.header + "\np1," + row.record + "\n"
						r0, r := New(opts...).ParseTyped(t.Context(), location.NewSourceID("p.csv"), "Person", strings.NewReader(in), person)
						raws, pres, codes = r0, r, errorCodes(r)
					}
					if len(raws) != 1 {
						t.Fatalf("csv parse: %d instances, %s", len(raws), pres)
					}
					if !reflect.DeepEqual(raws[0].Properties, fromJSON.Properties) {
						t.Errorf("properties differ\ncsv:  %#v\njson: %#v", raws[0].Properties, fromJSON.Properties)
					}
					if wantCodes := slices.Sorted(slices.Values(want.csvErrors)); !slices.Equal(codes, wantCodes) {
						t.Errorf("parse Error codes = %q, want %q\n%s", codes, wantCodes, pres)
					}
					for _, allow := range []bool{false, true} {
						vo := []instance.Option{instance.WithStrictPropertyNames(mode.strict), instance.WithAllowUnknownFields(allow)}
						fromCSV, document := verdict(t, s, raws[0], vo...), verdict(t, s, fromJSON, vo...)
						if !slices.Equal(fromCSV, document) {
							t.Errorf("allow=%v: the validator answers the two differently\ncsv:  %q\njson: %q", allow, fromCSV, document)
						}
						if allow {
							continue
						}
						got := make([]string, len(document))
						for i, line := range document {
							got[i], _, _ = strings.Cut(line, " ")
						}
						if wantVerdict := slices.Sorted(slices.Values(want.verdict)); !slices.Equal(got, wantVerdict) {
							t.Errorf("the validator's codes for the document = %q, want %q\n%q", got, wantVerdict, document)
						}
					}
				})
			}
		}
	}
}
