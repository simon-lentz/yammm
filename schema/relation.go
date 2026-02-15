package schema

import (
	"iter"
	"slices"

	"github.com/simon-lentz/yammm/location"
)

// RelationKind identifies the kind of relationship.
type RelationKind uint8

const (
	// RelationAssociation represents an association between types.
	// Associations may have edge properties.
	RelationAssociation RelationKind = iota
	// RelationComposition represents a composition (part-of) relationship.
	// Compositions do not have edge properties.
	RelationComposition
)

// String returns the name of the relation kind.
func (k RelationKind) String() string {
	switch k {
	case RelationAssociation:
		return "association"
	case RelationComposition:
		return "composition"
	default:
		return "unknown"
	}
}

// Relation represents a relationship between types.
// Relations are immutable after schema completion.
type Relation struct {
	kind            RelationKind
	name            string        // DSL name (e.g., "OWNER")
	fieldName       string        // lower_snake(name), cached at completion
	target          TypeRef       // syntactic reference
	targetID        TypeID        // resolved identity
	span            location.Span // source location
	doc             string        // documentation comment
	optional        bool          // forward multiplicity: optional?
	many            bool          // forward multiplicity: many?
	backref         string        // reverse relationship name
	reverseOptional bool          // reverse multiplicity: optional?
	reverseMany     bool          // reverse multiplicity: many?
	owner           string        // declaring type name
	properties      []*Property   // edge properties (associations only)
	sealed          bool          // true after completion; prevents further mutation
}

// NewRelation creates a new Relation. This is primarily for internal use;
// relations are typically created during schema parsing and completion.
func NewRelation(
	kind RelationKind,
	name string,
	fieldName string,
	target TypeRef,
	targetID TypeID,
	span location.Span,
	doc string,
	optional, many bool,
	backref string,
	reverseOptional, reverseMany bool,
	owner string,
	properties []*Property,
) *Relation {
	return &Relation{
		kind:            kind,
		name:            name,
		fieldName:       fieldName,
		target:          target,
		targetID:        targetID,
		span:            span,
		doc:             doc,
		optional:        optional,
		many:            many,
		backref:         backref,
		reverseOptional: reverseOptional,
		reverseMany:     reverseMany,
		owner:           owner,
		properties:      properties,
	}
}

// Kind returns the relation kind (association or composition).
func (r *Relation) Kind() RelationKind {
	return r.kind
}

// Name returns the DSL name of the relation (e.g., "OWNER").
func (r *Relation) Name() string {
	return r.name
}

// FieldName returns the normalized field name used in instance data.
// This is lower_snake(Name()), e.g., "WORKS_AT" → "works_at".
// Cached at schema completion for performance.
func (r *Relation) FieldName() string {
	return r.fieldName
}

// Target returns the syntactic type reference for diagnostics.
func (r *Relation) Target() TypeRef {
	return r.target
}

// TargetID returns the resolved canonical type identity.
func (r *Relation) TargetID() TypeID {
	return r.targetID
}

// SetTargetID sets the resolved canonical type identity.
// Internal use only; called during schema completion.
// Panics if called after Seal().
func (r *Relation) SetTargetID(id TypeID) {
	if r.sealed {
		panic("relation: cannot mutate sealed relation")
	}
	r.targetID = id
}

// Seal prevents further mutation of the relation.
// Called during schema completion after target resolution.
func (r *Relation) Seal() {
	r.sealed = true
}

// IsSealed reports whether the relation has been sealed.
func (r *Relation) IsSealed() bool {
	return r.sealed
}

// Span returns the source location of this relation declaration.
func (r *Relation) Span() location.Span {
	return r.span
}

// Documentation returns the documentation comment, if any.
func (r *Relation) Documentation() string {
	return r.doc
}

// IsOptional reports whether the forward direction is optional.
func (r *Relation) IsOptional() bool {
	return r.optional
}

// IsMany reports whether the forward direction allows many targets.
func (r *Relation) IsMany() bool {
	return r.many
}

// Backref returns the reverse relationship name, if any.
func (r *Relation) Backref() string {
	return r.backref
}

// ReverseMultiplicity returns the reverse direction multiplicity.
// Returns (optional, many).
func (r *Relation) ReverseMultiplicity() (optional, many bool) {
	return r.reverseOptional, r.reverseMany
}

// Owner returns the name of the type that declares this relation.
func (r *Relation) Owner() string {
	return r.owner
}

// Properties returns an iterator over edge properties.
// For compositions, this returns an empty iterator.
func (r *Relation) Properties() iter.Seq[*Property] {
	return func(yield func(*Property) bool) {
		for _, p := range r.properties {
			if !yield(p) {
				return
			}
		}
	}
}

// PropertiesSlice returns a defensive copy of edge properties.
// For compositions, this returns an empty slice.
func (r *Relation) PropertiesSlice() []*Property {
	return slices.Clone(r.properties)
}

// Property returns the edge property with the given name, if it exists.
//
// Uses linear search. Edge properties are typically few (0-3), so O(n)
// lookup is acceptable and avoids additional memory overhead of an index.
func (r *Relation) Property(name string) (*Property, bool) {
	for _, p := range r.properties {
		if p.name == name {
			return p, true
		}
	}
	return nil, false
}

// IsAssociation reports whether this relation is an association.
func (r *Relation) IsAssociation() bool {
	return r.kind == RelationAssociation
}

// IsComposition reports whether this relation is a composition.
func (r *Relation) IsComposition() bool {
	return r.kind == RelationComposition
}

// HasProperties reports whether this relation has edge properties.
// Always false for compositions; may be true for associations.
func (r *Relation) HasProperties() bool {
	return len(r.properties) > 0
}

// Equal reports whether two relations are structurally equal.
// Compares: name, kind, target TypeID (or syntactic target if unresolved),
// multiplicities, backref, reverse multiplicity, edge properties.
// NOT compared: span, docs (declaration site-specific).
// Edge properties are compared by name set (order-independent).
// Enables deduplication of identical relations from distinct ancestors.
func (r *Relation) Equal(other *Relation) bool {
	if r == nil || other == nil {
		return r == other
	}
	if r.name != other.name {
		return false
	}
	if r.kind != other.kind {
		return false
	}
	// Compare targets: use semantic identity (targetID) when resolved,
	// fall back to syntactic comparison when either is unresolved
	if !r.targetID.IsZero() && !other.targetID.IsZero() {
		// Both resolved: compare by semantic identity
		if r.targetID != other.targetID {
			return false
		}
	} else {
		// At least one unresolved: fall back to syntactic comparison
		if r.target.String() != other.target.String() {
			return false
		}
	}
	if r.optional != other.optional || r.many != other.many {
		return false
	}
	if r.backref != other.backref {
		return false
	}
	if r.reverseOptional != other.reverseOptional || r.reverseMany != other.reverseMany {
		return false
	}
	if len(r.properties) != len(other.properties) {
		return false
	}
	// Compare edge properties by name set (order-independent)
	ownProps := make(map[string]*Property, len(r.properties))
	for _, p := range r.properties {
		ownProps[p.Name()] = p
	}
	for _, op := range other.properties {
		p, ok := ownProps[op.Name()]
		if !ok || !p.Equal(op) {
			return false
		}
	}
	return true
}
