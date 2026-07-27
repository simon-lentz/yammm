package schema_test

import (
	"slices"
	"testing"
)

func TestAnnotation_Inheritance_PropertyLevelViaSharedPointer(t *testing.T) {
	t.Parallel()
	// A child inherits the parent's annotated property unchanged; the
	// annotation travels with the shared *Property, no merge code needed.
	s := loadOK(t, `schema "main"
type Base {
	id String primary
	state String @index
}
type Derived extends Base {
	extra String
}`)
	p := typeProperty(t, schemaType(t, s, "Derived"), "state")
	if got, want := propAnnNames(p), []string{"index"}; !slices.Equal(got, want) {
		t.Errorf("inherited state annotations: got %v, want %v", got, want)
	}
}

func TestAnnotation_Inheritance_TypeLevelLinearizes(t *testing.T) {
	t.Parallel()
	// An abstract parent's @@index appears in a concrete child's AllAnnotations.
	s := loadOK(t, `schema "main"
abstract type Base {
	id String primary
	state String
	published_on Date
	@@index(state, published_on)
}
type Derived extends Base {
	extra String
}`)
	if got, want := typeAnnNames(schemaType(t, s, "Derived")), []string{"index"}; !slices.Equal(got, want) {
		t.Errorf("Derived AllAnnotations: got %v, want %v", got, want)
	}
}

func TestAnnotation_Inheritance_ExactDuplicateDedupsKeepFirst(t *testing.T) {
	t.Parallel()
	// Child re-declares the same @@index the parent already has: two identical
	// composites dedup to one in the child's merged view (keep-first).
	s := loadOK(t, `schema "main"
abstract type Base {
	id String primary
	state String
	@@index(state)
}
type Derived extends Base {
	@@index(state)
}`)
	if got := typeAnnNames(schemaType(t, s, "Derived")); len(got) != 1 {
		t.Errorf("want 1 deduped @@index, got %v", got)
	}
}

func TestAnnotation_Inheritance_DifferentArgListsBothSurvive(t *testing.T) {
	t.Parallel()
	// Two @@index differing only in argument list are distinct indexes: both
	// survive, own first then inherited.
	s := loadOK(t, `schema "main"
abstract type Base {
	id String primary
	state String
	region String
	@@index(state)
}
type Derived extends Base {
	@@index(region)
}`)
	anns := schemaType(t, s, "Derived").AllAnnotationsSlice()
	if len(anns) != 2 {
		t.Fatalf("want 2 @@index, got %d", len(anns))
	}
	if got, want := argTexts(anns[0]), []string{"region"}; !slices.Equal(got, want) {
		t.Errorf("first (own) args: got %v, want %v", got, want)
	}
	if got, want := argTexts(anns[1]), []string{"state"}; !slices.Equal(got, want) {
		t.Errorf("second (inherited) args: got %v, want %v", got, want)
	}
}
