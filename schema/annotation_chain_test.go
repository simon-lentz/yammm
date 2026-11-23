package schema_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
)

// A subtype's deliberate shadowing binds its OWN subtypes too: the annotation
// must not reappear further down the chain.
//
// The linearized ancestor list is transitively closed and lists a parent's
// ancestors before the parent, so a grandchild sees both the grandparent's
// annotated declaration and the parent's un-annotated re-declaration. Merging
// them as if they were independent siblings kept the grandparent's copy and left
// a type and its own subtype disagreeing about the same property's write shape.
func TestAnnotation_Chain_GrandchildDoesNotResurrectShadowedWriteOnce(t *testing.T) {
	t.Parallel()
	s, res := loadNoErr(t, `schema "main"
abstract type Tracked {
	first_seen Timestamp required @writeOnce
}
type Shadowed extends Tracked {
	id String primary
	first_seen Timestamp required
}
type Grandchild extends Shadowed {
	extra String
}`)

	if _, ok := typeProperty(t, schemaType(t, s, "Shadowed"), "first_seen").Annotation("writeOnce"); ok {
		t.Error("Shadowed.first_seen should not carry the shadowed @writeOnce")
	}
	if _, ok := typeProperty(t, schemaType(t, s, "Grandchild"), "first_seen").Annotation("writeOnce"); ok {
		t.Error("Grandchild.first_seen must inherit Shadowed's decision, not resurrect Tracked's @writeOnce")
	}
	// One shadowing declaration, one warning — and none against Grandchild,
	// which dropped nothing.
	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 1 {
		t.Errorf("want exactly 1 W_ANNOTATION_SHADOWED (Shadowed's), got %d: %v", n, res)
	}
}

// The same resurrection through the narrowing branch, which unioned the shadowed
// ancestor's annotations into the narrower survivor explicitly.
func TestAnnotation_Chain_GrandchildDoesNotResurrectAcrossNarrowing(t *testing.T) {
	t.Parallel()
	s, _ := loadNoErr(t, `schema "main"
abstract type Base {
	code String @index
}
type Mid extends Base {
	id String primary
	code String required
}
type Leaf extends Mid {
	extra String
}`)

	if _, ok := typeProperty(t, schemaType(t, s, "Mid"), "code").Annotation("index"); ok {
		t.Error("Mid.code should not carry the shadowed @index")
	}
	if _, ok := typeProperty(t, schemaType(t, s, "Leaf"), "code").Annotation("index"); ok {
		t.Error("Leaf.code must inherit Mid's decision, not resurrect Base's @index")
	}
}

// A parent that re-declares an annotated property with a DIFFERENT annotation
// replaces the set rather than adding to it, and its subtypes see the
// replacement.
func TestAnnotation_Chain_GrandchildSeesReplacedAnnotationSet(t *testing.T) {
	t.Parallel()
	s, _ := loadNoErr(t, `schema "main"
abstract type Base {
	state String @index
}
type Mid extends Base {
	id String primary
	state String @writeOnce
}
type Leaf extends Mid {
	extra String
}`)

	for _, typeName := range []string{"Mid", "Leaf"} {
		got := propAnnNames(typeProperty(t, schemaType(t, s, typeName), "state"))
		slices.Sort(got)
		if want := []string{"writeOnce"}; !slices.Equal(got, want) {
			t.Errorf("%s.state annotations: got %v, want %v", typeName, got, want)
		}
	}
}

// A shadowing decision made by a parent is not re-blamed on a grandchild that
// re-declares the same property: by the time the grandchild is merged, its
// nearest annotated ancestor carries nothing to drop.
func TestAnnotation_Chain_ReshadowingGrandchildDoesNotWarn(t *testing.T) {
	t.Parallel()
	_, res := loadNoErr(t, `schema "main"
abstract type Base {
	first_seen Timestamp @writeOnce
}
type Mid extends Base {
	id String primary
	first_seen Timestamp
}
type Leaf extends Mid {
	first_seen Timestamp
}`)

	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 1 {
		t.Errorf("only Mid drops an annotation; want 1 W_ANNOTATION_SHADOWED, got %d: %v", n, res)
	}
}

