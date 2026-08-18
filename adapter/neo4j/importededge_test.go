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
// The refusal is a recorded gap, not a design: ShapeForSchema walks the entry
// schema's own types, so an imported type has no shape to resolve. Graph mode
// therefore cannot write edges out of imported sources at all. What the index
// re-key changed is the refusal, not the gap — it is now stated as an identity,
// so it names the type that has no shape instead of a rendered form that
// happened to miss a name-keyed map. Closing the gap makes this test red, which
// is the point — it is the marker for the work.
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
	if !strings.Contains(err.Error(), id.String()) {
		t.Errorf("refusal does not name the unresolved identity %s: %v", id, err)
	}
}

// A transitively imported type renders to a bare name, because the entry
// schema holds no alias for the schema that declares it. When the entry schema
// declares its own type of that name, the two render identically.
const collidingShapeEntry = `schema "entry"

import "mid.yammm" as mid

type Hub {
	code String primary
	name String required
}
`

const collidingShapeMid = `schema "mid"

import "deep.yammm" as deep
`

const collidingShapeDeep = `schema "deep"

type Hub {
	id String primary
}
`

// TestBatchNodeQueries_RefusesATransitivelyImportedTypeSharingALocalName pins
// the reason GraphShape is keyed by identity. A name-keyed index binds the deep
// Hub's instances to the entry Hub's shape — a different label, different merge
// keys, different required fields — and writes them under it without a word.
// Identity has no such coincidence: the deep Hub has no shape, and the writer
// says so.
func TestBatchNodeQueries_RefusesATransitivelyImportedTypeSharingALocalName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(collidingShapeEntry),
		"mid.yammm":   []byte(collidingShapeMid),
		"deep.yammm":  []byte(collidingShapeDeep),
	}, "entry.yammm", ".", schema.WithSourcesOnly())
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}

	local, ok := s.Type("Hub")
	if !ok {
		t.Fatal("the entry schema's own Hub did not resolve")
	}
	var deep *schema.Type
	for _, ty := range s.Closure() {
		for _, cand := range ty.TypesSlice() {
			if cand.Name() == "Hub" && cand.ID() != local.ID() {
				deep = cand
			}
		}
	}
	if deep == nil {
		t.Fatal("fixture is vacuous: the deep Hub did not resolve through the closure")
	}
	if schema.TagForm(s, deep.ID()) != local.Name() {
		t.Fatalf("fixture is vacuous: the deep Hub renders as %q, not as the local name %q",
			schema.TagForm(s, deep.ID()), local.Name())
	}

	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{deep.ID()},
		Instances: map[schema.TypeID][]graph.InstanceParts{deep.ID(): {{
			TypeName:   schema.TagForm(s, deep.ID()),
			TypeID:     deep.ID(),
			PrimaryKey: immutable.WrapKey([]any{"d1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "d1"}),
		}}},
	})
	if res.HasErrors() {
		t.Fatalf("assembling: %s", res)
	}

	a := neo4j.New()
	shapes, shapeRes := a.ShapeForSchema(ctx, s)
	if shapeRes.HasErrors() {
		t.Fatalf("shape: %s", shapeRes)
	}
	if _, ok := shapes.Types[local.ID()]; !ok {
		t.Fatal("fixture is vacuous: the entry schema's Hub has no shape to be mistaken for")
	}

	if _, err := a.BatchNodeQueries(ctx, built, shapes); err == nil {
		t.Error("BatchNodeQueries wrote the deep Hub under the entry Hub's shape")
	} else if !strings.Contains(err.Error(), deep.ID().String()) {
		t.Errorf("refusal does not name the unresolved identity %s: %v", deep.ID(), err)
	}
}
