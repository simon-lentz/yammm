package csv

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// What an empty cell holds is decided by the schema, not the wire: csv.Writer
// writes "" and a missing value as the same empty field, and csv.Reader reads a
// quoted empty field as a bare one. These tests pin each arm of that rule and
// the three places the writer acts because the wire cannot: two refusals and
// one quoted field.

const emptyCellSchema = `schema "empty_cells"

type Target {
	key String primary
}

type Pair {
	a String primary
	b String primary
}

type Holder {
	id String primary
	note String required
	memo String
	tags List<String> required
	opt List<String>
	count Integer
	--> ONE (_) Target
	--> PAIR (_) Pair
	--> MANY (_:many) Target {
		label String required
		hint String
	}
}

type Solo {
	code String primary
}
`

func loadEmptyCellSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), emptyCellSchema, "empty_cells.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	return s
}

// emptyCellSnapshot validates and adds every instance in order, so a target
// precedes the instance that points at it.
func emptyCellSnapshot(t *testing.T, s *schema.Schema, rows []struct {
	typeName string
	props    map[string]any
},
) *graph.Snapshot {
	t.Helper()
	ctx := context.Background()
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, r := range rows {
		vi, vres := v.ValidateOne(ctx, r.typeName, instance.RawInstance{Properties: r.props})
		if vres.HasErrors() {
			t.Fatalf("validate %s %v: %s", r.typeName, r.props, vres.String())
		}
		if res := g.Add(ctx, vi); !res.OK() {
			t.Fatalf("add %s: %s", r.typeName, res.String())
		}
	}
	if res := g.Check(ctx); !res.OK() {
		t.Fatalf("fixture graph incomplete: %s", res.String())
	}
	return g.Snapshot()
}

type typedRow = struct {
	typeName string
	props    map[string]any
}

// reparse marshals snap, parses every type the writer emitted back through its
// own output, in order, and rebuilds the graph, failing on any error.
func reparse(t *testing.T, s *schema.Schema, snap *graph.Snapshot, order []string) (map[string][]byte, map[string][]instance.RawInstance, *graph.Snapshot) {
	t.Helper()
	ctx := context.Background()
	a := New(WithSchema(s))
	files, err := a.MarshalSnapshot(ctx, snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	parsed := make(map[string][]instance.RawInstance)
	for _, typeName := range order {
		if _, written := files[typeName]; !written {
			continue // no instances, so the writer emitted no file
		}
		typ, _ := s.Type(typeName)
		raws, pres := a.ParseTyped(ctx, location.NewSourceID(typeName+".csv"), typeName, bytes.NewReader(files[typeName]), typ)
		if pres.HasErrors() {
			t.Fatalf("ParseTyped rejected the writer's own %s output: %s\n%s", typeName, pres.String(), files[typeName])
		}
		parsed[typeName] = raws
		for i, raw := range raws {
			vi, vres := v.ValidateOne(ctx, typeName, raw)
			if vres.HasErrors() {
				t.Fatalf("validator rejected re-parsed %s[%d] %v: %s\n%s", typeName, i, raw.Properties, vres.String(), files[typeName])
			}
			if res := g.Add(ctx, vi); !res.OK() {
				t.Fatalf("re-add %s[%d]: %s", typeName, i, res.String())
			}
		}
	}
	if res := g.Check(ctx); !res.OK() {
		t.Fatalf("re-built graph incomplete: %s\n%s", res.String(), files)
	}
	return files, parsed, g.Snapshot()
}

func TestEmptyCell_RequiredStringAndListSurviveTheRoundTrip(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Holder", map[string]any{"id": "h1", "note": "", "tags": []any{}}},
	})

	files, parsed, again := reparse(t, s, snap, []string{"Target", "Pair", "Holder"})

	props := parsed["Holder"][0].Properties
	if got, ok := props["note"]; !ok || got != "" {
		t.Errorf("required String %q: got %#v (present %v), want the empty string", "note", got, ok)
	}
	if got, ok := props["tags"]; !ok || !reflect.DeepEqual(got, []any{}) {
		t.Errorf("required List %q: got %#v (present %v), want an empty list", "tags", got, ok)
	}

	second, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), again)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(files["Holder"], second["Holder"]) {
		t.Fatalf("round trip is not the identity\nfirst:\n%s\nsecond:\n%s", files["Holder"], second["Holder"])
	}
}

