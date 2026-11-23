package snapshot_test

import (
	"iter"
	"reflect"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/recordtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

const loadedRecordsSource = `schema "records"

part type Bolt {
	code String primary
}

part type Line {
	sku String primary
	qty Integer required
	*-> BOLTS (many) Bolt
}

type Company {
	id String primary
	name String required
}

type Order {
	id String primary
	note String
	--> PLACED_BY (one) Company {
		role String
	}
	--> AUDITED_BY (one) Company
	*-> LINES (many) Line
}
`

func loadedRecordsSnapshot(t *testing.T) graph.Snapshot {
	t.Helper()
	ctx := t.Context()
	s, err := schema.LoadString(ctx, loadedRecordsSource, "records.yammm")
	if err != nil {
		t.Fatal(err)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	add := func(typ string, props map[string]any) {
		t.Helper()
		valid, err := v.ValidateOne(ctx, typ, instance.RawInstance{Properties: props})
		if err != nil {
			t.Fatal(err)
		}
		_ = g.Add(ctx, valid)
	}
	add("Company", map[string]any{"id": "c1", "name": "n"})
	add("Order", map[string]any{
		"id":         "o1",
		"note":       "first",
		"placed_by":  map[string]any{"_target_id": "c1", "role": "buyer"},
		"audited_by": map[string]any{"_target_id": "c1"},
		"lines":      []any{map[string]any{"sku": "a", "qty": int64(1), "bolts": []any{map[string]any{"code": "m4"}}}, map[string]any{"sku": "b", "qty": int64(2)}},
	})
	add("Order", map[string]any{"id": "o2", "placed_by": map[string]any{"_target_id": "missing"}})
	add("Order", map[string]any{
		"id":    "o1",
		"note":  "the duplicate",
		"lines": []any{map[string]any{"sku": "c", "qty": int64(3)}},
	})
	return g.Snapshot()
}

// A .ys file persists no span and no validator claim:
// [graph.Instance.Validated] documents the loaded answer.
func loadedRecordsOptions() recordtest.Options {
	return recordtest.Options{
		OmitTypes: []reflect.Type{reflect.TypeFor[location.Span]()},
		SkipMethod: func(owner, method string) bool {
			return owner == "Instance" && method == "Validated"
		},
	}
}

// composedWalk reaches the composed children, which the walker cannot reach from a
// root because [graph.Instance.Composed] takes an argument.
func composedWalk(t *testing.T, snap graph.Snapshot) [][]string {
	t.Helper()
	var walks [][]string
	for inst := range snap.Instances() {
		for _, relation := range inst.ComposedRelations() {
			for child := range inst.Composed(relation) {
				walks = append(walks, recordtest.Walk(child, loadedRecordsOptions()))
				for _, nested := range child.ComposedRelations() {
					for grandchild := range child.Composed(nested) {
						walks = append(walks, recordtest.Walk(grandchild, loadedRecordsOptions()))
					}
				}
			}
		}
	}
	if len(walks) == 0 {
		t.Fatal("the fixture holds no composed child")
	}
	return walks
}

func TestLoad_EveryRecordAccessorAnswersAsTheBuiltSnapshot(t *testing.T) {
	ctx := t.Context()
	built := loadedRecordsSnapshot(t)
	data, err := snapshot.Marshal(ctx, built)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := snapshot.Unmarshal(ctx, data, built.Schema())
	if err != nil {
		t.Fatal(err)
	}

	yammmtest.Diff(t, recordtest.Walk(built, loadedRecordsOptions()), recordtest.Walk(loaded, loadedRecordsOptions()))
	yammmtest.Diff(t, composedWalk(t, built), composedWalk(t, loaded))

	for _, snap := range []graph.Snapshot{built, loaded} {
		checkRecordIdentity(t, snap)
	}
}

// checkRecordIdentity asks the accessors that take an argument, which the walker
// cannot reach, and holds each reached record to the one its other accessors name.
func checkRecordIdentity(t *testing.T, snap graph.Snapshot) {
	t.Helper()
	order, ok := snap.Schema().Type("Order")
	if !ok {
		t.Fatal("no type Order")
	}
	instances := slices.Collect(snap.InstancesOf(order.ID()))
	if len(instances) != 2 {
		t.Fatalf("InstancesOf yields %d, want 2", len(instances))
	}
	o1 := instances[0]

	byKey, ok := snap.InstanceByKey(order.ID(), o1.PrimaryKey().String())
	if !ok || o1 != byKey {
		t.Error("InstancesOf and InstanceByKey must name one record")
	}

	edges := slices.Collect(snap.EdgesFrom(o1))
	if len(edges) != 2 {
		t.Fatalf("EdgesFrom yields %d, want 2", len(edges))
	}
	for _, e := range edges {
		if e.Source() != o1 {
			t.Error("an edge's source and the instance it was read from must name one record")
		}
		if !slices.Contains(slices.Collect(snap.Edges()), e) {
			t.Error("EdgesFrom and Edges must name one record")
		}
	}
	placed := edges[slices.IndexFunc(edges, func(e graph.Edge) bool { return e.Relation() == "PLACED_BY" })]
	role, ok := placed.Property("role")
	if !ok {
		t.Fatal("no edge property role")
	}
	if text, ok := role.AsString(); !ok || text != "buyer" {
		t.Errorf("role = %q, %v, want buyer", text, ok)
	}

	if got := o1.ComposedCount("LINES"); got != 2 {
		t.Errorf("ComposedCount = %d, want 2", got)
	}
	if got := len(slices.Collect(o1.Composed("LINES"))); got != 2 {
		t.Errorf("Composed yields %d, want 2", got)
	}
	if got := len(slices.Collect(o1.Composed("ABSENT"))); got != 0 {
		t.Errorf("Composed of an absent relation yields %d, want 0", got)
	}

	var unresolved []string
	for u := range snap.Unresolved() {
		unresolved = append(unresolved, u.Relation())
	}
	slices.Sort(unresolved)
	yammmtest.Diff(t, []string{"AUDITED_BY", "PLACED_BY"}, unresolved)

	duplicates := slices.Collect(snap.Duplicates())
	if len(duplicates) != 1 {
		t.Fatalf("Duplicates yields %d, want 1", len(duplicates))
	}
	if duplicates[0].Conflict() != o1 {
		t.Error("a duplicate's conflict and the root it met must name one record")
	}
}

const typedRecordsSource = `schema "typed"

type Company {
	id UUID primary
	founded Date
	seen Timestamp
	rate Float
	tags List<String>
	on Boolean
	vector Vector[3]
}

type Office {
	id String primary
	--> HOUSED_BY (one) Company {
		since Date
	}
	--> AUDITED_BY (one) Company
}
`

// The canonicalizer is active only for a schema that names Date, Timestamp or UUID,
// so this fixture holds the same answers over the path [loadedRecordsSource] skips.
func TestLoad_TypedValuesAnswerAsTheBuiltSnapshot(t *testing.T) {
	ctx := t.Context()
	s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.LoadString(ctx, typedRecordsSource, "typed.yammm", schema.WithWarnings(warn))
	})
	if diagtest.Err(res) != nil {
		t.Fatal(res)
	}
	const company = "9b2e1c70-6f0a-4f11-8a2d-0d6b4c1f7e10"
	v := instance.NewValidator(s)
	g := graph.New(s)
	add := func(typ string, props map[string]any) {
		t.Helper()
		valid, err := v.ValidateOne(ctx, typ, instance.RawInstance{Properties: props})
		if err != nil {
			t.Fatal(err)
		}
		if err := g.Add(ctx, valid); err != nil {
			t.Fatal(err)
		}
	}
	add("Company", map[string]any{
		"id": company, "founded": "2026-01-02", "seen": "2026-01-02T03:04:05Z", "rate": 1.5,
		"tags": []any{"a", "b"}, "on": true, "vector": []any{1.0, 2.0, 3.0},
	})
	add("Office", map[string]any{
		"id":         "o1",
		"housed_by":  map[string]any{"_target_id": company, "since": "2026-02-03"},
		"audited_by": map[string]any{"_target_id": company},
	})
	add("Office", map[string]any{"id": "o2", "housed_by": map[string]any{"_target_id": "9b2e1c70-6f0a-4f11-8a2d-0d6b4c1f7e11"}})
	built := g.Snapshot()

	data, res := diagtest.Issues(t, func() ([]byte, error) {
		return snapshot.Marshal(ctx, built)
	})
	if diagtest.Err(res) != nil {
		t.Fatal(res)
	}
	loaded, res := diagtest.Capture(t, func(warn func(diag.Issue)) (graph.Snapshot, error) {
		return snapshot.Unmarshal(ctx, data, s, snapshot.WithWarnings(warn))
	})
	if diagtest.Err(res) != nil {
		t.Fatal(res)
	}
	yammmtest.Diff(t, recordtest.Walk(built, loadedRecordsOptions()), recordtest.Walk(loaded, loadedRecordsOptions()))
}

