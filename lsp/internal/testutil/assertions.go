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

// AssertLocationURI checks that a location has the expected URI suffix.
func AssertLocationURI(t *testing.T, loc protocol.Location, expectedSuffix string) {
	t.Helper()

	if !strings.HasSuffix(loc.URI, expectedSuffix) {
		t.Errorf("location URI %q does not end with %q", loc.URI, expectedSuffix)
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