// An optional property's empty value and its null write the same empty cell,
// and the schema cannot separate them: an optional "" or [] reads back as null.
// This is the documented residue of the rule, not a defect it misses.
func TestEmptyCell_OptionalEmptyValueReadsBackAsNull(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Holder", map[string]any{"id": "h1", "note": "n", "tags": []any{"t"}, "memo": "", "opt": []any{}}},
	})

	_, parsed, _ := reparse(t, s, snap, []string{"Target", "Pair", "Holder"})

	props := parsed["Holder"][0].Properties
	for _, name := range []string{"memo", "opt"} {
		if got := props[name]; got != nil {
			t.Errorf("optional %q: got %#v, want nil — the documented residue", name, got)
		}
	}
}

// A required property whose kind has no empty rendering is missing, and the
// validator says so.
func TestEmptyCell_RequiredKindWithNoEmptyRenderingIsMissing(t *testing.T) {
	t.Parallel()
	const src = `schema "counts"

type Tally {
	id String primary
	count Integer required
	day Date required
}
`
	s, res := schema.LoadString(t.Context(), src, "counts.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	typ, _ := s.Type("Tally")
	raws, pres := New().ParseTyped(context.Background(), location.NewSourceID("tally.csv"), "Tally",
		strings.NewReader("id,count,day\nt1,,\n"), typ)
	if pres.HasErrors() {
		t.Fatalf("ParseTyped: %s", pres.String())
	}
	for _, name := range []string{"count", "day"} {
		if got := raws[0].Properties[name]; got != nil {
			t.Errorf("%q: got %#v, want nil", name, got)
		}
	}
	_, vres := instance.NewValidator(s).ValidateOne(context.Background(), "Tally", raws[0])
	for _, name := range []string{"count", "day"} {
		if !hasIssue(vres, diag.E_MISSING_REQUIRED, name) {
			t.Errorf("want E_MISSING_REQUIRED naming %q, got %s", name, vres.String())
		}
	}
}

func TestEmptyCell_NoSchemaTypeKeepsEveryCellAsItsString(t *testing.T) {
	t.Parallel()
	raws, pres := New().ParseTyped(context.Background(), location.NewSourceID("raw.csv"), "Row",
		strings.NewReader("a,b,c.d\nx,,\n"), nil)
	if pres.HasErrors() {
		t.Fatalf("ParseTyped: %s", pres.String())
	}
	want := map[string]any{"a": "x", "b": "", "c.d": ""}
	if !reflect.DeepEqual(raws[0].Properties, want) {
		t.Fatalf("got %#v, want %#v", raws[0].Properties, want)
	}
}

const unionSchema = `schema "union"

type A {
	id String primary
	alpha String
}

type B {
	id String primary
	beta String
	--> REF (_) A
}
`

// A header that unions several types' columns is the only shape a multi-type
// file can take, so every row carries empty cells in columns its type does not
// declare. Those cells are skipped, not nulled.
func TestEmptyCell_UnionHeaderParsesEveryRow(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), unionSchema, "union.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	in := "kind,id,alpha,beta,ref._target_id\n" +
		"A,a1,one,,\n" +
		"B,b1,,two,a1\n"
	byType, pres := New(WithTypeColumn("kind"), WithSchema(s)).ParseWithTypeColumn(context.Background(),
		location.NewSourceID("union.csv"), strings.NewReader(in), func(name string) *schema.Type {
			typ, _ := s.Type(name)
			return typ
		})
	if pres.HasErrors() {
		t.Fatalf("ParseWithTypeColumn: %s", pres.String())
	}
	v := instance.NewValidator(s)
	for _, typeName := range []string{"A", "B"} {
		for _, raw := range byType[typeName] {
			if _, vres := v.ValidateOne(context.Background(), typeName, raw); vres.HasErrors() {
				t.Errorf("%s %v: %s", typeName, raw.Properties, vres.String())
			}
		}
	}
	if _, has := byType["A"][0].Properties["beta"]; has {
		t.Errorf("A's row carries B's column: %v", byType["A"][0].Properties)
	}
}

