package snapshot_test

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const numberRuleSchema = `schema "numbers"

type Ratio = Float

type Gauge {
	id String primary
	count Integer
	level Float
	ratio Ratio
	levels List<Float>
	counts List<Integer>
	points Vector[2]
	--> LINK (_:many) Gauge {
		weight Float
	}
}
`

func loadNumberRuleSchema(t *testing.T) (schema.Schema, schema.TypeID) {
	t.Helper()
	s, res := schema.LoadString(t.Context(), numberRuleSchema, "numbers.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	return s, mustTypeID(t, s, "Gauge")
}

// numberRuleDoc writes one Gauge g1 holding the property name as the literal
// spelled, with an edge to a second Gauge g2 whose weight is spelled likewise.
func numberRuleDoc(t *testing.T, s schema.Schema, gauge schema.TypeID, name string, spelled any) []byte {
	t.Helper()
	weight := json.Number("1.5")
	if name == "weight" {
		weight = spelled.(json.Number)
	}
	props := map[string]any{"id": "g1"}
	if name != "weight" {
		props[name] = spelled
	}
	return snappartstest.Document(t, s, snapparts.Parts{
		Types: []schema.TypeID{gauge},
		Instances: []snapparts.Instance{
			{TypeID: gauge, PrimaryKey: immutable.WrapKey([]any{"g1"}), Properties: immutable.WrapProperties(props)},
			{TypeID: gauge, PrimaryKey: immutable.WrapKey([]any{"g2"}), Properties: immutable.WrapProperties(map[string]any{"id": "g2"})},
		},
		Edges: []snapparts.Edge{{
			Relation: "LINK", SourceType: gauge, SourceKey: immutable.WrapKey([]any{"g1"}), TargetKey: immutable.WrapKey([]any{"g2"}),
			Properties: immutable.WrapProperties(map[string]any{"weight": weight}),
		}},
	})
}

// sameNumber reports whether got is want, a float64 compared by its bits so a
// negative zero is not zero.
func sameNumber(got, want any) bool {
	g, gf := got.(float64)
	w, wf := want.(float64)
	if gf || wf {
		return gf && wf && math.Float64bits(g) == math.Float64bits(w)
	}
	if gl, ok := got.([]any); ok {
		wl, ok := want.([]any)
		if !ok || len(gl) != len(wl) {
			return false
		}
		for i := range gl {
			if !sameNumber(gl[i], wl[i]) {
				return false
			}
		}
		return true
	}
	return got == want
}

// The reader reads a number by the value rule at the position the schema
// declares, and the writer writes what it holds: a literal at a Float is its
// nearest float64, whatever its spelling, an integer literal past int64 at an
// Integer stays the literal, and a literal no finite float64 holds stays the
// literal anywhere. The write is a fixpoint of the next load.
func TestLoad_ReadsANumberAtTheKindItsPositionDeclares(t *testing.T) {
	t.Parallel()
	s, gauge := loadNumberRuleSchema(t)
	for _, c := range []struct {
		name    string
		prop    string
		spelled any
		want    any
		written string
	}{
		{"an integer at an Integer", "count", json.Number("5"), int64(5), `"count":5,`},
		{"-0 at an Integer", "count", json.Number("-0"), int64(0), `"count":0,`},
		{"an integer past int64 at an Integer", "count", json.Number("9223372036854775808"), json.Number("9223372036854775808"), `"count":9223372036854775808,`},
		{"an integer below int64 at an Integer", "count", json.Number("-9223372036854775809"), json.Number("-9223372036854775809"), `"count":-9223372036854775809,`},
		{"a whole float at an Integer", "count", json.Number("5.0"), float64(5), `"count":5.0,`},
		{"an integer at a Float", "level", json.Number("5"), float64(5), `"level":5.0}`},
		{"-0 at a Float", "level", json.Number("-0"), math.Copysign(0, -1), `"level":-0.0}`},
		{"an integer past 2^53 at a Float", "level", json.Number("9007199254740993"), float64(9007199254740992), `"level":9007199254740992.0}`},
		{"an integer past int64 at a Float", "level", json.Number("99999999999999999999"), float64(1e20), `"level":100000000000000000000.0}`},
		{"an exponent at a Float", "level", json.Number("1e2"), float64(100), `"level":100.0}`},
		{"a literal no float64 holds at a Float", "level", json.Number("1e400"), json.Number("1e400"), `"level":1e400}`},
		{"an integer at an alias of Float", "ratio", json.Number("7"), float64(7), `"ratio":7.0}`},
		{"integers in a List of Float", "levels", []any{json.Number("1"), json.Number("-0"), json.Number("2.5")}, []any{float64(1), math.Copysign(0, -1), 2.5}, `"levels":[1.0,-0.0,2.5]}`},
		{"integers in a List of Integer", "counts", []any{json.Number("1"), json.Number("18446744073709551616")}, []any{int64(1), json.Number("18446744073709551616")}, `"counts":[1,18446744073709551616],`},
		{"integers in a Vector", "points", []any{json.Number("3"), json.Number("4")}, []any{float64(3), float64(4)}, `"points":[3.0,4.0]}`},
		{"an integer at an edge property", "weight", json.Number("2"), float64(2), `"weight":2.0`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			doc := numberRuleDoc(t, s, gauge, c.prop, c.spelled)
			snap, res := snapshot.Load(t.Context(), doc, s)
			if !res.OK() {
				t.Fatalf("Load: %s", res)
			}
			g1, ok := snap.InstanceByKey(gauge, graph.FormatKey("g1"))
			if !ok {
				t.Fatal("g1 did not load")
			}
			var got any
			if c.prop == "weight" {
				for e := range snap.Edges() {
					got = e.Properties().Clone()["weight"]
				}
			} else {
				got = g1.Properties().Clone()[c.prop]
			}
			if !sameNumber(got, c.want) {
				t.Errorf("loaded %#v, want %#v", got, c.want)
			}
			data, mres := snapshot.Marshal(t.Context(), snap)
			if !mres.OK() {
				t.Fatalf("Marshal: %s", mres)
			}
			if !strings.Contains(string(data), c.written) {
				t.Errorf("the document does not state %s:\n%s", c.written, data)
			}
			again, lres := snapshot.Load(t.Context(), data, s)
			if !lres.OK() {
				t.Fatalf("second Load: %s", lres)
			}
			data2, mres2 := snapshot.Marshal(t.Context(), again)
			if !mres2.OK() {
				t.Fatalf("second Marshal: %s", mres2)
			}
			if string(data2) != string(data) {
				t.Errorf("the write is not a fixpoint of the next load:\n%s\n%s", data, data2)
			}
		})
	}
}

