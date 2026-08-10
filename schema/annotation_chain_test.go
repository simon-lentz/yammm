package schema_test

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
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
// declared *Property, so the shadowing was re-detected once per ancestor in the
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
	if res.HasErrors() {
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
	if res.HasErrors() {
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
	if res.HasErrors() {
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

// The E_PROPERTY_CONFLICT dedup must report one diagnostic per clash — a pair
// of narrowing chains the walk could not unify — whatever member of a chain is
// the survivor or the incomer at the point of detection. This one table pins
// every shape that breaks a weaker dedup scheme: a scalar key (property name,
// [2]*Property pair, incomer Origin, constraint-kind pair) double-reports one
// clash or collapses two, and single-representative matching with a role swap
// silently DROPS conflicts two ways — through a wide own survivor pairwise-
// compatible with both of two disjoint incomers, and through a survivor swap
// bridging two clashes with no own declaration involved. The rows therefore
// span the two dimensions every scheme must get right: an own re-declaration
// of the conflicted property (present and absent), and cross-clash
// compatibility bridges (own-widening and own-free).
//
// Counts are deterministic for a given schema, and for these pinned shapes
// stable across extends-clause order — with the one declared exception the two
// butterfly rows pin: grouping is first-fit, so three or more mutually tangled
// same-name declarations partition differently under different extends orders,
// and the count moves with the partition (3 here, then 2). Both partitions are
// sound — each side is a chain, so no report ever absorbs a conflict
// incompatible with the one it names — so the variance changes how finely
// conflicts are split, never whether one is reported.
func TestAnnotation_Chain_ConflictDedupByLineage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		schema string
		want   int
	}{
		{
			// Narrowing on the SURVIVING side is absorbed by the survivor swap.
			name: "narrowing survivor",
			schema: `abstract type Wide { x String }
abstract type Narrow extends Wide { x String required }
abstract type Other { x Integer }
abstract type L extends Other { y String }
type T extends Wide, Other, Narrow, L { id String primary }`,
			want: 1,
		},
		{
			// Narrowing on the CONFLICTING side: both members clash with the
			// survivor but are one lineage, so one report — regardless of order.
			name: "narrowing conflict side, order A",
			schema: `abstract type Wide { x String }
abstract type Other { x Integer }
abstract type NarrowOther extends Other { x Integer required }
type T extends Wide, Other, NarrowOther { id String primary }`,
			want: 1,
		},
		{
			name: "narrowing conflict side, order B",
			schema: `abstract type Wide { x String }
abstract type Other { x Integer }
abstract type NarrowOther extends Other { x Integer required }
type T extends Other, NarrowOther, Wide { id String primary }`,
			want: 1,
		},
		{
			// Narrowing on BOTH sides: still one clash between two lineages.
			name: "narrowing both sides",
			schema: `abstract type Wide { x String }
abstract type WideN extends Wide { x String required }
abstract type Other { x Integer }
abstract type OtherN extends Other { x Integer required }
type T extends Wide, WideN, Other, OtherN { id String primary }`,
			want: 1,
		},
		{
			// The same incomer carried forward through both arms of a diamond.
			name: "carried-forward incomer via diamond",
			schema: `abstract type Base { x Integer }
abstract type P1 extends Base { p1 String }
abstract type P2 extends Base { p2 String }
abstract type Wide { x String }
type T extends Wide, P1, P2 { id String primary }`,
			want: 1,
		},
		{
			// Three genuinely independent clashes of DIFFERENT kinds: two problems.
			name: "three distinct kinds",
			schema: `abstract type A { x String }
abstract type B { x Integer }
abstract type D { x Boolean }
type C extends A, B, D { id String primary }`,
			want: 2,
		},
		{
			// Three genuinely independent clashes of the SAME kind (disjoint
			// Enums, none narrowing another): still two problems.
			name: "three independent same-kind",
			schema: `abstract type A { x Enum["a", "b"] }
abstract type B { x Enum["c", "d"] }
abstract type D { x Enum["e", "f"] }
type C extends A, B, D { id String primary }`,
			want: 2,
		},
		{
			// An own declaration that widens two DISJOINT inherited definitions
			// is rejected against each: two clashes, even though the own
			// survivor is pairwise compatible with both incomers. Matching a
			// stored representative in either role assignment bridges these
			// into one report, hiding the second conflict until the first is
			// fixed.
			name: "own widens two disjoint incomers",
			schema: `abstract type M1 { x Integer[0,10] }
abstract type M2 { x Integer[20,30] }
type T extends M1, M2 {
	id String primary
	x Integer[0,100]
}`,
			want: 2,
		},
		{
			// No own declaration anywhere: the survivor narrows mid-walk
			// ([0,50] to [40,50]) and the second clash's members each chain
			// with one side of the FIRST clash, role-crossed ([0,30] under
			// [0,50]; [40,50] under [40,90]). Role-fixed whole-side matching
			// keeps the two clashes distinct; a role-swapped check bridges
			// them into one.
			name: "own-free cross-clash bridge",
			schema: `abstract type MS1 { x Integer[0,50] }
abstract type MP1 { x Integer[40,90] }
abstract type MS2 { x Integer[40,50] }
abstract type MP2 { x Integer[0,30] }
type T extends MS1, MP1, MS2, MP2 { id String primary }`,
			want: 2,
		},
		{
			// The same four mixins in a different extends order. First-fit
			// grouping partitions them differently — MP2 narrows the survivor
			// before MS2 arrives, so MS2 chains with the recorded MP1 and joins
			// that clash instead of opening its own. One report, and the
			// related spans still name all four (pinned by
			// [TestAnnotation_Chain_ConflictNamesEveryDeclarationUnderPermutation]).
			// Pinning BOTH orders is the point: the count is order-dependent
			// and that must be visible here rather than discovered downstream.
			name: "own-free cross-clash bridge, permuted",
			schema: `abstract type MS1 { x Integer[0,50] }
abstract type MP1 { x Integer[40,90] }
abstract type MS2 { x Integer[40,50] }
abstract type MP2 { x Integer[0,30] }
type T extends MS1, MP1, MP2, MS2 { id String primary }`,
			want: 1,
		},
		{
			// The conflicting side accretes only members that chain with EVERY
			// recorded member: String-required and String[9,_] each chain with
			// the plain String but not with each other, so they are two
			// clashes. Matching against a stored representative alone
			// collapses them into one.
			name: "representative is not the side",
			schema: `abstract type A { x Integer }
abstract type B { x String }
abstract type C { x String required }
abstract type D { x String[9,_] }
type T extends A, B, C, D { id String primary }`,
			want: 2,
		},
		{
			// Own widens two incomers that chain with EACH OTHER: one clash —
			// the conflicting side is one chain under the shared own survivor.
			name: "own widens one incomer chain",
			schema: `abstract type M1 { x Integer[0,10] }
abstract type M2 { x Integer[0,50] }
type T extends M1, M2 {
	id String primary
	x Integer[0,100]
}`,
			want: 1,
		},
		{
			// Overlapping-but-incomparable incomers under a wide own survivor:
			// neither contains the other, so two clashes — even though a
			// single narrower own declaration (their meet, [5,10]) would
			// satisfy both. Finding that meet requires seeing both intervals,
			// which is exactly what one bridged report hides.
			name: "own widens overlapping-incomparable incomers",
			schema: `abstract type M1 { x Integer[0,10] }
abstract type M2 { x Integer[5,15] }
type T extends M1, M2 {
	id String primary
	x Integer[0,100]
}`,
			want: 2,
		},
		{
			// Declarations from UNRELATED types chain: the walk unifies by
			// constraint compatibility, not by declaring-type relatedness, and
			// clash identity must match it — B and C share no ancestor yet
			// form one conflicting chain against the Integer survivor.
			name: "unrelated compatible incomers chain",
			schema: `abstract type A { x Integer }
abstract type B { x String }
abstract type C { x String required }
type T extends A, B, C { id String primary }`,
			want: 1,
		},
		{
			// Two unrelated declarations that are structurally EQUAL are one
			// chain: one clash, with both declarations named by its related
			// spans.
			name: "equal unrelated incomers",
			schema: `abstract type M1 { x String }
abstract type M2 { x String }
abstract type W { x Integer }
type T extends W, M1, M2 { id String primary }`,
			want: 1,
		},
		{
			// Both sides are chains: the survivor narrows [0,50] to [0,40]
			// before the first detection, then [70,80] chains with the
			// recorded [60,90]. One clash. Related-span completeness for this
			// shape is pinned by [TestAnnotation_Chain_ConflictRelatedSpans].
			name: "two chains collapse",
			schema: `abstract type M1 { x Integer[0,50] }
abstract type M2 { x Integer[0,40] }
abstract type M3 { x Integer[60,90] }
abstract type M4 { x Integer[70,80] }
type T extends M1, M2, M3, M4 { id String primary }`,
			want: 1,
		},
		{
			// A clash is reported once per affected type: at the type that
			// introduces it and again at each descendant whose own merge
			// re-detects it from the raw ancestors.
			name: "descendant re-reports",
			schema: `abstract type M1 { x String }
abstract type M2 { x Integer }
abstract type C extends M1, M2 { c String }
type D extends C { id String primary }`,
			want: 2,
		},
		{
			// First-fit grouping under one extends order: B2 chains with A1
			// (recorded first) but not with A2, which then opens its own
			// clash, and B1 chains with neither recorded side. Three clashes.
			name: "butterfly order splits",
			schema: `abstract type A1 { x String[0,20] }
abstract type A2 { x String[5,10] }
abstract type B1 { x String required }
abstract type B2 { x String[1,3] required }
type T extends A1, B2, A2, B1 {
	id String primary
	x Integer
}`,
			want: 3,
		},
		{
			// The same four mixins grouped along chain lines by a different
			// order: {A1, A2} and {B1, B2}. Reordering an extends clause can
			// split groups (more reports, each genuine), never merge them.
			name: "butterfly order chains",
			schema: `abstract type A1 { x String[0,20] }
abstract type A2 { x String[5,10] }
abstract type B1 { x String required }
abstract type B2 { x String[1,3] required }
type T extends A1, A2, B1, B2 {
	id String primary
	x Integer
}`,
			want: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := loadStringErr(t, "schema \"main\"\n"+tc.schema)
			if got := codeCounts(res)[diag.E_PROPERTY_CONFLICT]; got != tc.want {
				t.Errorf("E_PROPERTY_CONFLICT count: got %d, want %d\n%v", got, tc.want, res)
			}
		})
	}
}