// Skipping is for EMPTY cells only: a value in a column the row's type does not
// declare is still reported, by the validator, for a plain column and an edge
// column alike.
func TestEmptyCell_ValueInAnUndeclaredColumnIsStillReported(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), unionSchema, "union.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	in := "kind,id,alpha,beta,ref._target_id\n" +
		"A,a1,one,stray,a9\n"
	byType, pres := New(WithTypeColumn("kind"), WithSchema(s)).ParseWithTypeColumn(context.Background(),
		location.NewSourceID("union.csv"), strings.NewReader(in), func(name string) *schema.Type {
			typ, _ := s.Type(name)
			return typ
		})
	if pres.HasErrors() {
		t.Errorf("the parser judges no name; the validator does: %s", pres.String())
	}
	_, vres := instance.NewValidator(s).ValidateOne(context.Background(), "A", byType["A"][0])
	for _, name := range []string{"beta", "ref"} {
		if !hasIssue(vres, diag.E_UNKNOWN_FIELD, name) {
			t.Errorf("want E_UNKNOWN_FIELD naming %s, got %s", name, vres.String())
		}
	}
}

// Inside a present group an empty foreign-key segment is a key value, and an
// empty required edge property is the empty string. Only a group whose every
// cell is empty is absent.
func TestEmptyCell_EmptyForeignKeyComponentAndRequiredEdgePropertyAreValues(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Target", map[string]any{"key": ""}},
		{"Target", map[string]any{"key": "t2"}},
		{"Pair", map[string]any{"a": "", "b": "x"}},
		{"Holder", map[string]any{
			"id": "h1", "note": "n", "tags": []any{"t"},
			"pair": map[string]any{"_target_a": "", "_target_b": "x"},
			"many": []any{
				map[string]any{"_target_key": "", "label": ""},
				map[string]any{"_target_key": "t2", "label": "l2"},
			},
		}},
	})

	files, parsed, again := reparse(t, s, snap, []string{"Target", "Pair", "Holder"})

	holder := parsed["Holder"][0].Properties
	pair, ok := holder["pair"].(map[string]any)
	if !ok || pair["_target_a"] != "" || pair["_target_b"] != "x" {
		t.Errorf("pair: got %#v, want both key components with _target_a empty", holder["pair"])
	}
	many, ok := holder["many"].([]any)
	if !ok || len(many) != 2 {
		t.Fatalf("many: got %#v, want two targets", holder["many"])
	}
	first, _ := many[0].(map[string]any)
	if got, has := first["label"]; !has || got != "" {
		t.Errorf("required edge property on the empty-keyed target: got %#v (present %v), want the empty string", got, has)
	}
	if _, has := first["hint"]; has {
		t.Errorf("optional edge property with no value: got %#v, want it absent", first["hint"])
	}

	second, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), again)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(files["Holder"], second["Holder"]) {
		t.Fatalf("round trip is not the identity\nfirst:\n%s\nsecond:\n%s", files["Holder"], second["Holder"])
	}
}

// One target whose every key component is "" and which carries no edge-property
// value writes every cell of its group empty — the absent-group marker. The
// wire cannot tell the two apart, so the writer refuses rather than let the
// association vanish on the way back.
func TestEmptyCell_LoneTargetWhoseGroupRendersEmptyIsRefused(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Target", map[string]any{"key": ""}},
		{"Holder", map[string]any{"id": "h1", "note": "n", "tags": []any{"t"}, "one": map[string]any{"_target_key": ""}}},
	})

	_, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), snap)
	if err == nil {
		t.Fatal("MarshalSnapshot accepted an association its own parser would read as absent")
	}
	for _, want := range []string{`"ONE"`, "h1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}

	var sink bytes.Buffer
	err = New(WithSchema(s)).WriteSnapshot(context.Background(), func(string) (io.Writer, error) { return &sink, nil }, snap)
	if err == nil || !strings.Contains(err.Error(), `"ONE"`) {
		t.Errorf("WriteSnapshot: got %v, want the same refusal", err)
	}
}

