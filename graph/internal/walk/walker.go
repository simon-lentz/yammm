package walk

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sort"
	"strings"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/trace"
)

// ErrNilVisitor is returned when Walk or WalkInstance is called with a nil visitor.
var ErrNilVisitor = errors.New("walk: nil visitor")

// WalkOption configures the walker behavior.
type WalkOption func(*walkConfig)

type walkConfig struct {
	logger *slog.Logger
}

// WithLogger enables debug logging during traversal.
func WithLogger(logger *slog.Logger) WalkOption {
	return func(cfg *walkConfig) {
		cfg.logger = logger
	}
}

// Walk traverses the graph result, calling visitor methods.
//
// Traversal order is deterministic:
//   - Types are visited in lexicographic order
//   - Instances within a type are visited in primary key order
//   - Properties are visited in alphabetic order
//   - Edges are visited in sorted order
//   - Compositions are visited in relation name order
//
// Returns on first error from visitor or if context is cancelled.
func Walk(ctx context.Context, result *graph.Result, visitor Visitor, opts ...WalkOption) error {
	// Nil context check - must come first for consistent contract
	// (nil context always panics, even if result is also nil)
	if ctx == nil {
		panic("walk.Walk: nil context")
	}

	if result == nil {
		return nil
	}

	if visitor == nil {
		return ErrNilVisitor
	}

	cfg := walkConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Operation boundary logging
	op := trace.Begin(ctx, cfg.logger, "yammm.walk.graph",
		slog.Int("types_count", len(result.Types())),
	)

	w := &walker{
		result:  result,
		visitor: visitor,
		config:  cfg,
	}

	err := w.walk(ctx)
	op.End(err)
	return err
}

// WalkInstance traverses a single instance subtree.
//
// This is useful for traversing just one instance and its composed children
// without visiting the entire graph.
//
// Note: WalkInstance does not call VisitEdge. Edges require the full graph
// context (built by Walk) to resolve. If edge visits are needed, use Walk
// with a result that contains the instance.
//
// Returns on first error from visitor or if context is cancelled.
func WalkInstance(ctx context.Context, inst *graph.Instance, visitor Visitor, opts ...WalkOption) error {
	// Nil context check - must come first for consistent contract
	// (nil context always panics, even if inst is also nil)
	if ctx == nil {
		panic("walk.WalkInstance: nil context")
	}

	if inst == nil {
		return nil
	}

	if visitor == nil {
		return ErrNilVisitor
	}

	cfg := walkConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}

	// Operation boundary logging
	op := trace.Begin(ctx, cfg.logger, "yammm.walk.instance",
		slog.String("type", inst.TypeName()),
		slog.String("pk", inst.PrimaryKey().String()),
	)

	w := &walker{
		visitor: visitor,
		config:  cfg,
	}

	err := w.walkInstance(ctx, inst, nil)
	op.End(err)
	return err
}

type walker struct {
	result  *graph.Result
	visitor Visitor
	config  walkConfig
}

func (w *walker) walk(ctx context.Context) error {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return err //nolint:wrapcheck // context errors should be returned unwrapped
	}

	// Build edge lookup for efficient edge retrieval per instance
	edgesBySource := w.buildEdgeLookup()

	// Visit types in sorted order
	for _, typeName := range w.result.Types() {
		// Check context between types
		if err := ctx.Err(); err != nil {
			return err //nolint:wrapcheck // context errors should be returned unwrapped
		}

		// Visit instances in sorted order
		for _, inst := range w.result.InstancesOf(typeName) {
			if err := w.walkInstance(ctx, inst, edgesBySource); err != nil {
				return err
			}
		}
	}

	return nil
}

