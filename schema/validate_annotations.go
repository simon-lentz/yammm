package schema

import (
	"iter"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// AnnotationPlacement identifies where an annotation may appear.
type AnnotationPlacement uint8

const (
	// PlacementProperty is a property-trailing @name annotation.
	PlacementProperty AnnotationPlacement = iota
	// PlacementType is a type-body @@name member.
	PlacementType
)

// String returns the annotation sigil for the placement ("@" or "@@").
func (p AnnotationPlacement) String() string {
	//exhaustive:enforce
	switch p {
	case PlacementProperty:
		return "@"
	case PlacementType:
		return "@@"
	default:
		return "@?"
	}
}

// placementWord returns the human word for the placement.
func (p AnnotationPlacement) placementWord() string {
	if p == PlacementType {
		return "type"
	}
	return "property"
}

// AnnotationSpec is the read-only description of a registered annotation,
// exposed for editor tooling (hover, completion). It is intentionally minimal —
// no eligibility predicates, no arity ints, and no registration entry point:
// the blessed set is curated in the core.
type AnnotationSpec struct {
	name    string
	place   AnnotationPlacement
	doc     string
	argHint string
}

// Name returns the annotation name (without the @ / @@ sigil).
func (s AnnotationSpec) Name() string { return s.name }

// Placement returns whether the annotation is property-level or type-level.
func (s AnnotationSpec) Placement() AnnotationPlacement { return s.place }

// Documentation returns a one-line description for hover.
func (s AnnotationSpec) Documentation() string { return s.doc }

// ArgHint returns a human-readable argument description for completion
// (e.g. "cosine | euclidean", "property, …"), or "" for a no-argument
// annotation.
func (s AnnotationSpec) ArgHint() string { return s.argHint }

// AnnotationSpecs returns every registered annotation spec, ordered by
// placement then name for deterministic tooling output. Both index placements
// are included as distinct entries.
//
// Name and placement come from the registry KEY, which is what every lookup
// resolves against, so the description editor tooling offers cannot drift from
// the annotation the loader accepts.
func AnnotationSpecs() []AnnotationSpec {
	specs := make([]AnnotationSpec, 0, len(annotationRegistry))
	for key, spec := range annotationRegistry {
		specs = append(specs, AnnotationSpec{
			name:    key.name,
			place:   key.placement,
			doc:     spec.doc,
			argHint: spec.argHint,
		})
	}
	slices.SortFunc(specs, func(a, b AnnotationSpec) int {
		if a.place != b.place {
			return int(a.place) - int(b.place)
		}
		return strings.Compare(a.name, b.name)
	})
	return specs
}

// annotationKey keys the registry by placement and name, so @index (property)
// and @@index (type) are distinct specs with their own arity, argument shape,
// eligibility, and documentation.
type annotationKey struct {
	placement AnnotationPlacement
	name      string
}

// annotationSpec is the internal registry entry: the description plus a validate
// closure that checks one occurrence and stamps validated argument kinds,
// reporting through the completer's collector.
//
// It deliberately does NOT restate the name or placement — those are the map key
// (see [annotationKey]), the only spelling any lookup consults, so a spec cannot
// disagree with the annotation it is registered under.
type annotationSpec struct {
	doc      string
	argHint  string
	validate func(c *completer, t *Type, prop *Property, a *Annotation)
}

// annotationRegistry is the curated built-in set. Adding a kind is one entry;
// there is no external registration API.
var annotationRegistry = map[annotationKey]annotationSpec{
	{PlacementProperty, "index"}: {
		doc:      "Single-property range index on a scalar property.",
		argHint:  "",
		validate: validateIndexProperty,
	},
	{PlacementType, "index"}: {
		doc:      "Composite range index over ordered scalar properties.",
		argHint:  "property, …",
		validate: validateIndexType,
	},
	{PlacementProperty, "vector"}: {
		doc:      "Approximate-nearest-neighbour vector index on a Vector property.",
		argHint:  vectorArgHint,
		validate: validateVectorProperty,
	},
	{PlacementProperty, "fulltext"}: {
		doc:      "Single-property fulltext index on a text property (String, Pattern, or Enum).",
		argHint:  "",
		validate: validateFulltextProperty,
	},
	{PlacementType, "fulltext"}: {
		doc:      "Fulltext index over one or more text properties, scored across fields.",
		argHint:  "property, …",
		validate: validateFulltextType,
	},
	{PlacementProperty, "writeOnce"}: {
		doc:      "Marks a property immutable after node creation (set on create only).",
		argHint:  "",
		validate: validateWriteOnceProperty,
	},
}

// validateAnnotations validates every OWN annotation on every type: known name
// and placement, arity and argument kinds per the registry spec, property-ref
// resolution against the linearized property set, target eligibility, and
// duplicate rules. Inherited annotations were validated when their declaring
// type completed, so only own declarations are checked here (the declaring-site
// pattern shared with primary-key type validation). Own sets are read raw —
// Type.Annotations and each own property's annotations — never the linearized
// AllAnnotations/AllProperties, so an own-body duplicate cannot be masked by
// inheritance dedup. Validated argument kinds are stamped on success, before
// the type is sealed. This is the third caller of hasUnresolvedSupertype.
func (c *completer) validateAnnotations() {
	for _, t := range c.schema.types {
		c.validateTypeAnnotations(t)
		for p := range t.Properties() {
			c.validatePropertyAnnotations(t, p)
		}
	}
}

// validatePropertyAnnotations validates the property-trailing annotations on
// one own property, rejecting a repeated annotation name on the property.
func (c *completer) validatePropertyAnnotations(t *Type, prop *Property) {
	seen := make(map[string]*Annotation)
	for _, a := range prop.annotations {
		// An annotation on a line of its own binds to the property ABOVE it,
		// because the grammar's property-trailing annotation* does not care
		// about line breaks. A reader used to prefix decorators writes it
		// expecting the property BELOW, and nothing else in the load
		// distinguishes the two readings: the schema is accepted and the wrong
		// property carries the marker into the emitted store DDL. Refuse to
		// pick between the readings instead.
		if a.detachedFrom > 0 {
			c.annotationErrorf(a, a.Span(), diag.E_INVALID_ANNOTATION,
				"annotation @%s is written on its own line but attaches to property %q declared on line %d; "+
					"put it on that property's line, or use @@%s for a type-level annotation",
				a.name, prop.Name(), a.detachedFrom, a.name)
			continue
		}

		spec, known := annotationRegistry[annotationKey{PlacementProperty, a.name}]

		if a.argsMalformed {
			// Unreachable: nothing sets argsMalformed. Kept until the field goes.
			if !known {
				c.reportUnknownOrMisplaced(a, PlacementProperty)
			}
			continue
		}
		if first, dup := seen[a.name]; dup {
			c.duplicateAnnotation(a, first,
				"property %q declares annotation @%s more than once", prop.Name(), a.name)
			continue
		}
		seen[a.name] = a

		if !known {
			c.reportUnknownOrMisplaced(a, PlacementProperty)
			continue
		}
		spec.validate(c, t, prop, a)
	}
}

// validateTypeAnnotations validates the type-level @@ members of one type,
// rejecting an exact-duplicate member (name plus ordered argument texts).
func (c *completer) validateTypeAnnotations(t *Type) {
	seen := make(map[string]*Annotation)
	for _, a := range t.annotations {
		spec, known := annotationRegistry[annotationKey{PlacementType, a.name}]

		if a.argsMalformed {
			// Unreachable: nothing sets argsMalformed. Kept until the field goes.
			if !known {
				c.reportUnknownOrMisplaced(a, PlacementType)
			}
			continue
		}
		// identity() is computed once and reused for the insert: it builds a
		// string, and the probe and the insert always agree by construction.
		id := a.identity()
		if first, dup := seen[id]; dup {
			c.duplicateAnnotation(a, first,
				"type %q declares %s more than once", t.Name(), annotationDisplay(a))
			continue
		}
		seen[id] = a

		if !known {
			c.reportUnknownOrMisplaced(a, PlacementType)
			continue
		}
		spec.validate(c, t, nil, a)
	}
}

// annotationErrorf reports a diagnostic against annotation a and records that a
// drew one, so [completer.flushShadowedAnnotations] does not later advise a user
// to re-state an annotation this load rejected.
//
// Every annotation-blaming error must record the annotation, or the shadow
// warning will read it as usable. Two helpers do the recording: this one for a
// single-span error, and [completer.annotationErrorWithRelated] for one that
// also carries a related span (which completer.errorf cannot). Those two are the
// complete set — an annotation-blaming error reported by any other route is a
// bug. ([completer.reportAnnotationDisagreement] deliberately does NOT record:
// the annotation it blames is valid and re-statable, see its doc.)
//
// span is separate from a because a target-eligibility error is anchored at the
// offending property, not at the annotation.
func (c *completer) annotationErrorf(a *Annotation, span location.Span, code diag.Code, format string, args ...any) {
	c.diagnosedAnnotations[a] = true
	c.errorf(span, code, format, args...)
}

// annotationErrorWithRelated reports an annotation-blaming error that carries a
// related span, recording the annotation exactly as [completer.annotationErrorf]
// does. Used where completer.errorf's single span is not enough: a duplicate
// (pointing at the first occurrence) and a redundancy (pointing at the emitter).
func (c *completer) annotationErrorWithRelated(a *Annotation, span location.Span, code diag.Code, related []location.RelatedInfo, format string, args ...any) {
	c.diagnosedAnnotations[a] = true
	c.errorfRelated(span, code, related, format, args...)
}

// reportUnknownOrMisplaced reports a placement mismatch when the name is
// registered at the other placement, and a genuinely unknown annotation
// otherwise. The placement-mismatch path is a registry lookup miss on
// (placement, name), not a special case.
func (c *completer) reportUnknownOrMisplaced(a *Annotation, place AnnotationPlacement) {
	other := PlacementType
	if place == PlacementType {
		other = PlacementProperty
	}
	if _, exists := annotationRegistry[annotationKey{other, a.name}]; exists {
		c.annotationErrorf(a, a.Span(), diag.E_INVALID_ANNOTATION,
			"%s%s is a %s-level annotation; write it as %s%s",
			place, a.name, other.placementWord(), other, a.name)
		return
	}
	c.annotationErrorf(a, a.Span(), diag.E_UNKNOWN_ANNOTATION, "unknown annotation %s%s", place, a.name)
}

// duplicateAnnotation reports E_INVALID_ANNOTATION at the second occurrence with
// a related span at the first, matching the duplicate-property diagnostic shape.
func (c *completer) duplicateAnnotation(dup, first *Annotation, format string, args ...any) {
	c.annotationErrorWithRelated(dup, dup.Span(), diag.E_INVALID_ANNOTATION,
		[]location.RelatedInfo{{Span: first.Span(), Message: "first declared here"}},
		format, args...)
}

// validateIndexProperty checks @index: no arguments, and a scalar-kind target
// that is not a sole primary key (a composite-PK member is allowed — the
// composite backing index does not serve single-property lookups on it).
//
// Redundancy is judged over the types that actually emit the uniqueness
// constraint, not over the type carrying the annotation; see
// [completer.indexOnPrimaryKeyIsRedundant].
func validateIndexProperty(c *completer, t *Type, prop *Property, a *Annotation) {
	if a.argCount() > 0 {
		c.annotationErrorf(a, a.Span(), diag.E_INVALID_ANNOTATION,
			"@index takes no arguments; for a composite index use @@index(...) at the type level")
		return
	}
	if c.annotationTargetTypeUnknown(prop.Constraint()) {
		return // the target's type is already diagnosed; blaming the annotation would bury it
	}
	if !isIndexableScalar(prop.Constraint()) {
		c.annotationErrorf(a, prop.Span(), diag.E_INVALID_ANNOTATION_TARGET,
			"@index requires a scalar property; property %q is %s",
			prop.Name(), constraintKindName(prop.Constraint()))
		return
	}
	if !prop.IsPrimaryKey() {
		return
	}
	if emitter, redundant := c.indexOnPrimaryKeyIsRedundant(t); redundant {
		c.reportIndexRedundancy(a, prop.Span(), t, emitter,
			"@index on primary-key property %q is redundant: %q keys on it alone, so the uniqueness constraint it emits already backs an index",
			prop.Name(), emitter.Name())
	}
}

// reportIndexRedundancy reports the "@index over a sole primary key is
// redundant" error, at span, naming the emitter that made it redundant. When the
// emitter is not the annotated type t — t may be an abstract mixin that emits
// nothing — a related span points at the emitter.
//
// Only @index reaches here: @@index over a sole primary key is accepted per
// docs/SPEC.md (see validateIndexType). The helper stays generic in its message
// so that asymmetry lives at the one call site, not baked into this shape.
func (c *completer) reportIndexRedundancy(a *Annotation, span location.Span, t, emitter *Type, format string, args ...any) {
	var related []location.RelatedInfo
	if emitter != t {
		related = []location.RelatedInfo{{
			Span:    emitter.Span(),
			Message: "sole primary key emitted here",
		}}
	}
	c.annotationErrorWithRelated(a, span, diag.E_INVALID_ANNOTATION_TARGET, related, format, args...)
}

// indexOnPrimaryKeyIsRedundant reports whether EVERY concrete type that emits a
// uniqueness constraint over t's primary key keys on ONE property, and returns
// one such emitter to name in the diagnostic. Only where a constraint covers the
// property alone does its backing index already serve single-property lookups,
// which is what makes @index on a primary-key property redundant.
//
// The emitters are t itself when it is concrete, plus every concrete descendant.
// Counting primary keys on the declaring type alone is unsound: an abstract
// mixin emits no constraints at all, and its key is only settled once a concrete
// type assembles it, so a composite key split across two mixins reads as a sole
// key on each of them and a legitimate @index is rejected at load — while the
// same key declared in one type body is accepted.
//
// EVERY, not ANY, is deliberate. One annotation serves every emitter, so when
// emitters disagree — one keys on the property alone, another keys on it
// compositely and genuinely needs the index — there is no edit that satisfies
// both, and reporting an error the user cannot act on is worse than the
// redundant index the mixed case emits. The rule therefore reports only what can
// be safely removed. This is the one case where a redundant index still ships;
// `yammm neo4j diff` surfaces it against a live database.
//
// Emitters are measured by their EXTRACTED primary keys, via the non-allocating
// [Type.primaryKeyCount], not by counting `primary` flags. But an extracted
// count of 1 is only trustworthy once the key shape is settled: a rejected
// primary ([Type.hasRejectedPrimary]) means a composite the user intended
// currently reads as sole, so the check defers there rather than stacking a
// redundancy error on the E_INVALID_PRIMARY_KEY_TYPE the user has to fix first.
//
// It also stays silent when there is no concrete emitter (nothing is emitted, so
// nothing is redundant) and when any emitter still has an unresolved supertype
// that could contribute a co-key — the deferral its sibling checks perform
// (validateIndexType's deferUnknown, validatePrimaryKeys).
//
// KNOWN LIMIT — descendants declared in OTHER schemas are invisible, because
// completion records subtypes for local types only. An abstract mixin annotated
// in one file and made concrete in another therefore draws nothing, while the
// same model written in a single file is rejected. Closing that needs
// registry-wide descendant visibility at validation time; until then the rule is
// sound for what it can see and silent beyond it, which errs toward accepting.
func (c *completer) indexOnPrimaryKeyIsRedundant(t *Type) (*Type, bool) {
	var first *Type
	for e := range c.concreteEmitters(t) {
		// Defer on any emitter whose key shape is not settled: an unresolved
		// supertype may add a co-key, and a rejected primary means the intended
		// composite currently reads as sole. Either way the sole-vs-composite
		// question has no stable answer yet, and firing would stack a redundancy
		// error on the real one (the unresolved reference, or the bad key type).
		if c.hasUnresolvedSupertype(e) || e.hasRejectedPrimary() {
			return nil, false
		}
		if e.primaryKeyCount() != 1 {
			return nil, false
		}
		if first == nil {
			first = e
		}
	}
	return first, first != nil
}

// concreteEmitters yields the types that emit store-level constraints covering
// t's members: t itself when it is not abstract, plus each of its concrete
// descendants ([Type.SubTypes] is transitively closed). An abstract type
// receives no Neo4j label and emits nothing.
func (c *completer) concreteEmitters(t *Type) iter.Seq[*Type] {
	return func(yield func(*Type) bool) {
		if !t.IsAbstract() && !yield(t) {
			return
		}
		for ref := range t.SubTypes() {
			sub := c.resolveTypeID(ref.ID())
			if sub == nil || sub.IsAbstract() {
				continue
			}
			if !yield(sub) {
				return
			}
		}
	}
}

// validateVectorProperty checks @vector: exactly one similarity keyword and a
// Vector-constrained target.
func validateVectorProperty(c *completer, _ *Type, prop *Property, a *Annotation) {
	if len(a.args) != 1 {
		c.annotationErrorf(a, a.Span(), diag.E_INVALID_ANNOTATION,
			"@vector takes exactly one similarity keyword: %s", strings.Join(vectorSimilarityFunctions, " or "))
		return
	}
	// A quoted argument is a distinct mistake from an unrecognised keyword, and
	// annotationTokenKind exists precisely so the two can be told apart. Only
	// suggest un-quoting when the unquoted value is actually a valid keyword;
	// otherwise the "write @vector(x)" advice would just produce a second error.
	if a.args[0].isLiteral() {
		if isVectorSimilarity(a.args[0].text) {
			c.annotationErrorf(a, a.args[0].span, diag.E_INVALID_ANNOTATION,
				"@vector similarity must be a bare keyword, not a quoted string; write @vector(%s)", a.args[0].text)
		} else {
			c.annotationErrorf(a, a.args[0].span, diag.E_INVALID_ANNOTATION,
				"@vector similarity must be a bare keyword %s, not a literal", strings.Join(vectorSimilarityFunctions, " or "))
		}
		return
	}
	if !isVectorSimilarity(a.args[0].text) {
		c.annotationErrorf(a, a.args[0].span, diag.E_INVALID_ANNOTATION,
			"unknown @vector similarity %q: must be %s", a.args[0].text, strings.Join(vectorSimilarityFunctions, " or "))
		return
	}
	if c.annotationTargetTypeUnknown(prop.Constraint()) {
		return
	}
	if !isVectorConstraint(prop.Constraint()) {
		c.annotationErrorf(a, prop.Span(), diag.E_INVALID_ANNOTATION_TARGET,
			"@vector requires a Vector property; property %q is %s",
			prop.Name(), constraintKindName(prop.Constraint()))
		return
	}
	a.setArgKind(0, ArgKeyword)
}

// validateWriteOnceProperty checks @writeOnce: no arguments, and never on a
// primary-key property (sole or composite member) — every merge match key is
// immutable on match by construction, so the marker would be redundant and
// confusing there.
func validateWriteOnceProperty(c *completer, _ *Type, prop *Property, a *Annotation) {
	if a.argCount() > 0 {
		c.annotationErrorf(a, a.Span(), diag.E_INVALID_ANNOTATION, "@writeOnce takes no arguments")
		return
	}
	if prop.IsPrimaryKey() {
		c.annotationErrorf(a, prop.Span(), diag.E_INVALID_ANNOTATION_TARGET,
			"@writeOnce cannot annotate primary-key property %q; a merge match key is immutable on match by construction",
			prop.Name())
	}
}

// validateFulltextProperty checks @fulltext: no arguments, and a text-kind
// target. Primary-key members are allowed, sole or composite: a uniqueness
// constraint's backing index is a range index and cannot serve fulltext
// queries, so a fulltext index over a primary key is a distinct, legitimate
// object — the @index sole-key redundancy rule does not transfer.
func validateFulltextProperty(c *completer, _ *Type, prop *Property, a *Annotation) {
	if a.argCount() > 0 {
		c.annotationErrorf(a, a.Span(), diag.E_INVALID_ANNOTATION,
			"@fulltext takes no arguments; for a multi-property fulltext index use @@fulltext(...) at the type level")
		return
	}
	if c.annotationTargetTypeUnknown(prop.Constraint()) {
		return // the target's type is already diagnosed; blaming the annotation would bury it
	}
	if !isFulltextEligible(prop.Constraint()) {
		c.annotationErrorf(a, prop.Span(), diag.E_INVALID_ANNOTATION_TARGET,
			"@fulltext requires a text property (String, Pattern, or Enum); property %q is %s",
			prop.Name(), constraintKindName(prop.Constraint()))
	}
}

// validateFulltextType checks @@fulltext: at least one property-reference
// argument, no duplicate references, and each reference resolving to a text
// property of the type (own or inherited; primary-key members are allowed —
// see validateFulltextProperty for why the @index redundancy rule does not
// transfer). Unknown-property reporting defers when the type has an unresolved
// supertype, mirroring validateIndexType.
//
// Literal arguments are rejected, and that rejection is a reserved surface,
// not just hygiene: a trailing string literal is the extension slot for a
// future analyzer option, which stays additive only while v1 refuses it.
func validateFulltextType(c *completer, t *Type, _ *Property, a *Annotation) {
	if a.argCount() == 0 {
		c.annotationErrorf(a, a.Span(), diag.E_INVALID_ANNOTATION, "@@fulltext requires at least one property reference")
		return
	}
	deferUnknown := c.hasUnresolvedSupertype(t)
	seenRef := make(map[string]bool)
	for i := range a.args {
		arg := a.args[i]
		if arg.isLiteral() {
			c.annotationErrorf(a, arg.span, diag.E_INVALID_ANNOTATION,
				"@@fulltext arguments must be property references, not literals")
			continue
		}
		if seenRef[arg.text] {
			c.annotationErrorf(a, arg.span, diag.E_INVALID_ANNOTATION,
				"@@fulltext references property %q more than once", arg.text)
			continue
		}
		seenRef[arg.text] = true

		ref, ok := t.Property(arg.text)
		if !ok {
			if !deferUnknown {
				c.annotationErrorf(a, arg.span, diag.E_UNKNOWN_ANNOTATION_TARGET,
					"@@fulltext references unknown property %q of type %q", arg.text, t.Name())
			}
			continue
		}
		if c.annotationTargetTypeUnknown(ref.Constraint()) {
			continue
		}
		if !isFulltextEligible(ref.Constraint()) {
			c.annotationErrorf(a, arg.span, diag.E_INVALID_ANNOTATION_TARGET,
				"@@fulltext member %q must be a text property (String, Pattern, or Enum); it is %s",
				arg.text, constraintKindName(ref.Constraint()))
			continue
		}
		a.setArgKind(i, ArgPropertyRef)
	}
}

// validateIndexType checks @@index: at least one property-reference argument,
// no duplicate references, and each reference resolving to a scalar property of
// the type (own or inherited; primary-key members are allowed). Unknown-property
// reporting defers when the type has an unresolved supertype, since the
// reference may live on a not-yet-visible cross-schema ancestor.
func validateIndexType(c *completer, t *Type, _ *Property, a *Annotation) {
	if a.argCount() == 0 {
		c.annotationErrorf(a, a.Span(), diag.E_INVALID_ANNOTATION, "@@index requires at least one property reference")
		return
	}
	deferUnknown := c.hasUnresolvedSupertype(t)
	seenRef := make(map[string]bool)
	for i := range a.args {
		arg := a.args[i]
		if arg.isLiteral() {
			c.annotationErrorf(a, arg.span, diag.E_INVALID_ANNOTATION,
				"@@index arguments must be property references, not literals")
			continue
		}
		if seenRef[arg.text] {
			c.annotationErrorf(a, arg.span, diag.E_INVALID_ANNOTATION,
				"@@index references property %q more than once", arg.text)
			continue
		}
		seenRef[arg.text] = true

		ref, ok := t.Property(arg.text)
		if !ok {
			if !deferUnknown {
				c.annotationErrorf(a, arg.span, diag.E_UNKNOWN_ANNOTATION_TARGET,
					"@@index references unknown property %q of type %q", arg.text, t.Name())
			}
			continue
		}
		if c.annotationTargetTypeUnknown(ref.Constraint()) {
			continue
		}
		if !isIndexableScalar(ref.Constraint()) {
			c.annotationErrorf(a, arg.span, diag.E_INVALID_ANNOTATION_TARGET,
				"@@index member %q must be a scalar property; it is %s",
				arg.text, constraintKindName(ref.Constraint()))
			continue
		}
		// No sole-primary-key redundancy check here, unlike @index: docs/SPEC.md
		// lists "primary-key members allowed" for @@index with no caveat, framing
		// the redundancy rule as @index-only. @@index is the explicit-shape
		// construct, so a one-member @@index over a sole key is a deliberate
		// request, not an accident to flag. The two placements differ here by
		// design; resolving the difference the other way is a SPEC change.
		a.setArgKind(i, ArgPropertyRef)
	}
}

// annotationTargetTypeUnknown reports whether an annotation target's type cannot
// be judged at annotation-validation time, in which case the eligibility check
// must defer. Two shapes qualify, and both already carry their own diagnostic:
//
//   - A nil constraint, left by parse-error recovery on a property whose type
//     failed to build (E_SYNTAX or E_INVALID_CONSTRAINT). Checking eligibility
//     here would blame the annotation for the type's failure and describe a
//     property that plainly declares a type as "missing type".
//   - A constraint bottoming out in an unresolved alias (a deferred import, a
//     cyclic local chain), which alias resolution owns.
//
// The second test is shared with the primary-key rule via
// [completer.primaryKeyTypeDeferred]. That predicate is named and documented for
// its primary-key caller, but what it computes — "this constraint's terminal is
// not knowable yet" — is the annotation question too. It is shared rather than
// duplicated so one notion of "unresolved terminal" governs both; anyone
// NARROWING it for a primary-key reason must keep that in mind, or annotation
// targets start drawing eligibility errors stacked on the alias error the user
// actually needs to fix.
func (c *completer) annotationTargetTypeUnknown(constraint Constraint) bool {
	return constraint == nil || c.primaryKeyTypeDeferred(constraint)
}

// isIndexableScalar reports whether a property constraint's underlying kind is a
// scalar type eligible for a range index. Vector, List, and unresolved aliases
// are not indexable scalars. Aliases are resolved to their terminal first.
func isIndexableScalar(constraint Constraint) bool {
	if constraint == nil {
		return false
	}
	//exhaustive:enforce
	switch ResolveAlias(constraint).Kind() {
	case KindString, KindUUID, KindEnum, KindPattern, KindInteger,
		KindFloat, KindBoolean, KindDate, KindTimestamp:
		return true
	case KindVector, KindList, KindAlias:
		return false
	default:
		return false
	}
}

// isFulltextEligible reports whether a property constraint's underlying kind is
// text and therefore eligible for a fulltext index: String, Pattern, and Enum —
// the subset of the range-indexable scalars whose values a fulltext analyzer
// tokenizes. UUID is deliberately excluded: it is stored as a string but is an
// opaque identifier, not tokenized text. Aliases are resolved to their terminal
// first.
func isFulltextEligible(constraint Constraint) bool {
	if constraint == nil {
		return false
	}
	//exhaustive:enforce
	switch ResolveAlias(constraint).Kind() {
	case KindString, KindPattern, KindEnum:
		return true
	case KindUUID, KindInteger, KindFloat, KindBoolean, KindDate,
		KindTimestamp, KindVector, KindList, KindAlias:
		return false
	default:
		return false
	}
}

// constraintKindName returns the resolved kind name for diagnostics.
func constraintKindName(constraint Constraint) string {
	if constraint == nil {
		return "missing type"
	}
	return ResolveAlias(constraint).Kind().String()
}

// vectorSimilarityFunctions is the blessed set of @vector similarity keywords —
// the single source of truth consumed by isVectorSimilarity, the @vector
// diagnostic, the registry arg hint, and (via VectorSimilarityFunctions) the LSP
// completer. Adding or renaming a function is one edit here.
var vectorSimilarityFunctions = []string{"cosine", "euclidean"}

// vectorArgHint is the @vector completion hint, derived from
// vectorSimilarityFunctions so the displayed set cannot drift from the accepted
// one.
var vectorArgHint = strings.Join(vectorSimilarityFunctions, " | ")

// VectorSimilarityFunctions returns the similarity keywords the @vector
// annotation accepts, in canonical order. Editor tooling sources its completion
// set from this so a suggested keyword cannot drift from what the loader accepts.
func VectorSimilarityFunctions() []string {
	return slices.Clone(vectorSimilarityFunctions)
}

// isVectorSimilarity reports whether s is a recognised @vector similarity
// keyword.
func isVectorSimilarity(s string) bool {
	return slices.Contains(vectorSimilarityFunctions, s)
}

// annotationDisplay renders a type-level annotation the way it is written in
// source: "@@name(a, b)" with arguments, "@@name" without. The grammar's
// argument list requires at least one argument, so an unconditional "(%s)" would
// show the user "@@name()" — a spelling that is itself a syntax error.
func annotationDisplay(a *Annotation) string {
	if len(a.args) == 0 {
		return PlacementType.String() + a.name
	}
	texts := make([]string, 0, len(a.args))
	for _, arg := range a.args {
		// displayText re-quotes a string literal (whose stored text is unquoted)
		// but leaves a number, boolean, or regex as its own spelling — echoing a
		// spelling the source actually contained, whatever the literal's kind.
		texts = append(texts, arg.displayText())
	}
	return PlacementType.String() + a.name + "(" + strings.Join(texts, ", ") + ")"
}
