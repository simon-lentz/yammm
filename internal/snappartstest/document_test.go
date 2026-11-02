package snappartstest_test

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const documentShapesSchema = `schema "shapes"

part type Card {
	last4 String primary
}

type Company {
	id String primary
}

type Empty {
	id String primary
}

type Person {
	id String primary
	score Float
	--> EMPLOYER (_) Company { since String }
	--> MENTOR (_) Company
	*-> CARDS (_:many) Card
}
`

func documentShapes(t *testing.T) (schema.Schema, snapparts.Parts) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), documentShapesSchema, "shapes.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	company, person, card, empty := typeID(t, s, "Company"), typeID(t, s, "Person"), typeID(t, s, "Card"), typeID(t, s, "Empty")
	key := func(k string) immutable.Key { return immutable.WrapKey([]any{k}) }
	props := func(m map[string]any) immutable.Properties { return immutable.WrapProperties(m) }
	return s, snapparts.Parts{
		Types: []schema.TypeID{empty},
		Instances: []snapparts.Instance{
			{TypeID: company, PrimaryKey: key("c1"), Properties: props(map[string]any{"id": "c1"})},
			{
				TypeID: person, PrimaryKey: key("p1"), Properties: props(map[string]any{"id": "p1", "score": float64(2)}),
				Provenance: location.NewProvenance("feed", path.Root().Key("rows"), location.Span{}),
				Composed: map[string][]snapparts.Instance{"CARDS": {{
					TypeID: card, PrimaryKey: key("4242"), Properties: props(map[string]any{"last4": "4242"}),
				}}},
			},
			{TypeID: person, PrimaryKey: key("p2"), Properties: props(map[string]any{"id": "p2"})},
		},
		Edges: []snapparts.Edge{
			{
				Relation: "EMPLOYER", SourceType: person, SourceKey: key("p1"), TargetKey: key("c1"),
				Properties: props(map[string]any{"since": "2020"}),
			},
			{Relation: "EMPLOYER", SourceType: person, SourceKey: key("p2"), TargetKey: key("c1")},
		},
		Duplicates: []snapparts.Instance{
			{TypeID: person, PrimaryKey: key("p2"), Properties: props(map[string]any{"id": "p2", "score": 3.5})},
		},
		Unresolved: []snapparts.Unresolved{
			{SourceType: person, SourceKey: key("p1"), Relation: "MENTOR", TargetKey: key("c9"), Reason: "target_missing"},
		},
		Attestation: &snapparts.Attestation{Values: true},
	}
}

// Document states each shape where Marshal writes it: a table row for every type
// named or reached, a group for every root type and denoted type, an edge under the
// root its source key spells, a composed child with its type, and a whole float
// with its indicator.
func TestDocument_WritesEveryShapeWhereTheFormatHoldsIt(t *testing.T) {
	t.Parallel()
	s, parts := documentShapes(t)
	header := `{"version":` + strconv.Itoa(snapshot.MinReadableVersion) +
		`,"schema_name":"shapes","schema_source":` + strconv.Quote(s.SourceID().String()) +
		`,"schema_hash":` + strconv.Quote(schema.StructuralHash(s)) +
		`,"schema_hash_algorithm":` + strconv.Itoa(schema.StructuralHashVersion) +
		`,"integrity_hash":"","features":[],"attestation":{"values":true,"associations":false}}`
	want := `{"yammm_snapshot":` + header +
		`,"types":[{"schema":"shapes","name":"Card"},{"schema":"shapes","name":"Company"},{"schema":"shapes","name":"Empty"},{"schema":"shapes","name":"Person"}]` +
		`,"instances":[` +
		`{"type":1,"items":[{"key":["c1"],"properties":{"id":"c1"},"provenance":null}]},` +
		`{"type":2,"items":[]},` +
		`{"type":3,"items":[` +
		`{"key":["p1"],"properties":{"id":"p1","score":2.0},` +
		`"edges":{"EMPLOYER":[{"target_type":1,"target_key":["c1"],"properties":{"since":"2020"}}]},` +
		`"composed":{"CARDS":[{"key":["4242"],"type":0,"properties":{"last4":"4242"},"provenance":null}]},` +
		`"provenance":{"source_name":"feed","path":"$.rows"}},` +
		`{"key":["p2"],"properties":{"id":"p2"},"edges":{"EMPLOYER":[{"target_type":1,"target_key":["c1"],"properties":{}}]},"provenance":null}` +
		`]}]` +
		`,"diagnostics":{` +
		`"duplicates":[{"type":3,"key":["p2"],"instance":{"key":["p2"],"properties":{"id":"p2","score":3.5},"provenance":null},"conflict":{"type":3,"key":["p2"]}}],` +
		`"unresolved":[{"source_type":3,"source_key":["p1"],"relation":"MENTOR","target_type":1,"target_key":["c9"],"required":false,"reason":"target_missing"}]` +
		`}}`
	if got := string(snappartstest.Document(t, s, parts)); got != want {
		t.Errorf("Document wrote\n%s\nwant\n%s", got, want)
	}
}

