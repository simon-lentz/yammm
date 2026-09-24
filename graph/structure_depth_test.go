package graph_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const depthSchema = `schema "depth"

type Root {
	id String primary
	*-> MID (many) Mid
}

part type Mid {
	id String primary
	*-> LEAF (many) Leaf
}

part type Leaf {
	id String primary
	label String
}
`

// TestRebuildSnapshot_HoldsEveryInstanceRuleAtEveryDepth drives each
// per-instance rule at the two positions the instance walk reaches below a
// root's children: a grandchild, and a composed child of a duplicate record's
// instance. Each fault must draw its own rule's refusal there.
func TestRebuildSnapshot_HoldsEveryInstanceRuleAtEveryDepth(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), depthSchema, "depth.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	root, mid, leaf := mustTypeID(t, s, "Root"), mustTypeID(t, s, "Mid"), mustTypeID(t, s, "Leaf")
	node := func(id schema.TypeID, key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key}),
		}
	}

	faults := []struct {
		name   string
		code   diag.Code
		phrase string
		breaks func(*graph.InstanceParts)
	}{
		{"identity", diag.E_INTERNAL, "zero type identity at composed child position", func(ip *graph.InstanceParts) {
			ip.TypeID = schema.TypeID{}
		}},
		{"slot", diag.E_GRAPH_UNKNOWN_RELATION, `holds composed children under "BOGUS"`, func(ip *graph.InstanceParts) {
			ip.Composed = map[string][]graph.InstanceParts{"BOGUS": {node(leaf, "z")}}
		}},
		{"key", diag.E_GRAPH_INVALID_PK, `primary key property "id" is absent or null`, func(ip *graph.InstanceParts) {
			ip.Properties = immutable.WrapProperties(map[string]any{})
		}},
		{"names", diag.E_INTERNAL, `holds property "ghost"`, func(ip *graph.InstanceParts) {
			ip.Properties = immutable.WrapProperties(map[string]any{"id": ip.PrimaryKey.Get(0).Unwrap(), "ghost": "x"})
		}},
	}
	positions := []struct {
		name  string
		parts func(fault func(*graph.InstanceParts)) graph.SnapshotParts
	}{
		{"grandchild", func(fault func(*graph.InstanceParts)) graph.SnapshotParts {
			l := node(leaf, "l1")
			fault(&l)
			m := node(mid, "m1")
			m.Composed = map[string][]graph.InstanceParts{"LEAF": {l}}
			r := node(root, "r1")
			r.Composed = map[string][]graph.InstanceParts{"MID": {m}}
			return graph.SnapshotParts{Types: []schema.TypeID{root}, Instances: []graph.InstanceParts{r}}
		}},
		{"duplicate's composed child", func(fault func(*graph.InstanceParts)) graph.SnapshotParts {
			m := node(mid, "m1")
			fault(&m)
			dup := node(root, "r1")
			dup.Composed = map[string][]graph.InstanceParts{"MID": {m}}
			return graph.SnapshotParts{
				Types:     []schema.TypeID{root},
				Instances: []graph.InstanceParts{node(root, "r1")},
				Duplicates: []graph.DuplicateParts{{
					Instance: dup,
				}},
			}
		}},
	}
	for _, pos := range positions {
		for _, f := range faults {
			t.Run(pos.name+"/"+f.name, func(t *testing.T) {
				t.Parallel()
				_, res := graph.RebuildSnapshot(s, pos.parts(f.breaks))
				found := false
				for issue := range res.Issues() {
					if issue.Code() == f.code && strings.Contains(issue.Message(), f.phrase) {
						found = true
					}
				}
				if !found {
					t.Errorf("no %s naming %q: %s", f.code, f.phrase, res)
				}
			})
		}
	}
}

// TestRebuildSnapshot_RefusesTwoSiblingsAtOneKey pins the slot rule Graph.Add
// applies to a (many) composition of a keyed part: two children spelling one
// canonical key are one address, at a root's slot and at a child's.
func TestRebuildSnapshot_RefusesTwoSiblingsAtOneKey(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), depthSchema, "depth.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	root, mid, leaf := mustTypeID(t, s, "Root"), mustTypeID(t, s, "Mid"), mustTypeID(t, s, "Leaf")
	node := func(id schema.TypeID, key string) graph.InstanceParts {
		return graph.InstanceParts{TypeID: id, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{"id": key})}
	}
	atRoot := node(root, "r1")
	atRoot.Composed = map[string][]graph.InstanceParts{"MID": {node(mid, "m1"), node(mid, "m1")}}
	m := node(mid, "m1")
	m.Composed = map[string][]graph.InstanceParts{"LEAF": {node(leaf, "l1"), node(leaf, "l2"), node(leaf, "l1")}}
	atChild := node(root, "r1")
	atChild.Composed = map[string][]graph.InstanceParts{"MID": {m}}
	for name, r := range map[string]graph.InstanceParts{"a root's slot": atRoot, "a child's slot": atChild} {
		_, res := graph.RebuildSnapshot(s, graph.SnapshotParts{Types: []schema.TypeID{root}, Instances: []graph.InstanceParts{r}})
		if !res.HasCode(diag.E_DUPLICATE_COMPOSED_PK) || !strings.Contains(res.String(), `at key ["`) {
			t.Errorf("%s: RebuildSnapshot = %s, want E_DUPLICATE_COMPOSED_PK naming the key", name, res)
		}
	}
}