// A list holding one empty element renders the cell an empty list renders, so
// the element would vanish on the way back — read as [] where the list is
// required and as null where it is optional. The writer refuses it either way.
func TestEmptyCell_ListOfOneEmptyElementIsRefused(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	for _, props := range []map[string]any{
		{"id": "h1", "note": "n", "tags": []any{""}},
		{"id": "h1", "note": "n", "tags": []any{"t"}, "opt": []any{""}},
	} {
		snap := emptyCellSnapshot(t, s, []typedRow{{"Holder", props}})
		_, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), snap)
		if err == nil {
			t.Errorf("%v: MarshalSnapshot accepted a list its own parser would read as empty", props)
			continue
		}
		if !strings.Contains(err.Error(), "one empty element") || !strings.Contains(err.Error(), "h1") {
			t.Errorf("error %q does not name the property's cause and the instance", err)
		}
		var sink bytes.Buffer
		if err := New(WithSchema(s)).WriteSnapshot(context.Background(), func(string) (io.Writer, error) { return &sink, nil }, snap); err == nil || !strings.Contains(err.Error(), "one empty element") {
			t.Errorf("%v: WriteSnapshot: got %v, want the same refusal", props, err)
		}
	}
}

// A single-column row holding the empty string is written as a quoted empty
// field: csv.Writer would write a blank line, which csv.Reader skips.
func TestEmptyCell_SingleColumnEmptyKeyIsWrittenQuoted(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Solo", map[string]any{"code": ""}},
		{"Solo", map[string]any{"code": " "}},
		{"Solo", map[string]any{"code": "sA"}},
	})

	files, parsed, _ := reparse(t, s, snap, []string{"Solo"})

	if got, want := string(files["Solo"]), "code\n\" \"\n\"\"\nsA\n"; got != want {
		t.Errorf("bytes: got %q, want %q", got, want)
	}
	var keys []any
	for _, raw := range parsed["Solo"] {
		keys = append(keys, raw.Properties["code"])
	}
	// The snapshot orders instances by key, and [" "] sorts before [""].
	if want := []any{" ", "", "sA"}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys read back: got %#v, want %#v", keys, want)
	}
}

func TestCoerceStringValue_RefusesAListKindWithoutAListConstraint(t *testing.T) {
	t.Parallel()
	lc := schema.NewListConstraint(schema.NewStringConstraint())
	got, err := New().coerceStringValue("a|b", &lc)
	if err == nil {
		t.Fatalf("got %#v, want an error: a list cell must not pass through as one string", got)
	}
}

func hasIssue(res diag.Result, code diag.Code, mention string) bool {
	for issue := range res.Issues() {
		if issue.Code() == code && strings.Contains(issue.Message(), mention) {
			return true
		}
	}
	return false
}