// An ancestor that merely INHERITS an annotated property yields the same
// declared Property, so the shadowing was re-detected once per ancestor in the
// chain and printed the identical warning twice.
func TestAnnotation_Chain_ShadowWarningNotDuplicatedThroughChain(t *testing.T) {
	t.Parallel()
	_, res := loadNoErr(t, `schema "main"
type Base {
	id String primary
	first_seen Timestamp @writeOnce
}
type Mid extends Base {
	m String
}
type Leaf extends Mid {
	first_seen Timestamp
}`)

	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 1 {
		t.Errorf("one shadowing declaration must draw one warning, got %d: %v", n, res)
	}
}

// Both arms of a diamond carry the same declared property forward, so the
// shadowing was detected once per arm plus once for the shared ancestor.
func TestAnnotation_Chain_ShadowWarningNotDuplicatedThroughDiamond(t *testing.T) {
	t.Parallel()
	_, res := loadNoErr(t, `schema "main"
abstract type A {
	id String primary
	x String @index
}
abstract type B extends A {
	b String
}
abstract type C extends A {
	c String
}
type D extends B, C {
	x String
}`)

	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 1 {
		t.Errorf("one shadowed annotation reaching a diamond by both arms is one warning, got %d: %v", n, res)
	}
}

// Shadowing a chain must not disable union with an INCOMPARABLE mixin, and the
// outcome must not depend on which is listed first: the shadowed ancestor drops
// out either way, the mixin's annotation survives either way.
func TestAnnotation_Chain_ShadowedChainStillUnionsWithMixin(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"Mid, Mixin", "Mixin, Mid"} {
		s, _ := loadNoErr(t, `schema "main"
abstract type Base {
	state String @writeOnce
}
abstract type Mid extends Base {
	state String
}
abstract type Mixin {
	state String @index
}
type Leaf extends `+order+` {
	id String primary
}`)
		got := propAnnNames(typeProperty(t, schemaType(t, s, "Leaf"), "state"))
		slices.Sort(got)
		if want := []string{"index"}; !slices.Equal(got, want) {
			t.Errorf("extends %s: Leaf.state annotations: got %v, want %v", order, got, want)
		}
	}
}

// Annotation inheritance must work across an import boundary: the merge reads
// direct supertypes' merged views through resolveTypeRef's registry branch, and
// a cross-schema ancestor's TypeID is produced by a different completer. Every
// other annotation test is single-schema, so nothing else exercises that path.
func TestAnnotation_CrossSchema_InheritsAnnotation(t *testing.T) {
	t.Parallel()
	s, res := loadAnnotationImport(t,
		`schema "base"
abstract type Tracked {
	first_seen Timestamp @writeOnce
}`,
		`schema "main"
import "./base" as base
type Doc extends base.Tracked {
	id String primary
}`)
	if diagtest.Err(res) != nil {
		t.Fatalf("cross-file load should succeed: %v", res)
	}
	if _, ok := typeProperty(t, schemaType(t, s, "Doc"), "first_seen").Annotation("writeOnce"); !ok {
		t.Error("Doc.first_seen should inherit the imported ancestor's @writeOnce")
	}
}

// A shadowing decision made in one file binds a subtype declared in another.
func TestAnnotation_CrossSchema_ShadowingHoldsAcrossImport(t *testing.T) {
	t.Parallel()
	s, res := loadAnnotationImport(t,
		`schema "base"
abstract type Tracked {
	first_seen Timestamp @writeOnce
}
abstract type Shadowed extends Tracked {
	first_seen Timestamp
}`,
		`schema "main"
import "./base" as base
type Doc extends base.Shadowed {
	id String primary
}`)
	if diagtest.Err(res) != nil {
		t.Fatalf("cross-file load should succeed: %v", res)
	}
	if _, ok := typeProperty(t, schemaType(t, s, "Doc"), "first_seen").Annotation("writeOnce"); ok {
		t.Error("Doc must honour the imported parent's shadowing, not resurrect the grandparent's @writeOnce")
	}
}