func TestSnapshot_AnEarlyBreakStopsEachIterator(t *testing.T) {
	snap := loadedRecordsSnapshot(t)
	order, ok := snap.Schema().Type("Order")
	if !ok {
		t.Fatal("no type Order")
	}
	first := slices.Collect(snap.InstancesOf(order.ID()))[0]

	for name, seq := range map[string]iter.Seq[graph.Instance]{
		"Composed":    first.Composed("LINES"),
		"InstancesOf": snap.InstancesOf(order.ID()),
		"Instances":   snap.Instances(),
	} {
		n := 0
		for range seq {
			n++
			break
		}
		if n != 1 {
			t.Errorf("%s ran %d steps past its break, want 1", name, n)
		}
	}
}

func TestAdd_AnEdgeWithNoPropertyHoldsTheZeroProperties(t *testing.T) {
	ctx := t.Context()
	s, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.LoadString(ctx, loadedRecordsSource, "records.yammm", schema.WithWarnings(warn))
	})
	if diagtest.Err(res) != nil {
		t.Fatal(res)
	}
	company, res := diagtest.Issues(t, func() (instance.ValidInstance, error) {
		return instance.NewValidator(s).ValidateOne(ctx, "Company", instance.RawInstance{
			Properties: map[string]any{"id": "c1", "name": "n"},
		})
	})
	if diagtest.Err(res) != nil {
		t.Fatal(res)
	}
	g := graph.New(s)
	if err := g.Add(ctx, company); err != nil {
		t.Fatal(err)
	}
	target := map[string]any{"_target_id": "c1"}
	inst := instancetest.Valid(t, s, "Order", map[string]any{"id": "o1", "placed_by": target, "audited_by": target})
	if err := g.Add(ctx, inst); err != nil {
		t.Fatal(err)
	}
	var edges int
	for e := range g.Snapshot().Edges() {
		edges++
		if !immutable.IsNilContainer(e.Properties()) {
			t.Errorf("%s holds an empty container, want the zero Properties", e.Relation())
		}
	}
	if edges != 2 {
		t.Fatalf("Edges yields %d, want 2", edges)
	}
}
