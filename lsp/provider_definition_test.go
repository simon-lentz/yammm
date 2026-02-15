package lsp

import (
	"log/slog"
	"os"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestResolveSymbolDefinition_LocalType(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://schema.yammm")
	span := location.Range(sourceID, 5, 1, 10, 1)
	selectionSpan := location.Range(sourceID, 5, 6, 5, 12)

	typ := schema.NewType("Person", sourceID, span, "A person type", false, false)

	s := NewServer(testLogger(), Config{})

	sym := &Symbol{
		Name:      "Person",
		Kind:      SymbolType,
		SourceID:  sourceID,
		Range:     span,
		Selection: selectionSpan,
		Data:      typ,
	}

	// Snapshot with nil Sources triggers the fallback path in symbolToLocation
	snapshot := &Snapshot{
		Root:    "/test",
		Sources: nil, // Will use fallback path
	}

	result, err := s.resolveSymbolDefinition(snapshot, sym)
	if err != nil {
		t.Fatalf("resolveSymbolDefinition error: %v", err)
	}

	loc, ok := result.(*protocol.Location)
	if !ok {
		t.Fatalf("expected *protocol.Location, got %T", result)
	}

	if loc == nil {
		t.Fatal("expected non-nil location")
	}

	// URI is returned (actual URI depends on RemapPathToURI which is tested separately)
	if loc.URI == "" {
		t.Error("expected non-empty URI")
	}

	// Range should be from the selection span (0-indexed: line 5 -> 4)
	if loc.Range.Start.Line != 4 {
		t.Errorf("Range.Start.Line = %d; want 4", loc.Range.Start.Line)
	}
	if loc.Range.Start.Character != 5 {
		t.Errorf("Range.Start.Character = %d; want 5", loc.Range.Start.Character)
	}
}

func TestResolveSymbolDefinition_ImportWithoutSchema(t *testing.T) {
	t.Parallel()

	mainSourceID := location.MustNewSourceID("test://main.yammm")
	importedSourceID := location.MustNewSourceID("test://missing.yammm")
	importSpan := location.Range(mainSourceID, 1, 1, 1, 30)

	// Create an import WITHOUT a resolved schema (unresolved import)
	imp := schema.NewImport("./missing", "missing", importedSourceID, importSpan)
	// Don't call imp.SetSchema() - simulates unresolved import

	s := NewServer(testLogger(), Config{})

	sym := &Symbol{
		Name:     "missing",
		Kind:     SymbolImport,
		SourceID: mainSourceID,
		Range:    importSpan,
		Data:     imp,
	}

	snapshot := &Snapshot{
		Root: "/test",
	}

	result, err := s.resolveSymbolDefinition(snapshot, sym)
	if err != nil {
		t.Fatalf("resolveSymbolDefinition error: %v", err)
	}

	// Should return nil when import is unresolved (schema not loaded)
	if result != nil {
		t.Errorf("expected nil result for unresolved import, got %v", result)
	}
}

func TestResolveReferenceDefinition_NotFound(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://schema.yammm")

	s := NewServer(testLogger(), Config{})

	// Create empty snapshot - ResolveTypeReference will return nil
	snapshot := &Snapshot{
		Root:            "/test",
		SymbolsBySource: map[location.SourceID]*SymbolIndex{},
	}

	// Reference to a non-existent type
	ref := &ReferenceSymbol{
		TargetName: "NonExistent",
		Qualifier:  "",
		Span:       location.Range(sourceID, 5, 10, 5, 20),
	}

	result, err := s.resolveReferenceDefinition(snapshot, ref, sourceID)
	if err != nil {
		t.Fatalf("resolveReferenceDefinition error: %v", err)
	}

	// Should return nil when reference cannot be resolved
	if result != nil {
		t.Errorf("expected nil result for unresolved reference, got %v", result)
	}
}

func TestSymbolToLocation(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://types.yammm")
	selectionSpan := location.Range(sourceID, 3, 6, 3, 12)
	fullSpan := location.Range(sourceID, 3, 1, 8, 1)

	s := NewServer(testLogger(), Config{})

	sym := &Symbol{
		Name:      "Customer",
		Kind:      SymbolType,
		SourceID:  sourceID,
		Range:     fullSpan,
		Selection: selectionSpan,
	}

	// Snapshot with nil Sources to test fallback path
	snapshot := &Snapshot{
		Root:    "/test",
		Sources: nil,
	}

	loc := s.symbolToLocation(snapshot, sym)

	if loc == nil {
		t.Fatal("expected non-nil location")
	}

	// URI is returned (actual URI depends on RemapPathToURI which is tested separately)
	if loc.URI == "" {
		t.Error("expected non-empty URI")
	}

	// Should use selection span (0-indexed from 1-indexed)
	if loc.Range.Start.Line != 2 {
		t.Errorf("Range.Start.Line = %d; want 2", loc.Range.Start.Line)
	}
	if loc.Range.Start.Character != 5 {
		t.Errorf("Range.Start.Character = %d; want 5", loc.Range.Start.Character)
	}
}

func TestSymbolToLocation_NilSymbol(t *testing.T) {
	t.Parallel()

	s := NewServer(testLogger(), Config{})

	snapshot := &Snapshot{
		Root: "/test",
	}

	loc := s.symbolToLocation(snapshot, nil)
	if loc != nil {
		t.Error("expected nil location for nil symbol")
	}
}

func TestSymbolToLocation_ZeroRange(t *testing.T) {
	t.Parallel()

	sourceID := location.MustNewSourceID("test://types.yammm")

	s := NewServer(testLogger(), Config{})

	sym := &Symbol{
		Name:     "NoRange",
		Kind:     SymbolType,
		SourceID: sourceID,
		Range:    location.Span{}, // zero span
	}

	snapshot := &Snapshot{
		Root: "/test",
	}

	loc := s.symbolToLocation(snapshot, sym)
	if loc != nil {
		t.Error("expected nil location for symbol with zero range")
	}
}
