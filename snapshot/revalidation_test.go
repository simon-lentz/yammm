package snapshot_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The P1 matrix schema: one type carrying a bound, an enum, a pattern, an
// invariant, an edge with a declared property, a required association, and a
// required composition — each violated by exactly one matrix row.
const revalSchema = `schema "reval"

type Target {
	id String primary
}

part type Item {
	sku Pattern["^sku-"] required
}

type Thing {
	id String primary
	count Integer[1, 10]
	state Enum["on", "off"]
	code Pattern["^c-"]

	--> LINKS (_:many) Target { note String }
	--> OWNER (one) Target

	*-> ITEMS (one:many) Item

	! "id must not be banned" id != "banned"
}
`

func revalLoadSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), revalSchema, "reval.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res.String())
	}
	return s
}

func revalItem(t *testing.T, s *schema.Schema, sku string) *instance.ValidInstance {
	t.Helper()
	item, _ := s.Type("Item")
	return instancetest.VI(
		"Item",
		instancetest.TypeID(item.ID()),
		instancetest.Props(map[string]any{"sku": sku}),
	)
}

// revalDocument marshals one bypass-built Thing so Load sees a document the
// validator never approved. A Target "x1" rides along so an edge to it
// resolves — an unresolved edge moves to the diagnostics section and never
// reaches the instance's edges.
func revalDocument(t *testing.T, s *schema.Schema, opts ...instancetest.VIOption) []byte {
	t.Helper()
	thing, _ := s.Type("Thing")
	target, _ := s.Type("Target")
	base := []instancetest.VIOption{
		instancetest.TypeID(thing.ID()),
		instancetest.PK("t1"),
	}
	g := graph.New(s)
	x1 := instancetest.VI(
		"Target",
		instancetest.TypeID(target.ID()),
		instancetest.PK("x1"),
		instancetest.Props(map[string]any{"id": "x1"}),
	)
	if r := g.Add(t.Context(), x1); !r.OK() {
		t.Fatalf("Add Target: %s", r.String())
	}
	if r := g.Add(t.Context(), instancetest.VI("Thing", append(base, opts...)...)); !r.OK() {
		t.Fatalf("Add: %s", r.String())
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres.String())
	}
	return data
}

// TestLoad_RevalidationMatrix pins the audit's P1 matrix: every constraint
// class the validator enforces is reported under WithRevalidation and
// silent without it.
func TestLoad_RevalidationMatrix(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)

	goodItems := func() instancetest.VIOption {
		return instancetest.Composed(map[string]immutable.Value{
			"ITEMS": immutable.Wrap([]*instance.ValidInstance{revalItem(t, s, "sku-1")}),
		})
	}

	cases := []struct {
		name string
		opts []instancetest.VIOption
		want diag.Code
	}{
		{
			name: "bound",
			opts: []instancetest.VIOption{
				instancetest.Props(map[string]any{"id": "t1", "count": int64(99)}),
				goodItems(),
			},
			want: diag.E_CONSTRAINT_FAIL,
		},
		{
			name: "enum",
			opts: []instancetest.VIOption{
				instancetest.Props(map[string]any{"id": "t1", "state": "neither"}),
				goodItems(),
			},
			want: diag.E_CONSTRAINT_FAIL,
		},
		{
			name: "pattern",
			opts: []instancetest.VIOption{
				instancetest.Props(map[string]any{"id": "t1", "code": "bad"}),
				goodItems(),
			},
			want: diag.E_CONSTRAINT_FAIL,
		},
		{
			name: "invariant",
			opts: []instancetest.VIOption{
				instancetest.PK("banned"),
				instancetest.Props(map[string]any{"id": "banned"}),
				goodItems(),
			},
			want: diag.E_INVARIANT_FAIL,
		},
		{
			name: "edge_shape",
			opts: []instancetest.VIOption{
				instancetest.Props(map[string]any{"id": "t1"}),
				goodItems(),
				instancetest.Edges(map[string]*instance.ValidEdgeData{
					"LINKS": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
						instance.NewValidEdgeTarget(
							immutable.WrapKey([]any{"x1"}),
							immutable.WrapProperties(map[string]any{"bogus": "v"}),
						),
					}),
				}),
			},
			want: diag.E_UNKNOWN_EDGE_FIELD,
		},
		{
			name: "required_composition",
			opts: []instancetest.VIOption{
				instancetest.Props(map[string]any{"id": "t1"}),
			},
			want: diag.E_UNRESOLVED_REQUIRED_COMPOSITION,
		},
		{
			name: "composed_child_constraint",
			opts: []instancetest.VIOption{
				instancetest.Props(map[string]any{"id": "t1"}),
				instancetest.Composed(map[string]immutable.Value{
					"ITEMS": immutable.Wrap([]*instance.ValidInstance{revalItem(t, s, "bad")}),
				}),
			},
			want: diag.E_CONSTRAINT_FAIL,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := revalDocument(t, s, tc.opts...)

			// Silent without the option.
			_, plain := snapshot.Load(t.Context(), data, s)
			if plain.HasErrors() {
				t.Fatalf("plain Load refused the document: %s", plain.String())
			}
			if plain.HasCode(tc.want) {
				t.Fatalf("plain Load already reports %s; the option gates nothing", tc.want)
			}

			// Reported under the option, still loading at Warning severity.
			snap, res := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Warning))
			if res.HasErrors() || snap == nil {
				t.Fatalf("Warning-severity revalidation refused the load: %s", res.String())
			}
			if !res.HasCode(tc.want) {
				t.Errorf("revalidation did not report %s: %s", tc.want, res.String())
			}

			// Error severity refuses the document.
			snap, res = snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
			if snap != nil || !res.HasErrors() {
				t.Error("Error-severity revalidation returned a snapshot for a failing document")
			}
		})
	}
}