// An imported mixin and a local mixin are incomparable, so their annotations
// union — the cross-schema form of the mixin rule.
func TestAnnotation_CrossSchema_UnionsWithLocalMixin(t *testing.T) {
	t.Parallel()
	s, res := loadAnnotationImport(t,
		`schema "base"
abstract type Audited {
	state String @writeOnce
}`,
		`schema "main"
import "./base" as base
abstract type Searchable {
	state String @index
}
type Doc extends base.Audited, Searchable {
	id String primary
}`)
	if diagtest.Err(res) != nil {
		t.Fatalf("cross-file load should succeed: %v", res)
	}
	got := propAnnNames(typeProperty(t, schemaType(t, s, "Doc"), "state"))
	slices.Sort(got)
	if want := []string{"index", "writeOnce"}; !slices.Equal(got, want) {
		t.Errorf("Doc.state annotations: got %v, want %v", got, want)
	}
}

// A property carrying the same annotation name twice is one error, reported
// against the declaration. Building the inherited set by comparing an
// annotation with its own duplicate added a second, self-contradictory
// "inherits conflicting @x ... from Base and Base" on every subtype.
func TestAnnotation_Chain_DuplicateOnAncestorIsOneDiagnostic(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type Base {
	id String primary
	emb Vector[8] @vector(cosine) @vector(euclidean)
}
type Derived extends Base {
	x String
}`)
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1})
}

// A conflict must name the supertypes the two annotations reached the type
// through. Reading a declaring scope off merged clones named the same ancestor
// on both sides of an "inherits ... from X and Y" message.
func TestAnnotation_Chain_ConflictNamesContributingSupertypes(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type Base { emb Vector[8] }
abstract type Cos { emb Vector[8] @vector(cosine) }
abstract type Euc { emb Vector[8] @vector(euclidean) }
abstract type X extends Base, Cos { xx String }
abstract type M extends Base, Euc { mm String }
type C extends X, M { id String primary }`)

	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1})

	var msg string
	for i := range res.Issues() {
		if i.Code() == diag.E_INVALID_ANNOTATION {
			msg = i.Message()
			break
		}
	}
	if msg == "" {
		t.Fatal("no E_INVALID_ANNOTATION was reported")
	}
	if !strings.Contains(msg, "from X and M") {
		t.Errorf("conflict should name the contributing supertypes X and M; got %q", msg)
	}
}

// Two incomparable mixins that drop the SAME annotation name are two distinct
// declarations the re-declaration shadowed, so both must be reported. Keying the
// dedup on (property, annotation name) surfaced only the first.
func TestAnnotation_Chain_SameNameMixinsBothWarn(t *testing.T) {
	t.Parallel()
	_, res := loadNoErr(t, `schema "main"
abstract type A {
	id String primary
	x String @index
}
abstract type B {
	x String @index
}
type C extends A, B {
	x String
}`)
	if n := codeCounts(res)[diag.W_ANNOTATION_SHADOWED]; n != 2 {
		t.Errorf("both mixins' dropped @index must be reported, got %d: %v", n, res)
	}
}

// The warning must not advise re-stating an annotation the same load rejects:
// following that advice produces a second copy of the same error. It is
// therefore emitted after annotation validation, which is what establishes
// whether the annotation is usable at all.
func TestAnnotation_Chain_NoShadowWarningForRejectedAnnotation(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type Base {
	id String primary
	x String @nope
}
type Derived extends Base {
	x String
}`)
	wantCounts(t, res, map[diag.Code]int{diag.E_UNKNOWN_ANNOTATION: 1})
}

// Same for an annotation that is known but ineligible for its target.
func TestAnnotation_Chain_NoShadowWarningForIneligibleAnnotation(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type Base {
	id String primary
	v Vector[4] @index
}
type Derived extends Base {
	v Vector[4]
}`)
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_ANNOTATION_TARGET: 1})
}

