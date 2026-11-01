package graph_test

import (
	"context"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEdgesFrom_NilSnapshot(t *testing.T) {
	var snap graph.Snapshot
	assert.Nil(t, slices.Collect(snap.EdgesFrom(graph.Instance{})))
}

func TestEdgesFrom_NilInstance(t *testing.T) {
	s := testSchemaWithAssociation(t)
	g := graph.New(s)

	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme"})
	g.Add(context.Background(), company)

	snap := g.Snapshot()
	assert.Nil(t, slices.Collect(snap.EdgesFrom(graph.Instance{})))
}

func TestEdgesFrom_NoEdges(t *testing.T) {
	s := testSchemaWithOptionalAssociation(t)
	g := graph.New(s)

	person := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"})
	g.Add(context.Background(), person)

	snap := g.Snapshot()
	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	require.Len(t, instances, 1)

	assert.Nil(t, slices.Collect(snap.EdgesFrom(instances[0])))
}

func TestEdgesFrom_SingleEdge(t *testing.T) {
	s := testSchemaWithAssociation(t)
	g := graph.New(s)

	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})

	g.Add(context.Background(), company)
	g.Add(context.Background(), person)

	snap := g.Snapshot()
	personInst := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))[0]

	edges := slices.Collect(snap.EdgesFrom(personInst))
	require.Len(t, edges, 1)
	assert.Equal(t, "EMPLOYER", edges[0].Relation())
	assert.Equal(t, "Person", edges[0].Source().TypeName())
	assert.Equal(t, "Company", edges[0].Target().TypeName())
}

func TestEdgesFrom_MultipleEdges(t *testing.T) {
	s := testSchemaWithManyAssociation(t)
	g := graph.New(s)

	c1 := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme"})
	c2 := mustValidInstance(t, s, "Company", []any{"c2"}, map[string]any{"id": "c2", "name": "Beta"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYERS", [][]any{{"c1"}, {"c2"}})

	g.Add(context.Background(), c1)
	g.Add(context.Background(), c2)
	g.Add(context.Background(), person)

	snap := g.Snapshot()
	personInst := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))[0]

	edges := slices.Collect(snap.EdgesFrom(personInst))
	require.Len(t, edges, 2)
	assert.Equal(t, "EMPLOYERS", edges[0].Relation())
	assert.Equal(t, "EMPLOYERS", edges[1].Relation())
	// Both edges carry the same relation, so the target key is the only arm
	// that can pin the order.
	assert.Equal(t, `["c1"]`, edges[0].Target().PrimaryKey().String())
	assert.Equal(t, `["c2"]`, edges[1].Target().PrimaryKey().String())
}

func TestEdgesFrom_InstanceFromDifferentSnapshot(t *testing.T) {
	s := testSchemaWithAssociation(t)

	// Build two separate snapshots
	g1 := graph.New(s)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme"})
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}})
	g1.Add(context.Background(), company)
	g1.Add(context.Background(), person)
	snap1 := g1.Snapshot()

	g2 := graph.New(s)
	g2.Add(context.Background(), company)
	g2.Add(context.Background(), person)
	snap2 := g2.Snapshot()

	// Instance from snap1 should not be found in snap2's index (pointer identity)
	personFromSnap1 := slices.Collect(snap1.InstancesOf(mustTypeID(t, s, "Person")))[0]
	assert.Nil(t, slices.Collect(snap2.EdgesFrom(personFromSnap1)))
}
