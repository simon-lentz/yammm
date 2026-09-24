package snapshot_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// compositionSlotSchema gives every part type the same key, so a child filed under the
// wrong slot differs from a right one in its identity alone.
const compositionSlotSchema = `schema "slots"

part type Badge {
	code String primary
	*-> STAMPS (many) Stamp
}

part type Note {
	code String primary
}

part type Stamp {
	code String primary
}

type Company {
	id String primary
}

type Employee {
	staff_id String primary
	*-> BADGES (many) Badge
	*-> NOTE (one) Note
	--> WORKS_AT (_:one) Company
}
`

// slotCase files one child of type child under relation rel, at depth 1 (under
// Employee e1) or depth 2 (under e2's Badge b2).
type slotCase struct {
	depth      int
	rel, child string
	legal      bool
}

// slotCases is every relation name the parent's position can meet — each
// composition, an association and an undeclared name — crossed with every part
// type. Only a composition holding its declared target is legal.
func slotCases() []slotCase {
	parts := []string{"Badge", "Note", "Stamp"}
	legal := map[string]string{"BADGES": "Badge", "NOTE": "Note", "STAMPS": "Stamp"}
	var out []slotCase
	for depth, rels := range map[int][]string{
		1: {"BADGES", "NOTE", "WORKS_AT", "NOPE"},
		2: {"STAMPS", "BADGES", "NOPE"},
	} {
		for _, rel := range rels {
			for _, child := range parts {
				out = append(out, slotCase{depth: depth, rel: rel, child: child, legal: legal[rel] == child &&
					(depth == 2) == (rel == "STAMPS")})
			}
		}
	}
	return out
}

func (c slotCase) String() string {
	return fmt.Sprintf("depth%d/%s/%s", c.depth, c.rel, c.child)
}

func loadSlotSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), compositionSlotSchema, "slots.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func partsChild(t *testing.T, s *schema.Schema, typeName, code string) graph.InstanceParts {
	t.Helper()
	return graph.InstanceParts{
		TypeID: mustTypeID(t, s, typeName), PrimaryKey: immutable.WrapKey([]any{code}),
		Properties: immutable.WrapProperties(map[string]any{"code": code}),
	}
}

// slotParts files the case's child in parts: e1 holds it at depth 1, e2's b2
// at depth 2, and e2 also holds a Note so every part type is in play.
func slotParts(t *testing.T, s *schema.Schema, c slotCase) graph.SnapshotParts {
	t.Helper()
	emp := mustTypeID(t, s, "Employee")
	child := partsChild(t, s, c.child, "x1")
	e1 := graph.InstanceParts{
		TypeID: emp, PrimaryKey: immutable.WrapKey([]any{"e1"}),
		Properties: immutable.WrapProperties(map[string]any{"staff_id": "e1"}),
	}
	b2 := partsChild(t, s, "Badge", "b2")
	if c.depth == 1 {
		e1.Composed = map[string][]graph.InstanceParts{c.rel: {child}}
	} else {
		b2.Composed = map[string][]graph.InstanceParts{c.rel: {child}}
	}
	e2 := graph.InstanceParts{
		TypeID: emp, PrimaryKey: immutable.WrapKey([]any{"e2"}),
		Properties: immutable.WrapProperties(map[string]any{"staff_id": "e2"}),
		Composed: map[string][]graph.InstanceParts{
			"BADGES": {b2},
			"NOTE":   {partsChild(t, s, "Note", "n2")},
		},
	}
	return graph.SnapshotParts{Types: []schema.TypeID{emp}, Instances: []graph.InstanceParts{e1, e2}}
}

func validChild(t *testing.T, s *schema.Schema, typeName, code string, composed map[string]immutable.Value) *instance.ValidInstance {
	t.Helper()
	return instance.NewValidInstance(typeName, mustTypeID(t, s, typeName), immutable.WrapKey([]any{code}),
		immutable.WrapProperties(map[string]any{"code": code}), nil, composed, nil)
}

// addSlot files the case's child through Graph.Add, the contract's other
// implementation, and reports whether Add accepted both roots.
func addSlot(t *testing.T, s *schema.Schema, c slotCase) bool {
	t.Helper()
	ctx := context.Background()
	emp := mustTypeID(t, s, "Employee")
	child := validChild(t, s, c.child, "x1", nil)
	var e1Composed, b2Composed map[string]immutable.Value
	if c.depth == 1 {
		e1Composed = map[string]immutable.Value{c.rel: immutable.Wrap([]any{child})}
	} else {
		b2Composed = map[string]immutable.Value{c.rel: immutable.Wrap([]any{child})}
	}
	e1 := instance.NewValidInstance("Employee", emp, immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"staff_id": "e1"}), nil, e1Composed, nil)
	e2 := instance.NewValidInstance("Employee", emp, immutable.WrapKey([]any{"e2"}),
		immutable.WrapProperties(map[string]any{"staff_id": "e2"}), nil, map[string]immutable.Value{
			"BADGES": immutable.Wrap([]any{validChild(t, s, "Badge", "b2", b2Composed)}),
			"NOTE":   immutable.Wrap([]any{validChild(t, s, "Note", "n2", nil)}),
		}, nil)
	g := graph.New(s)
	return g.Add(ctx, e1).OK() && g.Add(ctx, e2).OK()
}

