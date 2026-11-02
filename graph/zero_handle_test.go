package graph_test

import (
	"iter"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

func wantNone[T any](t *testing.T, name string, seq iter.Seq[T]) {
	t.Helper()
	if got := slices.Collect(seq); len(got) != 0 {
		t.Errorf("%s yielded %d records, want none", name, len(got))
	}
}

func TestZeroHandles_ReportZeroAndAnswerAsHoldingNothing(t *testing.T) {
	var (
		snap graph.Snapshot
		inst graph.Instance
		edge graph.Edge
		dup  graph.Duplicate
		unr  graph.UnresolvedEdge
	)
	for name, zero := range map[string]bool{
		"Snapshot":              snap.IsZero(),
		"Instance":              inst.IsZero(),
		"Edge":                  edge.IsZero(),
		"Duplicate":             dup.IsZero(),
		"UnresolvedEdge":        unr.IsZero(),
		"Snapshot.Schema":       snap.Schema().IsZero(),
		"Instance.Provenance":   inst.Provenance().IsZero(),
		"Edge.Source":           edge.Source().IsZero(),
		"Edge.Target":           edge.Target().IsZero(),
		"Duplicate.Instance":    dup.Instance().IsZero(),
		"Duplicate.Conflict":    dup.Conflict().IsZero(),
		"UnresolvedEdge.Source": unr.Source().IsZero(),
	} {
		if !zero {
			t.Errorf("%s must be zero", name)
		}
	}

	wantNone(t, "AllInstances", snap.AllInstances())
	wantNone(t, "InstancesOf", snap.InstancesOf(schema.TypeID{}))
	wantNone(t, "Edges", snap.Edges())
	wantNone(t, "EdgesFrom", snap.EdgesFrom(inst))
	wantNone(t, "Duplicates", snap.Duplicates())
	wantNone(t, "Unresolved", snap.Unresolved())
	wantNone(t, "Composed", inst.Composed("R"))

	if got := snap.Types(); len(got) != 0 {
		t.Errorf("Types = %v, want none", got)
	}
	if found, ok := snap.InstanceByKey(schema.TypeID{}, "k"); ok || !found.IsZero() {
		t.Errorf("InstanceByKey = %v, %v, want the zero handle and false", found, ok)
	}
	if got := inst.TypeName(); got != "" {
		t.Errorf("TypeName = %q, want empty", got)
	}
	if got := inst.ComposedRelations(); len(got) != 0 {
		t.Errorf("ComposedRelations = %v, want none", got)
	}
	if inst.Validated() {
		t.Error("Validated = true on the zero Instance")
	}
	if got := snap.Attestation(); got != nil {
		t.Errorf("Attestation = %v, want nil", got)
	}
}

func TestSnapshot_EdgesFromAnswersNothingWithAZeroOnEitherSide(t *testing.T) {
	ctx := t.Context()
	s, res := schema.LoadString(ctx, "schema \"z\"\n\ntype T {\n\tid String primary\n}\n\ntype U {\n\tid String primary\n\t--> TO (one) T\n}\n", "z.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	v := instance.NewValidator(s)
	g := graph.New(s)
	for typ, props := range map[string]map[string]any{
		"T": {"id": "t"},
		"U": {"id": "u", "to": map[string]any{"_target_id": "t"}},
	} {
		valid, res := v.ValidateOne(ctx, typ, instance.RawInstance{Properties: props})
		if !res.OK() {
			t.Fatal(res)
		}
		if res := g.Add(ctx, valid); res.HasErrors() {
			t.Fatal(res)
		}
	}
	snap := g.Snapshot()
	if snap.IsZero() || snap.Schema().IsZero() {
		t.Fatal("a graph's snapshot and its schema must name records")
	}
	edges := slices.Collect(snap.Edges())
	if len(edges) != 1 {
		t.Fatalf("Edges yields %d, want 1", len(edges))
	}
	wantNone(t, "EdgesFrom of a zero Instance", snap.EdgesFrom(graph.Instance{}))
	wantNone(t, "EdgesFrom on a zero Snapshot", graph.Snapshot{}.EdgesFrom(edges[0].Source()))
}
