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

// newDataType creates a new DataType.
//
// This is a low-level API primarily for:
//   - Internal use during schema parsing
//   - Advanced use cases like building schemas programmatically via Builder
//
// Most users should load schemas from .yammm files using the load package.
func newDataType(name string, constraint Constraint, span location.Span, doc string) *DataType {
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

// setConstraint sets the constraint (called during alias resolution).
// Internal use only; called during schema completion.
// Panics if called after seal().
func (d *DataType) setConstraint(c Constraint) {
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

// seal marks the data type as immutable.
// Called by the loader after schema completion.
func (d *DataType) seal() {
	d.sealed = true
}

// isSealed reports whether the data type has been sealed.
func (d *DataType) isSealed() bool {
	return d.sealed
}
