// Package walk provides structured traversal of the graph using the visitor pattern.
//
// The walker provides a clean abstraction for traversing validated instance
// graphs with callbacks at each structural element.
//
// # Entry Points
//
// [Walk] traverses an entire [graph.Snapshot] in deterministic order.
// [Instance] traverses a single [graph.Instance] and its compositions,
// useful for per-instance processing without a full snapshot walk.
//
// # Visitor Pattern
//
// The [Visitor] interface defines callbacks for different elements encountered
// during traversal:
//
//   - EnterInstance / ExitInstance: Called when entering/leaving an instance
//   - VisitProperty: Called for each property on an instance
//   - VisitEdge: Called for each resolved association edge
//   - EnterComposition / ExitComposition: Called when entering/leaving a composition
//
// Implementations can implement only the callbacks they care about by embedding
// [BaseVisitor], which provides no-op implementations for all methods.
//
// # Traversal Order
//
// The walker provides deterministic traversal order:
//
//  1. Types are visited in lexicographic order
//  2. Instances within a type are visited in primary key order
//  3. Properties are visited in alphabetic order
//  4. Edges are visited in sorted order
//  5. Compositions are visited in relation name order
//  6. Composed children are visited in primary key or index order
//
// # Options
//
// Both [Walk] and [Instance] accept [Option] values:
//
//   - [WithLogger]: structured logger for traversal diagnostics
//
// # Context Support
//
// The [Walk] and [Instance] functions accept a context for cancellation support.
// If the context is cancelled, traversal stops and returns the context error.
//
// # Error Handling
//
// Visitor methods return errors to stop traversal. If any visitor method
// returns a non-nil error, traversal stops immediately and the function
// returns that error.
//
// # Usage
//
//	type MyVisitor struct {
//	    walk.BaseVisitor
//	    count int
//	}
//
//	func (v *MyVisitor) EnterInstance(_ *graph.Instance) error {
//	    v.count++
//	    return nil
//	}
//
//	snap := g.Snapshot()
//	visitor := &MyVisitor{}
//	if err := walk.Walk(ctx, snap, visitor); err != nil {
//	    // handle error
//	}
//	fmt.Println("Total instances:", visitor.count)
//
// # Thread Safety
//
// [Walk] and [Instance] are stateless functions safe for concurrent use.
// Thread safety of the traversal depends on the [Visitor] implementation.
//
// # Dependencies
//
//	graph/walk  ──imports──▶  graph, immutable
package walk
