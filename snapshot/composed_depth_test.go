package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
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
			link["next"] = below
		}
		below = []any{link}
	}
	return map[string]any{"id": "r", "next": below}
}

// The reader's depth bound is the validator's: a tree at the bound validates, writes
// and loads back, and a tree one level deeper is refused by both.
func TestRead_TheComposedDepthBoundIsTheValidators(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.LoadString(ctx, composedDepthSchema, "depth.yammm", schema.WithWarnings(warn))
	})
	if diagtest.Err(res) != nil {
		t.Fatalf("schema: %s", res)
	}
	v := instance.NewValidator(s)

	vi, res := diagtest.Issues(t, func() (instance.ValidInstance, error) {
		return v.ValidateOne(ctx, "Root", instance.RawInstance{Properties: nestedRoot(instance.MaxComposedDepth)})
	})
	if diagtest.Err(res) != nil {
		t.Fatalf("validate at the bound: %s", res)
	}
	g := graph.New(s)
	if err := g.Add(ctx, vi); err != nil {
		t.Fatalf("add: %s", err)
	}
	data, res := diagtest.Issues(t, func() ([]byte, error) {
		return snapshot.Marshal(ctx, g.Snapshot())
	})
	if diagtest.Err(res) != nil {
		t.Fatalf("marshal: %s", res)
	}
	if _, err := snapshot.Unmarshal(ctx, rehashDocument(t, data), s); err != nil {
		t.Errorf("Load at the bound: %s", err)
	}
	if err := snapshot.Verify(ctx, rehashDocument(t, data), s); err != nil {
		t.Errorf("Verify at the bound: %s", err)
	}

	if _, err := v.ValidateOne(ctx, "Root", instance.RawInstance{Properties: nestedRoot(instance.MaxComposedDepth + 1)}); err == nil {
		t.Error("the validator accepts a tree one level past the bound")
	}
	doc := string(data)
	const leaf = `{"key":["c"],"type":0,"properties":{"code":"c"},"provenance":null}`
	deeper := []byte(editOnce(t, doc, leaf,
		`{"key":["c"],"type":0,"properties":{"code":"c"},"composed":{"NEXT":[`+leaf+`]},"provenance":null}`))
	_, l := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(ctx, rehashDocument(t, deeper), s, snapshot.WithWarnings(warn))
	})
	for surface, got := range map[string]diag.Result{"Load": l, "Verify": diagtest.CaptureErr(t, func(warn func(diag.Issue)) error {
		return snapshot.Verify(ctx, rehashDocument(t, deeper), s, snapshot.WithWarnings(warn))
	})} {
		if got.Len() != 1 || !got.HasCode(diag.E_SNAPSHOT_DEPTH_EXCEEDED) {
			t.Errorf("%s one level past the bound: %s, want one %s", surface, got, diag.E_SNAPSHOT_DEPTH_EXCEEDED)
		}
	}
}
