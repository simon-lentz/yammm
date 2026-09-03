package schema

import (
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/location"
)

// Type represents a type definition in a schema.
// Types are immutable after schema completion.
type Type struct {
	name       string
	schemaName string // owning schema's name for cross-schema display
	sourceID   location.SourceID
	span       location.Span
	nameSpan   location.Span // precise span of just the type name (for go-to-definition)
	doc        string
	isAbstract bool
	isPart     bool

	// Own members (declared in this type body)
	properties   []*Property
	associations []*Relation
	compositions []*Relation
	invariants   []*Invariant
	annotations  []*Annotation // type-level @@name(args) members, in source order

	// Computed at completion (linearized order)
	allProperties   []*Property
	primaryKeys     []*Property
	allAssociations []*Relation
	allCompositions []*Relation
	allInvariants   []*Invariant
	allAnnotations  []*Annotation // own type-level annotations then inherited, deduped keep-first

	// Inheritance
	inherits   []TypeRef         // declared extends clause
	superTypes []ResolvedTypeRef // linearized ancestors
	subTypes   []ResolvedTypeRef // LOCAL subtypes only; completeTypes records a subtype only where its supertype is in the same schema (a cross-schema descendant is invisible here — the known limit concreteEmitters and the @index redundancy rule depend on)

	// O(1) lookup indices
	propByName map[string]*Property
	relByName  map[string]*Relation

	// suppressedAnns records, per property name, the annotation names this
	// type's inheritance DROPPED — a re-declaration that did not re-state what
	// it inherited. Subtypes read it to tell a deliberate drop from a plain
	// absence: absence unions, a drop that another supertype contradicts is an
	// error. Nil for the overwhelming majority of types, which drop nothing.
	// See [completer.inheritedAnnotations].
	suppressedAnns map[string]map[string]bool

	// Cached canonical property map (lowercase -> canonical name)
	canonicalMap map[string]string

	// Immutability enforcement
	sealed bool // true after completion; prevents further mutation
}

// newType creates a new Type. This is primarily for internal use;
// types are typically created during schema parsing and completion.
func newType(
	name string,
	sourceID location.SourceID,
	span location.Span,
	doc string,
	isAbstract, isPart bool,
) *Type {
	return &Type{
		name:       name,
		sourceID:   sourceID,
		span:       span,
		doc:        doc,
		isAbstract: isAbstract,
		isPart:     isPart,
		propByName: make(map[string]*Property),
		relByName:  make(map[string]*Relation),
	}
}

// Name returns the type name.
func (t *Type) Name() string {
	return t.name
}

// ID returns the canonical TypeID for this type.
func (t *Type) ID() TypeID {
	return TypeID{schemaPath: t.sourceID, name: t.name}
}

// SourceID returns the source identity of the schema containing this type.
func (t *Type) SourceID() location.SourceID {
	return t.sourceID
}

// SchemaName returns the name of the schema containing this type.
// Used for cross-schema display when no import alias exists.
func (t *Type) SchemaName() string {
	return t.schemaName
}

// Span returns the source location of this type declaration.
func (t *Type) Span() location.Span {
	return t.span
}

// NameSpan returns the precise source location of just the type name.
// This is more accurate than Span() for go-to-definition operations.
// Returns a zero span if not set (e.g., for programmatically-created types).
func (t *Type) NameSpan() location.Span {
	return t.nameSpan
}

// setNameSpan sets the precise span of the type name.
// This must be called before the type is sealed.
func (t *Type) setNameSpan(span location.Span) {
	if t.sealed {
		panic("cannot modify sealed type")
	}
	t.nameSpan = span
}

// Documentation returns the documentation comment, if any.
func (t *Type) Documentation() string {
	return t.doc
}

// IsAbstract reports whether this type is abstract.
// Abstract types cannot be directly instantiated.
func (t *Type) IsAbstract() bool {
	return t.isAbstract
}

// IsPart reports whether this type is a part type.
// Part types can only be instantiated as compositions.
func (t *Type) IsPart() bool {
	return t.isPart
}

// Property returns the property with the given name (own or inherited), if it exists.
// Uses O(1) lookup.
func (t *Type) Property(name string) (*Property, bool) {
	p, ok := t.propByName[name]
	return p, ok
}

// Properties returns an iterator over properties declared in this type body.
// Does NOT include inherited properties; use AllProperties for that.
func (t *Type) Properties() iter.Seq[*Property] {
	return func(yield func(*Property) bool) {
		for _, p := range t.properties {
			if !yield(p) {
				return
			}
		}
	}
}

