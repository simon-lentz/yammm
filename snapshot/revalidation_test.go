package snapshot_test

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The revalidation matrix schema: one type carrying a bound, an enum, a pattern, an
// invariant, an edge with a declared property, a required association, and a
// required composition — each violated by exactly one matrix row.
const revalSchema = `schema "reval"

type Target {
	id String primary
}

part type Item {
	sku Pattern["^sku-"] required
}

type Thing {
	id String primary
	count Integer[1, 10]
	state Enum["on", "off"]
	code Pattern["^c-"]

	--> LINKS (_:many) Target { note String }
	--> OWNER (one) Target

	*-> ITEMS (one:many) Item

	! "id must not be banned" id != "banned"
}
`

func revalLoadSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), revalSchema, "reval.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	return s
}

// revalThing is Thing t1 holding a value at every constrained position and one
// item, which the validator accepts.
func revalThing() map[string]any {
	return map[string]any{
		"id": "t1", "count": int64(5), "state": "on", "code": "c-9",
		"items": []any{map[string]any{"sku": "sku-1"}},
	}
}

// revalEdit rewrites a document's text: from is a regular expression.
type revalEdit struct{ from, to string }

// revalDocumentOf marshals insts, applies edits to the text and rehashes it, so
// Load reads a document whose integrity holds and whose values no validator
// approved. Its header states so.
func revalDocumentOf(t *testing.T, s schema.Schema, insts []instance.ValidInstance, edits ...revalEdit) []byte {
	t.Helper()
	g := graph.New(s)
	for _, inst := range insts {
		if r := g.Add(t.Context(), inst); !r.OK() {
			t.Fatalf("Add: %s", r.String())
		}
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres.String())
	}
	for _, e := range append([]revalEdit{{`"attestation":\{"values":true`, `"attestation":{"values":false`}}, edits...) {
		re := regexp.MustCompile(e.from)
		if !re.Match(data) {
			t.Fatalf("no %s in %s", e.from, data)
		}
		data = re.ReplaceAllLiteral(data, []byte(e.to))
	}
	return rehashDocument(t, data)
}

// revalDocument is the document of the Thing props states. A Target "x1" rides
// along so an edge to it resolves: an unresolved edge moves to the diagnostics
// section and never reaches the instance's edges.
func revalDocument(t *testing.T, s schema.Schema, props map[string]any, edits ...revalEdit) []byte {
	t.Helper()
	return revalDocumentOf(t, s, []instance.ValidInstance{
		instancetest.Valid(t, s, "Target", map[string]any{"id": "x1"}),
		instancetest.Valid(t, s, "Thing", props),
	}, edits...)
}

// TestLoad_RevalidationMatrix pins the revalidation matrix: every constraint
// class the validator enforces is reported under WithRevalidation and
// silent without it.
func TestLoad_RevalidationMatrix(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)

	cases := []struct {
		name string
		edit revalEdit
		want diag.Code
	}{
		{"bound", revalEdit{`"count":5`, `"count":99`}, diag.E_CONSTRAINT_FAIL},
		{"enum", revalEdit{`"state":"on"`, `"state":"neither"`}, diag.E_CONSTRAINT_FAIL},
		{"pattern", revalEdit{`"code":"c-9"`, `"code":"bad"`}, diag.E_CONSTRAINT_FAIL},
		{"invariant", revalEdit{`"t1"`, `"banned"`}, diag.E_INVARIANT_FAIL},
		{"required_composition", revalEdit{`,"composed":\{"ITEMS":\[.*?\]\}`, ""}, diag.E_UNRESOLVED_REQUIRED_COMPOSITION},
		{"composed_child_constraint", revalEdit{`"sku":"sku-1"`, `"sku":"bad"`}, diag.E_CONSTRAINT_FAIL},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := revalDocument(t, s, revalThing(), tc.edit)

			// Silent without the option.
			_, plain := snapshot.Load(t.Context(), data, s)
			if plain.HasErrors() {
				t.Fatalf("plain Load refused the document: %s", plain.String())
			}
			if plain.HasCode(tc.want) {
				t.Fatalf("plain Load already reports %s; the option gates nothing", tc.want)
			}

			// Reported under the option, still loading at Warning severity.
			snap, res := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Warning))
			if res.HasErrors() || snap.IsZero() {
				t.Fatalf("Warning-severity revalidation refused the load: %s", res.String())
			}
			if !res.HasCode(tc.want) {
				t.Errorf("revalidation did not report %s: %s", tc.want, res.String())
			}

			// Error severity refuses the document.
			snap, res = snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
			if !snap.IsZero() || !res.HasErrors() {
				t.Error("Error-severity revalidation returned a snapshot for a failing document")
			}
		})
	}
}

