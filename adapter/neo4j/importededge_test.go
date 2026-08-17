package neo4j_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// The entry schema declares no type of its own, so every instance below is an
// imported one and its rendered name is alias-qualified.
const importedEdgeEntry = `schema "geo"

import "base.yammm" as base
`

const importedEdgeBase = `schema "base"

type Basin {
	id String primary
	--> NEAR (_) Basin { seen Timestamp }
}
`

// TestBatchEdgeQueries_RefusesAnImportedSourceType pins what the writer does
// with an edge whose source type the entry schema imports: it refuses, naming
// the type, rather than emitting a relationship under a shape it never
// resolved.
//
// The refusal is a recorded gap, not a design: ShapeForSchema keys its type map
// by bare name while the edge signature carries the rendered, alias-qualified
// form, so the two never meet for an imported type. Graph mode therefore cannot
// write edges out of imported sources at all. Closing that gap makes this test
// red, which is the point — it is the marker for the work.
func TestBatchEdgeQueries_RefusesAnImportedSourceType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(importedEdgeEntry),
		"base.yammm":  []byte(importedEdgeBase),
	}, "entry.yammm", ".", schema.WithSourcesOnly())
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	basin, ok := s.ResolveType(schema.NewTypeRef("base", "Basin", location.Span{}))
	if !ok {
		t.Fatal("base.Basin did not resolve through the entry schema")
	}
	id := basin.ID()

	node := func(k string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   "base.Basin",
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{k}),
			Properties: immutable.WrapProperties(map[string]any{"id": k}),
		}
	}
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types:     []schema.TypeID{id},
		Instances: map[schema.TypeID][]graph.InstanceParts{id: {node("b1"), node("b2")}},
		Edges: []graph.EdgeParts{{
			Relation:   "NEAR",
			SourceType: id, SourceKey: immutable.WrapKey([]any{"b1"}),
			TargetType: id, TargetKey: immutable.WrapKey([]any{"b2"}),
			Properties: immutable.WrapProperties(map[string]any{"seen": "2026-08-17T12:00:00Z"}),
		}},
	})
	if res.HasErrors() {
		t.Fatalf("assembling: %s", res)
	}

	// The fixture is only meaningful while the rendered name is unresolvable
	// through the schema's local table — that is what splits the two lookups.
	if _, ok := s.Type("base.Basin"); ok {
		t.Fatal("fixture is vacuous: the rendered name resolves, so the lookups agree")
	}

	a := neo4j.New()
	shapes, shapeRes := a.ShapeForSchema(ctx, s)
	if shapeRes.HasErrors() {
		t.Fatalf("shape: %s", shapeRes)
	}

	_, err := a.BatchEdgeQueries(ctx, built, shapes)
	if err == nil {
		t.Fatal("BatchEdgeQueries wrote an edge whose source shape it never resolved")
	}
	if !strings.Contains(err.Error(), "base.Basin") {
		t.Errorf("refusal does not name the unresolved type: %v", err)
	}
}
