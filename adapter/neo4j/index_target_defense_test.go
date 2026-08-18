package neo4j

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// defenseModel is a schema every public entry point accepts. Its annotations
// are borrowed below and paired with properties they do not name, which is the
// shape the target re-checks defend against: a model assembled outside the
// entry points that guarantee the pairing is sound.
func defenseModel(t *testing.T) *schema.Type {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("defense").
		AddType("Entity").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("title", schema.NewStringConstraint()).
		WithProperty("tags", schema.NewListConstraint(schema.NewStringConstraint())).
		WithProperty("count", schema.NewIntegerConstraint()).
		WithProperty("embedding", schema.NewVectorConstraint(3)).
		WithProperty("flat", schema.NewVectorConstraint(3)).
		WithPropertyAnnotation("title", "index").
		WithPropertyAnnotation("title", "fulltext").
		WithPropertyAnnotation("embedding", "vector", "cosine").
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("defense model must build cleanly: %v", result.Err())
	}
	typ, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity missing from the defense model")
	}
	return typ
}

func annotationOn(t *testing.T, typ *schema.Type, prop, name string) (*schema.Property, *schema.Annotation) {
	t.Helper()
	p, ok := typ.Property(prop)
	if !ok {
		t.Fatalf("property %q missing", prop)
	}
	ann, ok := p.Annotation(name)
	if !ok {
		t.Fatalf("property %q carries no @%s", prop, name)
	}
	return p, ann
}

func propertyOn(t *testing.T, typ *schema.Type, name string) *schema.Property {
	t.Helper()
	p, ok := typ.Property(name)
	if !ok {
		t.Fatalf("property %q missing", name)
	}
	return p
}

// The three index-target re-checks are unreachable through any public entry
// point — [schema] proves that by exhaustion — so they are driven directly.
// Without this, deleting any of them leaves the suite green while the adapter
// silently emits DDL the DSL's own rule forbids.
func TestIndexTargetDefense_RejectsTargetsTheLoaderWouldHaveRejected(t *testing.T) {
	t.Parallel()
	typ := defenseModel(t)
	_, indexAnn := annotationOn(t, typ, "title", "index")
	_, fulltextAnn := annotationOn(t, typ, "title", "fulltext")
	_, vectorAnn := annotationOn(t, typ, "embedding", "vector")

	t.Run("scalar target rejects a List", func(t *testing.T) {
		t.Parallel()
		collector := diag.NewCollector(0)
		ok := validScalarTarget(typ, propertyOn(t, typ, "tags"), indexAnn, collector, reportedTargets{})
		if ok {
			t.Error("validScalarTarget accepted a List — a range index over it is not emittable")
		}
		if !collector.Result().HasCode(E_NEO4J_INVALID_INDEX_TARGET) {
			t.Errorf("want %s, got: %v", E_NEO4J_INVALID_INDEX_TARGET, collector.Result())
		}
	})

	t.Run("fulltext target rejects a non-text property", func(t *testing.T) {
		t.Parallel()
		collector := diag.NewCollector(0)
		ok := validFulltextTarget(typ, propertyOn(t, typ, "count"), fulltextAnn, collector, reportedTargets{})
		if ok {
			t.Error("validFulltextTarget accepted an Integer — a fulltext index over it is not emittable")
		}
		if !collector.Result().HasCode(E_NEO4J_INVALID_INDEX_TARGET) {
			t.Errorf("want %s, got: %v", E_NEO4J_INVALID_INDEX_TARGET, collector.Result())
		}
	})

	t.Run("vector target rejects a non-Vector property", func(t *testing.T) {
		t.Parallel()
		collector := diag.NewCollector(0)
		// A valid dimension, so only the not-a-Vector guard can fire: a zero
		// one is caught by the guard after it and the branch stays unpinned.
		ok := validVectorTarget(typ, propertyOn(t, typ, "title"), vectorAnn,
			schema.NewVectorConstraint(3), false, collector, reportedTargets{})
		if ok {
			t.Error("validVectorTarget accepted a String — the declared ANN index would be dropped silently")
		}
		if !collector.Result().HasCode(E_NEO4J_INVALID_INDEX_TARGET) {
			t.Errorf("want %s, got: %v", E_NEO4J_INVALID_INDEX_TARGET, collector.Result())
		}
		if !strings.Contains(collector.Result().String(), "not a Vector") {
			t.Errorf("the diagnostic does not name the reason: %v", collector.Result())
		}
	})

	t.Run("vector target rejects a non-positive dimension", func(t *testing.T) {
		t.Parallel()
		collector := diag.NewCollector(0)
		ok := validVectorTarget(typ, propertyOn(t, typ, "flat"), vectorAnn,
			schema.NewVectorConstraint(0), true, collector, reportedTargets{})
		if ok {
			t.Error("validVectorTarget accepted dimension 0 — Neo4j aborts the DDL apply on it")
		}
		if !collector.Result().HasCode(E_NEO4J_INVALID_INDEX_TARGET) {
			t.Errorf("want %s, got: %v", E_NEO4J_INVALID_INDEX_TARGET, collector.Result())
		}
	})

	t.Run("each accepts the target it is meant to", func(t *testing.T) {
		t.Parallel()
		collector := diag.NewCollector(0)
		if !validScalarTarget(typ, propertyOn(t, typ, "title"), indexAnn, collector, reportedTargets{}) {
			t.Error("validScalarTarget rejected a String")
		}
		if !validFulltextTarget(typ, propertyOn(t, typ, "title"), fulltextAnn, collector, reportedTargets{}) {
			t.Error("validFulltextTarget rejected a String")
		}
		if !validVectorTarget(typ, propertyOn(t, typ, "embedding"), vectorAnn,
			schema.NewVectorConstraint(3), true, collector, reportedTargets{}) {
			t.Error("validVectorTarget rejected a 3-dimensional Vector")
		}
		if collector.Result().HasErrors() {
			t.Errorf("valid targets drew diagnostics: %v", collector.Result())
		}
	})
}