// Loaded returns what the reader makes of the document Document writes.
func TestLoaded_ReadsEveryShapeBack(t *testing.T) {
	t.Parallel()
	s, parts := documentShapes(t)
	snap := snappartstest.Loaded(t, s, parts)

	if att := snap.Attestation(); !att.Values || att.Associations {
		t.Errorf("attestation = %+v, want values alone", att)
	}
	if got := slices.Collect(snap.InstancesOf(typeID(t, s, "Empty"))); len(got) != 0 {
		t.Errorf("the denoted type holds %d instances, want 0", len(got))
	}
	if !slices.Contains(snap.Types(), typeID(t, s, "Empty")) {
		t.Errorf("types = %v, want the denoted type among them", snap.Types())
	}

	people := slices.Collect(snap.InstancesOf(typeID(t, s, "Person")))
	if len(people) != 2 {
		t.Fatalf("read %d people, want 2", len(people))
	}
	p1 := people[0]
	if got := p1.Properties().Clone()["score"]; got != float64(2) {
		t.Errorf("score = %#v, want the float64 2", got)
	}
	if p1.Provenance().SourceName() != "feed" || p1.Provenance().Path().String() != "$.rows" {
		t.Errorf("provenance = %v, want feed at $.rows", p1.Provenance())
	}
	if cards := slices.Collect(p1.Composed("CARDS")); len(cards) != 1 || cards[0].TypeID() != typeID(t, s, "Card") {
		t.Errorf("composed = %v, want one Card", cards)
	}

	var since []string
	for e := range snap.Edges() {
		v, _ := e.Properties().Clone()["since"].(string)
		since = append(since, e.Source().PrimaryKey().String()+"="+v)
	}
	slices.Sort(since)
	if want := []string{`["p1"]=2020`, `["p2"]=`}; !slices.Equal(since, want) {
		t.Errorf("edges = %q, want %q", since, want)
	}

	if got := slices.Collect(snap.Duplicates()); len(got) != 1 || got[0].Instance().Properties().Clone()["score"] != 3.5 {
		t.Errorf("duplicates = %v, want one holding the score 3.5", got)
	}
	if got := slices.Collect(snap.Unresolved()); len(got) != 1 || got[0].Relation() != "MENTOR" || got[0].Reason() != "target_missing" {
		t.Errorf("unresolved = %v, want one MENTOR target_missing", got)
	}
}