// TestRootIneligibility_TakesAddsOrder pins that a type breaking two root rules
// draws the rule Graph.Add checks first: a keyless abstract type is keyless.
func TestRootIneligibility_TakesAddsOrder(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"o\"\n\nabstract type A {\n\tname String\n}\n", "o.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	a := mustTypeID(t, s, "A")
	add := graph.New(s).Add(t.Context(), instance.NewValidInstance("A", a, immutable.WrapKey([]any{"x"}), immutable.WrapProperties(nil), nil, nil, nil))
	if !add.HasCode(diag.E_GRAPH_MISSING_PK) {
		t.Fatalf("control: Add = %s, want E_GRAPH_MISSING_PK", add)
	}
	_, rres := graph.RebuildSnapshot(s, graph.SnapshotParts{Types: []schema.TypeID{a}})
	if !strings.Contains(rres.String(), "declares no primary key") {
		t.Errorf("RebuildSnapshot = %s, want the keyless rule Add reports", rres)
	}
}

// TestRebuildSnapshot_WalksBelowAnUnresolvableInstance pins that an instance
// whose identity does not resolve still has its children judged, and that a
// child whose identity does not resolve draws that refusal alone.
func TestRebuildSnapshot_WalksBelowAnUnresolvableInstance(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), depthSchema, "depth.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	ghostSchema, gres := schema.LoadString(t.Context(), "schema \"ghost\"\n\ntype G {\n\tid String primary\n}\n", "ghost.yammm")
	if gres.HasErrors() {
		t.Fatalf("load: %s", gres)
	}
	root, ghost := mustTypeID(t, s, "Root"), mustTypeID(t, ghostSchema, "G")
	node := func(id schema.TypeID, key string) graph.InstanceParts {
		return graph.InstanceParts{TypeID: id, PrimaryKey: immutable.WrapKey([]any{key}), Properties: immutable.WrapProperties(map[string]any{"id": key})}
	}

	underGhost := node(ghost, "g1")
	underGhost.Composed = map[string][]graph.InstanceParts{"MID": {{PrimaryKey: immutable.WrapKey([]any{"m1"})}}}
	_, res = graph.RebuildSnapshot(s, graph.SnapshotParts{Instances: []graph.InstanceParts{underGhost}})
	if !strings.Contains(res.String(), "zero type identity at composed child position") {
		t.Errorf("a zero-typed child under an unresolvable root: RebuildSnapshot = %s, want the child refused", res)
	}

	ghostChild := node(root, "r1")
	ghostChild.Composed = map[string][]graph.InstanceParts{"MID": {node(ghost, "g1")}}
	_, res = graph.RebuildSnapshot(s, graph.SnapshotParts{Types: []schema.TypeID{root}, Instances: []graph.InstanceParts{ghostChild}})
	if !strings.Contains(res.String(), "unresolvable type identity") || res.HasCode(diag.E_GRAPH_INVALID_COMPOSITION) {
		t.Errorf("an unresolvable child: RebuildSnapshot = %s, want the identity refusal alone", res)
	}
}

// TestRebuildSnapshot_RefusesAnIneligibleRootTypeNoTypesEntryNames pins that
// the root rule judges each root's own type, not only the types entries.
func TestRebuildSnapshot_RefusesAnIneligibleRootTypeNoTypesEntryNames(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"a\"\n\nabstract type A {\n\tid String primary\n}\n", "a.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	a := mustTypeID(t, s, "A")
	_, res = graph.RebuildSnapshot(s, graph.SnapshotParts{Instances: []graph.InstanceParts{{
		TypeID: a, PrimaryKey: immutable.WrapKey([]any{"x"}), Properties: immutable.WrapProperties(map[string]any{"id": "x"}),
	}}})
	if !strings.Contains(res.String(), "is abstract") {
		t.Errorf("RebuildSnapshot = %s, want the abstract root refused", res)
	}
}