// revalCleanDocument marshals a validator-built Target and Thing, so a
// revalidating load of it reports nothing about the data.
func revalCleanDocument(t *testing.T, s schema.Schema) []byte {
	t.Helper()
	v := instance.NewValidator(s)

	g := graph.New(s)
	target, res := v.ValidateOne(t.Context(), "Target", instance.RawInstance{Properties: map[string]any{"id": "x1"}})
	if !res.OK() {
		t.Fatalf("validate Target: %s", res.String())
	}
	thing, res := v.ValidateOne(t.Context(), "Thing", instance.RawInstance{Properties: map[string]any{
		"id":    "t1",
		"count": int64(5),
		"state": "on",
		"code":  "c-9",
		"owner": map[string]any{"_target_id": "x1"},
		"links": []any{map[string]any{"_target_id": "x1", "note": "n"}},
		"items": []any{map[string]any{"sku": "sku-1"}},
	}})
	if !res.OK() {
		t.Fatalf("validate Thing: %s", res.String())
	}
	for _, vi := range []instance.ValidInstance{target, thing} {
		if r := g.Add(t.Context(), vi); !r.OK() {
			t.Fatalf("Add: %s", r.String())
		}
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres.String())
	}
	return data
}

func TestLoad_RevalidationCleanDataSilent(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalCleanDocument(t, s)

	snap, lres := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
	if snap.IsZero() || lres.HasErrors() {
		t.Fatalf("clean validator-built data failed revalidation: %s", lres.String())
	}
	for issue := range lres.Issues() {
		t.Errorf("clean data drew an issue: %s", issue.Message())
	}
}

// cancelAfterPolls is a context that reports itself cancelled from its n-th
// Err poll on. Nothing under Load selects on Done, so a load can be cancelled
// at any one of the points it polls.
type cancelAfterPolls struct {
	mu    sync.Mutex
	polls int
	after int
}

func (c *cancelAfterPolls) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *cancelAfterPolls) Done() <-chan struct{}       { return nil }
func (c *cancelAfterPolls) Value(any) any               { return nil }

func (c *cancelAfterPolls) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.polls++
	if c.polls >= c.after {
		return context.Canceled
	}
	return nil
}

func (c *cancelAfterPolls) polled() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.polls
}

// TestLoad_RevalidationReportsACancelledRowAtItsOwnSeverity pins that a row
// the revalidator could not finish reports the Fatal E_CONTEXT_CANCELLED the
// validator raised, not a Warning about the data, whatever severity the option
// names. The load is cancelled at each of its polls in turn, which puts one
// cancellation inside a revalidated row, where the issue carries the row's
// root path.
func TestLoad_RevalidationReportsACancelledRowAtItsOwnSeverity(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalCleanDocument(t, s)

	sawRow := false
	for after := 1; ; after++ {
		ctx := &cancelAfterPolls{after: after}
		snap, res := snapshot.Load(ctx, data, s, snapshot.WithRevalidation(diag.Warning))
		if ctx.polled() < after {
			if snap.IsZero() {
				t.Fatalf("an uncancelled load failed: %s", res)
			}
			break
		}
		if !snap.IsZero() {
			t.Fatalf("poll %d: a cancelled load returned a snapshot", after)
		}
		if !res.HasCode(diag.E_CONTEXT_CANCELLED) {
			t.Fatalf("poll %d: a cancelled load did not report the cancellation: %s", after, res)
		}
		if n := res.Len(); n != 1 {
			t.Errorf("poll %d: one cancellation was reported %d times: %s", after, n, res)
		}
		for issue := range res.Issues() {
			if issue.Code() != diag.E_CONTEXT_CANCELLED {
				t.Errorf("poll %d: a cancelled load reported %s %s: %s", after, issue.Severity(), issue.Code(), issue.Message())
				continue
			}
			if issue.Severity() != diag.Fatal {
				t.Errorf("poll %d: the cancellation is %s, want Fatal: %s", after, issue.Severity(), issue.Message())
			}
			if strings.HasPrefix(issue.Message(), "revalidation of") {
				t.Errorf("poll %d: the cancellation was reported as a finding about the data: %s", after, issue.Message())
			}
			if issue.Path() == "$" {
				sawRow = true
			}
		}
	}
	if !sawRow {
		t.Fatal("no cancellation landed inside a revalidated row")
	}
}

