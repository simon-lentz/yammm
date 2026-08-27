package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Relations are resolved by the absolute target identity completion records
// (Relation.TargetID), not by re-resolving declaring-schema-relative names
// against the entry schema. The fixtures below pin each cross-schema shape:
// imported-type declarations, name shadowing, and cross-schema inheritance.

// loadCrossSchema loads an in-memory multi-source schema rooted at entry.yammm.
func loadCrossSchema(t *testing.T, sources map[string]string) *schema.Schema {
	t.Helper()
	m := make(map[string][]byte, len(sources))
	for k, v := range sources {
		m[k] = []byte(v)
	}
	s, result := schema.LoadSourcesWithEntry(t.Context(), m, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if result.HasErrors() {
		t.Fatalf("load: %v", result.Err())
	}
	return s
}

// An association declared on an imported type resolves its target through the
// declaring schema's own import alias, which the entry schema does not know.
func TestValidate_ImportedTypeQualifiedEdgeTarget(t *testing.T) {
	s := loadCrossSchema(t, map[string]string{
		"entry.yammm": `schema "geo"

import "common.yammm" as common

type Anchor {
	id String primary
}
`,
		"common.yammm": `schema "common"

import "base.yammm" as base

type Region {
	code String primary
	--> IN_BASIN (_) base.Basin
}
`,
		"base.yammm": `schema "base"

type Basin {
	id String primary
}
`,
	})

	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(t.Context(), "common.Region", instance.RawInstance{
		Properties: map[string]any{
			"code":     "R0001",
			"in_basin": map[string]any{"_target_id": "B1"},
		},
	})
	if !result.OK() {
		t.Fatalf("imported-type edge must validate: %s", result)
	}
	edge, ok := valid.Edge("IN_BASIN")
	if !ok {
		t.Fatal("IN_BASIN edge missing from validated instance")
	}
	if got := len(edge.Targets()); got != 1 {
		t.Errorf("target count = %d, want 1", got)
	}
}

// shadowedRegionSchema declares Region in both the entry and the imported
// schema with different primary keys; common.City's unqualified target must
// resolve to common's Region (PK "id"), never the entry's (PK "code").
func shadowedRegionSchema(t *testing.T) *schema.Schema {
	t.Helper()
	return loadCrossSchema(t, map[string]string{
		"entry.yammm": `schema "app"

import "common.yammm" as common

type Region {
	code String primary
}
`,
		"common.yammm": `schema "common"

type Region {
	id String primary
}

type City {
	code String primary
	--> IN_REGION (_) Region
}
`,
	})
}

func TestValidate_ShadowedEdgeTarget(t *testing.T) {
	s := shadowedRegionSchema(t)
	v := instance.NewValidator(s)

	t.Run("correct FK for the declaring schema's target is accepted", func(t *testing.T) {
		valid, result := v.ValidateOne(t.Context(), "common.City", instance.RawInstance{
			Properties: map[string]any{
				"code":      "C1",
				"in_region": map[string]any{"_target_id": "R1"},
			},
		})
		if !result.OK() {
			t.Fatalf("correct-keyed edge must validate: %s", result)
		}
		if valid == nil {
			t.Fatal("validated instance is nil")
		}
	})

	t.Run("FK keyed by the shadowing entry type is rejected", func(t *testing.T) {
		valid, result := v.ValidateOne(t.Context(), "common.City", instance.RawInstance{
			Properties: map[string]any{
				"code":      "C1",
				"in_region": map[string]any{"_target_code": "R1"},
			},
		})
		if valid != nil {
			t.Error("wrong-keyed edge must not produce a valid instance")
		}
		if result.OK() {
			t.Fatal("wrong-keyed edge must be rejected")
		}
		if !result.HasCode(instance.ErrMissingFKTarget) {
			t.Errorf("want E_MISSING_FK_TARGET, got: %s", result)
		}
	})
}

// A composition declared on an imported type validates its children without
// round-tripping the declaring type's bare name through the entry schema.
func TestValidate_ImportedTypeComposition(t *testing.T) {
	s := loadCrossSchema(t, map[string]string{
		"entry.yammm": `schema "app"

import "common.yammm" as common

type Anchor {
	id String primary
}
`,
		"common.yammm": `schema "common"

type Widget {
	id String primary
	*-> HAS_PIECE (many) Piece
}

part type Piece {
	name String required
}
`,
	})

	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(t.Context(), "common.Widget", instance.RawInstance{
		Properties: map[string]any{
			"id": "W1",
			"has_piece": []any{
				map[string]any{"name": "p1"},
				map[string]any{"name": "p2"},
			},
		},
	})
	if !result.OK() {
		t.Fatalf("imported-type composition must validate: %s", result)
	}
	foundPiece := false
	for rel, children := range valid.Compositions() {
		if rel == "HAS_PIECE" && !children.IsNil() {
			foundPiece = true
		}
	}
	if !foundPiece {
		t.Fatal("HAS_PIECE children missing from validated instance")
	}
}

// crossInheritanceSchema gives the entry type a cross-schema abstract parent
// whose association and composition targets are declaring-schema-local names.
func crossInheritanceSchema(t *testing.T) *schema.Schema {
	t.Helper()
	return loadCrossSchema(t, map[string]string{
		"entry.yammm": `schema "app"

import "common.yammm" as common

type City extends common.Located {
	code String primary
}
`,
		"common.yammm": `schema "common"

abstract type Located {
	--> IN_REGION (_) Region
	*-> HAS_MARKER (many) Marker
}

type Region {
	id String primary
}

part type Marker {
	label String primary
}
`,
	})
}

func TestValidate_CrossSchemaInheritedRelations(t *testing.T) {
	s := crossInheritanceSchema(t)
	v := instance.NewValidator(s)

	valid, result := v.ValidateOne(t.Context(), "City", instance.RawInstance{
		Properties: map[string]any{
			"code":       "C1",
			"in_region":  map[string]any{"_target_id": "R1"},
			"has_marker": []any{map[string]any{"label": "m1"}},
		},
	})
	if !result.OK() {
		t.Fatalf("inherited cross-schema members must validate: %s", result)
	}
	edge, ok := valid.Edge("IN_REGION")
	if !ok {
		t.Fatal("IN_REGION edge missing from validated instance")
	}
	if got := len(edge.Targets()); got != 1 {
		t.Errorf("target count = %d, want 1", got)
	}
	foundMarker := false
	for rel, children := range valid.Compositions() {
		if rel == "HAS_MARKER" && !children.IsNil() {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Fatal("HAS_MARKER children missing from validated instance")
	}
}

// The duplicate-child-PK check must run against the declaring schema's target
// type even when the entry schema shadows its bare name with a PK-less part.
func TestValidate_ShadowedCompositionDuplicatePK(t *testing.T) {
	s := loadCrossSchema(t, map[string]string{
		"entry.yammm": `schema "app"

import "common.yammm" as common

part type Item {
	note String
}

type Anchor {
	id String primary
}
`,
		"common.yammm": `schema "common"

type Box {
	id String primary
	*-> HOLDS (many) Item
}

part type Item {
	sku String primary
}
`,
	})

	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(t.Context(), "common.Box", instance.RawInstance{
		Properties: map[string]any{
			"id": "B1",
			"holds": []any{
				map[string]any{"sku": "S1"},
				map[string]any{"sku": "S1"},
			},
		},
	})
	if valid != nil {
		t.Error("duplicate composed PKs must not produce a valid instance")
	}
	if result.OK() {
		t.Fatal("duplicate composed PKs must be rejected")
	}
	if !result.HasCode(instance.ErrDuplicateComposedPK) {
		t.Errorf("want E_DUPLICATE_COMPOSED_PK, got: %s", result)
	}
}

// SchemaBuilder shares the relation-target resolution path.
func TestSchemaBuilder_CrossSchemaTargets(t *testing.T) {
	t.Run("EdgeTo on an inherited cross-schema association", func(t *testing.T) {
		s := crossInheritanceSchema(t)
		b, err := instance.BuilderFor(s, "City")
		if err != nil {
			t.Fatalf("BuilderFor: %v", err)
		}

		raw, err := b.
			Property("code", "C1").
			EdgeTo("IN_REGION", "R1").
			Build()
		if err != nil {
			t.Fatalf("EdgeTo must resolve the declaring schema's target: %v", err)
		}

		edge, ok := raw.Properties["in_region"].(map[string]any)
		if !ok {
			t.Fatalf("edge field missing: %v", raw.Properties)
		}
		if got := edge["_target_id"]; got != "R1" {
			t.Errorf("FK must be keyed by the true target's PK; _target_id = %v", got)
		}

		v := instance.NewValidator(s)
		valid, result := v.ValidateOne(t.Context(), "City", raw)
		if !result.OK() {
			t.Fatalf("built instance must validate: %s", result)
		}
		if valid == nil {
			t.Fatal("validated instance is nil")
		}
	})
}

// TestValidate_DanglingQualifiedTargetRejectedAtBuild pins that the dangling
// cross-schema relation target the instance layer once had to handle at
// validation time is now rejected at schema construction: a registry-less
// Builder draws E_UNKNOWN_TYPE and nils the schema, so no validator can see a
// zero-TargetID relation through a public entry point.
func TestValidate_DanglingQualifiedTargetRejectedAtBuild(t *testing.T) {
	s, result := schema.NewBuilder().
		WithName("app").
		AddType("Node").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("LINKED", schema.NewTypeRef("ext", "Thing", location.Span{}), true, false).
		Done().
		Build()
	if s != nil {
		t.Fatal("a dangling qualified relation target must nil the schema")
	}
	if !result.HasCode(diag.E_UNKNOWN_TYPE) {
		t.Errorf("want E_UNKNOWN_TYPE, got: %s", result)
	}
}