// The JSON adapter is the other implementation of the same contract, and it
// carries "" and [] on its own wire. A graph holding the empty values the CSV
// rule carries — a required "" and [], a "" key and key component, a required
// "" edge property, a single-column "" key — must come back from a CSV round
// trip exactly as it comes back from a JSON one, compared through the JSON
// writer's bytes.
func TestEmptyCell_CSVRoundTripAgreesWithTheJSONAdapter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadEmptyCellSchema(t)
	order := []string{"Target", "Pair", "Solo", "Holder"}
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Target", map[string]any{"key": ""}},
		{"Target", map[string]any{"key": "t2"}},
		{"Pair", map[string]any{"a": "", "b": "x"}},
		{"Solo", map[string]any{"code": ""}},
		{"Holder", map[string]any{
			"id": "h1", "note": "", "tags": []any{},
			"pair": map[string]any{"_target_a": "", "_target_b": "x"},
			"many": []any{
				map[string]any{"_target_key": "", "label": ""},
				map[string]any{"_target_key": "t2", "label": "l2", "hint": "h"},
			},
		}},
		{"Holder", map[string]any{"id": "", "note": "n", "tags": []any{"", "b"}, "count": int64(3)}},
	})

	_, _, viaCSV := reparse(t, s, snap, order)

	ja := jsonadapter.New()
	doc, err := ja.MarshalObject(ctx, snap)
	if err != nil {
		t.Fatalf("json MarshalObject: %v", err)
	}
	byType, pres := ja.ParseObject(ctx, location.NewSourceID("graph.json"), doc)
	if pres.HasErrors() {
		t.Fatalf("json ParseObject: %s", pres.String())
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, typeName := range order {
		for _, raw := range byType[typeName] {
			vi, vres := v.ValidateOne(ctx, typeName, raw)
			if vres.HasErrors() {
				t.Fatalf("json re-validate %s: %s", typeName, vres.String())
			}
			if res := g.Add(ctx, vi); !res.OK() {
				t.Fatalf("json re-add %s: %s", typeName, res.String())
			}
		}
	}
	viaJSON := g.Snapshot()

	fromCSV, err := ja.MarshalObject(ctx, viaCSV)
	if err != nil {
		t.Fatalf("json MarshalObject of the CSV round trip: %v", err)
	}
	fromJSON, err := ja.MarshalObject(ctx, viaJSON)
	if err != nil {
		t.Fatalf("json MarshalObject of the JSON round trip: %v", err)
	}
	if !bytes.Equal(fromCSV, fromJSON) {
		t.Fatalf("the CSV round trip disagrees with the JSON one\nvia CSV:  %s\nvia JSON: %s", fromCSV, fromJSON)
	}
	if !bytes.Equal(fromJSON, doc) {
		t.Fatalf("the JSON round trip is not the identity\nfirst:  %s\nsecond: %s", doc, fromJSON)
	}
}

const sharedFieldSchema = `schema "shared"

type X {
	id String primary
	code String
}

type Y {
	code String primary
}

type A {
	aid String primary
	--> REF (_) X
}

type B {
	bid String primary
	--> REF (_) Y
}
`

// Two types whose associations share a field name but target different keys
// put both key columns in a union header. The column that names no key of the
// row's target is skipped when empty, which needs the target's keys — X also
// declares a plain property "code", which is not a key, so the column Y's key
// names stays foreign to A's row.
func TestEmptyCell_UnionHeaderWithASharedAssociationFieldParsesEveryRow(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), sharedFieldSchema, "shared.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	in := "kind,aid,bid,ref._target_id,ref._target_code\n" +
		"A,a1,,x1,\n" +
		"B,,b1,,y1\n"
	byType, pres := New(WithTypeColumn("kind"), WithSchema(s)).ParseWithTypeColumn(context.Background(),
		location.NewSourceID("shared.csv"), strings.NewReader(in), func(name string) *schema.Type {
			typ, _ := s.Type(name)
			return typ
		})
	if pres.HasErrors() {
		t.Fatalf("ParseWithTypeColumn: %s", pres.String())
	}
	want := map[string]map[string]any{
		"A": {"_target_id": "x1"},
		"B": {"_target_code": "y1"},
	}
	v := instance.NewValidator(s)
	for typeName, ref := range want {
		raw := byType[typeName][0]
		if got := raw.Properties["ref"]; !reflect.DeepEqual(got, ref) {
			t.Errorf("%s ref: got %#v, want %#v", typeName, got, ref)
		}
		if _, vres := v.ValidateOne(context.Background(), typeName, raw); vres.HasErrors() {
			t.Errorf("%s: %s", typeName, vres.String())
		}
	}
}