// EVERY declaration on both sides is named by exactly one related span,
// labeled by side, so collapsing chains into one report never hides a
// declaration a fix might touch.
//
// The surviving side is the whole merged-into chain, not just the survivor at
// the moment of detection. M1's [0,50] is the first survivor and M2's [0,40]
// replaces it before any conflict is detected; both are on the surviving side,
// because narrowing either one is a way to resolve the clash and widening
// either one re-opens it. Recording only the survivor as of a detection omitted
// M1 here, and in the mirror case — where the narrower declaration arrives
// AFTER the conflict — omitted the declaration that actually survives.
func TestAnnotation_Chain_ConflictRelatedSpans(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type M1 { x Integer[0,50] }
abstract type M2 { x Integer[0,40] }
abstract type M3 { x Integer[60,90] }
abstract type M4 { x Integer[70,80] }
type T extends M1, M2, M3, M4 { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_PROPERTY_CONFLICT: 1})

	// One declaration per line: M1 on 2 and M2 on 3 merged into the survivor;
	// M3 on 4 and M4 on 5 are the conflicting chain.
	gotLines := make(map[int]int)
	sideByLine := make(map[int]string)
	for i := range res.Issues() {
		if i.Code() == diag.E_PROPERTY_CONFLICT {
			for _, r := range i.Related() {
				gotLines[r.Span.Start.Line]++
				sideByLine[r.Span.Start.Line] = r.Message
			}
		}
	}
	if wantLines := map[int]int{2: 1, 3: 1, 4: 1, 5: 1}; !maps.Equal(gotLines, wantLines) {
		t.Errorf("related-span lines: got %v, want exactly one at each of %v", gotLines, wantLines)
	}
	wantSides := map[int]string{
		2: "merged into the surviving definition here",
		3: "merged into the surviving definition here",
		4: "conflicting definition declared here",
		5: "conflicting definition declared here",
	}
	if !maps.Equal(sideByLine, wantSides) {
		t.Errorf("related-span side labels by line: got %v, want %v", sideByLine, wantSides)
	}
}

