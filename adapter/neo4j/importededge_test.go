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

// TestBatchEdgeQueries_ImportedSourceTypeWritesUnderClosureShape pins that an
// edge whose source type the entry schema imports resolves its shape through
// the closure walk, by identity: the statement merges on the imported type's
// own label even though the rendered name is in no schema's local table.
func TestBatchEdgeQueries_ImportedSourceTypeWritesUnderClosureShape(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(importedEdgeEntry),
		"base.yammm":  []byte(importedEdgeBase),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
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
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{k}),
			Properties: immutable.WrapProperties(map[string]any{"id": k}),
		}
	}
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{id},
		Instances: []graph.InstanceParts{
			node("b1"),
			node("b2"),
		},
		Edges: []graph.EdgeParts{{
			Relation:   "NEAR",
			SourceType: id, SourceKey: immutable.WrapKey([]any{"b1"}),
			TargetKey:  immutable.WrapKey([]any{"b2"}),
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

	if _, ok := shapes.Types[id]; !ok {
		t.Fatal("the closure walk did not produce a shape for the imported source type")
	}

	queries, err := a.BatchEdgeQueries(ctx, built, shapes)
	if err != nil {
		t.Fatalf("BatchEdgeQueries: %v", err)
	}
	if len(queries) != 1 {
		t.Fatalf("got %d edge queries; want 1", len(queries))
	}
	for _, want := range []string{"base__Basin", "NEAR"} {
		if !strings.Contains(queries[0].Statement, want) {
			t.Errorf("edge statement %q does not contain %q", queries[0].Statement, want)
		}
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

// TestShapeForSchema_TransitivelyImportedTypeKeepsItsOwnLabel pins the reason
// GraphShape is keyed by identity. The deep Hub renders the same bare name as
// the entry schema's own Hub — a different label, different merge keys,
// different required fields — and it gets its own closure-provided shape rather
// than being merged into the entry Hub's.
//
// It asserts the SHAPE and not a write, because the deep Hub can no longer hold
// a root instance: the entry schema cannot name it. Nothing in a snapshot can
// carry it to a write path, so the shape is where the identity keying is
// observable.
//
// Mutation: keying GraphShape.Types by schema.TagForm instead of by TypeID
// collapses the pair and turns this red.
func TestShapeForSchema_TransitivelyImportedTypeKeepsItsOwnLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, res := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(collidingShapeEntry),
		"mid.yammm":   []byte(collidingShapeMid),
		"deep.yammm":  []byte(collidingShapeDeep),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
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
	if _, addressable := schema.AddressableTag(s, deep.ID()); addressable {
		t.Fatal("fixture is vacuous: the entry schema can name the deep Hub")
	}

	a := neo4j.New()
	shapes, shapeRes := a.ShapeForSchema(ctx, s)
	if shapeRes.HasErrors() {
		t.Fatalf("shape: %s", shapeRes)
	}

	localShape, ok := shapes.Types[local.ID()]
	if !ok {
		t.Fatal("the entry schema's Hub has no shape")
	}
	deepShape, ok := shapes.Types[deep.ID()]
	if !ok {
		t.Fatal("the deep Hub has no shape: the closure walk keyed the two identities as one")
	}
	if localShape.Label == deepShape.Label {
		t.Fatalf("the two identities share label %q", localShape.Label)
	}
	if !strings.Contains(deepShape.Label, "deep") {
		t.Errorf("the deep Hub's label is %q, not one derived from its declaring schema", deepShape.Label)
	}
}