// Inside a present group an empty cell stands for an empty segment on EVERY
// target, so a hand-written file may leave an edge-property column blank for a
// multi-target group. The writer writes n-1 separators instead; both read alike.
func TestEmptyCell_BlankColumnInAMultiTargetGroupIsEmptyOnEveryTarget(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	typ, _ := s.Type("Holder")
	for _, cells := range []string{",", ",|"} {
		in := "id,note,tags,many._target_key,many.label,many.hint\n" +
			"h1,n,t,t1|t2,l1|l2" + cells + "\n"
		raws, pres := New(WithSchema(s)).ParseTyped(context.Background(), location.NewSourceID("h.csv"), "Holder",
			strings.NewReader(in), typ)
		if pres.HasErrors() {
			t.Fatalf("hint cell %q: %s", strings.TrimPrefix(cells, ","), pres.String())
		}
		many, _ := raws[0].Properties["many"].([]any)
		if len(many) != 2 {
			t.Fatalf("hint cell %q: got %#v, want two targets", strings.TrimPrefix(cells, ","), raws[0].Properties["many"])
		}
		for i, target := range many {
			obj, _ := target.(map[string]any)
			if _, has := obj["hint"]; has {
				t.Errorf("hint cell %q: target %d carries an optional hint: %#v", strings.TrimPrefix(cells, ","), i, obj)
			}
		}
	}

	// A blank REQUIRED String edge-property column is "" on every target.
	in := "id,note,tags,many._target_key,many.label\nh1,n,t,t1|t2,\n"
	raws, pres := New(WithSchema(s)).ParseTyped(context.Background(), location.NewSourceID("h.csv"), "Holder",
		strings.NewReader(in), typ)
	if pres.HasErrors() {
		t.Fatalf("blank label column: %s", pres.String())
	}
	many, _ := raws[0].Properties["many"].([]any)
	for i, target := range many {
		if obj, _ := target.(map[string]any); obj["label"] != "" {
			t.Errorf("target %d label: got %#v, want the empty string", i, obj["label"])
		}
	}
}