// The schema-level manifestation of constraint-equality reflexivity
// ([TestAliasConstraint_Equal_ReflexiveWhenUndecidable]): a property whose
// datatype cannot be resolved must merge with itself as the closure re-presents
// it, not be reported as conflicting with its own declaration. The Builder
// reaches this shape because it accepts an alias-of-alias datatype the DSL
// grammar cannot express.
func TestAnnotation_Chain_UnresolvableDatatypeDoesNotSelfConflict(t *testing.T) {
	t.Parallel()
	b := schema.NewBuilder().WithName("main")
	b.AddDataType("Money", schema.NewAliasConstraint("ext.Amount", nil))
	b.AddType("A").WithProperty("amount", schema.NewAliasConstraint("Money", nil))
	b.AddType("B").Extends(schema.NewTypeRef("", "A", location.Span{}))
	b.AddType("C").Extends(schema.NewTypeRef("", "B", location.Span{}))
	b.AddType("D").Extends(schema.NewTypeRef("", "A", location.Span{})).
		Extends(schema.NewTypeRef("", "B", location.Span{})).
		Extends(schema.NewTypeRef("", "C", location.Span{})).
		WithPrimaryKey("id", schema.NewStringConstraint())
	_, res := b.Build()

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
abstract type A { --> Rel (one) U }
abstract type B { --> Rel (one) V }
abstract type C { --> Rel (one) V }
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
abstract type A { --> Rel (one) U }
abstract type B { --> Rel (one) V }
abstract type C { --> Rel (one) W }
type T extends A, B, C { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_RELATION_COLLISION: 2})
}