func TestLoad_RevalidationCleanDataSilent(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	v := instance.NewValidator(s)

	g := graph.New(s)
	target, res := v.ValidateOne(t.Context(), "Target", instance.RawInstance{Properties: map[string]any{"id": "x1"}})
	if !res.OK() {
		t.Fatalf("validate Target: %s", res.String())
	}
	thing, res := v.ValidateOne(t.Context(), "Thing", instance.RawInstance{Properties: map[string]any{
		"id":    "t1",
		"count": int64(5),
		"state": "on",
		"code":  "c-9",
		"owner": map[string]any{"_target_id": "x1"},
		"links": []any{map[string]any{"_target_id": "x1", "note": "n"}},
		"items": []any{map[string]any{"sku": "sku-1"}},
	}})
	if !res.OK() {
		t.Fatalf("validate Thing: %s", res.String())
	}
	for _, vi := range []*instance.ValidInstance{target, thing} {
		if r := g.Add(t.Context(), vi); !r.OK() {
			t.Fatalf("Add: %s", r.String())
		}
	}
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres.String())
	}

	snap, lres := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Error))
	if snap == nil || lres.HasErrors() {
		t.Fatalf("clean validator-built data failed revalidation: %s", lres.String())
	}
	for issue := range lres.Issues() {
		t.Errorf("clean data drew an issue: %s", issue.Message())
	}
}

// TestLoad_RevalidationUnknownRelation pins that a document carrying edges
// under a relation name the type does not declare is reported, never
// silently skipped — RebuildSnapshot accepts such parts, so the wire can
// hold them.
func TestLoad_RevalidationUnknownRelation(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	thing, _ := s.Type("Thing")
	target, _ := s.Type("Target")

	node := func(id schema.TypeID, tag, k string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   tag,
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{k}),
			Properties: immutable.WrapProperties(map[string]any{"id": k}),
		}
	}
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{thing.ID(), target.ID()},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			thing.ID():  {node(thing.ID(), "Thing", "t1")},
			target.ID(): {node(target.ID(), "Target", "x1")},
		},
		Edges: []graph.EdgeParts{{
			Relation:   "BOGUS",
			SourceType: thing.ID(), SourceKey: immutable.WrapKey([]any{"t1"}),
			TargetType: target.ID(), TargetKey: immutable.WrapKey([]any{"x1"}),
		}},
	})
	if res.HasErrors() {
		t.Fatalf("assembling: %s", res)
	}
	data, mres := snapshot.Marshal(t.Context(), built)
	if mres.HasErrors() {
		t.Fatalf("Marshal: %s", mres.String())
	}

	_, lres := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Warning))
	if !lres.HasCode(diag.E_GRAPH_UNKNOWN_RELATION) {
		t.Errorf("edges under an undeclared relation were not reported: %s", lres.String())
	}
}

// TestLoad_RevalidationUnresolvedRequired pins W_SNAPSHOT_UNRESOLVED_REQUIRED:
// an unresolved record for a Required association is reported only under the
// option.
func TestLoad_RevalidationUnresolvedRequired(t *testing.T) {
	t.Parallel()
	s := revalLoadSchema(t)
	data := revalDocument(
		t, s,
		instancetest.Props(map[string]any{"id": "t1"}),
		instancetest.Composed(map[string]immutable.Value{
			"ITEMS": immutable.Wrap([]*instance.ValidInstance{revalItem(t, s, "sku-1")}),
		}),
		instancetest.Edges(map[string]*instance.ValidEdgeData{
			"OWNER": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
				instance.NewValidEdgeTarget(immutable.WrapKey([]any{"missing"}), immutable.Properties{}),
			}),
		}),
	)

	_, plain := snapshot.Load(t.Context(), data, s)
	if plain.HasCode(diag.W_SNAPSHOT_UNRESOLVED_REQUIRED) {
		t.Fatal("the unresolved-required warning fired without the option")
	}

	_, res := snapshot.Load(t.Context(), data, s, snapshot.WithRevalidation(diag.Warning))
	if !res.HasCode(diag.W_SNAPSHOT_UNRESOLVED_REQUIRED) {
		t.Errorf("a Required unresolved record was not reported: %s", res.String())
	}
}
