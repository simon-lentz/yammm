package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
)

// U+FFFD is a character: a key holding it is its own address.
func TestAdd_AcceptsAKeyHoldingTheReplacementCharacter(t *testing.T) {
	t.Parallel()
	s := loadAs(t, "schema \"keyutf8\"\n\ntype Holder {\n\tid String primary\n}\n")
	holder := mustTypeID(t, s, "Holder")
	g := graph.New(s)
	if res := g.Add(t.Context(), instancetest.Valid(t, s, "Holder", map[string]any{"id": "a�b"})); !res.OK() {
		t.Fatalf("Add: %s", res)
	}
	inst, ok := g.Snapshot().InstanceByKey(holder, graph.FormatKey("a�b"))
	if !ok {
		t.Fatalf("the instance is not at its address")
	}
	got, err := graph.ParseKey(inst.PrimaryKey().String())
	if err != nil || len(got) != 1 || got[0] != "a�b" {
		t.Errorf("ParseKey read the key back as %q, %v", got, err)
	}
}