// PropertiesSlice returns a defensive copy of properties declared in this type.
func (t *Type) PropertiesSlice() []*Property {
	return slices.Clone(t.properties)
}

// AllProperties returns an iterator over all properties (own and inherited).
// Properties are returned in linearized order: own first, then inherited.
func (t *Type) AllProperties() iter.Seq[*Property] {
	return func(yield func(*Property) bool) {
		for _, p := range t.allProperties {
			if !yield(p) {
				return
			}
		}
	}
}

// AllPropertiesSlice returns a defensive copy of all properties (own and inherited).
func (t *Type) AllPropertiesSlice() []*Property {
	return slices.Clone(t.allProperties)
}

// PrimaryKeys returns an iterator over primary key properties.
func (t *Type) PrimaryKeys() iter.Seq[*Property] {
	return func(yield func(*Property) bool) {
		for _, p := range t.primaryKeys {
			if !yield(p) {
				return
			}
		}
	}
}

// PrimaryKeysSlice returns a defensive copy of primary key properties.
func (t *Type) PrimaryKeysSlice() []*Property {
	return slices.Clone(t.primaryKeys)
}

// primaryKeyCount returns the number of EXTRACTED primary keys without copying
// the slice. Unexported and non-allocating, for the completer's per-type checks
// on the load path; external callers use [Type.PrimaryKeysSlice].
func (t *Type) primaryKeyCount() int {
	return len(t.primaryKeys)
}

// hasRejectedPrimary reports whether the type declares a `primary` property that
// key extraction did NOT accept — a primary whose datatype is ineligible
// (E_INVALID_PRIMARY_KEY_TYPE), so the flag is set but the property is absent
// from primaryKeys. Where this holds the key SHAPE is not yet settled: the user
// intended a composite that currently reads as sole, so rules that turn on
// soleness must defer rather than fire on the transient count.
func (t *Type) hasRejectedPrimary() bool {
	declared := 0
	for _, p := range t.allProperties {
		if p.IsPrimaryKey() {
			declared++
		}
	}
	return declared > len(t.primaryKeys)
}

// HasPrimaryKey reports whether this type has at least one primary key property.
func (t *Type) HasPrimaryKey() bool {
	return len(t.primaryKeys) > 0
}

// Relation returns the relation with the given name (own or inherited), if it exists.
// Uses O(1) lookup.
func (t *Type) Relation(name string) (*Relation, bool) {
	r, ok := t.relByName[name]
	return r, ok
}

// Associations returns an iterator over associations declared in this type body.
func (t *Type) Associations() iter.Seq[*Relation] {
	return func(yield func(*Relation) bool) {
		for _, r := range t.associations {
			if !yield(r) {
				return
			}
		}
	}
}

// AssociationsSlice returns a defensive copy of associations declared in this type.
func (t *Type) AssociationsSlice() []*Relation {
	return slices.Clone(t.associations)
}

// AllAssociations returns an iterator over all associations (own and inherited).
func (t *Type) AllAssociations() iter.Seq[*Relation] {
	return func(yield func(*Relation) bool) {
		for _, r := range t.allAssociations {
			if !yield(r) {
				return
			}
		}
	}
}

// AllAssociationsSlice returns a defensive copy of all associations.
func (t *Type) AllAssociationsSlice() []*Relation {
	return slices.Clone(t.allAssociations)
}

// Compositions returns an iterator over compositions declared in this type body.
func (t *Type) Compositions() iter.Seq[*Relation] {
	return func(yield func(*Relation) bool) {
		for _, r := range t.compositions {
			if !yield(r) {
				return
			}
		}
	}
}

// CompositionsSlice returns a defensive copy of compositions declared in this type.
func (t *Type) CompositionsSlice() []*Relation {
	return slices.Clone(t.compositions)
}

// AllCompositions returns an iterator over all compositions (own and inherited).
func (t *Type) AllCompositions() iter.Seq[*Relation] {
	return func(yield func(*Relation) bool) {
		for _, r := range t.allCompositions {
			if !yield(r) {
				return
			}
		}
	}
}

// AllCompositionsSlice returns a defensive copy of all compositions.
func (t *Type) AllCompositionsSlice() []*Relation {
	return slices.Clone(t.allCompositions)
}

// Invariants returns an iterator over invariants declared on this type.
func (t *Type) Invariants() iter.Seq[*Invariant] {
	return func(yield func(*Invariant) bool) {
		for _, i := range t.invariants {
			if !yield(i) {
				return
			}
		}
	}
}

