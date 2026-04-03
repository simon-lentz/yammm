package testutil

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
)

// AssertHoverContains checks that hover result contains expected text.
func AssertHoverContains(t *testing.T, hover *protocol.Hover, expectedText string) {
	t.Helper()

	if hover == nil {
		t.Fatal("expected hover result, got nil")
	}

	if !strings.Contains(hover.Contents.Value, expectedText) {
		t.Errorf("hover content %q does not contain %q", hover.Contents.Value, expectedText)
	}
}

// AssertHoverKind checks that hover result has expected markup kind.
func AssertHoverKind(t *testing.T, hover *protocol.Hover, expectedKind protocol.MarkupKind) {
	t.Helper()

	if hover == nil {
		t.Fatal("expected hover result, got nil")
	}

	if hover.Contents.Kind != expectedKind {
		t.Errorf("hover kind = %q; want %q", hover.Contents.Kind, expectedKind)
	}
}

// AssertLocationLine checks that a location points to the expected line.
func AssertLocationLine(t *testing.T, loc protocol.Location, expectedLine int) {
	t.Helper()

	if int(loc.Range.Start.Line) != expectedLine {
		t.Errorf("location line = %d; want %d", loc.Range.Start.Line, expectedLine)
	}
}

// AssertLocationURI checks that a location has the expected URI suffix.
func AssertLocationURI(t *testing.T, loc protocol.Location, expectedSuffix string) {
	t.Helper()

	if !strings.HasSuffix(loc.URI, expectedSuffix) {
		t.Errorf("location URI %q does not end with %q", loc.URI, expectedSuffix)
	}
}

// AssertCompletionContains checks that completion result contains an item with the given label.
func AssertCompletionContains(t *testing.T, result any, expectedLabel string) {
	t.Helper()

	items := extractCompletionItems(t, result)
	for _, item := range items {
		if item.Label == expectedLabel {
			return
		}
	}
	t.Errorf("completion result does not contain item with label %q", expectedLabel)
}

// AssertCompletionNotContains checks that completion result does not contain an item with the given label.
func AssertCompletionNotContains(t *testing.T, result any, label string) {
	t.Helper()

	items := extractCompletionItems(t, result)
	for _, item := range items {
		if item.Label == label {
			t.Errorf("completion result should not contain item with label %q", label)
			return
		}
	}
}

// extractCompletionItems extracts completion items from a completion result.
func extractCompletionItems(t *testing.T, result any) []protocol.CompletionItem {
	t.Helper()

	switch v := result.(type) {
	case nil:
		return nil
	case []protocol.CompletionItem:
		return v
	default:
		t.Fatalf("unexpected completion result type: %T", result)
		return nil
	}
}

// AssertDocumentSymbolsCount checks that document symbols result has expected count.
func AssertDocumentSymbolsCount(t *testing.T, result any, expectedCount int) {
	t.Helper()

	symbols := extractDocumentSymbols(t, result)
	if len(symbols) != expectedCount {
		t.Errorf("document symbols count = %d; want %d", len(symbols), expectedCount)
	}
}

// AssertDocumentSymbolExists checks that a symbol with the given name exists,
// including recursively searching through nested children.
func AssertDocumentSymbolExists(t *testing.T, result any, name string) {
	t.Helper()

	symbols := extractDocumentSymbols(t, result)
	if findSymbolRecursive(symbols, name) {
		return
	}
	t.Errorf("document symbol %q not found", name)
}

// HasDocumentSymbol reports whether a symbol with the given name exists in the result.
// Useful for polling-based test assertions.
func HasDocumentSymbol(result any, name string) bool {
	var symbols []protocol.DocumentSymbol
	switch v := result.(type) {
	case nil:
		return false
	case []protocol.DocumentSymbol:
		symbols = v
	default:
		return false
	}
	return findSymbolRecursive(symbols, name)
}

// findSymbolRecursive recursively searches for a symbol by name in a symbol tree.
func findSymbolRecursive(symbols []protocol.DocumentSymbol, name string) bool {
	for _, sym := range symbols {
		if sym.Name == name {
			return true
		}
		if findSymbolRecursive(sym.Children, name) {
			return true
		}
	}
	return false
}

// extractDocumentSymbols extracts document symbols from a result.
func extractDocumentSymbols(t *testing.T, result any) []protocol.DocumentSymbol {
	t.Helper()

	switch v := result.(type) {
	case nil:
		return nil
	case []protocol.DocumentSymbol:
		return v
	case []any:
		symbols := make([]protocol.DocumentSymbol, 0, len(v))
		for _, item := range v {
			if sym, ok := item.(protocol.DocumentSymbol); ok {
				symbols = append(symbols, sym)
			}
		}
		return symbols
	default:
		t.Fatalf("unexpected document symbols result type: %T", result)
		return nil
	}
}

// AssertFormattingApplied checks that formatting edits were returned.
func AssertFormattingApplied(t *testing.T, edits []protocol.TextEdit) {
	t.Helper()

	if len(edits) == 0 {
		t.Error("expected formatting edits, got none")
	}
}

// AssertNoFormattingNeeded checks that no formatting edits were needed.
func AssertNoFormattingNeeded(t *testing.T, edits []protocol.TextEdit) {
	t.Helper()

	if len(edits) > 0 {
		t.Errorf("expected no formatting edits, got %d", len(edits))
	}
}

// AssertDiagnosticCount checks that a specific number of diagnostics were published.
func AssertDiagnosticCount(t *testing.T, diags []protocol.Diagnostic, expectedCount int) {
	t.Helper()

	if len(diags) != expectedCount {
		t.Errorf("diagnostic count = %d; want %d", len(diags), expectedCount)
	}
}

// AssertDiagnosticHasCode checks that a diagnostic with the given code exists.
func AssertDiagnosticHasCode(t *testing.T, diags []protocol.Diagnostic, expectedCode string) {
	t.Helper()

	for _, diag := range diags {
		if diag.Code != nil && diag.Code.Value == expectedCode {
			return
		}
	}
	t.Errorf("no diagnostic with code %q found", expectedCode)
}
