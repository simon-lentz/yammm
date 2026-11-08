package graph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const loadIdentitySource = "schema \"loads\"\n\ntype T {\n\tid String primary\n}\n"

// assertAnotherLoadPanic fails t unless add panics naming another schema load.
func assertAnotherLoadPanic(t *testing.T, add func()) {
	t.Helper()
	defer func() {
		t.Helper()
		got, _ := recover().(string)
		if !strings.Contains(got, "another schema load") {
			t.Errorf("panic = %q, want one naming another schema load", got)
		}
	}()
	add()
}

// One text loaded twice is two loads. The panic is the graph's whole judgement of
// the load, so it holds at every entry point, for the same text as for a different one.
func TestAdd_InstanceOfAnotherLoadPanics(t *testing.T) {
	t.Parallel()
	s := loadSchemaAt(t, loadIdentitySource, "loads.yammm")
	for name, other := range map[string]schema.Schema{
		"same text": loadSchemaAt(t, loadIdentitySource, "loads.yammm"),
		"different text": loadSchemaAt(t,
			"schema \"loads\"\n\ntype T {\n\tid String primary\n\tqty String\n}\n", "loads.yammm"),
		"outside the closure": loadSchemaAt(t, loadIdentitySource, "elsewhere.yammm"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inst := instancetest.Valid(t, other, "T", map[string]any{"id": "t1"})
			g := graph.New(s)
			assertAnotherLoadPanic(t, func() { g.Add(t.Context(), inst) })
			ba := graph.NewBatchAssembler(t.Context(), s)
			assertAnotherLoadPanic(t, func() { _ = ba.AddValid(inst) })
			assertAnotherLoadPanic(t, func() { _, _ = ba.AddValidOrMerge(inst) })
			if n := len(slices.Collect(g.Snapshot().AllInstances())); n != 0 {
				t.Errorf("the graph holds %d instances after the panic, want 0", n)
			}
		})
	}
}

// diamondClosure loads an entry that imports two schemas, each importing a third.
func diamondClosure(t *testing.T) map[string]schema.Schema {
	t.Helper()
	return loadClosure(t, "a.yammm", map[string]string{
		"a.yammm": `schema "a"

import "b" as b
import "c" as c

type Top {
	id String primary
	*-> SUB (many) c.Part
}
`,
		"b.yammm": `schema "b"

import "d" as d

type Mid {
	id String primary
	*-> DEEP (many) d.Leaf
}
`,
		"c.yammm": `schema "c"

import "d" as d

part type Part {
	id String primary
	*-> DEEP (_:many) d.Leaf
}
`,
		"d.yammm": `schema "d"

part type Leaf {
	id String primary
}

type Root {
	id String primary
}
`,
	})
}

func TestClosure_DiamondImportGivesOneTypeHandle(t *testing.T) {
	t.Parallel()
	members := diamondClosure(t)
	viaB, ok := members["b"].ResolveTypeName("d.Leaf")
	if !ok {
		t.Fatal("b does not resolve d.Leaf")
	}
	viaC, ok := members["c"].ResolveTypeName("d.Leaf")
	if !ok {
		t.Fatal("c does not resolve d.Leaf")
	}
	own, ok := members["d"].Type("Leaf")
	if !ok {
		t.Fatal("d does not declare Leaf")
	}
	byID, ok := members["a"].TypeByID(own.ID())
	if !ok {
		t.Fatal("the entry does not resolve Leaf's identity")
	}
	if viaB != viaC || viaC != own || byID != own {
		t.Error("one type holds more than one handle across a diamond import")
	}
}

// A load holds one handle per type, so a validator over a member of one closure
// serves a graph over any member that can name the root.
func TestAdd_TakesAValidatorOfAnyMemberOfOneLoad(t *testing.T) {
	t.Parallel()
	members := diamondClosure(t)
	for _, c := range []struct {
		name, validator, typeName, graph string
		props                            map[string]any
	}{
		{"composed child across imports", "a", "Top", "a", map[string]any{"id": "x", "sub": []any{
			map[string]any{"id": "p", "deep": []any{map[string]any{"id": "l"}}},
		}}},
		{"entry validator, directly imported root", "a", "b.Mid", "a", map[string]any{"id": "x"}},
		{"member validator, graph over the entry", "b", "Mid", "a", map[string]any{"id": "x"}},
		{"entry validator, graph over the member", "a", "b.Mid", "b", map[string]any{"id": "x"}},
		{"member validator, root the entry reaches through another import", "d", "Root", "a", map[string]any{"id": "x"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			inst := instancetest.Valid(t, members[c.validator], c.typeName, c.props)
			g := graph.New(members[c.graph])
			if r := g.Add(t.Context(), inst); !r.OK() {
				t.Fatalf("Add: %s", r)
			}
			if n := len(slices.Collect(g.Snapshot().AllInstances())); n != 1 {
				t.Errorf("the graph holds %d roots, want 1", n)
			}
		})
	}
}

// A member's closure does not hold the type of the schema that imports it.
func TestAdd_TypeOutsideTheGraphsClosurePanics(t *testing.T) {
	t.Parallel()
	members := diamondClosure(t)
	inst := instancetest.Valid(t, members["a"], "Top", map[string]any{"id": "x"})
	g := graph.New(members["b"])
	assertAnotherLoadPanic(t, func() { g.Add(t.Context(), inst) })
}

// A snapshot crosses loads through the import, and an assembler validates over
// its own schema. Only an instance another load validated panics.
func TestNewBatchAssemblerFromSnapshot_SeedOfAnotherLoad(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	s := loadSchemaAt(t, loadIdentitySource, "loads.yammm")
	first := graph.New(s)
	if r := first.Add(ctx, instancetest.Valid(t, s, "T", map[string]any{"id": "t1"})); !r.OK() {
		t.Fatalf("Add: %s", r)
	}
	data, res := snapshot.Marshal(ctx, first.Snapshot())
	if res.HasErrors() {
		t.Fatalf("Marshal: %s", res)
	}
	seed, res := snapshot.Load(ctx, data, s)
	if res.HasErrors() {
		t.Fatalf("Load: %s", res)
	}

	again := loadSchemaAt(t, loadIdentitySource, "loads.yammm")
	ba, res := graph.NewBatchAssemblerFromSnapshot(ctx, again, seed)
	if res.HasErrors() {
		t.Fatalf("NewBatchAssemblerFromSnapshot: %s", res)
	}
	if err := ba.Add("T", instance.RawInstance{Properties: map[string]any{"id": "t2"}}); err != nil {
		t.Errorf("the assembler's own Add: %v", err)
	}
	assertAnotherLoadPanic(t, func() {
		_ = ba.AddValid(instancetest.Valid(t, s, "T", map[string]any{"id": "t3"}))
	})
}