// The third way a declaration reaches the surviving side: absorbed by the
// keep-first branch as structurally equal, never a survivor and never a
// conflict. M2 is byte-identical to M1 and merges silently, but it is a
// declaration a fix must touch — narrowing only M1 leaves M2 behind and the
// next load reports M1-vs-M2. Nothing is superseded here, which is what makes
// this distinct from the survivor-swap cases either side of it.
func TestAnnotation_Chain_ConflictNamesAbsorbedEqualSibling(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type M1 { x String }
abstract type M2 { x String }
abstract type M3 { x Integer }
type T extends M1, M2, M3 { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_PROPERTY_CONFLICT: 1})

	gotLines := make(map[int]string)
	for i := range res.Issues() {
		if i.Code() == diag.E_PROPERTY_CONFLICT {
			for _, r := range i.Related() {
				gotLines[r.Span.Start.Line] = r.Message
			}
		}
	}
	want := map[int]string{
		2: "merged into the surviving definition here",
		3: "merged into the surviving definition here",
		4: "conflicting definition declared here",
	}
	if !maps.Equal(gotLines, want) {
		t.Errorf("related spans: got %v, want %v (M2 on line 3 was absorbed as equal and must still be named)",
			gotLines, want)
	}
}

// The mirror of the case above, and the one a detection-time record cannot get
// right at all: the narrower declaration that becomes the survivor arrives
// AFTER the conflict is detected. A2 is what type C actually inherits, so a
// report that names only A1 and B1 points the user at a declaration that did
// not survive and hides the one that did — they narrow A1, reload, and meet a
// conflict between A1 and A2 the first load never mentioned.
func TestAnnotation_Chain_ConflictNamesLateSurvivor(t *testing.T) {
	t.Parallel()
	res := loadStringErr(t, `schema "main"
abstract type A1 { x String[5,_] }
abstract type B1 { x Integer }
abstract type A2 { x String[10,_] }
type C extends A1, B1, A2 { id String primary }`)
	wantCounts(t, res, map[diag.Code]int{diag.E_PROPERTY_CONFLICT: 1})

	var msg string
	gotLines := make(map[int]string)
	for i := range res.Issues() {
		if i.Code() == diag.E_PROPERTY_CONFLICT {
			msg = i.Message()
			for _, r := range i.Related() {
				gotLines[r.Span.Start.Line] = r.Message
			}
		}
	}
	want := map[int]string{
		2: "merged into the surviving definition here",
		4: "merged into the surviving definition here",
		3: "conflicting definition declared here",
	}
	if !maps.Equal(gotLines, want) {
		t.Errorf("related spans: got %v, want %v (A2 on line 4 is the survivor and must be named)", gotLines, want)
	}
	// The message names what survived — A2 — not the superseded A1.
	if !strings.Contains(msg, "A2") {
		t.Errorf("message should name the surviving declaration A2; got %q", msg)
	}
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

// Grouping is order-dependent, but COMPLETENESS is not: whichever way the
// partition falls, every participating declaration is named. This is what makes
// the order variance cost precision rather than information, and it is the
// property that must hold for collapsing chains into one report to be safe.
func TestAnnotation_Chain_ConflictNamesEveryDeclarationUnderPermutation(t *testing.T) {
	t.Parallel()
	const mixins = `abstract type MS1 { x Integer[0,50] }
abstract type MP1 { x Integer[40,90] }
abstract type MS2 { x Integer[40,50] }
abstract type MP2 { x Integer[0,30] }
`
	for _, order := range []string{"MS1, MP1, MS2, MP2", "MS1, MP1, MP2, MS2"} {
		res := loadStringErr(t, "schema \"main\"\n"+mixins+"type T extends "+order+" { id String primary }")
		named := make(map[int]bool)
		for i := range res.Issues() {
			if i.Code() == diag.E_PROPERTY_CONFLICT {
				for _, r := range i.Related() {
					named[r.Span.Start.Line] = true
				}
			}
		}
		for line := 2; line <= 5; line++ {
			if !named[line] {
				t.Errorf("extends order %q: declaration on line %d is never named (named: %v)", order, line, named)
			}
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

// A related location with no span renders as a bare "note:" line pointing
// nowhere and is dropped outright by the LSP, so the two surfaces disagree
// about what the diagnostic contains. Builder-built schemas carry no spans,
// which is where this arises.
func TestAnnotation_Chain_SpanlessRelatedLocationsAreOmitted(t *testing.T) {
	t.Parallel()
	b := schema.NewBuilder().WithName("main")
	b.AddType("A").WithProperty("x", schema.NewStringConstraint())
	b.AddType("B").WithProperty("x", schema.NewIntegerConstraint())
	b.AddType("C").
		Extends(schema.NewTypeRef("", "A", location.Span{})).
		Extends(schema.NewTypeRef("", "B", location.Span{})).
		WithPrimaryKey("id", schema.NewStringConstraint())
	_, res := b.Build()

	saw := false
	for i := range res.Issues() {
		if i.Code() != diag.E_PROPERTY_CONFLICT {
			continue
		}
		saw = true
		if n := len(i.Related()); n != 0 {
			t.Errorf("span-less related locations must be omitted, got %d", n)
		}
	}
	if !saw {
		t.Fatal("expected the conflict this case exists to describe")
	}
}