// One structural conflict is one diagnostic, however many ancestors carry the
// clashing property forward.
func TestAnnotation_Chain_PropertyConflictReportedOnce(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type G1 { x String }
abstract type G2 { x Integer }
abstract type A extends G1 { aa String }
abstract type B extends G2 { bb String }
type C extends A, B { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_PROPERTY_CONFLICT: 1})
}

// The merged view must carry the annotation objects of the ancestor that
// actually determines it. When a nearer ancestor re-states an annotation the
// structural survivor already had, the two sets match structurally, and keeping
// the survivor's objects left editor navigation pointing at the shadowed
// grandparent's declaration.
func TestAnnotation_Chain_MergedViewCarriesEffectiveAncestorsAnnotation(t *testing.T) {
	t.Parallel()
	s, _ := loadNoErr(t, `schema "main"
abstract type A {
	id String primary
	x String @index
}
abstract type B extends A {
	x String @index
}
type D extends B {
	extra String
}`)
	bAnn, ok := typeProperty(t, schemaType(t, s, "B"), "x").Annotation("index")
	if !ok {
		t.Fatal("B.x should carry its own @index")
	}
	dAnn, ok := typeProperty(t, schemaType(t, s, "D"), "x").Annotation("index")
	if !ok {
		t.Fatal("D.x should inherit @index")
	}
	if dAnn.Span() != bAnn.Span() {
		t.Errorf("D.x's @index should be B's restatement (span %v), got span %v", bAnn.Span(), dAnn.Span())
	}
}

