package snapshot_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const danglingKeySchema = `schema "dk"

type U {
	id String primary
}

type V {
	id String primary
	day Date primary
}

type W {
	day Date primary
}

type X {
	code String[3, 3] primary
}

type S {
	id UUID primary
	--> TO_U (_:many) U
}

part type Line {
	n Integer[0, 5]
}

type T {
	id String primary
	age Integer[0, 150]
	*-> LINES (_:many) Line
	--> TO_U (_:many) U
	--> TO_V (_:many) V
	--> TO_W (_:many) W
	--> TO_X (_:many) X
}
`

const sourceUUID = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"

// danglingKeyDoc marshals a graph whose root t1 holds one edge under each of
// its associations, every one resolving, a composed Line, and a target_missing
// record under TO_U naming u9; and whose root S, keyed by sourceUUID, holds one
// edge to u2.
func danglingKeyDoc(t *testing.T) (schema.Schema, []byte) {
	t.Helper()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, danglingKeySchema, "dk.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, raw := range []struct {
		typ   string
		props map[string]any
	}{
		{"U", map[string]any{"id": "u1"}},
		{"U", map[string]any{"id": "u2"}},
		{"S", map[string]any{"id": sourceUUID, "to_u": []any{map[string]any{"_target_id": "u2"}}}},
		{"V", map[string]any{"id": "v1", "day": "2024-01-02"}},
		{"W", map[string]any{"day": "2024-01-02"}},
		{"X", map[string]any{"code": "abc"}},
		{"T", map[string]any{
			"id": "t1", "age": int64(30),
			"lines": []any{map[string]any{"n": int64(1)}},
			"to_u":  []any{map[string]any{"_target_id": "u1"}, map[string]any{"_target_id": "u9"}},
			"to_v":  []any{map[string]any{"_target_id": "v1", "_target_day": "2024-01-02"}},
			"to_w":  []any{map[string]any{"_target_day": "2024-01-02"}},
			"to_x":  []any{map[string]any{"_target_code": "abc"}},
		}},
	} {
		vi, vr := v.ValidateOne(ctx, raw.typ, instance.RawInstance{Properties: raw.props})
		if vr.HasErrors() {
			t.Fatalf("validate %s: %s", raw.typ, vr.String())
		}
		if r := g.Add(ctx, vi); r.HasErrors() {
			t.Fatalf("add %s: %s", raw.typ, r.String())
		}
	}
	doc, mr := snapshot.Marshal(ctx, g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr.String())
	}
	return s, doc
}

func readIssues(res diag.Result) []string {
	var got []string
	for issue := range res.Issues() {
		got = append(got, issue.Code().String()+": "+issue.Message())
	}
	return got
}