// slotWireDoc marshals the legal document every wire case edits: e1 holds
// Badge x1 under BADGES, and e2 holds Badge b2 with Stamp x1 under STAMPS.
func slotWireDoc(t *testing.T, s *schema.Schema) ([]byte, map[string]int) {
	t.Helper()
	legal := slotParts(t, s, slotCase{depth: 1, rel: "BADGES", child: "Badge"})
	legal.Instances[1].Composed["BADGES"][0].Composed = map[string][]graph.InstanceParts{
		"STAMPS": {partsChild(t, s, "Stamp", "x1")},
	}
	built, res := graph.RebuildSnapshot(s, legal)
	if res.HasErrors() {
		t.Fatalf("the legal document was refused: %s", res)
	}
	data, mres := snapshot.Marshal(context.Background(), built)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	var doc struct {
		Types []struct{ Name string } `json:"types"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	rows := map[string]int{}
	for i, e := range doc.Types {
		rows[e.Name] = i
	}
	return data, rows
}

// slotWire edits the legal document so the case's child sits where the case
// puts it, as a foreign writer's document would.
func slotWire(t *testing.T, data []byte, rows map[string]int, c slotCase) []byte {
	t.Helper()
	anchor := fmt.Sprintf(`"composed":{"BADGES":[{"key":["x1"],"type":%d,`, rows["Badge"])
	repl := fmt.Sprintf(`"composed":{%q:[{"key":["x1"],"type":%d,`, c.rel, rows[c.child])
	if c.depth == 2 {
		anchor = fmt.Sprintf(`"STAMPS":[{"key":["x1"],"type":%d,`, rows["Stamp"])
		repl = fmt.Sprintf(`%q:[{"key":["x1"],"type":%d,`, c.rel, rows[c.child])
	}
	if n := strings.Count(string(data), anchor); n != 1 {
		t.Fatalf("anchor %s occurs %d times in %s", anchor, n, data)
	}
	return []byte(strings.Replace(string(data), anchor, repl, 1))
}

// TestCompositionSlot_EveryImplementationAgrees holds the slot rule over the
// whole class: Graph.Add, graph.RebuildSnapshot and snapshot.Load each accept
// a composed child exactly when its slot is a composition of the parent's type
// and the child is that composition's declared target.
func TestCompositionSlot_EveryImplementationAgrees(t *testing.T) {
	t.Parallel()
	s := loadSlotSchema(t)
	data, rows := slotWireDoc(t, s)
	cases := slotCases()
	if len(cases) != 21 {
		t.Fatalf("the class holds %d cases, want 21", len(cases))
	}
	for _, c := range cases {
		t.Run(c.String(), func(t *testing.T) {
			t.Parallel()
			if got := addSlot(t, s, c); got != c.legal {
				t.Errorf("Graph.Add accepted=%v, want %v", got, c.legal)
			}
			_, rres := graph.RebuildSnapshot(s, slotParts(t, s, c))
			if got := !rres.HasErrors(); got != c.legal {
				t.Errorf("RebuildSnapshot accepted=%v, want %v: %s", got, c.legal, rres)
			}
			_, lres := snapshot.Load(context.Background(), slotWire(t, data, rows, c), s, snapshot.WithIntegrityCheck(false))
			if got := !lres.HasErrors(); got != c.legal {
				t.Errorf("Load accepted=%v, want %v: %s", got, c.legal, lres)
			}
		})
	}
}

// TestLoad_RefusesTwoSiblingsAtOneKey pins the slot rule Graph.Add applies to a
// (many) composition at the reader: a document whose two badges spell one key,
// at a root's slot and at a badge's own, is refused by Load and by Verify.
func TestLoad_RefusesTwoSiblingsAtOneKey(t *testing.T) {
	t.Parallel()
	s := loadSlotSchema(t)
	emp := mustTypeID(t, s, "Employee")
	stamps := partsChild(t, s, "Badge", "b1")
	stamps.Composed = map[string][]graph.InstanceParts{"STAMPS": {partsChild(t, s, "Stamp", "s1"), partsChild(t, s, "Stamp", "s2")}}
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{Types: []schema.TypeID{emp}, Instances: []graph.InstanceParts{{
		TypeID: emp, PrimaryKey: immutable.WrapKey([]any{"e1"}),
		Properties: immutable.WrapProperties(map[string]any{"staff_id": "e1"}),
		Composed:   map[string][]graph.InstanceParts{"BADGES": {stamps, partsChild(t, s, "Badge", "b2")}},
	}}})
	if res.HasErrors() {
		t.Fatalf("rebuild: %s", res)
	}
	data, mres := snapshot.Marshal(t.Context(), built)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	for name, from := range map[string]string{"a root's slot": `"b2"`, "a badge's slot": `"s2"`} {
		doc := strings.ReplaceAll(string(data), from, strings.Replace(from, "2", "1", 1))
		if doc == string(data) {
			t.Fatalf("%s: fixture shape changed; %s not found", name, from)
		}
		for surface, got := range map[string]diag.Result{
			"Load": func() diag.Result {
				_, r := snapshot.Load(t.Context(), []byte(doc), s, snapshot.WithIntegrityCheck(false))
				return r
			}(),
			"Verify": snapshot.Verify(t.Context(), []byte(doc), s, snapshot.WithIntegrityCheck(false)),
		} {
			if !got.HasCode(diag.E_DUPLICATE_COMPOSED_PK) {
				t.Errorf("%s, %s: %s, want E_DUPLICATE_COMPOSED_PK", name, surface, got)
			}
		}
	}
}
