package snapshot_test

import (
	"bytes"
	"maps"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const judgedOnceSchema = `schema "judged_once"

type N {
	id String[1, 4] primary
}

type T {
	id String primary
	size Integer
	--> TO_N (_) N
	! "size small" size < 10
}
`

// Revalidation judges a target key at the address it names, once: a key its
// root's constraint refuses draws that root's one issue, not one more at each
// edge naming it, and a root whose edge names it is judged whole, so its own
// break is still reported.
func TestRevalidation_ATargetKeyIsJudgedOnceAtItsRoot(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), judgedOnceSchema, "judged_once.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	g := graph.New(s)
	for _, in := range []struct {
		typ   string
		props map[string]any
	}{
		{"N", map[string]any{"id": "n1"}},
		{"T", map[string]any{"id": "t1", "size": int64(1), "to_n": map[string]any{"_target_id": "n1"}}},
		{"T", map[string]any{"id": "t2", "size": int64(2), "to_n": map[string]any{"_target_id": "n1"}}},
	} {
		if r := g.Add(t.Context(), instancetest.Valid(t, s, in.typ, in.props)); !r.OK() {
			t.Fatalf("add %s: %s", in.typ, r)
		}
	}
	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	// N's key leaves its bounds at every place the document states it, and t2
	// breaks its invariant.
	if bytes.Count(data, []byte(`"n1"`)) != 4 || bytes.Count(data, []byte(`"size":2`)) != 1 {
		t.Fatalf("the document does not hold the fixture's values:\n%s", data)
	}
	data = bytes.ReplaceAll(data, []byte(`"n1"`), []byte(`"toolong"`))
	data = rehashDocument(t, bytes.Replace(data, []byte(`"size":2`), []byte(`"size":20`), 1))

	want := map[diag.Code]int{diag.E_CONSTRAINT_FAIL: 1, diag.E_INVARIANT_FAIL: 1}
	_, lres := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
	if got := errorCodes(lres); !maps.Equal(got, want) {
		t.Errorf("Load: %v, want %v:\n%s", got, want, lres)
	}
	if got := errorCodes(snapshot.Verify(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))); !maps.Equal(got, want) {
		t.Errorf("Verify: %v, want %v", got, want)
	}
}

const respelledSourceSchema = `schema "respelled_source"

type U {
	id String primary
}

type S {
	id UUID primary
	n Integer
	--> TO_U (_:many) U
	! "n small" n < 10
}
`

// A record's source key is compared as written: a record that spells its
// source's UUID in upper case is malformed and names no root, so the root it
// would name is still revalidated and its own break still reported.
func TestRevalidation_ARecordsRespelledSourceLeavesTheRootJudged(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), respelledSourceSchema, "respelled_source.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	const id = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11"
	g := graph.New(s)
	in := instancetest.Valid(t, s, "S", map[string]any{"id": id, "n": int64(1), "to_u": []any{map[string]any{"_target_id": "u9"}}})
	if r := g.Add(t.Context(), in); r.HasErrors() {
		t.Fatalf("add S: %s", r)
	}
	data, mr := snapshot.Marshal(t.Context(), g.Snapshot())
	if mr.HasErrors() {
		t.Fatalf("marshal: %s", mr)
	}
	source, n := `"source_key":["`+id+`"]`, `"n":1`
	if bytes.Count(data, []byte(source)) != 1 || bytes.Count(data, []byte(n)) != 1 {
		t.Fatalf("the document does not hold the fixture's values:\n%s", data)
	}
	data = bytes.Replace(data, []byte(source), []byte(`"source_key":["`+strings.ToUpper(id)+`"]`), 1)
	data = rehashDocument(t, bytes.Replace(data, []byte(n), []byte(`"n":20`), 1))

	want := map[diag.Code]int{diag.E_SNAPSHOT_MALFORMED: 1, diag.E_INVARIANT_FAIL: 1}
	revalidate := snapshot.WithRevalidation(diag.Error)
	if _, lres := snapshot.Load(t.Context(), data, s, revalidate); !maps.Equal(errorCodes(lres), want) {
		t.Errorf("Load: %v, want %v:\n%s", errorCodes(lres), want, lres)
	}
	if vres := snapshot.Verify(t.Context(), data, s, revalidate); !maps.Equal(errorCodes(vres), want) {
		t.Errorf("Verify: %v, want %v:\n%s", errorCodes(vres), want, vres)
	}
}