// An edge that names no root draws the one reference issue, whatever its target
// key's components hold, and revalidation judges no component of it that its
// key property refuses. A target_missing record names a target as an edge
// does: revalidation judges its key unless a refused unit stands at its target,
// as it judges an edge's.
func TestRead_AnEdgeThatNamesNoRootDrawsOneIssueForItsKey(t *testing.T) {
	t.Parallel()
	s, doc := danglingKeyDoc(t)
	const (
		edgeU  = `"target_key":["u1"]`
		edgeV  = `"target_key":["v1","2024-01-02"]`
		edgeW  = `"target_key":["2024-01-02"]`
		edgeX  = `"target_key":["abc"]`
		lostU9 = `"target_key":["u9"]`
	)
	dangling := func(phrase string) []string {
		return []string{diag.E_SNAPSHOT_DANGLING_REFERENCE.String() + ": edge target " + phrase}
	}
	for _, c := range []struct {
		name  string
		edits [][2]string
		plain []string
		// reval is what a read under revalidation at Error adds to plain.
		reval []string
	}{
		{name: "untouched"},
		{
			name:  "a key that resolves to no root",
			edits: [][2]string{{edgeU, strings.Replace(edgeU, `"u1"`, `"u7"`, 1)}},
			plain: dangling(`dk#U[["u7"]]`),
		},
		{
			name:  "a number at a String key",
			edits: [][2]string{{edgeU, strings.Replace(edgeU, `"u1"`, `5`, 1)}},
			plain: dangling(`dk#U[[5]]`),
		},
		{
			name:  "a null at a String key",
			edits: [][2]string{{edgeU, strings.Replace(edgeU, `"u1"`, `null`, 1)}},
			plain: dangling(`dk#U[[null]]`),
		},
		{
			name:  "a bool at a String key",
			edits: [][2]string{{edgeU, strings.Replace(edgeU, `"u1"`, `true`, 1)}},
			plain: dangling(`dk#U[[true]]`),
		},
		{
			name:  "a String key outside its length bounds",
			edits: [][2]string{{edgeX, `"target_key":["abcd"]`}},
			plain: dangling(`dk#X[["abcd"]]`),
		},
		{
			name:  "a Date key no calendar holds",
			edits: [][2]string{{edgeW, strings.Replace(edgeW, `"2024-01-02"`, `"2024-13-45"`, 1)}},
			plain: dangling(`dk#W[["2024-13-45"]]`),
		},
		{
			name:  "a composite key with one component its property refuses",
			edits: [][2]string{{edgeV, `"target_key":["v1","2024-13-45"]`}},
			plain: dangling(`dk#V[["v1","2024-13-45"]]`),
		},
		{
			name:  "a composite key with every component its property refuses",
			edits: [][2]string{{edgeV, `"target_key":[1,"2024-02-30"]`}},
			plain: dangling(`dk#V[[1,"2024-02-30"]]`),
		},
		{
			name: "a refused key beside a property its constraint refuses",
			edits: [][2]string{
				{edgeU, strings.Replace(edgeU, `"u1"`, `5`, 1)},
				{`"age":30`, `"age":200`},
			},
			plain: dangling(`dk#U[[5]]`),
			reval: []string{diag.E_CONSTRAINT_FAIL.String() + `: revalidation of dk#T[["t1"]]: property "age"`},
		},
		{
			name: "a key that resolves to no root beside a composed child its constraint refuses",
			edits: [][2]string{
				{edgeU, strings.Replace(edgeU, `"u1"`, `"u7"`, 1)},
				{`"n":1`, `"n":9`},
			},
			plain: dangling(`dk#U[["u7"]]`),
			reval: []string{diag.E_CONSTRAINT_FAIL.String() + `: revalidation of dk#T[["t1"]]: `},
		},
		{
			// A refused value stops the root's later passes, as a member error does.
			name: "a refused key beside a composed child its constraint refuses",
			edits: [][2]string{
				{edgeU, strings.Replace(edgeU, `"u1"`, `5`, 1)},
				{`"n":1`, `"n":9`},
			},
			plain: dangling(`dk#U[[5]]`),
		},
		{
			name:  "a refused key on the second edge of a list",
			edits: [][2]string{{edgeX, edgeX + `,"properties":{}},{"target_type":6,"target_key":["abcd"]`}},
			plain: dangling(`dk#X[["abcd"]]`),
		},
		{
			name: "a refused key on an edge of a source whose UUID key is spelled in upper case",
			edits: [][2]string{
				{`"key":["` + sourceUUID + `"]`, `"key":["` + strings.ToUpper(sourceUUID) + `"]`},
				{`"id":"` + sourceUUID + `"`, `"id":"` + strings.ToUpper(sourceUUID) + `"`},
				{`"target_key":["u2"]`, `"target_key":[5]`},
			},
			plain: dangling(`dk#U[[5]] in dk#S[["a0eebc99`),
		},
		{
			name: "a refused key at the address a refused root states",
			edits: [][2]string{
				{edgeU, `"target_key":[5]`},
				{`"properties":{"id":"u1"}`, `"properties":{"id":5}`},
			},
			plain: []string{diag.E_SNAPSHOT_MALFORMED.String() + `: instance of dk#U stores key ["u1"], which component 0 disagrees with property "id"`},
		},
		{
			name: "a refused key at the address a refused root is stored at",
			edits: [][2]string{
				{edgeU, `"target_key":[5]`},
				{`"key":["u1"],"properties":{"id":"u1"}`, `"key":[5],"properties":{}`},
			},
			plain: []string{diag.E_SNAPSHOT_MALFORMED.String() + `: instance of dk#U stores key [5], which cannot agree with key property "id": it is absent or null`},
		},
		{
			name: "a record with a refused key at the address a refused root states",
			edits: [][2]string{
				{lostU9, `"target_key":[5]`},
				{`"properties":{"id":"u1"}`, `"properties":{"id":5}`},
			},
			plain: []string{diag.E_SNAPSHOT_MALFORMED.String() + `: instance of dk#U stores key ["u1"], which component 0 disagrees with property "id"`},
		},
		{
			name: "an edge and a record with a refused key at the address a refused root is stored at",
			edits: [][2]string{
				{edgeU, `"target_key":[5]`},
				{lostU9, `"target_key":[5]`},
				{`"key":["u1"],"properties":{"id":"u1"}`, `"key":[5],"properties":{}`},
			},
			plain: []string{diag.E_SNAPSHOT_MALFORMED.String() + `: instance of dk#U stores key [5], which cannot agree with key property "id": it is absent or null`},
		},
		{
			name: "a refused key on a dangling edge beside a record holding the same key",
			edits: [][2]string{
				{edgeU, strings.Replace(edgeU, `"u1"`, `null`, 1)},
				{lostU9, `"target_key":[null]`},
			},
			plain: dangling(`dk#U[[null]]`),
			reval: []string{diag.E_TYPE_MISMATCH.String() + `: revalidation of dk#T[["t1"]]: FK field "_target_id": expected string, got null`},
		},
		{
			name:  "a record whose key its property refuses",
			edits: [][2]string{{lostU9, `"target_key":[5]`}},
			reval: []string{diag.E_TYPE_MISMATCH.String() + `: revalidation of dk#T[["t1"]]: FK field "_target_id": expected string, got json.Number`},
		},
		{
			name:  "a key of another arity",
			edits: [][2]string{{edgeU, strings.Replace(edgeU, `["u1"]`, `["u1","x"]`, 1)}},
			plain: []string{diag.E_SNAPSHOT_MALFORMED.String() + `: edge 0 of dk#T[["t1"]] under "TO_U" carries a 2-part target key`},
		},
		{
			name:  "a list at a key component",
			edits: [][2]string{{edgeU, strings.Replace(edgeU, `["u1"]`, `[["u1"]]`, 1)}},
			plain: []string{diag.E_SNAPSHOT_MALFORMED.String() + `: edge 0 of ["t1"] under "TO_U" carries target key [["u1"]], whose component 0 is not a scalar`},
		},
	} {
		data := doc
		for _, e := range c.edits {
			if bytes.Count(data, []byte(e[0])) != 1 {
				t.Fatalf("%s: the document does not hold %q once:\n%s", c.name, e[0], doc)
			}
			data = bytes.Replace(data, []byte(e[0]), []byte(e[1]), 1)
		}
		for _, mode := range []struct {
			name string
			opts []snapshot.LoadOption
			want []string
		}{
			{"plain", nil, c.plain},
			{"revalidate", []snapshot.LoadOption{snapshot.WithRevalidation(diag.Error)}, append(append([]string(nil), c.plain...), c.reval...)},
		} {
			opts := append([]snapshot.LoadOption{snapshot.WithIntegrityCheck(false)}, mode.opts...)
			t.Run(c.name+"/Load/"+mode.name, func(t *testing.T) {
				t.Parallel()
				_, res := snapshot.Load(t.Context(), data, s, opts...)
				matchIssues(t, readIssues(res), mode.want)
			})
			t.Run(c.name+"/Verify/"+mode.name, func(t *testing.T) {
				t.Parallel()
				matchIssues(t, readIssues(snapshot.Verify(t.Context(), data, s, opts...)), mode.want)
			})
		}
	}
}

