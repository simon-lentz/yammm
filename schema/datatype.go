package schema

import (
	"github.com/simon-lentz/yammm/location"
)

// DataType represents a named data type alias in a schema.
// Data types provide reusable constraint definitions that can be
// referenced by name in property declarations.
type DataType struct {
	name       string
	constraint Constraint
	span       location.Span
	doc        string
	sealed     bool // true after loading is complete; prevents further mutation
}

// NewDataType creates a new DataType.
//
// This is a low-level API primarily for:
//   - Internal use during schema parsing
//   - Advanced use cases like building schemas programmatically via Builder
//
// Most users should load schemas from .yammm files using the load package.
func NewDataType(name string, constraint Constraint, span location.Span, doc string) *DataType {
	return &DataType{
		name:       name,
		constraint: constraint,
		span:       span,
		doc:        doc,
	}
}

// Name returns the data type name.
func (d *DataType) Name() string {
	return d.name
}

// Constraint returns the constraint this data type defines.
func (d *DataType) Constraint() Constraint {
	return d.constraint
}

// SetConstraint sets the constraint (called during alias resolution).
// Internal use only; called during schema completion.
// Panics if called after Seal().
func (d *DataType) SetConstraint(c Constraint) {
	if d.sealed {
		panic("datatype: cannot mutate sealed datatype")
	}
	d.constraint = c
}

// Span returns the source location of this data type declaration.
func (d *DataType) Span() location.Span {
	return d.span
}

// Documentation returns the documentation comment, if any.
func (d *DataType) Documentation() string {
	return d.doc
}

// Seal marks the data type as immutable.
// Called by the loader after schema completion.
// This is not part of the public API and may be removed in future versions.
func (d *DataType) Seal() {
	d.sealed = true
}

// IsSealed reports whether the data type has been sealed.
func (d *DataType) IsSealed() bool {
	return d.sealed
}
