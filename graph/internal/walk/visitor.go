package walk

import (
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
)

// Visitor receives callbacks during graph traversal.
//
// Each method returns an error to stop traversal. If any method returns
// a non-nil error, traversal stops immediately.
//
// For partial implementations, embed [BaseVisitor] to get no-op defaults
// for methods you don't need.
type Visitor interface {
	// EnterInstance is called when entering an instance.
	// The instance is the current node being visited.
	EnterInstance(inst *graph.Instance) error

	// ExitInstance is called when leaving an instance.
	// Called after all properties, edges, and compositions have been visited.
	ExitInstance(inst *graph.Instance) error

	// VisitProperty is called for each property on an instance.
	// Properties are visited in alphabetic order by name.
	VisitProperty(inst *graph.Instance, name string, value immutable.Value) error

	// VisitEdge is called for each resolved association edge.
	// Edges are visited in sorted order per Result.Edges() ordering.
	VisitEdge(edge *graph.Edge) error

	// EnterComposition is called when entering a composition.
	// The relation name identifies which composition is being entered.
	EnterComposition(inst *graph.Instance, relationName string) error

	// ExitComposition is called when leaving a composition.
	// Called after all composed children have been visited.
	ExitComposition(inst *graph.Instance, relationName string) error
}

// BaseVisitor provides no-op implementations of all Visitor methods.
// Embed this in your visitor to only implement the methods you need.
type BaseVisitor struct{}

// EnterInstance does nothing and returns nil.
func (BaseVisitor) EnterInstance(*graph.Instance) error {
	return nil
}

// ExitInstance does nothing and returns nil.
func (BaseVisitor) ExitInstance(*graph.Instance) error {
	return nil
}

// VisitProperty does nothing and returns nil.
func (BaseVisitor) VisitProperty(*graph.Instance, string, immutable.Value) error {
	return nil
}

// VisitEdge does nothing and returns nil.
func (BaseVisitor) VisitEdge(*graph.Edge) error {
	return nil
}

// EnterComposition does nothing and returns nil.
func (BaseVisitor) EnterComposition(*graph.Instance, string) error {
	return nil
}

// ExitComposition does nothing and returns nil.
func (BaseVisitor) ExitComposition(*graph.Instance, string) error {
	return nil
}