// Without the target's keys the parser cannot tell an empty key segment from
// another type's column, so it is absent. With them, a key kind that has no
// empty value — a Date — is absent too; the validator reports the component.
func TestEmptyCell_EmptyKeySegmentIsAbsentWithoutTheTargetsKeys(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	typ, _ := s.Type("Holder")
	in := "id,note,tags,pair._target_a,pair._target_b\nh1,n,t,,x\n"
	raws, pres := New().ParseTyped(context.Background(), location.NewSourceID("h.csv"), "Holder",
		strings.NewReader(in), typ)
	if pres.HasErrors() {
		t.Fatalf("ParseTyped: %s", pres.String())
	}
	if got, want := raws[0].Properties["pair"], map[string]any{"_target_b": "x"}; !reflect.DeepEqual(got, want) {
		t.Errorf("without WithSchema: got %#v, want %#v", got, want)
	}

	const dated = `schema "dated"

type Day {
	on Date primary
}

type Log {
	id String primary
	--> AT (_) Day
}
`
	ds, res := schema.LoadString(t.Context(), dated, "dated.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	logType, _ := ds.Type("Log")
	raws, pres = New(WithSchema(ds)).ParseTyped(context.Background(), location.NewSourceID("log.csv"), "Log",
		strings.NewReader("id,at._target_on\nl1,\n"), logType)
	if pres.HasErrors() {
		t.Fatalf("ParseTyped: %s", pres.String())
	}
	if _, has := raws[0].Properties["at"]; has {
		t.Errorf("an all-empty group is absent: got %#v", raws[0].Properties["at"])
	}
}

// A lone ""-keyed target that carries an edge-property value writes a present
// group, so it is written and read back; one whose required edge property is
// also "" renders every cell empty and is refused.
func TestEmptyCell_LoneEmptyKeyedTargetWithAnEdgeValueRoundTrips(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	withValue := emptyCellSnapshot(t, s, []typedRow{
		{"Target", map[string]any{"key": ""}},
		{"Holder", map[string]any{
			"id": "h1", "note": "n", "tags": []any{"t"},
			"many": []any{map[string]any{"_target_key": "", "label": "l"}},
		}},
	})
	_, parsed, _ := reparse(t, s, withValue, []string{"Target", "Holder"})
	many, _ := parsed["Holder"][0].Properties["many"].([]any)
	if len(many) != 1 {
		t.Fatalf("many: got %#v, want one target", parsed["Holder"][0].Properties["many"])
	}
	if obj, _ := many[0].(map[string]any); obj["_target_key"] != "" || obj["label"] != "l" {
		t.Errorf("target: got %#v, want key \"\" and label l", obj)
	}

	allEmpty := emptyCellSnapshot(t, s, []typedRow{
		{"Target", map[string]any{"key": ""}},
		{"Holder", map[string]any{
			"id": "h1", "note": "n", "tags": []any{"t"},
			"many": []any{map[string]any{"_target_key": "", "label": ""}},
		}},
	})
	if _, err := New(WithSchema(s)).MarshalSnapshot(context.Background(), allEmpty); err == nil || !strings.Contains(err.Error(), `"MANY"`) {
		t.Errorf("a lone target whose key and edge properties all render empty: got %v, want the refusal", err)
	}
}

// A refused WriteSnapshot flushes what it wrote before the refusal, as a
// cancelled one does, so the destination still parses as far as it goes.
func TestEmptyCell_RefusedWriteSnapshotFlushesTheRowsBeforeIt(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	snap := emptyCellSnapshot(t, s, []typedRow{
		{"Holder", map[string]any{"id": "a1", "note": "n", "tags": []any{"t"}}},
		{"Holder", map[string]any{"id": "b1", "note": "n", "tags": []any{""}}},
	})
	var sink bytes.Buffer
	err := New(WithSchema(s)).WriteSnapshot(context.Background(), func(string) (io.Writer, error) { return &sink, nil }, snap)
	if err == nil || !strings.Contains(err.Error(), "one empty element") {
		t.Fatalf("WriteSnapshot: got %v, want the list refusal", err)
	}
	lines := strings.Split(strings.TrimSuffix(sink.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "count,id,") || !strings.Contains(lines[1], ",a1,") {
		t.Fatalf("got %q, want the header and a1's row", sink.String())
	}
}

func TestCoerceStringValue_AFailedListIsAnUntypedNil(t *testing.T) {
	t.Parallel()
	for _, c := range []schema.Constraint{
		schema.NewListConstraint(schema.NewIntegerConstraint()),
		schema.NewVectorConstraint(2),
	} {
		got, err := New().coerceStringValue("x", c)
		if err == nil {
			t.Fatalf("%s: want an error", c)
		}
		if got != nil {
			t.Errorf("%s: got %#v beside the error, want an untyped nil", c, got)
		}
	}
}

// A required property whose kind has an empty value reaches the validator as
// that value and draws the constraint's own reason, as the JSON adapter's ""
// and [] do; a pattern that admits "" accepts it.
func TestEmptyCell_RequiredKindWithAnEmptyValueReachesTheValidator(t *testing.T) {
	t.Parallel()
	const src = `schema "kinds"

type K {
	id String primary
	u UUID required
	v Vector[2] required
	s String[1, 5] required
	l List<String>[1, 3] required
	p Pattern["^a*$"] required
}
`
	s, res := schema.LoadString(t.Context(), src, "kinds.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	typ, _ := s.Type("K")
	raws, pres := New().ParseTyped(context.Background(), location.NewSourceID("k.csv"), "K",
		strings.NewReader("id,u,v,s,l,p\nk1,,,,,\n"), typ)
	if pres.HasErrors() {
		t.Fatalf("ParseTyped: %s", pres.String())
	}
	want := map[string]any{"id": "k1", "u": "", "v": []any{}, "s": "", "l": []any{}, "p": ""}
	if !reflect.DeepEqual(raws[0].Properties, want) {
		t.Fatalf("got %#v, want %#v", raws[0].Properties, want)
	}
	_, vres := instance.NewValidator(s).ValidateOne(context.Background(), "K", raws[0])
	for _, name := range []string{`"u"`, `"v"`, `"s"`, `"l"`} {
		if !hasIssue(vres, diag.E_CONSTRAINT_FAIL, name) {
			t.Errorf("want E_CONSTRAINT_FAIL naming %s, got %s", name, vres.String())
		}
	}
	if hasIssue(vres, diag.E_CONSTRAINT_FAIL, `"p"`) || hasIssue(vres, diag.E_MISSING_REQUIRED, `"p"`) {
		t.Errorf("a pattern that admits the empty string refused it: %s", vres.String())
	}
}

// Inside a present group, an empty segment of a Date key — a kind with no empty
// value — is absent even with the target's keys, and the parser draws no
// coercion error for it.
func TestEmptyCell_EmptyDateKeySegmentInAPresentGroupIsAbsent(t *testing.T) {
	t.Parallel()
	const dated = `schema "dated_many"

type Day {
	on Date primary
}

type Log {
	id String primary
	--> DAYS (_:many) Day
}
`
	s, res := schema.LoadString(t.Context(), dated, "dated_many.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	typ, _ := s.Type("Log")
	raws, pres := New(WithSchema(s)).ParseTyped(context.Background(), location.NewSourceID("log.csv"), "Log",
		strings.NewReader("id,days._target_on\nl1,2026-01-01|\n"), typ)
	if pres.HasErrors() {
		t.Fatalf("ParseTyped: %s", pres.String())
	}
	want := []any{map[string]any{"_target_on": "2026-01-01"}, map[string]any{}}
	if got := raws[0].Properties["days"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// An empty column naming an edge property the association does not declare is
// skipped, as an empty undeclared plain column is; a value there reaches the
// validator, which reports it as it reports the JSON object's key.
func TestEmptyCell_UndeclaredEdgePropertyColumnIsSkippedWhenEmpty(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	typ, _ := s.Type("Holder")
	for _, c := range []struct {
		cell      string
		wantIssue bool
	}{{"", false}, {"x", true}} {
		in := "id,note,tags,many._target_key,many.label,many.bogus\nh1,n,t,t1,l," + c.cell + "\n"
		raws, pres := New(WithSchema(s)).ParseTyped(context.Background(), location.NewSourceID("h.csv"), "Holder",
			strings.NewReader(in), typ)
		if pres.HasErrors() {
			t.Fatalf("cell %q: the parser judges no name: %s", c.cell, pres.String())
		}
		_, vres := instance.NewValidator(s).ValidateOne(context.Background(), "Holder", raws[0])
		if got := hasIssue(vres, diag.E_UNKNOWN_EDGE_FIELD, "bogus"); got != c.wantIssue {
			t.Errorf("cell %q: E_UNKNOWN_EDGE_FIELD naming bogus = %v, want %v (%s)", c.cell, got, c.wantIssue, vres.String())
		}
	}
}

// A single-field row goes through writeRecord's quoted path, whose flush and
// raw write each report a device failure as a row failure.
func TestEmptyCell_QuotedEmptyRowReportsAWriterFailure(t *testing.T) {
	t.Parallel()
	s := loadEmptyCellSchema(t)
	snap := emptyCellSnapshot(t, s, []typedRow{{"Solo", map[string]any{"code": ""}}})
	for _, failOn := range []int{1, 2} { // 1: the flush of the header; 2: the quoted field
		w := &nthFailingWriter{failOn: failOn}
		err := New(WithSchema(s)).WriteSnapshot(context.Background(), func(string) (io.Writer, error) { return w, nil }, snap)
		if err == nil || !strings.Contains(err.Error(), "csv write row") {
			t.Errorf("failing write %d: got %v, want a row failure", failOn, err)
		}
	}
}

// nthFailingWriter fails its failOn-th Write and accepts every other.
type nthFailingWriter struct {
	failOn, calls int
}

func (w *nthFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls == w.failOn {
		return 0, errDevice
	}
	return len(p), nil
}

// A bypass-built snapshot can hold a null property and a null list element,
// which the validator never produces. Both write empty text, never a spelling
// of null.
func TestEmptyCell_BypassBuiltNullWritesEmptyText(t *testing.T) {
	t.Parallel()
	s := canonicalTestSchema(t)
	g := graph.New(s)
	sensorID, _ := s.Type("Sensor")
	sensor := instancetest.VI("Sensor", instancetest.TypeID(sensorID.ID()), instancetest.PK("s1"),
		instancetest.Props(map[string]any{"id": "s1", "created_at": nil, "samples": []any{nil, "2026-08-19T13:00:00Z"}}))
	if res := g.Add(context.Background(), sensor); !res.OK() {
		t.Fatalf("add: %s", res.String())
	}
	out, err := New().MarshalSnapshot(context.Background(), g.Snapshot())
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}
	if got, want := string(out["Sensor"]), "created_at,id,installed,run_id,samples,feed._target_at\n,s1,,,|2026-08-19T13:00:00Z,\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