// THE DIAMOND RULE. When one direct supertype drops an annotation and another
// carries it, the two arms disagree and there is no correct merge — honouring
// either silently overrides the other's decision. The loader refuses to pick and
// says how to settle it.
func TestAnnotation_Diamond_DisagreementIsAnError(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type Base { first_seen Timestamp @writeOnce }
abstract type Mutable extends Base { first_seen Timestamp }
abstract type Other extends Base { tag String }
type Doc extends Mutable, Other { id String primary }`)

	// The disagreement error at Doc, plus Mutable's own shadow warning — Mutable
	// really did drop @writeOnce, and that warning stands independent of Doc.
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1, diag.W_ANNOTATION_SHADOWED: 1})
	var msg string
	for i := range res.Issues() {
		if i.Code() == diag.E_INVALID_ANNOTATION {
			msg = i.Message()
			break
		}
	}
	for _, want := range []string{"@writeOnce", "Other", "Mutable", "re-state or omit"} {
		if !strings.Contains(msg, want) {
			t.Errorf("disagreement message should mention %q; got %q", want, msg)
		}
	}
}

// Re-stating on the subtype is the documented resolution, and it must actually
// resolve: the subtype's own declaration is authoritative.
func TestAnnotation_Diamond_RestatingResolves(t *testing.T) {
	t.Parallel()
	s, res := loadNoErr(t, `schema "main"
abstract type Base { first_seen Timestamp @writeOnce }
abstract type Mutable extends Base { first_seen Timestamp }
abstract type Other extends Base { tag String }
type Doc extends Mutable, Other {
	id String primary
	first_seen Timestamp @writeOnce
}`)
	if _, ok := typeProperty(t, schemaType(t, s, "Doc"), "first_seen").Annotation("writeOnce"); !ok {
		t.Error("Doc's own re-statement should settle the disagreement in favour of @writeOnce")
	}
	if res.HasCode(diag.E_INVALID_ANNOTATION) {
		t.Errorf("re-stating must clear the disagreement: %v", res)
	}
}

// Omitting it on the subtype settles it the other way, also without error.
func TestAnnotation_Diamond_OmittingResolves(t *testing.T) {
	t.Parallel()
	s, res := loadNoErr(t, `schema "main"
abstract type Base { first_seen Timestamp @writeOnce }
abstract type Mutable extends Base { first_seen Timestamp }
abstract type Other extends Base { tag String }
type Doc extends Mutable, Other {
	id String primary
	first_seen Timestamp
}`)
	if _, ok := typeProperty(t, schemaType(t, s, "Doc"), "first_seen").Annotation("writeOnce"); ok {
		t.Error("omitting on the subtype should settle the disagreement against @writeOnce")
	}
	if res.HasCode(diag.E_INVALID_ANNOTATION) {
		t.Errorf("omitting must clear the disagreement: %v", res)
	}
	// The resolution must be SILENT at Doc: a shadow warning here would tell the
	// user to undo the very fix they were told to make. Any W_ANNOTATION_SHADOWED
	// in the load belongs to Mutable (line 3), never to Doc's re-declaration.
	for i := range res.Issues() {
		if i.Code() == diag.W_ANNOTATION_SHADOWED && i.Span().Start.Line >= 6 {
			t.Errorf("Doc's resolving re-declaration must not draw a shadow warning; got at line %d: %s",
				i.Span().Start.Line, i.Message())
		}
	}
}

// A plain absence is NOT an opinion. The ordinary mixin union — one ancestor
// contributes the marker, the other simply never mentioned it — must stay silent,
// or the disagreement rule would swallow the feature it is meant to protect.
func TestAnnotation_Diamond_PlainAbsenceStillUnions(t *testing.T) {
	t.Parallel()
	s, res := loadNoErr(t, `schema "main"
abstract type A {
	id String primary
	created Timestamp
}
abstract type Audited {
	created Timestamp @writeOnce
}
type Entity extends A, Audited { name String }`)
	if _, ok := typeProperty(t, schemaType(t, s, "Entity"), "created").Annotation("writeOnce"); !ok {
		t.Error("an ancestor that never mentioned the annotation must not veto it")
	}
	if res.Len() != 0 {
		t.Errorf("the mixin union must stay silent, got: %v", res)
	}
}

// A drop propagates: the disagreement is detected even when the dropping arm is
// several levels up, because the suppression is carried forward.
func TestAnnotation_Diamond_DropPropagatesThroughChain(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type Base { first_seen Timestamp @writeOnce }
abstract type Mutable extends Base { first_seen Timestamp }
abstract type Deeper extends Mutable { extra String }
abstract type Other extends Base { tag String }
type Doc extends Deeper, Other { id String primary }`)
	// The disagreement at Doc, plus Mutable's own shadow warning for dropping it.
	wantCounts(t, res, map[diag.Code]int{diag.E_INVALID_ANNOTATION: 1, diag.W_ANNOTATION_SHADOWED: 1})
}

// Both arms dropping is agreement, not disagreement.
func TestAnnotation_Diamond_BothArmsDropIsSilent(t *testing.T) {
	t.Parallel()
	s, res := loadNoErr(t, `schema "main"
abstract type Base { first_seen Timestamp @writeOnce }
abstract type M1 extends Base { first_seen Timestamp }
abstract type M2 extends Base { first_seen Timestamp }
type Doc extends M1, M2 { id String primary }`)
	if _, ok := typeProperty(t, schemaType(t, s, "Doc"), "first_seen").Annotation("writeOnce"); ok {
		t.Error("both arms dropped it; it must stay dropped")
	}
	if res.HasCode(diag.E_INVALID_ANNOTATION) {
		t.Errorf("two arms agreeing is not a disagreement: %v", res)
	}
}

