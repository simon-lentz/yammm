package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// @index on a primary-key property is rejected only when EVERY concrete type
// that emits a uniqueness constraint over it keys on that property alone —
// there and only there does the constraint's backing index already serve the
// lookup. These cases pin each way that quantifier can come out.
//
// Rejections assert exact counts (via wantCounts) rather than mere presence, so
// a regression that reports once per emitter, or stacks a cascade error, fails
// here instead of passing unnoticed.

// A composite key assembled from two abstract mixins is composite where it is
// emitted, so @index on one member is useful, not redundant. Counting primary
// keys on the declaring mixin read the split key as sole and rejected this.
func TestAnnotation_IndexOnPrimary_CompositeAcrossMixins_Accepted(t *testing.T) {
	t.Parallel()
	loadNoErr(t, `schema "main"
abstract type A {
	a String primary @index
}
abstract type B {
	b String primary
}
type C extends A, B {
	x String
}`)
}

// Emitters disagreeing is the case with no satisfying edit: Base keys on `a`
// alone (redundant there) while Derived's key is composite (useful there), and
// one annotation serves both. The rule reports only what can be safely removed,
// so it stays silent — the one shape where a redundant index still ships.
func TestAnnotation_IndexOnPrimary_EmittersDisagree_Silent(t *testing.T) {
	t.Parallel()
	_, res := loadNoErr(t, `schema "main"
type Base {
	a String primary @index
}
type Derived extends Base {
	b String primary
}`)
	if n := codeCounts(res)[diag.E_INVALID_ANNOTATION_TARGET]; n != 0 {
		t.Errorf("no edit satisfies both emitters, so the rule must stay silent; got %d: %v", n, res)
	}
}

// An abstract type emits no constraint of its own, so redundancy is decided by
// its concrete descendants — here the single descendant keys on `a` alone.
func TestAnnotation_IndexOnPrimary_SoleKeyThroughAbstractBase_Rejected(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type Base {
	a String primary @index
}
type Concrete extends Base {
	x String
}`)
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1})
}

// Two concrete descendants that both key solely on the property draw ONE
// diagnostic, not one per emitter.
func TestAnnotation_IndexOnPrimary_TwoSoleKeyedDescendants_OneDiagnostic(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type Base {
	a String primary @index
}
type C1 extends Base {
	x String
}
type C2 extends Base {
	y String
}`)
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1})
}

// An abstract type with no concrete descendant emits nothing at all, so there
// is no constraint for the annotation to be redundant against. Asserted off the
// raw result rather than through loadNoErr, whose t.Fatalf on any error would
// abort before a targeted check could run and report the generic message.
func TestAnnotation_IndexOnPrimary_AbstractWithNoEmitter_Silent(t *testing.T) {
	t.Parallel()
	_, res := schema.LoadString(t.Context(), `schema "main"
abstract type Mixin {
	a String primary @index
}
type Unrelated {
	id String primary
}`, "main.yammm")
	if n := codeCounts(res)[diag.E_INVALID_ANNOTATION_TARGET]; n != 0 {
		t.Errorf("an abstract type with no concrete descendant emits no constraint to be redundant against; got %d: %v", n, res)
	}
	if res.HasErrors() {
		t.Errorf("schema should load clean: %v", res)
	}
}

// The deferral must hold when the unresolved supertype is on a DESCENDANT
// rather than on the annotated type: that descendant may inherit a co-key once
// the reference resolves, so its key is not yet known to be sole. Without this
// term the schema would be rejected outright in a registry-less context.
func TestAnnotation_IndexOnPrimary_DescendantDeferredSupertype_Silent(t *testing.T) {
	t.Parallel()
	_, res := schema.NewBuilder().
		WithName("main").
		AddType("Keyed").
		AsAbstract().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithPropertyAnnotation("id", "index").
		Done().
		AddType("Doc").
		Extends(schema.NewTypeRef("", "Keyed", location.Span{})).
		Extends(schema.NewTypeRef("ext", "Unresolved", location.Span{})).
		Done().
		Build()
	if res.HasCode(diag.E_INVALID_ANNOTATION_TARGET) {
		t.Errorf("a descendant with an unresolved supertype may still gain a co-key; got: %v", res)
	}
}

// The same deferral when the unresolved supertype is on the annotated type
// itself.
func TestAnnotation_IndexOnPrimary_DeferredSupertype_Silent(t *testing.T) {
	t.Parallel()
	_, res := schema.NewBuilder().
		WithName("main").
		AddType("Entity").
		Extends(schema.NewTypeRef("ext", "Base", location.Span{})).
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithPropertyAnnotation("id", "index").
		Done().
		Build()
	if res.HasCode(diag.E_INVALID_ANNOTATION_TARGET) {
		t.Errorf("@index on a primary key under an unresolved supertype must not be flagged redundant, got: %v", res)
	}
}

// KNOWN LIMIT, pinned so a change to it is deliberate: completion records
// subtypes for local types only, so a mixin annotated in one file and made
// concrete in another has no visible emitter and draws nothing — while the same
// model in a single file is rejected (see
// TestAnnotation_IndexOnPrimary_SoleKeyThroughAbstractBase_Rejected). Closing
// this needs registry-wide descendant visibility at validation time.
func TestAnnotation_IndexOnPrimary_CrossSchemaDescendantInvisible(t *testing.T) {
	t.Parallel()
	_, res := loadAnnotationImport(t,
		`schema "base"
abstract type Mixin {
	a String primary @index
}`,
		`schema "main"
import "./base" as base
type Concrete extends base.Mixin {
	x String
}`)
	if res.HasErrors() {
		t.Fatalf("cross-file load should succeed: %v", res)
	}
	if n := codeCounts(res)[diag.E_INVALID_ANNOTATION_TARGET]; n != 0 {
		t.Errorf("the cross-schema descendant is invisible to the rule, so it must stay silent; got %d: %v", n, res)
	}
}

// @@index over a sole primary key is ACCEPTED, unlike the equivalent @index.
// docs/SPEC.md lists "primary-key members allowed" for @@index with no
// redundancy caveat: @@index is the explicit-shape construct, so a one-member
// composite over a sole key is a deliberate request. The two placements differ
// here by design.
func TestAnnotation_IndexOnPrimary_TypeLevelOverSoleKeyAccepted(t *testing.T) {
	t.Parallel()
	loadNoErr(t, `schema "main"
type T {
	id String primary
	@@index(id)
}`)
}

// The redundancy check must defer when an emitter has a co-primary that key
// extraction rejected for its datatype: the intended composite currently reads
// as sole, so firing would stack a spurious redundancy error on the real
// E_INVALID_PRIMARY_KEY_TYPE. Without the hasRejectedPrimary guard this schema
// draws both.
func TestAnnotation_IndexOnPrimary_RejectedCoPrimaryDefers(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type T {
	a Integer primary @index
	b String primary
}`)
	// Vector/Integer-as-key is the real error; @index redundancy must NOT stack.
	if n := codeCounts(res)[diag.E_INVALID_ANNOTATION_TARGET]; n != 0 {
		t.Errorf("redundancy must defer while a co-primary is rejected; got %d E_INVALID_ANNOTATION_TARGET: %v", n, res)
	}
	if !res.HasCode(diag.E_INVALID_PRIMARY_KEY_TYPE) {
		t.Errorf("the rejected co-primary should still draw E_INVALID_PRIMARY_KEY_TYPE: %v", res)
	}
}
