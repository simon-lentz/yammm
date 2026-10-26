package yammm_test

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	csvadapter "github.com/simon-lentz/yammm/adapter/csv"
	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	neo4jadapter "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const echoSchema = `schema "echo"

type T {
	id String primary
	e Enum["a", "b"]
	d Date
	ts Timestamp
	tf Timestamp["2006-01-02"]
	u UUID
	p Pattern["^a$"]
	s String
	n Integer
	f Float
	b Boolean
	*-> PARTS (many) P
}

part type P {
	pid String primary
}
`

// The markers are long runs of one byte, so a message that repeats more than
// a value's head holds a longer run than the cut allows.
var (
	longText   = strings.Repeat("Z", 4096)
	longDigits = strings.Repeat("9", 4096)
)

// longestRun returns the longest run of b in s.
func longestRun(s string, b byte) int {
	longest, run := 0, 0
	for i := range len(s) {
		if s[i] == b {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// echoChecker judges a message by what it shows, not by how it was built: it
// is valid UTF-8, and it repeats no marker past the 32 bytes a value's head
// holds.
type echoChecker struct {
	t      *testing.T
	judged map[string]int
}

func (c *echoChecker) text(where, s string) {
	c.t.Helper()
	c.judged[where]++
	if !utf8.ValidString(s) {
		c.t.Errorf("%s: the message is not valid UTF-8: %.200q", where, s)
	}
	for _, b := range []byte{'Z', '9'} {
		if n := longestRun(s, b); n > 32 {
			c.t.Errorf("%s: the message repeats %d bytes of a value, want at most 32: %.200q", where, n, s)
		}
	}
}

func (c *echoChecker) result(where string, res diag.Result) {
	c.t.Helper()
	for issue := range res.Issues() {
		c.text(where, issue.Message())
		for _, d := range issue.Details() {
			if !utf8.ValidString(d.Value) {
				c.t.Errorf("%s: detail %s is not valid UTF-8: %.200q", where, d.Key, d.Value)
			}
		}
	}
}

func (c *echoChecker) err(where string, err error) {
	c.t.Helper()
	if err == nil {
		c.t.Errorf("%s: no error to judge", where)
		return
	}
	c.text(where, err.Error())
}

// Every message the data paths write about a long value repeats its head
// alone, and every message and detail is valid UTF-8, whichever door the
// value came through.
func TestMessages_RepeatAValueCutToItsHead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(ctx, echoSchema, "echo.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	typ, _ := s.Type("T")
	c := &echoChecker{t: t, judged: map[string]int{}}

	v := instance.NewValidator(s)
	for prop, val := range map[string]any{
		"e": longText, "d": longText, "ts": longText, "tf": longText, "u": longText, "p": longText,
		"s": longText + "\xff", "n": json.Number(longDigits), "f": json.Number(longDigits + "e400"),
	} {
		_, r := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{"id": "a", prop: val}})
		if r.OK() {
			t.Errorf("property %s: the long value was accepted", prop)
		}
		c.result("validator", r)
	}
	_, r := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{"id": "a", "te\xffxt": "x"}})
	c.result("validator", r)

	g := graph.New(s)
	for range 2 {
		inst, r := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{"id": longText}})
		if inst == nil {
			t.Fatal(r)
		}
		c.result("graph.Add", g.Add(ctx, inst))
	}
	c.result("graph.Add", g.Add(ctx, instance.NewValidInstance("T", typ.ID(), immutable.WrapKey([]any{"h1"}),
		immutable.WrapProperties(map[string]any{"id": "h1", "te\xffxt": "x"}), nil, nil, nil)))
	c.result("graph.AddComposed", g.AddComposed(ctx, typ.ID(), `["absent `+longText+`"]`, "NONE",
		instance.NewValidInstance("T", typ.ID(), immutable.WrapKey([]any{"c"}), immutable.WrapProperties(map[string]any{"id": "c"}), nil, nil, nil)))
	_, err := graph.ParseKey(longText)
	c.err("graph.ParseKey", err)

	bypass := graph.New(s)
	bypass.Add(ctx, instance.NewValidInstance("T", typ.ID(), immutable.WrapKey([]any{longText}),
		immutable.WrapProperties(map[string]any{"id": longText, "u": longText, "d": longText, "ts": longText, "tf": longText}), nil, nil, nil))
	for range 2 {
		inst, _ := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{"id": longText + "2"}})
		bypass.Add(ctx, inst)
	}
	data, r := snapshot.Marshal(ctx, bypass.Snapshot())
	c.result("snapshot.Marshal", r)
	if data != nil {
		_, r = snapshot.Load(ctx, data, s, snapshot.WithRevalidation(diag.Warning), snapshot.WithValueConformance(true))
		c.result("snapshot.Load", r)
	}

	unwritable := graph.New(s)
	unwritable.Add(ctx, instance.NewValidInstance("T", typ.ID(), immutable.WrapKey([]any{longText}),
		immutable.WrapProperties(map[string]any{"id": longText, "f": math.NaN()}), nil, nil, nil))
	_, err = jsonadapter.New().MarshalObject(ctx, unwritable.Snapshot())
	c.err("json writer", err)
	_, err = csvadapter.New().MarshalSnapshot(ctx, unwritable.Snapshot())
	c.err("csv writer", err)
	pt, _ := s.Type("P")
	composed := graph.New(s)
	composed.Add(ctx, instance.NewValidInstance("T", typ.ID(), immutable.WrapKey([]any{longText}), immutable.WrapProperties(map[string]any{"id": longText}), nil,
		map[string]immutable.Value{"PARTS": immutable.Wrap([]any{
			instance.NewValidInstance("P", pt.ID(), immutable.WrapKey([]any{"c"}), immutable.WrapProperties(map[string]any{"pid": "c"}), nil, nil, nil),
		})}, nil))
	_, err = csvadapter.New().MarshalSnapshot(ctx, composed.Snapshot())
	c.err("csv writer", err)
	ls, res := schema.LoadString(ctx, "schema \"links\"\n\ntype L {\n\tid String primary\n\t--> TO (many) M {\n\t\tw Float\n\t}\n}\n\ntype M {\n\tid String primary\n}\n", "links.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	lt, _ := ls.Type("L")
	mt, _ := ls.Type("M")
	linked := graph.New(ls)
	linked.Add(ctx, instance.NewValidInstance("M", mt.ID(), immutable.WrapKey([]any{longText}), immutable.WrapProperties(map[string]any{"id": longText}), nil, nil, nil))
	linked.Add(ctx, instance.NewValidInstance("L", lt.ID(), immutable.WrapKey([]any{"l"}), immutable.WrapProperties(map[string]any{"id": "l"}),
		map[string]*instance.ValidEdgeData{"TO": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{longText}), immutable.WrapProperties(map[string]any{"w": math.NaN()})),
		})}, nil, nil))
	_, err = jsonadapter.New().MarshalObject(ctx, linked.Snapshot())
	c.err("json writer", err)

	csvSource := location.MustNewSourceID("test://echo.csv")
	row := "x," + strings.Repeat(longText+",", 5) + longText + "\n" + "y," + longDigits + "," + longDigits + "e400,,,,\n"
	_, r = csvadapter.New().ParseTyped(ctx, csvSource, "T", strings.NewReader("id,n,f,b,d,ts,tf\n"+row), typ)
	if r.OK() {
		t.Error("csv: the long cells were accepted")
	}
	c.result("csv", r)

	for _, k := range []schema.Constraint{schema.NewTimestampConstraint(), schema.NewTimestampConstraintFormatted("2006-01-02"), schema.NewDateConstraint()} {
		_, err := neo4jadapter.Coerce(k, longText)
		c.err("neo4j.Coerce", err)
	}

	for _, where := range []string{"validator", "graph.Add", "graph.AddComposed", "graph.ParseKey", "snapshot.Load", "json writer", "csv writer", "csv", "neo4j.Coerce"} {
		if c.judged[where] == 0 {
			t.Errorf("%s wrote no message to judge", where)
		}
	}
}