// An integer literal past int64 survives a load at an Integer, and revalidation
// refuses it there; at a Float it is a float64 revalidation accepts.
func TestLoad_RevalidationRefusesAnIntegerPastInt64AtAnInteger(t *testing.T) {
	t.Parallel()
	s, gauge := loadNumberRuleSchema(t)
	for prop, refused := range map[string]bool{"count": true, "level": false} {
		doc := numberRuleDoc(t, s, gauge, prop, json.Number("9223372036854775808"))
		_, res := snapshot.Load(t.Context(), doc, s, snapshot.WithRevalidation(diag.Error))
		if got := res.HasCode(diag.E_TYPE_MISMATCH); got != refused {
			t.Errorf("%s: revalidation refused = %v, want %v: %s", prop, got, refused, res)
		}
	}
}

// A key component the value rule reads at another rendering than its document's
// is no key, wherever a document states one, and a component is held to its key
// property by kind: the integer 1 and the float 1.0 render alike and differ.
func TestRead_AKeyHoldsOnlyNumbersThatReadBack(t *testing.T) {
	t.Parallel()
	s, gauge := loadNumberRuleSchema(t)
	root := func(key, id any) snapparts.Instance {
		return snapparts.Instance{TypeID: gauge, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{"id": id})}
	}
	sound := root(json.Number("1"), json.Number("1"))
	for _, c := range []struct {
		name   string
		parts  snapparts.Parts
		phrase string
	}{
		{
			"a root key 1 whose property is 1.0",
			snapparts.Parts{Instances: []snapparts.Instance{root(json.Number("1"), json.Number("1.0"))}},
			`component 0 disagrees with property "id", which holds [1.0]`,
		},
		{
			"a root key 100 whose property is 1e2",
			snapparts.Parts{Instances: []snapparts.Instance{root(json.Number("100"), json.Number("1e2"))}},
			`component 0 disagrees with property "id", which holds [1e2]`,
		},
		{
			"a root key past int64",
			snapparts.Parts{Instances: []snapparts.Instance{root(json.Number("9223372036854775808"), json.Number("9223372036854775808"))}},
			`stores key [9223372036854775808], whose component 0 is not a scalar graph.ParseKey reads back`,
		},
		{
			"a root key -0",
			snapparts.Parts{Instances: []snapparts.Instance{root(json.Number("-0"), json.Number("-0"))}},
			`stores key [-0], whose component 0 is not a scalar graph.ParseKey reads back`,
		},
		{"a record's source key 1.0", snapparts.Parts{
			Instances: []snapparts.Instance{sound},
			Unresolved: []snapparts.Unresolved{{
				SourceType: gauge, SourceKey: immutable.WrapKey([]any{json.Number("1.0")}), Relation: "LINK",
				TargetKey: immutable.WrapKey([]any{"ghost"}), Reason: "target_missing",
			}},
		}, `unresolved record 0 states source key [1.0], whose component 0 is not a scalar graph.ParseKey reads back`},
		{"a record's source key past int64", snapparts.Parts{
			Instances: []snapparts.Instance{sound},
			Unresolved: []snapparts.Unresolved{{
				SourceType: gauge, SourceKey: immutable.WrapKey([]any{json.Number("18446744073709551616")}), Relation: "LINK",
				TargetKey: immutable.WrapKey([]any{"ghost"}), Reason: "target_missing",
			}},
		}, `unresolved record 0 states source key [18446744073709551616], whose component 0`},
		{"an edge's target key past int64", snapparts.Parts{
			Instances: []snapparts.Instance{sound},
			Edges: []snapparts.Edge{{
				Relation: "LINK", SourceType: gauge, SourceKey: immutable.WrapKey([]any{json.Number("1")}),
				TargetKey: immutable.WrapKey([]any{json.Number("9223372036854775808")}),
			}},
		}, `carries target key [9223372036854775808], whose component 0`},
		{"a duplicate's key 1.0", snapparts.Parts{
			Instances:  []snapparts.Instance{sound},
			Duplicates: []snapparts.Instance{root(json.Number("1.0"), json.Number("1"))},
		}, `whose component 0 is not a scalar graph.ParseKey reads back`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			c.parts.Types = []schema.TypeID{gauge}
			res := snapshot.Verify(t.Context(), snappartstest.Document(t, s, c.parts), s, snapshot.WithIssueLimit(0))
			found := false
			for issue := range res.Errors() {
				found = found || issue.Code() == diag.E_SNAPSHOT_MALFORMED && strings.Contains(issue.Message(), c.phrase)
			}
			if !found {
				t.Errorf("want %s naming %q: %s", diag.E_SNAPSHOT_MALFORMED, c.phrase, res)
			}
		})
	}

	doc := snappartstest.Document(t, s, snapparts.Parts{Types: []schema.TypeID{gauge}, Instances: []snapparts.Instance{sound}})
	if res := snapshot.Verify(t.Context(), doc, s); !res.OK() {
		t.Errorf("a key 1 whose property is 1 was refused: %s", res)
	}

	withDup := string(snappartstest.Document(t, s, snapparts.Parts{
		Types: []schema.TypeID{gauge}, Instances: []snapparts.Instance{sound}, Duplicates: []snapparts.Instance{sound},
	}))
	if res := snapshot.Verify(t.Context(), []byte(withDup), s); !res.OK() {
		t.Errorf("a duplicate of the key 1 was refused: %s", res)
	}
	for name, edit := range map[string][2]string{
		"a duplicate record's key 1.0": {`"duplicates":[{"type":0,"key":[1],`, `"duplicates":[{"type":0,"key":[1.0],`},
		"a conflict key 1.0":           {`"conflict":{"type":0,"key":[1]}`, `"conflict":{"type":0,"key":[1.0]}`},
	} {
		res := snapshot.Verify(t.Context(), []byte(editOnce(t, withDup, edit[0], edit[1])), s, snapshot.WithIssueLimit(0))
		if sbFailures(res) != 1 || !strings.Contains(res.String(), `states`) || !strings.Contains(res.String(), `[1.0], whose component 0 is not a scalar graph.ParseKey reads back`) {
			t.Errorf("%s: want its one refusal: %s", name, res)
		}
	}

	// A refused reference key addresses nothing, so the record draws no second
	// issue for the address it would name: a duplicate's key [1.0] beside its
	// instance's key [2], and a conflict key [1.0] where no root stands at [1].
	two := root(json.Number("2"), json.Number("2"))
	withDupOfTwo := string(snappartstest.Document(t, s, snapparts.Parts{
		Types: []schema.TypeID{gauge}, Instances: []snapparts.Instance{two}, Duplicates: []snapparts.Instance{two},
	}))
	for name, edit := range map[string][2]string{
		"a duplicate record's key 1.0 beside its instance's key 2": {`"duplicates":[{"type":0,"key":[2],`, `"duplicates":[{"type":0,"key":[1.0],`},
		"a conflict key 1.0 naming no root":                        {`"conflict":{"type":0,"key":[2]}`, `"conflict":{"type":0,"key":[1.0]}`},
	} {
		res := snapshot.Verify(t.Context(), []byte(editOnce(t, withDupOfTwo, edit[0], edit[1])), s, snapshot.WithIssueLimit(0))
		if sbFailures(res) != 1 || !strings.Contains(res.String(), `[1.0], whose component 0 is not a scalar graph.ParseKey reads back`) {
			t.Errorf("%s: want its one refusal: %s", name, res)
		}
	}

	// A record's source key is judged whether or not its source row resolves.
	withRecord := string(snappartstest.Document(t, s, snapparts.Parts{
		Types: []schema.TypeID{gauge}, Instances: []snapparts.Instance{sound},
		Unresolved: []snapparts.Unresolved{{
			SourceType: gauge, SourceKey: immutable.WrapKey([]any{json.Number("1.0")}), Relation: "LINK",
			TargetKey: immutable.WrapKey([]any{"ghost"}), Reason: "target_missing",
		}},
	}))
	res := snapshot.Verify(t.Context(), []byte(editOnce(t, withRecord, `"source_type":0`, `"source_type":9`)), s, snapshot.WithIssueLimit(0))
	if sbFailures(res) != 2 || !strings.Contains(res.String(), `source references types table row 9`) ||
		!strings.Contains(res.String(), `unresolved record 0 states source key [1.0], whose component 0`) {
		t.Errorf("a record of an unresolved row with source key 1.0: want both refusals: %s", res)
	}

	// A key property may spell its component's number otherwise, as long as both
	// read to one value of one kind.
	for _, kp := range [][2]json.Number{{"1.5", "1.50"}, {"0", "-0"}, {"100", "100"}} {
		doc := snappartstest.Document(t, s, snapparts.Parts{Types: []schema.TypeID{gauge}, Instances: []snapparts.Instance{root(kp[0], kp[1])}})
		if res := snapshot.Verify(t.Context(), doc, s); !res.OK() {
			t.Errorf("key [%s] with property %s was refused: %s", kp[0], kp[1], res)
		}
	}
}