// The schema-level manifestation of constraint-equality reflexivity
// ([TestAliasConstraint_Equal_ReflexiveWhenUndecidable]): a property whose
// datatype cannot be resolved must merge with itself as the closure re-presents
// it, not be reported as conflicting with its own declaration.
func TestAnnotation_Chain_UnresolvableDatatypeDoesNotSelfConflict(t *testing.T) {
	t.Parallel()
	_, res := diagtest.Capture(t, func(warn func(diag.Issue)) (schema.Schema, error) {
		return schema.LoadString(t.Context(), `schema "main"

abstract type A {
	amount ext.Amount
}

abstract type B extends A {
}

abstract type C extends B {
}

type D extends A, B, C {
	id String primary
}
`, "main.yammm", schema.WithWarnings(warn))
	})

	for i := range res.Issues() {
		if i.Code() == diag.E_PROPERTY_CONFLICT {
			t.Errorf("one declaration must not conflict with itself: %s", i.Message())
		}
	}
}

// Two ancestors declaring structurally identical rivals are ONE collision —
// one edit to the surviving declaration clears both — so they draw one
// diagnostic, and that diagnostic names every rival. Reporting once while
// naming only the first would send the user to fix B and meet C on reload.
func TestAnnotation_Chain_EqualRelationRivalsCollapseAndAreAllNamed(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type U { id String primary }
type V { id String primary }
abstract type A { --> REL (one) U }
abstract type B { --> REL (one) V }
abstract type C { --> REL (one) V }
type T extends A, B, C { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_RELATION_COLLISION: 1})

	gotLines := make(map[int]string)
	for i := range res.Issues() {
		if i.Code() == diag.E_RELATION_COLLISION {
			for _, r := range i.Related() {
				gotLines[r.Span.Start.Line] = r.Message
			}
		}
	}
	want := map[int]string{
		4: "surviving definition declared here",
		5: "conflicting definition declared here",
		6: "conflicting definition declared here",
	}
	if !maps.Equal(gotLines, want) {
		t.Errorf("related spans: got %v, want %v (both rivals B and C must be named)", gotLines, want)
	}
}

// Rivals that are NOT equal to each other stay separate collisions: two
// different targets are two problems, and one edit cannot resolve both.
func TestAnnotation_Chain_DistinctRelationRivalsStaySeparate(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
type U { id String primary }
type V { id String primary }
type W { id String primary }
abstract type A { --> REL (one) U }
abstract type B { --> REL (one) V }
abstract type C { --> REL (one) W }
type T extends A, B, C { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_RELATION_COLLISION: 2})
}

// A redundant extends clause — a supertype listed together with its own ancestor
// — must resolve as the ordinary chain it is, not as a disagreement between the
// two. directSuperTypes reduces to the maximal direct supertypes to make this
// hold; without that reduction the schema below spuriously fails. Pinned in both
// clause orders because the reduction must be order-independent.
func TestAnnotation_Diamond_RedundantExtendsIsNotADisagreement(t *testing.T) {
	t.Parallel()
	for _, order := range []string{"Drop, Base", "Base, Drop"} {
		_, res := loadNoErr(t, `schema "main"
abstract type Base { x String @writeOnce }
abstract type Drop extends Base { x String }
type Leaf extends `+order+` { id String primary }`)
		if res.HasCode(diag.E_INVALID_ANNOTATION) {
			t.Errorf("extends %s: an ancestor listed beside its descendant is a chain, not a disagreement: %v", order, res)
		}
	}
}

// A re-declaration the load REJECTED decides nothing about annotations. Letting
// it record a suppression made every subtype derive a disagreement from it,
// pointing the user at the subtype when the only real fix is at the rejected
// declaration.
func TestAnnotation_Chain_RejectedRedeclarationSuppressesNothing(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type Base { x String @index }
abstract type Mark { x String @index }
abstract type Mid extends Base { x Integer }
type Leaf extends Mid, Mark { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_PROPERTY_CONFLICT: 2})
}
