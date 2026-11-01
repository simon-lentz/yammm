package graph_test

import (
	"iter"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

const recordIteratorsSource = `schema "iterators"

part type Line {
	sku String primary
}

type Company {
	id String primary
}

type Person {
	id String primary
	--> EMPLOYER (_:many) Company
	*-> LINES (_:many) Line
}
`

func recordIteratorsSnapshot(t *testing.T, s schema.Schema) graph.Snapshot {
	t.Helper()
	ctx := t.Context()
	v := instance.NewValidator(s)
	g := graph.New(s)
	add := func(typeName string, props map[string]any) {
		t.Helper()
		inst, res := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
		if !res.OK() {
			t.Fatal(res)
		}
		if res := g.Add(ctx, inst); res.HasErrors() {
			t.Fatal(res)
		}
	}
	add("Company", map[string]any{"id": "c1"})
	add("Company", map[string]any{"id": "c2"})
	edges := func(ids ...string) []any {
		out := make([]any, 0, len(ids))
		for _, id := range ids {
			out = append(out, map[string]any{"_target_id": id})
		}
		return out
	}
	lines := []any{map[string]any{"sku": "a"}, map[string]any{"sku": "b"}}
	add("Person", map[string]any{"id": "p1", "employer": edges("c1", "c2", "gone1", "gone2"), "lines": lines})
	add("Person", map[string]any{"id": "p2"})
	repeated, res := v.ValidateOne(ctx, "Person", instance.RawInstance{Properties: map[string]any{"id": "p2"}})
	if !res.OK() {
		t.Fatal(res)
	}
	for range 2 {
		if res := g.Add(ctx, repeated); !res.HasErrors() {
			t.Fatal("a repeated key was accepted")
		}
	}
	return g.Snapshot()
}

// wantAscending checks that seq restarts, stops at a break and yields want records
// whose keys ascend.
func wantAscending[T comparable](t *testing.T, name string, seq iter.Seq[T], key func(T) immutable.Key, want int) {
	t.Helper()
	first := slices.Collect(seq)
	if len(first) != want {
		t.Fatalf("%s yielded %d records, want %d", name, len(first), want)
	}
	if !slices.Equal(first, slices.Collect(seq)) {
		t.Errorf("%s answered differently on its second range", name)
	}
	if !slices.IsSortedFunc(first, func(a, b T) int { return strings.Compare(key(a).String(), key(b).String()) }) {
		t.Errorf("%s yielded its records out of order", name)
	}
	ranged := 0
	for range seq {
		ranged++
		break
	}
	if ranged != 1 {
		t.Errorf("%s ranged %d times after a break, want 1", name, ranged)
	}
}

func TestRecordIterators_RestartStopAtABreakAndYieldInKeyOrder(t *testing.T) {
	s, res := schema.LoadString(t.Context(), recordIteratorsSource, "iterators.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	snap := recordIteratorsSnapshot(t, s)
	instanceKey := func(i graph.Instance) immutable.Key { return i.PrimaryKey() }
	edgeKey := func(e graph.Edge) immutable.Key { return e.Target().PrimaryKey() }

	persons := mustTypeID(t, s, "Person")
	wantAscending(t, "InstancesOf", snap.InstancesOf(persons), instanceKey, 2)
	wantAscending(t, "AllInstances", snap.AllInstances(), instanceKey, 4)
	wantAscending(t, "Edges", snap.Edges(), edgeKey, 2)
	p1 := slices.Collect(snap.InstancesOf(persons))[0]
	wantAscending(t, "EdgesFrom", snap.EdgesFrom(p1), edgeKey, 2)
	wantAscending(t, "Composed", p1.Composed("LINES"), instanceKey, 2)
	wantAscending(t, "Duplicates", snap.Duplicates(), func(d graph.Duplicate) immutable.Key { return d.Instance().PrimaryKey() }, 2)
	wantAscending(t, "Unresolved", snap.Unresolved(), func(u graph.UnresolvedEdge) immutable.Key {
		return immutable.WrapKey([]any{u.TargetKey()})
	}, 2)
}

func TestRebuild_AnUnresolvedEdgeWithNoPropertyHoldsTheZeroProperties(t *testing.T) {
	for name, extra := range map[string]string{
		"a schema with no value to canonicalize": "",
		"a schema with a value to canonicalize":  "at Timestamp\n",
	} {
		t.Run(name, func(t *testing.T) {
			s, res := schema.LoadString(t.Context(), `schema "empty"
type Company {
	id String primary
}
type Person {
	id String primary
	`+extra+`
	--> EMPLOYER (_:many) Company
}
`, "empty.yammm")
			if res.HasErrors() {
				t.Fatal(res)
			}
			person := mustTypeID(t, s, "Person")
			snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
				Types: []schema.TypeID{person},
				Instances: []graph.InstanceParts{{
					TypeID:     person,
					PrimaryKey: immutable.WrapKey([]any{"p1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "p1"}),
				}},
				Unresolved: []graph.UnresolvedParts{{
					SourceType: person,
					SourceKey:  immutable.WrapKey([]any{"p1"}),
					Relation:   "EMPLOYER",
					TargetKey:  immutable.WrapKey([]any{"gone"}),
					Reason:     "target_missing",
					Properties: immutable.WrapProperties(map[string]any{}),
				}},
			})
			if res.HasErrors() {
				t.Fatal(res)
			}
			held := slices.Collect(snap.Unresolved())
			if len(held) != 1 {
				t.Fatalf("Unresolved yielded %d records, want 1", len(held))
			}
			if !immutable.IsNilContainer(held[0].Properties()) {
				t.Error("the record holds an empty container, want the zero Properties")
			}
		})
	}
}
