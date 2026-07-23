package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

func TestAnnotation_Shadow_IdenticalRedeclarationDropsAndWarns(t *testing.T) {
	t.Parallel()
	// Child re-declares the parent property identically but without the
	// annotation: the child's pointer wins keep-first, dropping @writeOnce, and
	// a warning surfaces the silent write-shape regression.
	s, res := loadNoErr(t, `schema "main"
type Base {
	id String primary
	first_seen Timestamp @writeOnce
}
type Derived extends Base {
	first_seen Timestamp
}`)
	if _, ok := typeProperty(t, schemaType(t, s, "Derived"), "first_seen").Annotation("writeOnce"); ok {
		t.Error("Derived.first_seen should not carry the dropped inherited @writeOnce")
	}
	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 1 {
		t.Errorf("want exactly 1 W_ANNOTATION_SHADOWED, got %d: %v", n, res)
	}
}

func TestAnnotation_Shadow_NarrowingRedeclarationDropsAndWarns(t *testing.T) {
	t.Parallel()
	// Narrowing (optional -> required) also shadows the parent's annotation.
	_, res := loadNoErr(t, `schema "main"
type Base {
	id String primary
	state String @index
}
type Derived extends Base {
	state String required
}`)
	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 1 {
		t.Errorf("want exactly 1 W_ANNOTATION_SHADOWED, got %d: %v", n, res)
	}
}

func TestAnnotation_Shadow_RestatedAnnotationNoWarning(t *testing.T) {
	t.Parallel()
	// A re-declaration that re-states the annotation does not warn, and keeps it.
	s, res := loadNoErr(t, `schema "main"
type Base {
	id String primary
	first_seen Timestamp @writeOnce
}
type Derived extends Base {
	first_seen Timestamp @writeOnce
}`)
	if _, ok := typeProperty(t, schemaType(t, s, "Derived"), "first_seen").Annotation("writeOnce"); !ok {
		t.Error("Derived.first_seen should keep the re-stated @writeOnce")
	}
	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 0 {
		t.Errorf("want no W_ANNOTATION_SHADOWED, got %d: %v", n, res)
	}
}

func TestAnnotation_Shadow_DiamondEqualAncestorsSilent(t *testing.T) {
	t.Parallel()
	// Two ancestors declare Equal properties (one annotated); the child does
	// NOT re-declare. Keep-first between equal ancestors is silent — the dedup
	// exists precisely to merge equal ancestors — so no warning fires.
	s, res := loadNoErr(t, `schema "main"
abstract type A {
	id String primary
	x String @index
}
abstract type B {
	x String
}
type C extends A, B {
	extra String
}`)
	if _, ok := typeProperty(t, schemaType(t, s, "C"), "x").Annotation("index"); !ok {
		t.Error("C.x should keep A's @index (first-linearized ancestor survives)")
	}
	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 0 {
		t.Errorf("diamond dedup must be silent, got %d W_ANNOTATION_SHADOWED: %v", n, res)
	}
}

func TestAnnotation_Shadow_MultiAncestorTwoWarnings(t *testing.T) {
	t.Parallel()
	// A child re-declaring a property that two annotated ancestors both declare
	// draws one warning per shadowed ancestor.
	_, res := loadNoErr(t, `schema "main"
abstract type A {
	id String primary
	x String @index
}
abstract type B {
	x String @writeOnce
}
type C extends A, B {
	x String
}`)
	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 2 {
		t.Errorf("want 2 W_ANNOTATION_SHADOWED (one per shadowed ancestor), got %d: %v", n, res)
	}
}