// TestLoad_RefusesAnEdgeUnderAnUndeclaredRelation pins that an edge under a
// relation name the type does not declare as an association is a structural
// fault: Load and Verify refuse the document with no option.
func TestLoad_RefusesAnEdgeUnderAnUndeclaredRelation(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)

	linked := revalThing()
	linked["links"] = []any{map[string]any{"_target_id": "x1"}}
	data := revalDocument(t, s, linked)
	edited := bytes.Replace(data, []byte(`"LINKS":`), []byte(`"BOGUS":`), 1)
	if bytes.Equal(edited, data) {
		t.Fatalf("no LINKS edge to rename in %s", data)
	}
	if _, lres := snapshot.Load(t.Context(), edited, s, snapshot.WithIntegrityCheck(false)); !lres.HasCode(diag.E_GRAPH_UNKNOWN_RELATION) {
		t.Errorf("Load did not refuse an edge under an undeclared relation: %s", lres)
	}
	if vres := snapshot.Verify(t.Context(), edited, s, snapshot.WithIntegrityCheck(false)); !vres.HasCode(diag.E_GRAPH_UNKNOWN_RELATION) {
		t.Errorf("Verify did not refuse an edge under an undeclared relation: %s", vres)
	}
}

// TestLoad_RevalidationUnresolvedRequired pins W_SNAPSHOT_UNRESOLVED_REQUIRED:
// an unresolved record for a Required association is reported only under the
// option.
func TestLoad_RevalidationUnresolvedRequired(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	owned := revalThing()
	owned["owner"] = map[string]any{"_target_id": "missing"}
	data := revalDocument(t, s, owned)

	_, plain := snapshot.Load(t.Context(), data, s)
	if plain.HasCode(diag.W_SNAPSHOT_UNRESOLVED_REQUIRED) {
		t.Fatal("the unresolved-required warning fired without the option")
	}

	_, res := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Warning))
	if !res.HasCode(diag.W_SNAPSHOT_UNRESOLVED_REQUIRED) {
		t.Errorf("a Required unresolved record was not reported: %s", res.String())
	}
}

// TestLoad_RevalidationJudgesANumberByItsText pins that revalidation hands the
// validator the document's own number text: an Integer literal outside int64
// is refused on both sides of the range, where the value just below the
// minimum once rounded to math.MinInt64 and passed, and a float literal is
// refused at an Integer whole or not. The body is tampered under
// WithIntegrityCheck(false), because the writer never emits one.
func TestLoad_RevalidationJudgesANumberByItsText(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, `schema "num"

type T {
	id String primary
	n Integer
	f Float
	s String
}
`, "num.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	vi, vr := instance.NewValidator(s).ValidateOne(ctx, "T", instance.RawInstance{
		Properties: map[string]any{"id": "a", "n": int64(7), "f": 1.5, "s": "x"},
	})
	if vr.HasErrors() {
		t.Fatalf("validate: %s", vr.String())
	}
	g := graph.New(s)
	if r := g.Add(ctx, vi); r.HasErrors() {
		t.Fatalf("add: %s", r.String())
	}
	clean, mr := snapshot.Marshal(ctx, g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr.String())
	}
	for _, c := range []struct {
		name, from, to string
		code           diag.Code
		message        string
	}{
		{name: "untouched"},
		{name: "an Integer below int64", from: `"n":7`, to: `"n":-9223372036854775809`, code: diag.E_TYPE_MISMATCH, message: "an integer outside the int64 range: -9223372036854775809"},
		{name: "an Integer above int64", from: `"n":7`, to: `"n":9223372036854775808`, code: diag.E_TYPE_MISMATCH, message: "an integer outside the int64 range: 9223372036854775808"},
		{name: "a Float holding a wide integer", from: `"f":1.5`, to: `"f":99999999999999999999`},
		{name: "an Integer written 7.0", from: `"n":7`, to: `"n":7.0`, code: diag.E_TYPE_MISMATCH, message: "expected integer, got float 7.0"},
		{name: "an Integer written 7e0", from: `"n":7`, to: `"n":7e0`, code: diag.E_TYPE_MISMATCH, message: "expected integer, got float 7.0"},
		{name: "an Integer written 7.5", from: `"n":7`, to: `"n":7.5`, code: diag.E_TYPE_MISMATCH, message: "expected integer, got float 7.5"},
		{name: "an Integer written just below int64 with a point", from: `"n":7`, to: `"n":-9223372036854775809.0`, code: diag.E_TYPE_MISMATCH, message: "expected integer, got float -9223372036854776000.0"},
		{name: "a number at a String", from: `"s":"x"`, to: `"s":5`, code: diag.E_TYPE_MISMATCH, message: "expected string"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			data := clean
			if c.from != "" {
				if !bytes.Contains(data, []byte(c.from)) {
					t.Fatalf("the document does not hold %q", c.from)
				}
				data = bytes.Replace(clean, []byte(c.from), []byte(c.to), 1)
			}
			_, res := snapshot.Load(ctx, data, s,
				snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error))
			var got []string
			for issue := range res.Issues() {
				got = append(got, issue.Code().String()+": "+issue.Message())
			}
			if c.message == "" {
				if len(got) != 0 {
					t.Fatalf("issues = %v; want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.HasPrefix(got[0], c.code.String()+": ") || !strings.Contains(got[0], c.message) {
				t.Fatalf("issues = %v; want one %s holding %q", got, c.code, c.message)
			}
		})
	}
}