// matchIssues fails t unless got holds one issue per prefix in want, in order.
func matchIssues(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("issues = %q; want %d holding %q", got, len(want), want)
	}
	for i, w := range want {
		if !strings.HasPrefix(got[i], w) {
			t.Fatalf("issues = %q; want issue %d to start %q", got, i, w)
		}
	}
}

// Each relation of a root is judged for its own dangling edges, whichever order
// the root's relations are visited in, so the read repeats.
func TestRead_EachRelationsDanglingEdgesAreJudged(t *testing.T) {
	t.Parallel()
	s, doc := danglingKeyDoc(t)
	data := bytes.Replace(doc, []byte(`"target_key":["u1"]`), []byte(`"target_key":["u7"]`), 1)
	data = bytes.Replace(data, []byte(`"target_key":["abc"]`), []byte(`"target_key":["abcd"]`), 1)
	want := []string{
		diag.E_SNAPSHOT_DANGLING_REFERENCE.String() + `: edge target dk#U[["u7"]]`,
		diag.E_SNAPSHOT_DANGLING_REFERENCE.String() + `: edge target dk#X[["abcd"]]`,
	}
	for range 20 {
		_, res := snapshot.Load(t.Context(), data, s, snapshot.WithIntegrityCheck(false), snapshot.WithRevalidation(diag.Error))
		matchIssues(t, readIssues(res), want)
	}
}
