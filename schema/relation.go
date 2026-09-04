package schema

import (
	"iter"
	"slices"
	"strings"

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
	kind       RelationKind
	name       string               // DSL name (e.g., "OWNER")
	fieldName  string               // lower-case name, cached at completion
	target     TypeRef              // syntactic reference
	targetID   TypeID               // resolved identity
	span       location.Span        // source location
	doc        string               // documentation comment
	optional   bool                 // forward multiplicity: optional?
	many       bool                 // forward multiplicity: many?
	owner      string               // declaring type name
	properties []*Property          // edge properties (associations only)
	propByFold map[string]*Property // lowercased name → property, built at seal
	sealed     bool                 // true after completion; prevents further mutation
}

// newRelation creates a new Relation. This is primarily for internal use;
// relations are typically created during schema parsing and completion.
func newRelation(
	kind RelationKind,
	name string,
	fieldName string,
	target TypeRef,
	targetID TypeID,
	span location.Span,
	doc string,
	optional, many bool,
	owner string,
	properties []*Property,
) *Relation {
	return &Relation{
		kind:       kind,
		name:       name,
		fieldName:  fieldName,
		target:     target,
		targetID:   targetID,
		span:       span,
		doc:        doc,
		optional:   optional,
		many:       many,
		owner:      owner,
		properties: properties,
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

// FieldName returns the field name instance data and expressions use: the
// relation name in lower case, "WORKS_AT" → "works_at". A relation name is
// UPPER_SNAKE, so the two spellings differ only by case. Cached at completion.
func (r *Relation) FieldName() string {
	return r.fieldName
}

// Target returns the syntactic type reference for diagnostics.
func (r *Relation) Target() TypeRef {
	return r.target
}

// TargetID returns the target type's resolved identity. The declaring schema
// sets it when it resolves relation targets, before inheritance merges, so a
// relation read from a completed schema always carries it; it is zero only
// while completion is in progress or for a target that never resolved, which
// fails the load.
func (r *Relation) TargetID() TypeID {
	return r.targetID
}

// setTargetID sets the resolved canonical type identity.
// Internal use only; called during schema completion.
// Panics if called after seal().
func (r *Relation) setTargetID(id TypeID) {
	if r.sealed {
		panic("relation: cannot mutate sealed relation")
	}
	r.targetID = id
}

// seal prevents further mutation of the relation. Every relation is sealed
// exactly once, by the completer of the schema that declares it; a second
// call is a bug and panics like the mutators it protects.
func (r *Relation) seal() {
	if r.sealed {
		panic("relation: sealed twice")
	}
	r.propByFold = make(map[string]*Property, len(r.properties))
	for _, p := range r.properties {
		r.propByFold[strings.ToLower(p.name)] = p
	}
	r.sealed = true
}

// isSealed reports whether the relation has been sealed.
func (r *Relation) isSealed() bool {
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

// Owner returns the declaring type's name as spelled in the declaring schema.
// It is local to that schema: resolve it there, never against a reader's
// type index, where the same spelling may name a different type.
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

// PropertyFold returns the edge property whose lowercased name is lower, if
// one exists. The caller lowercases the input key; this is the case-folded
// half of [Relation.Property], answered from an index built at seal.
func (r *Relation) PropertyFold(lower string) (*Property, bool) {
	if r.propByFold != nil {
		p, ok := r.propByFold[lower]
		return p, ok
	}
	for _, p := range r.properties {
		if strings.ToLower(p.name) == lower {
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

// Equal reports whether two relations are one definition: the same name,
// kind, resolved target identity, multiplicities and edge properties (by name
// set). Span and docs are declaration-site facts and are not compared. A
// relation whose target never resolved is equal to nothing, itself included —
// the load that left it unresolved already carries that diagnostic.
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
	if r.targetID.IsZero() || other.targetID.IsZero() || r.targetID != other.targetID {
		return false
	}
	if r.optional != other.optional || r.many != other.many {
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