// An unresolved record's properties are read at the constraints its relation
// declares, as an edge's are.
func TestLoad_ReadsAnUnresolvedRecordsNumberAtItsRelationsKind(t *testing.T) {
	t.Parallel()
	s, gauge := loadNumberRuleSchema(t)
	doc := snappartstest.Document(t, s, snapparts.Parts{
		Types: []schema.TypeID{gauge},
		Instances: []snapparts.Instance{
			{TypeID: gauge, PrimaryKey: immutable.WrapKey([]any{"g1"}), Properties: immutable.WrapProperties(map[string]any{"id": "g1"})},
		},
		Unresolved: []snapparts.Unresolved{{
			SourceType: gauge, SourceKey: immutable.WrapKey([]any{"g1"}), Relation: "LINK",
			TargetKey: immutable.WrapKey([]any{"ghost"}), Reason: "target_missing",
			Properties: immutable.WrapProperties(map[string]any{"weight": json.Number("2")}),
		}},
	})
	snap, res := snapshot.Load(t.Context(), doc, s)
	if !res.OK() {
		t.Fatalf("Load: %s", res)
	}
	for rec := range snap.Unresolved() {
		if got, _ := rec.Properties().Get("weight"); got.Unwrap() != float64(2) {
			t.Errorf("the record's weight is %#v, want float64(2)", got.Unwrap())
		}
	}
	data, mres := snapshot.Marshal(t.Context(), snap)
	if !mres.OK() {
		t.Fatalf("Marshal: %s", mres)
	}
	if !strings.Contains(string(data), `"weight":2.0`) {
		t.Errorf("the record's weight is not written as a float:\n%s", data)
	}
}
