package graph

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

// recordHandler is a test handler that records log records for inspection.
type recordHandler struct {
	mu      sync.Mutex
	records []slog.Record
	level   slog.Level
}

func newRecordHandler(level slog.Level) *recordHandler { //nolint:unparam // test helper with fixed level
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

// hasAttr checks if any record has the given attribute key/value.
func hasAttr(records []slog.Record, key, value string) bool {
	for _, r := range records {
		var found bool
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key && a.Value.String() == value {
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

// countLevel counts records at the given level.
func countLevel(records []slog.Record, level slog.Level) int {
	count := 0
	for _, r := range records {
		if r.Level == level {
			count++
		}
	}
	return count
}

func TestGraph_Add_Logging(t *testing.T) {
	s := testSchemaWithAssociation(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add a Company instance
	company := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
	_, err := g.Add(context.Background(), company)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	records := h.Records()

	// Should have operation start and end logs
	if !hasOpName(records, "yammm.graph.add") {
		t.Error("expected yammm.graph.add operation to be logged")
	}

	// Should have type and pk attributes
	if !hasAttr(records, "type", "Company") {
		t.Error("expected type=Company attribute")
	}
	if !hasAttr(records, "pk", `["acme"]`) {
		t.Error(`expected pk=["acme"] attribute`)
	}
}

func TestGraph_Add_DuplicateLogging(t *testing.T) {
	s := testSchemaWithAssociation(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add first Company
	company1 := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
	_, err := g.Add(context.Background(), company1)
	if err != nil {
		t.Fatalf("First Add failed: %v", err)
	}

	// Add duplicate Company
	company2 := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Inc"})
	_, err = g.Add(context.Background(), company2)
	if err != nil {
		t.Fatalf("Second Add failed: %v", err)
	}

	records := h.Records()

	// Should have Warn level log for duplicate
	warnCount := countLevel(records, slog.LevelWarn)
	if warnCount == 0 {
		t.Error("expected Warn level log for duplicate primary key")
	}

	// Should have the duplicate message
	if !hasMessage(records, "duplicate primary key") {
		t.Error("expected 'duplicate primary key' message")
	}
}

func TestGraph_Add_EdgeResolutionLogging(t *testing.T) {
	s := testSchemaWithAssociation(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add Company first
	company := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
	_, err := g.Add(context.Background(), company)
	if err != nil {
		t.Fatalf("Add Company failed: %v", err)
	}

	// Add Person with edge to Company
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"}, "employer", [][]any{{"acme"}})
	_, err = g.Add(context.Background(), person)
	if err != nil {
		t.Fatalf("Add Person failed: %v", err)
	}

	records := h.Records()

	// Should log edge resolved
	if !hasMessage(records, "edge resolved") {
		t.Error("expected 'edge resolved' message")
	}
}

func TestGraph_Add_ForwardReferenceLogging(t *testing.T) {
	s := testSchemaWithOptionalAssociation(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add Person first with edge to non-existent Company (forward reference)
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"}, "employer", [][]any{{"acme"}})
	_, err := g.Add(context.Background(), person)
	if err != nil {
		t.Fatalf("Add Person failed: %v", err)
	}

	records := h.Records()

	// Should log forward reference created
	if !hasMessage(records, "forward reference created") {
		t.Error("expected 'forward reference created' message")
	}
}

func TestGraph_Add_PendingEdgesResolvedLogging(t *testing.T) {
	s := testSchemaWithOptionalAssociation(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add Person first with edge to non-existent Company (creates forward reference)
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"}, "employer", [][]any{{"acme"}})
	_, err := g.Add(context.Background(), person)
	if err != nil {
		t.Fatalf("Add Person failed: %v", err)
	}

	// Now add Company to resolve the forward reference
	company := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
	_, err = g.Add(context.Background(), company)
	if err != nil {
		t.Fatalf("Add Company failed: %v", err)
	}

	records := h.Records()

	// Should log pending edges resolved
	if !hasMessage(records, "pending edges resolved") {
		t.Error("expected 'pending edges resolved' message")
	}
}

func TestGraph_Check_Logging(t *testing.T) {
	s := testSchemaWithAssociation(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add person without employer (required relation)
	person := mustValidInstance(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"})
	_, err := g.Add(context.Background(), person)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Check should log the operation
	_, err = g.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	records := h.Records()

	// Should have check operation
	if !hasOpName(records, "yammm.graph.check") {
		t.Error("expected yammm.graph.check operation to be logged")
	}
}

func TestGraph_Check_UnresolvedLogging(t *testing.T) {
	s := testSchemaWithAssociation(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add person with reference to non-existent company (required relation)
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"}, "employer", [][]any{{"missing"}})
	_, err := g.Add(context.Background(), person)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Check should log unresolved
	_, err = g.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	records := h.Records()

	// Should have Warn level log for unresolved
	warnCount := countLevel(records, slog.LevelWarn)
	if warnCount == 0 {
		t.Error("expected Warn level log for unresolved required association")
	}

	// Should have the unresolved message
	if !hasMessage(records, "unresolved required association") {
		t.Error("expected 'unresolved required association' message")
	}
}

func TestGraph_AddComposed_Logging(t *testing.T) {
	s := testSchemaWithComposition(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add parent
	parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"})
	_, err := g.Add(context.Background(), parent)
	if err != nil {
		t.Fatalf("Add parent failed: %v", err)
	}

	// Add composed child
	child := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
	_, err = g.AddComposed(context.Background(), "Parent", FormatKey("p1"), "children", child)
	if err != nil {
		t.Fatalf("AddComposed failed: %v", err)
	}

	records := h.Records()

	// Should have add_composed operation
	if !hasOpName(records, "yammm.graph.add_composed") {
		t.Error("expected yammm.graph.add_composed operation to be logged")
	}

	// Should have parent and child attributes
	if !hasAttr(records, "parent_type", "Parent") {
		t.Error("expected parent_type=Parent attribute")
	}
	if !hasAttr(records, "relation", "children") {
		t.Error("expected relation=children attribute")
	}
}

func TestGraph_AddComposed_DuplicateLogging(t *testing.T) {
	s := testSchemaWithComposition(t)

	h := newRecordHandler(slog.LevelDebug)
	logger := slog.New(h)

	g := New(s, WithLogger(logger))

	// Add parent
	parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"})
	_, err := g.Add(context.Background(), parent)
	if err != nil {
		t.Fatalf("Add parent failed: %v", err)
	}

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
	_, err = g.AddComposed(context.Background(), "Parent", FormatKey("p1"), "children", child1)
	if err != nil {
		t.Fatalf("First AddComposed failed: %v", err)
	}

	// Add duplicate child (same PK)
	child2 := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1 Dup"})
	_, err = g.AddComposed(context.Background(), "Parent", FormatKey("p1"), "children", child2)
	if err != nil {
		t.Fatalf("Second AddComposed failed: %v", err)
	}

	records := h.Records()

	// Should have Warn level log for duplicate
	warnCount := countLevel(records, slog.LevelWarn)
	if warnCount == 0 {
		t.Error("expected Warn level log for duplicate composed child")
	}

	// Should have the duplicate message
	if !hasMessage(records, "duplicate composed child") {
		t.Error("expected 'duplicate composed child' message")
	}
}

func TestGraph_NoLogging_WhenNilLogger(t *testing.T) {
	s := testSchemaWithAssociation(t)

	// No logger - should not panic
	g := New(s)

	company := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
	_, err := g.Add(context.Background(), company)
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	_, err = g.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	// Test passes if no panic occurred
}
