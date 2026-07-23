package schema

import (
	"iter"
	"slices"

	"github.com/simon-lentz/yammm/location"
)

// DeclaringScopeKind identifies where a property was declared.
type DeclaringScopeKind uint8

const (
	// ScopeType indicates the property was declared in a type body.
	ScopeType DeclaringScopeKind = iota
	// ScopeRelation indicates the property was declared on a relation edge.
	ScopeRelation
)

// String returns a human-readable name for the scope kind.
func (k DeclaringScopeKind) String() string {
	switch k {
	case ScopeType:
		return "type"
	case ScopeRelation:
		return "relation"
	default:
		return "unknown"
	}
}

// DeclaringScope identifies where a property was declared.
// For inherited properties, this identifies the original declaring ancestor.
type DeclaringScope struct {
	kind    DeclaringScopeKind
	typeRef TypeRef // for ScopeType: preserves qualification
	relName string  // for ScopeRelation: relation name
}

// TypeScope creates a DeclaringScope for a property declared in a type body.
func TypeScope(typeRef TypeRef) DeclaringScope {
	return DeclaringScope{kind: ScopeType, typeRef: typeRef}
}

// RelationScope creates a DeclaringScope for an edge property on a relation.
func RelationScope(relationName string) DeclaringScope {
	return DeclaringScope{kind: ScopeRelation, relName: relationName}
}

// Kind returns the scope kind.
func (s DeclaringScope) Kind() DeclaringScopeKind {
	return s.kind
}

// IsType reports whether this is a type-declared property.
func (s DeclaringScope) IsType() bool {
	return s.kind == ScopeType
}

// IsRelation reports whether this is a relation edge property.
func (s DeclaringScope) IsRelation() bool {
	return s.kind == ScopeRelation
}

// TypeRef returns the type reference for ScopeType declarations.
// Returns zero value for ScopeRelation.
func (s DeclaringScope) TypeRef() TypeRef {
	return s.typeRef
}

// RelationName returns the relation name for ScopeRelation declarations.
// Returns empty string for ScopeType.
func (s DeclaringScope) RelationName() string {
	return s.relName
}

// TypeName returns the type name for ScopeType declarations.
// Panics if Kind() != ScopeType.
func (s DeclaringScope) TypeName() string {
	if s.kind != ScopeType {
		panic("DeclaringScope.TypeName called on non-type scope")
	}
	return s.typeRef.Name()
}

// String returns a human-readable representation.
// Returns "unknown" for zero-value DeclaringScope.
func (s DeclaringScope) String() string {
	switch s.kind {
	case ScopeType:
		return s.typeRef.String()
	case ScopeRelation:
		return s.relName
	default:
		return "unknown"
	}
}

// Property represents a property definition on a type.
// Properties are immutable after schema completion.
type Property struct {
	name         string
	span         location.Span
	doc          string
	constraint   Constraint
	dataTypeRef  DataTypeRef // syntactic reference with span (for alias constraints)
	optional     bool
	isPrimaryKey bool
	scope        DeclaringScope
	annotations  []*Annotation // property-trailing @name(args) decorators, in source order

	// origin points at the declared Property this one was synthesized from
	// during inheritance merging, and is nil on a declared property. See
	// [Property.Origin].
	origin *Property
}

// newProperty creates a new Property.
//
// This is a low-level API primarily for:
//   - Internal use during schema parsing and completion
//   - Advanced use cases like building schemas programmatically via Builder
//
// Most users should load schemas from .yammm files using the load package.
//
// The dataTypeRef parameter captures the syntactic reference (with span) when the
// constraint is an alias to a named DataType. Pass zero-value DataTypeRef for
// built-in constraints. This enables LSP navigation to DataType definitions.
func newProperty(
	name string,
	span location.Span,
	doc string,
	constraint Constraint,
	dataTypeRef DataTypeRef,
	optional bool,
	isPrimaryKey bool,
	scope DeclaringScope,
	annotations []*Annotation,
) *Property {
	return &Property{
		name:         name,
		span:         span,
		doc:          doc,
		constraint:   constraint,
		dataTypeRef:  dataTypeRef,
		optional:     optional,
		isPrimaryKey: isPrimaryKey,
		scope:        scope,
		annotations:  annotations,
	}
}

// Name returns the property name.
func (p *Property) Name() string {
	return p.name
}

// Span returns the source location of this property declaration.
func (p *Property) Span() location.Span {
	return p.span
}

// Documentation returns the documentation comment, if any.
func (p *Property) Documentation() string {
	return p.doc
}

// Constraint returns the typed constraint for this property.
func (p *Property) Constraint() Constraint {
	return p.constraint
}

// DataTypeRef returns the syntactic reference to a DataType, if any.
// Returns a zero-value DataTypeRef for built-in constraints (String, Integer, etc.).
// Use IsZero() to check if this property references a named DataType.
func (p *Property) DataTypeRef() DataTypeRef {
	return p.dataTypeRef
}