// A fixture that states what no document holds is a failed fixture, not a document
// that reads as another: encoding/json would write U+FFFD for bad text and fail on
// a NaN.
func TestDocument_FailsOnWhatNoDocumentHolds(t *testing.T) {
	t.Parallel()
	s, _ := documentShapes(t)
	person := typeID(t, s, "Person")
	root := func(props map[string]any) snapparts.Parts {
		return snapparts.Parts{Instances: []snapparts.Instance{{
			TypeID: person, PrimaryKey: immutable.WrapKey([]any{"p1"}), Properties: immutable.WrapProperties(props),
		}}}
	}
	other, res := schema.LoadString(t.Context(), `schema "other"

type Stranger {
	id String primary
}
`, "other.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	for _, c := range []struct {
		name  string
		parts snapparts.Parts
		want  string
	}{
		{"a time.Time", root(map[string]any{"id": "p1", "at": time.Unix(0, 0).UTC()}), "a value of type time.Time"},
		{"a uint64", root(map[string]any{"id": "p1", "n": uint64(1)}), "a value of type uint64"},
		{"a string that is not UTF-8", root(map[string]any{"id": "\xff"}), "the string"},
		{"a list element that is not UTF-8", root(map[string]any{"id": []any{"\xff"}}), "the string"},
		{"a member name that is not UTF-8", root(map[string]any{"i\xff": "p1"}), "the member"},
		{"a NaN", root(map[string]any{"score": math.NaN()}), "the float"},
		{"an infinity", root(map[string]any{"score": math.Inf(1)}), "the float"},
		{"a type outside the schema", snapparts.Parts{Types: []schema.TypeID{typeID(t, other, "Stranger")}}, "holds no type"},
		{"an edge whose source key names no root", snapparts.Parts{Edges: []snapparts.Edge{{
			Relation: "EMPLOYER", SourceType: person, SourceKey: immutable.WrapKey([]any{"p9"}), TargetKey: immutable.WrapKey([]any{"c1"}),
		}}}, "edges name a root"},
	} {
		tb := &recordingTB{TB: t}
		snappartstest.Document(tb, s, c.parts)
		if !slices.ContainsFunc(tb.fatals, func(msg string) bool { return strings.Contains(msg, c.want) }) {
			t.Errorf("%s: fatals = %q, want one naming %q", c.name, tb.fatals, c.want)
		}
	}
}

func typeID(t *testing.T, s schema.Schema, name string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("%s type missing", name)
	}
	return typ.ID()
}

// recordingTB records Fatalf instead of failing the test, so a helper's failure
// can be asserted on.
type recordingTB struct {
	testing.TB
	fatals []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
}

// The types table holds a row for a type only an edge's target or a record's source
// names, and each reference uses that row. A provenance's raw path is written as
// held. The reader then judges the document these rows state.
func TestDocument_WritesATableRowForATypeNoRootHolds(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "rows"

type Alpha {
	id String primary
}

type Beta {
	id String primary
	--> TO (_) Zeta
}

type Zeta {
	id String primary
}
`, "rows.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	alpha, beta, zeta := typeID(t, s, "Alpha"), typeID(t, s, "Beta"), typeID(t, s, "Zeta")
	key := func(k string) immutable.Key { return immutable.WrapKey([]any{k}) }
	const table = `"types":[{"schema":"rows","name":"Alpha"},{"schema":"rows","name":"Beta"},{"schema":"rows","name":"Zeta"}]`

	edge := string(snappartstest.Document(t, s, snapparts.Parts{
		Types: []schema.TypeID{alpha},
		Instances: []snapparts.Instance{{
			TypeID: beta, PrimaryKey: key("b"), Properties: immutable.WrapProperties(map[string]any{"id": "b"}),
		}},
		Edges: []snapparts.Edge{{Relation: "TO", SourceType: beta, SourceKey: key("b"), TargetKey: key("z")}},
	}))
	for _, want := range []string{table, `"target_type":2,"target_key":["z"]`} {
		if !strings.Contains(edge, want) {
			t.Errorf("the edge's document holds no %s:\n%s", want, edge)
		}
	}

	record := string(snappartstest.Document(t, s, snapparts.Parts{
		Types:      []schema.TypeID{alpha, zeta},
		Unresolved: []snapparts.Unresolved{{SourceType: beta, SourceKey: key("b"), Relation: "TO", TargetKey: key("z"), Reason: "target_missing"}},
	}))
	for _, want := range []string{table, `"source_type":1,"source_key":["b"]`} {
		if !strings.Contains(record, want) {
			t.Errorf("the record's document holds no %s:\n%s", want, record)
		}
	}
}

func TestDocument_WritesAProvenanceRawPathAsHeld(t *testing.T) {
	t.Parallel()
	s, parts := documentShapes(t)
	parts.Instances[0].Provenance = location.NewProvenance("feed", path.Root().Key("rows"), location.Span{}).WithRawPath("not a path")
	doc := string(snappartstest.Document(t, s, parts))
	if !strings.Contains(doc, `"source_name":"feed","path":"not a path"`) {
		t.Errorf("no raw path in:\n%s", doc)
	}
}
