package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const composedDepthSchema = `schema "depth"

type Root {
	id String primary
	*-> NEXT (_:many) Link
}

part type Link {
	code String primary
	*-> NEXT (_:many) Link
}
`

// nestedRoot is a Root holding a chain of levels composed children.
func nestedRoot(levels int) map[string]any {
	var below []any
	for range levels {
		link := map[string]any{"code": "c"}
		if below != nil {
			link["NEXT"] = below
		}
		below = []any{link}
	}
	return map[string]any{"id": "r", "NEXT": below}
}

// The reader's depth bound is the validator's: a tree at the bound validates, writes
// and loads back, and a tree one level deeper is refused by both.
func TestRead_TheComposedDepthBoundIsTheValidators(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := schema.LoadString(ctx, composedDepthSchema, "depth.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	v := instance.NewValidator(s)

	vi, res := v.ValidateOne(ctx, "Root", instance.RawInstance{Properties: nestedRoot(instance.MaxComposedDepth)})
	if res.HasErrors() {
		t.Fatalf("validate at the bound: %s", res)
	}
	g := graph.New(s)
	if r := g.Add(ctx, vi); r.HasErrors() {
		t.Fatalf("add: %s", r)
	}
	data, res := snapshot.Marshal(ctx, g.Snapshot())
	if res.HasErrors() {
		t.Fatalf("marshal: %s", res)
	}
	if _, res := snapshot.Load(ctx, data, s); res.HasErrors() {
		t.Errorf("Load at the bound: %s", res)
	}
	if res := snapshot.Verify(ctx, data, s); res.HasErrors() {
		t.Errorf("Verify at the bound: %s", res)
	}

	if _, res := v.ValidateOne(ctx, "Root", instance.RawInstance{Properties: nestedRoot(instance.MaxComposedDepth + 1)}); !res.HasErrors() {
		t.Error("the validator accepts a tree one level past the bound")
	}
	doc := sbIntegrity.ReplaceAllString(string(data), `"integrity_hash":""`)
	const leaf = `{"key":["c"],"type":0,"properties":{"code":"c"},"provenance":null}`
	deeper := []byte(editOnce(t, doc, leaf,
		`{"key":["c"],"type":0,"properties":{"code":"c"},"composed":{"NEXT":[`+leaf+`]},"provenance":null}`))
	_, l := snapshot.Load(ctx, deeper, s)
	for surface, got := range map[string]diag.Result{"Load": l, "Verify": snapshot.Verify(ctx, deeper, s)} {
		if got.Len() != 1 || !got.HasCode(diag.E_SNAPSHOT_DEPTH_EXCEEDED) {
			t.Errorf("%s one level past the bound: %s, want one %s", surface, got, diag.E_SNAPSHOT_DEPTH_EXCEEDED)
		}
	}
}