// setConstraint sets the constraint (called during alias resolution).
// Internal use only; called during schema completion.
func (p *Property) setConstraint(c Constraint) {
	p.constraint = c
}

// IsOptional reports whether this property is optional.
func (p *Property) IsOptional() bool {
	return p.optional
}

// IsRequired reports whether this property is required (not optional).
func (p *Property) IsRequired() bool {
	return !p.optional
}

// IsPrimaryKey reports whether this property is a primary key.
// Primary key properties are implicitly required.
func (p *Property) IsPrimaryKey() bool {
	return p.isPrimaryKey
}

// DeclaringScope returns where this property was declared.
// For inherited properties, returns the original declaring ancestor.
func (p *Property) DeclaringScope() DeclaringScope {
	return p.scope
}

// Annotations returns an iterator over this property's annotations in source
// order (the @name / @name(args) decorators trailing the property). Duplicate
// names are rejected during completion, so each name appears at most once.
func (p *Property) Annotations() iter.Seq[*Annotation] {
	return func(yield func(*Annotation) bool) {
		for _, a := range p.annotations {
			if !yield(a) {
				return
			}
		}
	}
}

// AnnotationsSlice returns a defensive copy of this property's annotations.
func (p *Property) AnnotationsSlice() []*Annotation {
	return slices.Clone(p.annotations)
}

// Annotation returns the annotation with the given name, if present. The result
// is unambiguous because duplicate annotation names on one property are a load
// error.
func (p *Property) Annotation(name string) (*Annotation, bool) {
	for _, a := range p.annotations {
		if a.name == name {
			return a, true
		}
	}
	return nil, false
}

// Origin returns the property as declared: p itself for a property a type
// declares, and the declaring ancestor's property for one synthesized while
// merging a property inherited from several ancestors (a merged view carries the
// union of those ancestors' annotations, which no single declared property
// holds). Never nil.
//
// Use it as the key of any identity-keyed map built from declared properties
// ([Type.PropertiesSlice], [Relation.PropertiesSlice]) and read back while
// iterating a merged view ([Type.AllPropertiesSlice]): the two views are not
// pointer-identical for a synthesized property, and Origin bridges them.
// Everything Origin discards is annotation state; name, constraint, datatype
// reference, optionality, primary-key status, span, and declaring scope are all
// preserved on the synthesized copy.
func (p *Property) Origin() *Property {
	if p.origin != nil {
		return p.origin
	}
	return p
}

// cloneWithAnnotations returns a shallow copy of p carrying the given annotation
// set in place of p's own. Used during linearization to give a type a merged
// view of a property inherited from multiple ancestors without mutating the
// shared ancestor *Property; every other field is preserved from p.
//
// The copy records p's declared origin so identity-keyed consumers can recover
// it (see [Property.Origin]). Origins are flattened, so a copy of a copy still
// points at the declared property rather than at an intermediate merge result.
func (p *Property) cloneWithAnnotations(anns []*Annotation) *Property {
	clone := *p
	clone.annotations = anns
	clone.origin = p.Origin()
	return &clone
}

// CanNarrowFrom reports whether this (child) property is a valid narrowing of
// the parent property. A child narrows its parent when:
//   - Names match (case-sensitive)
//   - PK status is identical (structural, cannot change)
//   - Optionality narrows: optional->required is allowed, required->optional is NOT
//   - Constraint narrows: child's valid set is a subset of parent's (via NarrowsTo)
//
// Both nil returns true (nil == nil). One nil returns false.
func (p *Property) CanNarrowFrom(parent *Property) bool {
	if p == nil || parent == nil {
		return p == parent
	}
	if p.name != parent.name {
		return false
	}
	if p.isPrimaryKey != parent.isPrimaryKey {
		return false
	}
	// Cannot widen: required -> optional
	if !parent.optional && p.optional {
		return false
	}
	// Both nil constraints: OK
	if p.constraint == nil && parent.constraint == nil {
		return true
	}
	// One nil, one non-nil: not compatible
	if p.constraint == nil || parent.constraint == nil {
		return false
	}
	return parent.constraint.NarrowsTo(p.constraint)
}

// Equal reports whether two properties are structurally equal.
// Compares: name (case-sensitive), optionality, PK status, constraint.
// NOT compared: span, docs, scope, annotations. Scope is excluded because
// properties inherited from different ancestors should be considered equal for
// deduplication purposes during type completion—two ancestors may define
// identical properties, and we want to merge them rather than flag a conflict.
// Annotations are excluded so a child re-declaration that drops an inherited
// annotation is still Equal (and the drop is surfaced by W_ANNOTATION_SHADOWED
// during linearization, not treated as a structural conflict).
func (p *Property) Equal(other *Property) bool {
	if p == nil || other == nil {
		return p == other
	}
	if p.name != other.name {
		return false
	}
	if p.optional != other.optional {
		return false
	}
	if p.isPrimaryKey != other.isPrimaryKey {
		return false
	}
	if p.constraint == nil {
		return other.constraint == nil
	}
	return p.constraint.Equal(other.constraint)
}
