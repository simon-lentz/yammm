package schema

import (
	"fmt"
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
func AnnotationSpecs() []AnnotationSpec {
	specs := make([]AnnotationSpec, 0, len(annotationRegistry))
	for _, spec := range annotationRegistry {
		specs = append(specs, AnnotationSpec{
			name:    spec.name,
			place:   spec.placement,
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

// annotationSpec is the internal registry entry: the public description plus a
// validate closure that checks one occurrence and stamps validated argument
// kinds, reporting through the completer's collector.
type annotationSpec struct {
	name      string
	placement AnnotationPlacement
	doc       string
	argHint   string
	validate  func(c *completer, t *Type, prop *Property, a *Annotation)
}

// annotationRegistry is the curated built-in set. Adding a kind is one entry;
// there is no external registration API.
var annotationRegistry = map[annotationKey]annotationSpec{
	{PlacementProperty, "index"}: {
		name: "index", placement: PlacementProperty,
		doc:      "Single-property range index on a scalar property.",
		argHint:  "",
		validate: validateIndexProperty,
	},
	{PlacementType, "index"}: {
		name: "index", placement: PlacementType,
		doc:      "Composite range index over ordered scalar properties.",
		argHint:  "property, …",
		validate: validateIndexType,
	},
	{PlacementProperty, "vector"}: {
		name: "vector", placement: PlacementProperty,
		doc:      "Approximate-nearest-neighbour vector index on a Vector property.",
		argHint:  vectorArgHint,
		validate: validateVectorProperty,
	},
	{PlacementProperty, "writeOnce"}: {
		name: "writeOnce", placement: PlacementProperty,
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
		if first, dup := seen[a.name]; dup {
			c.duplicateAnnotation(a, first,
				"property %q declares annotation @%s more than once", prop.Name(), a.name)
			continue
		}
		seen[a.name] = a

		spec, ok := annotationRegistry[annotationKey{PlacementProperty, a.name}]
		if !ok {
			c.reportUnknownOrMisplaced(a, PlacementProperty)
			continue
		}
		if a.argsMalformed {
			continue // the syntax error owns the diagnosis; see Annotation.argsMalformed
		}
		spec.validate(c, t, prop, a)
	}
}

// validateTypeAnnotations validates the type-level @@ members of one type,
// rejecting an exact-duplicate member (name plus ordered argument texts).
func (c *completer) validateTypeAnnotations(t *Type) {
	seen := make(map[string]*Annotation)
	for _, a := range t.annotations {
		if first, dup := seen[a.identity()]; dup {
			c.duplicateAnnotation(a, first,
				"type %q declares @@%s(%s) more than once", t.Name(), a.name, argList(a))
			continue
		}
		seen[a.identity()] = a

		spec, ok := annotationRegistry[annotationKey{PlacementType, a.name}]
		if !ok {
			c.reportUnknownOrMisplaced(a, PlacementType)
			continue
		}
		if a.argsMalformed {
			continue // the syntax error owns the diagnosis; see Annotation.argsMalformed
		}
		spec.validate(c, t, nil, a)
	}
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
		c.errorf(a.Span(), diag.E_INVALID_ANNOTATION,
			"%s%s is a %s-level annotation; write it as %s%s",
			place, a.name, other.placementWord(), other, a.name)
		return
	}
	c.errorf(a.Span(), diag.E_UNKNOWN_ANNOTATION, "unknown annotation %s%s", place, a.name)
}

// duplicateAnnotation reports E_INVALID_ANNOTATION at the second occurrence with
// a related span at the first, matching the duplicate-property diagnostic shape.
func (c *completer) duplicateAnnotation(dup, first *Annotation, format string, args ...any) {
	c.collector.Collect(
		diag.NewIssue(diag.Error, diag.E_INVALID_ANNOTATION, fmt.Sprintf(format, args...)).
			WithSpan(dup.Span()).
			WithRelated(location.RelatedInfo{Span: first.Span(), Message: "first declared here"}).Build(),
	)
}

// validateIndexProperty checks @index: no arguments, and a scalar-kind target
// that is not the type's sole primary key (a composite-PK member is allowed —
// the composite backing index does not serve single-property lookups on it).
//
// The sole-primary-key redundancy check defers when the type has an unresolved
// supertype: an unresolved ancestor's members are not merged, so primaryKeyCount
// would undercount a key that is actually composite once the ancestor resolves
// (where @index on one member IS allowed). This mirrors the deferral its sibling
// checks perform — validateIndexType's deferUnknown and validatePrimaryKeys.
func validateIndexProperty(c *completer, t *Type, prop *Property, a *Annotation) {
	if a.argCount() > 0 {
		c.errorf(a.Span(), diag.E_INVALID_ANNOTATION,
			"@index takes no arguments; for a composite index use @@index(...) at the type level")
		return
	}
	if c.annotationTargetTypeUnknown(prop.Constraint()) {
		return // the target's type is already diagnosed; blaming the annotation would bury it
	}
	if !isIndexableScalar(prop.Constraint()) {
		c.errorf(prop.Span(), diag.E_INVALID_ANNOTATION_TARGET,
			"@index requires a scalar property; property %q is %s",
			prop.Name(), constraintKindName(prop.Constraint()))
		return
	}
	if prop.IsPrimaryKey() && !c.hasUnresolvedSupertype(t) && primaryKeyCount(t) == 1 {
		c.errorf(prop.Span(), diag.E_INVALID_ANNOTATION_TARGET,
			"@index on sole primary-key property %q is redundant; its uniqueness constraint already backs an index",
			prop.Name())
	}
}

// validateVectorProperty checks @vector: exactly one similarity keyword and a
// Vector-constrained target.
func validateVectorProperty(c *completer, _ *Type, prop *Property, a *Annotation) {
	if len(a.args) != 1 || a.args[0].isLiteral() || !isVectorSimilarity(a.args[0].text) {
		c.errorf(a.Span(), diag.E_INVALID_ANNOTATION,
			"@vector takes exactly one similarity keyword: %s", strings.Join(vectorSimilarityFunctions, " or "))
		return
	}
	if c.annotationTargetTypeUnknown(prop.Constraint()) {
		return
	}
	if !isVectorConstraint(prop.Constraint()) {
		c.errorf(prop.Span(), diag.E_INVALID_ANNOTATION_TARGET,
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
		c.errorf(a.Span(), diag.E_INVALID_ANNOTATION, "@writeOnce takes no arguments")
		return
	}
	if prop.IsPrimaryKey() {
		c.errorf(prop.Span(), diag.E_INVALID_ANNOTATION_TARGET,
			"@writeOnce cannot annotate primary-key property %q; a merge match key is immutable on match by construction",
			prop.Name())
	}
}

// validateIndexType checks @@index: at least one property-reference argument,
// no duplicate references, and each reference resolving to a scalar property of
// the type (own or inherited; primary-key members are allowed). Unknown-property
// reporting defers when the type has an unresolved supertype, since the
// reference may live on a not-yet-visible cross-schema ancestor.
func validateIndexType(c *completer, t *Type, _ *Property, a *Annotation) {
	if a.argCount() == 0 {
		c.errorf(a.Span(), diag.E_INVALID_ANNOTATION, "@@index requires at least one property reference")
		return
	}
	deferUnknown := c.hasUnresolvedSupertype(t)
	seenRef := make(map[string]bool)
	for i := range a.args {
		arg := a.args[i]
		if arg.isLiteral() {
			c.errorf(arg.span, diag.E_INVALID_ANNOTATION,
				"@@index arguments must be property references, not literals")
			continue
		}
		if seenRef[arg.text] {
			c.errorf(arg.span, diag.E_INVALID_ANNOTATION,
				"@@index references property %q more than once", arg.text)
			continue
		}
		seenRef[arg.text] = true

		ref, ok := t.Property(arg.text)
		if !ok {
			if !deferUnknown {
				c.errorf(arg.span, diag.E_UNKNOWN_ANNOTATION_TARGET,
					"@@index references unknown property %q of type %q", arg.text, t.Name())
			}
			continue
		}
		if c.annotationTargetTypeUnknown(ref.Constraint()) {
			continue
		}
		if !isIndexableScalar(ref.Constraint()) {
			c.errorf(arg.span, diag.E_INVALID_ANNOTATION_TARGET,
				"@@index member %q must be a scalar property; it is %s",
				arg.text, constraintKindName(ref.Constraint()))
			continue
		}
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

// constraintKindName returns the resolved kind name for diagnostics.
func constraintKindName(constraint Constraint) string {
	if constraint == nil {
		return "missing type"
	}
	return ResolveAlias(constraint).Kind().String()
}

// primaryKeyCount counts the type's declared primary-key members over the
// merged property set (matching hasDeclaredPrimary semantics), so an inherited
// co-key is included. Used to distinguish a sole primary key from a composite.
func primaryKeyCount(t *Type) int {
	n := 0
	for p := range t.AllProperties() {
		if p.IsPrimaryKey() {
			n++
		}
	}
	return n
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

// argList joins an annotation's argument texts for a diagnostic message.
func argList(a *Annotation) string {
	texts := make([]string, 0, len(a.args))
	for _, arg := range a.args {
		texts = append(texts, arg.text)
	}
	return strings.Join(texts, ", ")
}
