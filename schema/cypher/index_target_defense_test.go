package cypher

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/mapping"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/schema"
)

// defenseModel is a schema every public entry point accepts. Its annotations
// are borrowed below and paired with properties they do not name, a pairing no
// schema an entry point returns holds, so only these tests reach the re-checks.
func defenseModel(t *testing.T) schema.Type {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "defense"

type Entity {
	id String primary
	title String required @index @fulltext
	tags List<String> required
	count Integer required
	embedding Vector[3] required @vector(cosine)
	flat Vector[3] required
}
`, "defense.yammm")
	if result.HasErrors() {
		t.Fatalf("defense model must build cleanly: %v", result.Err())
	}
	typ, ok := s.Type("Entity")
	if !ok {
		t.Fatal("Entity missing from the defense model")
	}
	return typ
}

func annotationOn(t *testing.T, typ schema.Type, prop, name string) (schema.Property, schema.Annotation) {
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

func propertyOn(t *testing.T, typ schema.Type, name string) schema.Property {
	t.Helper()
	p, ok := typ.Property(name)
	if !ok {
		t.Fatalf("property %q missing", name)
	}
	return p
}

// The three index-target re-checks are unreachable through any public entry
// point — [schema] proves that by exhaustion — so they are driven directly.
// Without this, deleting any of them leaves the suite green while the generator
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

	t.Run("vector target rejects a dimension outside 1 to the bound", func(t *testing.T) {
		t.Parallel()
		for _, d := range []int{0, parse.MaxIndexedVectorDimensions + 1} {
			collector := diag.NewCollector(0)
			ok := validVectorTarget(typ, propertyOn(t, typ, "flat"), vectorAnn,
				schema.NewVectorConstraint(d), true, collector, reportedTargets{})
			if ok {
				t.Errorf("validVectorTarget accepted dimension %d — Neo4j aborts the DDL apply on it", d)
			}
			if !collector.Result().HasCode(E_NEO4J_INVALID_INDEX_TARGET) {
				t.Errorf("dimension %d: want %s, got: %v", d, E_NEO4J_INVALID_INDEX_TARGET, collector.Result())
			}
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
		if !validVectorTarget(typ, propertyOn(t, typ, "embedding"), vectorAnn,
			schema.NewVectorConstraint(parse.MaxIndexedVectorDimensions), true, collector, reportedTargets{}) {
			t.Errorf("validVectorTarget rejected a %d-dimensional Vector, the bound Neo4j accepts", parse.MaxIndexedVectorDimensions)
		}
		if collector.Result().HasErrors() {
			t.Errorf("valid targets drew diagnostics: %v", collector.Result())
		}
	})
}

func TestTargetScope_AnUnknownPropertyNamesTheAnnotatedType(t *testing.T) {
	t.Parallel()
	book := defenseModel(t)
	if got := targetScope(book, schema.Property{}); got != (mapping.IdentifierSubject{TypeName: "Entity"}) {
		t.Errorf("targetScope = %+v", got)
	}
}