// TestLoad_RevalidationJudgesAnEdgePropertyNumberByItsText pins the same rule
// on an edge: an Integer edge property holding a literal just below int64 is
// refused as written, not passed as math.MinInt64, and a number at a
// target_missing record's target key is named as the decoder's json.Number.
func TestLoad_RevalidationJudgesAnEdgePropertyNumberByItsText(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, `schema "edgenum"

type U {
	id String primary
}

type T {
	id String primary
	--> LINKS (_:many) U { n Integer }
}
`, "edgenum.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	v := instance.NewValidator(s)
	u, ur := v.ValidateOne(ctx, "U", instance.RawInstance{Properties: map[string]any{"id": "u1"}})
	if ur.HasErrors() {
		t.Fatalf("validate U: %s", ur.String())
	}
	ti, tr := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: map[string]any{
		"id": "t1",
		"links": []any{
			map[string]any{"_target_id": "u1", "n": int64(7)},
			map[string]any{"_target_id": "u9", "n": int64(8)},
		},
	}})
	if tr.HasErrors() {
		t.Fatalf("validate T: %s", tr.String())
	}
	g := graph.New(s)
	for _, vi := range []instance.ValidInstance{u, ti} {
		if r := g.Add(ctx, vi); r.HasErrors() {
			t.Fatalf("add: %s", r.String())
		}
	}
	clean, mr := snapshot.Marshal(ctx, g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr.String())
	}
	for _, c := range []struct {
		name, from, to string
		code           diag.Code
		message        string
	}{
		{name: "untouched"},
		{name: "an edge property below int64", from: `"n":7`, to: `"n":-9223372036854775809`, code: diag.E_TYPE_MISMATCH, message: "an integer outside the int64 range: -9223372036854775809"},
		{name: "an edge property above int64", from: `"n":7`, to: `"n":9223372036854775808`, code: diag.E_TYPE_MISMATCH, message: "an integer outside the int64 range: 9223372036854775808"},
		{name: "a number at a record's target key", from: `"target_key":["u9"]`, to: `"target_key":[5]`, code: diag.E_TYPE_MISMATCH, message: `FK field "_target_id": expected string, got json.Number`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			data := clean
			if c.from != "" {
				if !bytes.Contains(data, []byte(c.from)) {
					t.Fatalf("the document does not hold %q:\n%s", c.from, clean)
				}
				data = bytes.Replace(clean, []byte(c.from), []byte(c.to), 1)
			}
			_, res := snapshot.Load(ctx, data, s,
				snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error))
			var got []string
			for issue := range res.Issues() {
				got = append(got, issue.Code().String()+": "+issue.Message())
			}
			if c.message == "" {
				if len(got) != 0 {
					t.Fatalf("issues = %v; want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.HasPrefix(got[0], c.code.String()+": ") || !strings.Contains(got[0], c.message) {
				t.Fatalf("issues = %v; want one %s holding %q", got, c.code, c.message)
			}
		})
	}
}