// InvariantsSlice returns a defensive copy of invariants.
func (t *Type) InvariantsSlice() []*Invariant {
	return slices.Clone(t.invariants)
}

// AllInvariants returns an iterator over all invariants (own and inherited).
func (t *Type) AllInvariants() iter.Seq[*Invariant] {
	return func(yield func(*Invariant) bool) {
		for _, i := range t.allInvariants {
			if !yield(i) {
				return
			}
		}
	}
}

// AllInvariantsSlice returns a defensive copy of all invariants (own and inherited).
func (t *Type) AllInvariantsSlice() []*Invariant {
	return slices.Clone(t.allInvariants)
}

// AnnotationsSlice returns a defensive copy of the type-level annotations
// declared in this type body (@@name(args) members). Does NOT include
// inherited annotations; use [Type.AllAnnotations] for that.
func (t *Type) AnnotationsSlice() []*Annotation {
	return slices.Clone(t.annotations)
}

// AllAnnotations returns an iterator over all type-level annotations (own and
// inherited) in linearized order: own first, then inherited, with exact
// duplicates deduplicated keep-first.
func (t *Type) AllAnnotations() iter.Seq[*Annotation] {
	return func(yield func(*Annotation) bool) {
		for _, a := range t.allAnnotations {
			if !yield(a) {
				return
			}
		}
	}
}

// AllAnnotationsSlice returns a defensive copy of all type-level annotations
// (own and inherited).
func (t *Type) AllAnnotationsSlice() []*Annotation {
	return slices.Clone(t.allAnnotations)
}

// Inherits returns an iterator over the declared extends clause (syntactic refs).
func (t *Type) Inherits() iter.Seq[TypeRef] {
	return func(yield func(TypeRef) bool) {
		for _, ref := range t.inherits {
			if !yield(ref) {
				return
			}
		}
	}
}

// InheritsSlice returns a defensive copy of the extends clause.
func (t *Type) InheritsSlice() []TypeRef {
	return slices.Clone(t.inherits)
}

// SuperTypes returns an iterator over linearized ancestors.
// Order is DFS left-to-right with keep-first deduplication.
func (t *Type) SuperTypes() iter.Seq[ResolvedTypeRef] {
	return func(yield func(ResolvedTypeRef) bool) {
		for _, ref := range t.superTypes {
			if !yield(ref) {
				return
			}
		}
	}
}

// SuperTypesSlice returns a defensive copy of linearized ancestors.
func (t *Type) SuperTypesSlice() []ResolvedTypeRef {
	return slices.Clone(t.superTypes)
}

// SubTypes returns an iterator over this type's known subtypes, which are
// LOCAL to the declaring schema. Completion records a subtype only when the
// supertype lives in the schema being completed, so a concrete descendant
// declared in an importing schema does not appear here. Consumers that emit
// per-label output for every concrete descendant of an abstract type must walk
// the closure ([Schema.Closure]) rather than rely on this iterator, or they
// will silently skip cross-schema descendants.
func (t *Type) SubTypes() iter.Seq[ResolvedTypeRef] {
	return func(yield func(ResolvedTypeRef) bool) {
		for _, ref := range t.subTypes {
			if !yield(ref) {
				return
			}
		}
	}
}

// SubTypesSlice returns a defensive copy of known subtypes. As with
// [Type.SubTypes], these are LOCAL to the declaring schema — see that method
// for why a cross-schema descendant is absent and what to walk instead.
func (t *Type) SubTypesSlice() []ResolvedTypeRef {
	return slices.Clone(t.subTypes)
}

// IsSubTypeOf reports whether this type is a subtype of the given type.
// Uses TypeID for cross-schema comparison.
func (t *Type) IsSubTypeOf(id TypeID) bool {
	for _, st := range t.superTypes {
		if st.id == id {
			return true
		}
	}
	return false
}

// CanonicalPropertyMap returns a map from lowercase property names to their
// canonical schema names. Used for case-insensitive property matching.
//
// The returned map is a defensive copy; callers may modify it freely.
// This method is O(n) on first call (builds map) and O(n) on subsequent calls
// (defensive copy). For single lookups, consider iterating AllProperties() directly.
func (t *Type) CanonicalPropertyMap() map[string]string {
	if t.canonicalMap != nil {
		return maps.Clone(t.canonicalMap) // Defensive copy to preserve immutability
	}
	// Compute on demand for unsealed types (during completion).
	// Intentionally not cached: allProperties may change until Seal() is called.
	result := make(map[string]string, len(t.allProperties))
	for _, p := range t.allProperties {
		lower := strings.ToLower(p.name)
		result[lower] = p.name
	}
	return result
}

