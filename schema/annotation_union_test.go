package schema_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// Equal properties inherited from two ancestors union their annotations, so a
// data-integrity marker like @writeOnce survives regardless of extends order.
// Here the *plain* ancestor is listed first — the ordering that previously
// dropped @writeOnce via silent keep-first.
func TestAnnotation_Union_ReverseOrderKeepsWriteOnce(t *testing.T) {
	t.Parallel()
	s, res := loadNoErr(t, `schema "main"
abstract type Base {
	id String primary
	created Timestamp
}
abstract type Audited {
	created Timestamp @writeOnce
}
type Entity extends Base, Audited {
	name String
}`)
	if _, ok := typeProperty(t, schemaType(t, s, "Entity"), "created").Annotation("writeOnce"); !ok {
		t.Error("Entity.created should inherit Audited's @writeOnce even though the plain ancestor is listed first")
	}
	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 0 {
		t.Errorf("ancestor-vs-ancestor union must be silent, got %d W_ANNOTATION_SHADOWED: %v", n, res)
	}
}

// The unioned annotation set is identical whichever order the ancestors are
// listed: the fix removes the extends-order dependence entirely.
func TestAnnotation_Union_OrderIndependent(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"Base, Audited", "Audited, Base"} {
		s := loadOK(t, `schema "main"
abstract type Base {
	id String primary
	created Timestamp
}
abstract type Audited {
	created Timestamp @writeOnce
}
type Entity extends `+order+` {
	name String
}`)
		if _, ok := typeProperty(t, schemaType(t, s, "Entity"), "created").Annotation("writeOnce"); !ok {
			t.Errorf("extends %s: Entity.created should carry @writeOnce", order)
		}
	}
}

// Distinct annotations contributed by two different Equal ancestors both land on
// the child (set union, not keep-first).
func TestAnnotation_Union_DistinctAnnotationsBothSurvive(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
abstract type A {
	id String primary
	state String @index
}
abstract type B {
	state String @writeOnce
}
type C extends A, B {
	extra String
}`)
	got := propAnnNames(typeProperty(t, schemaType(t, s, "C"), "state"))
	slices.Sort(got)
	if want := []string{"index", "writeOnce"}; !slices.Equal(got, want) {
		t.Errorf("C.state annotations: got %v, want %v", got, want)
	}
}

// When an earlier ancestor's narrower (required) property survives over a later
// ancestor's wider (optional) copy, the later ancestor's annotation still unions
// in — the annotation set does not depend on which ancestor won the narrowing.
func TestAnnotation_Union_NarrowingEarlierAncestorKeepsBothAnnotations(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
abstract type A {
	id String primary
	x String required @index
}
abstract type B {
	x String @writeOnce
}
type C extends A, B {
	extra String
}`)
	got := propAnnNames(typeProperty(t, schemaType(t, s, "C"), "x"))
	slices.Sort(got)
	if want := []string{"index", "writeOnce"}; !slices.Equal(got, want) {
		t.Errorf("C.x annotations: got %v, want %v", got, want)
	}
}

// When a later ancestor's narrower (required) property replaces an earlier
// ancestor's wider (optional) copy, the earlier ancestor's annotation still
// unions into the surviving narrower property.
func TestAnnotation_Union_NarrowingLaterAncestorKeepsBothAnnotations(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
abstract type A {
	id String primary
	x String @index
}
abstract type B {
	x String required @writeOnce
}
type C extends A, B {
	extra String
}`)
	x := typeProperty(t, schemaType(t, s, "C"), "x")
	got := propAnnNames(x)
	slices.Sort(got)
	if want := []string{"index", "writeOnce"}; !slices.Equal(got, want) {
		t.Errorf("C.x annotations: got %v, want %v", got, want)
	}
	if !x.IsRequired() {
		t.Error("C.x should be the narrower (required) version contributed by B")
	}
}

// A merged property is a synthesized copy, so it is NOT pointer-identical to the
// declared property any type's own slice holds. Origin bridges the two views —
// the contract every identity-keyed consumer of a merged property set depends on
// (the generator adapters key their datatype tables by the declared pointer).
func TestAnnotation_Union_OriginResolvesToDeclaredProperty(t *testing.T) {
	t.Parallel()
	s := loadOK(t, `schema "main"
abstract type A {
	id String primary
	created Timestamp
}
abstract type Audited {
	created Timestamp @writeOnce
}
type Entity extends A, Audited {
	name String
}`)
	declared := typeProperty(t, schemaType(t, s, "A"), "created")
	merged := typeProperty(t, schemaType(t, s, "Entity"), "created")

	if merged == declared {
		t.Fatal("precondition: the merged view should carry a synthesized copy, not A's declared property")
	}
	if merged.Origin() != declared {
		t.Error("Origin() on a merged property should return the declaring ancestor's declared property")
	}
	if declared.Origin() != declared {
		t.Error("Origin() on a declared property should return the property itself")
	}
	// The identity-bearing fields survive the copy; only annotations differ.
	if merged.Name() != declared.Name() || merged.Span() != declared.Span() ||
		merged.DeclaringScope().String() != declared.DeclaringScope().String() {
		t.Error("the synthesized copy should preserve name, span, and declaring scope")
	}
}

// Two Equal Vector ancestors demanding different @vector similarities is a
// genuine, unsatisfiable inheritance conflict — reported, not silently resolved
// by extends order.
func TestAnnotation_Union_ConflictingVectorSimilarity(t *testing.T) {
	t.Parallel()
	res := loadErr(t, `schema "main"
abstract type A {
	id String primary
	emb Vector[8] @vector(cosine)
}
abstract type B {
	emb Vector[8] @vector(euclidean)
}
type C extends A, B {
	extra String
}`)
	if !res.HasCode(diag.E_INVALID_ANNOTATION) {
		t.Errorf("want E_INVALID_ANNOTATION for conflicting inherited @vector similarities, got: %v", res)
	}
}

// One inheritance conflict is one diagnostic, however deep the chains carrying
// it are. mergeProperties visits every linearized ancestor and an ancestor that
// merely inherits the annotated property yields the same *Property, so the
// conflict is re-detected once per ancestor; only the first report is emitted.
func TestAnnotation_Union_ConflictReportedOncePerConflict(t *testing.T) {
	t.Parallel()
	res := loadErr(t, `schema "main"
abstract type G1 {
	emb Vector[8] @vector(cosine)
}
abstract type G2 {
	emb Vector[8] @vector(euclidean)
}
abstract type A extends G1 {}
abstract type B extends G2 {}
type C extends A, B {
	id String primary
}`)
	if n := codeCounts(res)[diag.E_INVALID_ANNOTATION]; n != 1 {
		t.Errorf("one conflicting @vector inheritance should report exactly 1 E_INVALID_ANNOTATION, got %d: %v", n, res)
	}
}
