package yammm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/snapparts"

	neo4jadapter "github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const echoSchema = `schema "echo"

type T {
	id String primary
	e Enum["a", "b"]
	d Date
	ts Timestamp
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
// alone, and every message and detail is valid UTF-8, whichever path the
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
		"e": longText, "d": longText, "ts": longText, "u": longText, "p": longText,
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
		if inst.IsZero() {
			t.Fatal(r)
		}
		c.result("graph.Add", g.Add(ctx, inst))
	}
	root := func(key string, props map[string]any) snapparts.Instance {
		return snapparts.Instance{TypeID: typ.ID(), PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(props)}
	}
	const marker = "marker"
	clean := snappartstest.Document(t, s, snapparts.Parts{
		Types:     []schema.TypeID{typ.ID()},
		Instances: []snapparts.Instance{root("h1", map[string]any{"id": "h1", "s": marker})},
	})
	for _, spelled := range []string{longText + "\xff", `\ud800` + longText} {
		_, r = snapshot.Load(ctx, bytes.Replace(clean, []byte(marker), []byte(spelled), 1), s)
		refused := false
		for issue := range r.Issues() {
			refused = refused || issue.Code() == diag.E_SNAPSHOT_MALFORMED
		}
		if !refused {
			t.Errorf("snapshot.Load of a string spelled %.20q drew no E_SNAPSHOT_MALFORMED: %.200s", spelled, r)
		}
		c.result("snapshot.Load", r)
	}
	_, err := graph.ParseKey(longText)
	c.err("graph.ParseKey", err)

	second := root(longText+"2", map[string]any{"id": longText + "2"})
	data := snappartstest.Document(t, s, snapparts.Parts{
		Types: []schema.TypeID{typ.ID()},
		Instances: []snapparts.Instance{
			root(longText, map[string]any{"id": longText, "u": longText, "d": longText, "ts": longText}),
			second,
		},
		Duplicates: []snapparts.Instance{second},
	})
	_, r = snapshot.Load(ctx, data, s, snapshot.WithRevalidation(diag.Warning), snapshot.WithValueConformance(true))
	c.result("snapshot.Load", r)

	for _, k := range []schema.Constraint{schema.NewTimestampConstraint(), schema.NewDateConstraint()} {
		_, err := neo4jadapter.CoerceParams(map[string]any{"v": longText}, neo4jadapter.ParamTypes{"v": k})
		c.err("neo4j.CoerceParams", err)
	}

	for _, where := range []string{"validator", "graph.Add", "graph.ParseKey", "snapshot.Load", "neo4j.CoerceParams"} {
		if c.judged[where] == 0 {
			t.Errorf("%s wrote no message to judge", where)
		}
	}
}
