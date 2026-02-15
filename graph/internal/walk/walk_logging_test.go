package walk

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

// recordHandler is a test handler that records log records for inspection.
type recordHandler struct {
	mu      sync.Mutex
	records []slog.Record
	level   slog.Level
}

func newRecordHandler(level slog.Level) *recordHandler {
	return &recordHandler{level: level}
}

func (h *recordHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}

func (h *recordHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *recordHandler) WithGroup(_ string) slog.Handler {
	return h
}

func (h *recordHandler) Records() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make([]slog.Record, len(h.records))
	copy(result, h.records)
	return result
}

// hasOpName checks if any record has the given operation name.
func hasOpName(records []slog.Record, opName string) bool {
	for _, r := range records {
		var found bool
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "op" && a.Value.String() == opName {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// hasMessage checks if any record has the given message.
func hasMessage(records []slog.Record, msg string) bool {
	for _, r := range records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

// testSchemaWithComposition creates a schema with parent-child composition.
func testSchemaWithComposition(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("walk_test").
		WithSourceID(location.MustNewSourceID("test://walk_test.yammm")).
		AddType("Child").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Parent").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithComposition("children", schema.LocalTypeRef("Child", location.Span{}), true, true).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build schema: %s", result.String())
	}
	return s
}

// testSchemaWithPKLessComposition creates a schema with PK-less child composition.
func testSchemaWithPKLessComposition(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("walk_pkless_test").
		WithSourceID(location.MustNewSourceID("test://walk_pkless.yammm")).
		AddType("Item").
		AsPart().
		WithProperty("value", schema.StringConstraint{}).
		Done().
		AddType("Container").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithComposition("items", schema.LocalTypeRef("Item", location.Span{}), true, true).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build pkless schema: %s", result.String())
	}
	return s
}

// mustValidInstance creates a ValidInstance for testing.
func mustValidInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance { //nolint:unparam // test helper with fixed type
	t.Helper()

	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found", typeName)
	}

	return instance.NewValidInstance(
		typeName,
		typ.ID(),
		immutable.WrapKey(pk),
		immutable.WrapProperties(props),
		nil, nil, nil,
	)
}

// mustValidPartInstance creates a ValidInstance for a part type.
func mustValidPartInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()

	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found", typeName)
	}

	return instance.NewValidInstance(
		typeName,
		typ.ID(),
		immutable.WrapKey(pk),
		immutable.WrapProperties(props),
		nil, nil, nil,
	)
}

// noopVisitor is a visitor that does nothing.
type noopVisitor struct {
	BaseVisitor
}

func TestWalk_Logging(t *testing.T) {
	s := testSchemaWithComposition(t)

	// Build a graph with instances
	g := graph.New(s)

	parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"})
	_, err := g.Add(context.Background(), parent)
	if err != nil {
		t.Fatalf("Add parent failed: %v", err)
	}

	child := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
	_, err = g.AddComposed(context.Background(), "Parent", graph.FormatKey("p1"), "children", child)
	if err != nil {
		t.Fatalf("AddComposed failed: %v", err)
	}

	result := g.Snapshot()

	// Set up logging
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	// Walk with logging
	visitor := &noopVisitor{}
	err = Walk(context.Background(), result, visitor, WithLogger(logger))
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	records := h.Records()

	// Should have walk operation
	if !hasOpName(records, "yammm.walk.graph") {
		t.Error("expected yammm.walk.graph operation to be logged")
	}

	// Should log visiting instances
	if !hasMessage(records, "visiting instance") {
		t.Error("expected 'visiting instance' message")
	}
}

func TestWalkInstance_Logging(t *testing.T) {
	s := testSchemaWithComposition(t)

	// Build a graph with instances
	g := graph.New(s)

	parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"})
	_, err := g.Add(context.Background(), parent)
	if err != nil {
		t.Fatalf("Add parent failed: %v", err)
	}

	result := g.Snapshot()
	instances := result.InstancesOf("Parent")
	if len(instances) == 0 {
		t.Fatal("no Parent instances found")
	}

	// Set up logging
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	// Walk single instance with logging
	visitor := &noopVisitor{}
	err = WalkInstance(context.Background(), instances[0], visitor, WithLogger(logger))
	if err != nil {
		t.Fatalf("WalkInstance failed: %v", err)
	}

	records := h.Records()

	// Should have walk.instance operation
	if !hasOpName(records, "yammm.walk.instance") {
		t.Error("expected yammm.walk.instance operation to be logged")
	}
}

func TestWalk_CompositionLogging(t *testing.T) {
	s := testSchemaWithComposition(t)

	// Build a graph with parent and children
	g := graph.New(s)

	parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"})
	_, err := g.Add(context.Background(), parent)
	if err != nil {
		t.Fatalf("Add parent failed: %v", err)
	}

	child := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
	_, err = g.AddComposed(context.Background(), "Parent", graph.FormatKey("p1"), "children", child)
	if err != nil {
		t.Fatalf("AddComposed failed: %v", err)
	}

	result := g.Snapshot()

	// Set up logging
	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	// Walk with logging
	visitor := &noopVisitor{}
	err = Walk(context.Background(), result, visitor, WithLogger(logger))
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}

	records := h.Records()

	// Should log entering composition
	if !hasMessage(records, "entering composition") {
		t.Error("expected 'entering composition' message")
	}
}

func TestWalk_NoLogging_WhenNilLogger(t *testing.T) {
	s := testSchemaWithComposition(t)

	// Build a graph
	g := graph.New(s)

	parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"})
	_, err := g.Add(context.Background(), parent)
	if err != nil {
		t.Fatalf("Add parent failed: %v", err)
	}

	result := g.Snapshot()

	// Walk without logger - should not panic
	visitor := &noopVisitor{}
	err = Walk(context.Background(), result, visitor)
	if err != nil {
		t.Fatalf("Walk failed: %v", err)
	}
	// Test passes if no panic occurred
}