func (w *walker) walkInstance(ctx context.Context, inst *graph.Instance, edgesBySource map[instanceKey][]*graph.Edge) error {
	// Check context before each instance
	if err := ctx.Err(); err != nil {
		return err //nolint:wrapcheck // context errors pass through unwrapped
	}

	// Enter instance
	if err := w.visitor.EnterInstance(inst); err != nil {
		return err //nolint:wrapcheck // visitor errors pass through unwrapped
	}

	trace.Debug(ctx, w.config.logger, "visiting instance",
		slog.String("type", inst.TypeName()),
		slog.String("pk", inst.PrimaryKey().String()),
	)

	// Visit properties in sorted order
	for name, value := range inst.Properties().SortedRange() {
		if err := w.visitor.VisitProperty(inst, name, value); err != nil {
			return err //nolint:wrapcheck // visitor errors pass through unwrapped
		}
	}

	// Visit edges for this instance
	if edgesBySource != nil {
		key := instanceKey{typeName: inst.TypeName(), pk: inst.PrimaryKey().String()}
		for _, edge := range edgesBySource[key] {
			if err := w.visitor.VisitEdge(edge); err != nil {
				return err //nolint:wrapcheck // visitor errors pass through unwrapped
			}
		}
	}

	// Visit compositions in sorted order
	if err := w.walkCompositions(ctx, inst, edgesBySource); err != nil {
		return err
	}

	// Exit instance
	if err := w.visitor.ExitInstance(inst); err != nil {
		return err //nolint:wrapcheck // visitor errors pass through unwrapped
	}

	return nil
}

func (w *walker) walkCompositions(ctx context.Context, inst *graph.Instance, edgesBySource map[instanceKey][]*graph.Edge) error {
	// Get composition relation names
	relationNames := w.getCompositionRelations(inst)
	if len(relationNames) == 0 {
		return nil
	}

	for _, relationName := range relationNames {
		children := inst.Composed(relationName)
		if len(children) == 0 {
			continue
		}

		// Sort children by primary key for deterministic ordering.
		// If children have no PK (len==0), preserve insertion order (index order).
		if len(children) > 1 && children[0].PrimaryKey().Len() > 0 {
			slices.SortFunc(children, func(a, b *graph.Instance) int {
				return strings.Compare(a.PrimaryKey().String(), b.PrimaryKey().String())
			})
		}

		// Enter composition
		if err := w.visitor.EnterComposition(inst, relationName); err != nil {
			return err //nolint:wrapcheck // visitor errors pass through unwrapped
		}

		trace.Debug(ctx, w.config.logger, "entering composition",
			slog.String("parent_type", inst.TypeName()),
			slog.String("parent_pk", inst.PrimaryKey().String()),
			slog.String("relation", relationName),
			slog.Int("children_count", len(children)),
		)

		// Visit composed children recursively
		for _, child := range children {
			if err := w.walkInstance(ctx, child, edgesBySource); err != nil {
				return err
			}
		}

		// Exit composition
		if err := w.visitor.ExitComposition(inst, relationName); err != nil {
			return err //nolint:wrapcheck // visitor errors pass through unwrapped
		}
	}

	return nil
}

// getCompositionRelations returns sorted relation names that have composed children.
func (w *walker) getCompositionRelations(inst *graph.Instance) []string {
	return inst.ComposedRelations()
}

// instanceKey identifies an instance for edge lookup.
type instanceKey struct {
	typeName string
	pk       string
}

// buildEdgeLookup creates a map from source instance to its outgoing edges.
func (w *walker) buildEdgeLookup() map[instanceKey][]*graph.Edge {
	edges := w.result.Edges()
	if len(edges) == 0 {
		return nil
	}

	lookup := make(map[instanceKey][]*graph.Edge)
	for _, edge := range edges {
		source := edge.Source()
		if source == nil {
			continue
		}
		key := instanceKey{
			typeName: source.TypeName(),
			pk:       source.PrimaryKey().String(),
		}
		lookup[key] = append(lookup[key], edge)
	}

	// Sort edges within each source for determinism
	for key, sourceEdges := range lookup {
		sort.Slice(sourceEdges, func(i, j int) bool {
			return edgeLess(sourceEdges[i], sourceEdges[j])
		})
		lookup[key] = sourceEdges
	}

	return lookup
}

// edgeLess compares edges for sorting.
//
// The nil target check is defensive programming - nil targets cannot occur
// in practice because edge creation (graph.go) only creates edges with
// resolved, non-nil targets. This check provides cheap insurance against
// potential invariant violations.
func edgeLess(a, b *graph.Edge) bool {
	// Compare by relation name first
	if a.Relation() != b.Relation() {
		return a.Relation() < b.Relation()
	}

	// Then by target type
	// Defensive: treat nil targets as equal (should never occur)
	if a.Target() != nil && b.Target() != nil {
		if a.Target().TypeName() != b.Target().TypeName() {
			return a.Target().TypeName() < b.Target().TypeName()
		}
		// Then by target key
		return a.Target().PrimaryKey().String() < b.Target().PrimaryKey().String()
	}

	return false
}