// CanonicalPropertyName maps a lowercased property name to the type's declared
// spelling. It answers the one question every [Type.CanonicalPropertyMap]
// caller asks without handing out a copy of the whole map.
func (t *Type) CanonicalPropertyName(lower string) (string, bool) {
	if t.canonicalMap != nil {
		name, ok := t.canonicalMap[lower]
		return name, ok
	}
	for _, p := range t.allProperties {
		if strings.ToLower(p.name) == lower {
			return p.name, true
		}
	}
	return "", false
}

// --- Internal setters used during completion ---

// seal marks the type as immutable.
// Called by the completer after type completion finishes.
func (t *Type) seal() {
	// Precompute cached maps for thread-safe access after sealing
	t.canonicalMap = make(map[string]string, len(t.allProperties))
	for _, p := range t.allProperties {
		lower := strings.ToLower(p.name)
		t.canonicalMap[lower] = p.name
	}
	t.sealed = true
}

// isSealed reports whether the type has been sealed.
func (t *Type) isSealed() bool {
	return t.sealed
}

// setSchemaName sets the owning schema's name (called during completion).
func (t *Type) setSchemaName(name string) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.schemaName = name
}

// setProperties sets the declared properties (called during completion).
func (t *Type) setProperties(properties []*Property) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.properties = properties
	for _, p := range properties {
		t.propByName[p.name] = p
	}
}

// setAssociations sets the declared associations (called during completion).
func (t *Type) setAssociations(associations []*Relation) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.associations = associations
	for _, r := range associations {
		t.relByName[r.name] = r
	}
}

// setCompositions sets the declared compositions (called during completion).
func (t *Type) setCompositions(compositions []*Relation) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.compositions = compositions
	for _, r := range compositions {
		t.relByName[r.name] = r
	}
}

// setInvariants sets the invariants (called during completion).
func (t *Type) setInvariants(invariants []*Invariant) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.invariants = invariants
}

// setAllInvariants sets all invariants including inherited (called during completion).
func (t *Type) setAllInvariants(all []*Invariant) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.allInvariants = all
}

// setAnnotations sets the type-level annotations declared in this body
// (called during completion).
func (t *Type) setAnnotations(annotations []*Annotation) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.annotations = annotations
}

// setAllAnnotations sets all type-level annotations including inherited
// (called during completion).
func (t *Type) setAllAnnotations(all []*Annotation) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.allAnnotations = all
}

// setInherits sets the declared extends clause (called during completion).
func (t *Type) setInherits(inherits []TypeRef) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.inherits = inherits
}

// setSuppressedAnnotations records the annotations this type's inheritance
// dropped, per property (called during completion). A nil map is the normal
// case and is stored as-is.
func (t *Type) setSuppressedAnnotations(m map[string]map[string]bool) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.suppressedAnns = m
}

// suppressedAnnotations returns the set of annotation names this type's
// inheritance dropped for the named property, or nil. Reading a nil map is
// safe, so callers need no guard.
func (t *Type) suppressedAnnotations(prop string) map[string]bool {
	return t.suppressedAnns[prop]
}

// setAllProperties sets all properties including inherited (called during completion).
func (t *Type) setAllProperties(all []*Property) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.allProperties = all
	// Update index with all properties
	for _, p := range all {
		t.propByName[p.name] = p
	}
}

// setPrimaryKeys sets the primary key properties (called during completion).
func (t *Type) setPrimaryKeys(pks []*Property) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.primaryKeys = pks
}

// setAllAssociations sets all associations including inherited (called during completion).
func (t *Type) setAllAssociations(all []*Relation) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.allAssociations = all
	for _, r := range all {
		t.relByName[r.name] = r
	}
}

// setAllCompositions sets all compositions including inherited (called during completion).
func (t *Type) setAllCompositions(all []*Relation) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.allCompositions = all
	for _, r := range all {
		t.relByName[r.name] = r
	}
}

// setSuperTypes sets the linearized ancestors (called during completion).
func (t *Type) setSuperTypes(supers []ResolvedTypeRef) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.superTypes = supers
}

// setSubTypes sets the known subtypes (called during completion).
func (t *Type) setSubTypes(subs []ResolvedTypeRef) {
	if t.sealed {
		panic("schema: cannot mutate sealed type")
	}
	t.subTypes = subs
}
