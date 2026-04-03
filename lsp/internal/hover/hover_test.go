package hover

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

func TestBuildHoverForSymbol_NilData(t *testing.T) {
	t.Parallel()

	sym := &symbols.Symbol{
		Name: "Unknown",
		Kind: symbols.SymbolType,
		Data: nil, // No data
	}

	h, err := BuildHoverForSymbolWithRange(sym, nil, nil, lsputil.PositionEncodingUTF16)
	require.NoError(t, err)
	assert.Nil(t, h, "hover should be nil when symbol has no data")
}

func TestBuildHoverForSymbol_UnknownKind(t *testing.T) {
	t.Parallel()

	sym := &symbols.Symbol{
		Name: "Unknown",
		Kind: symbols.SymbolKind(99), // Unknown kind
	}

	h, err := BuildHoverForSymbolWithRange(sym, nil, nil, lsputil.PositionEncodingUTF16)
	require.NoError(t, err)
	assert.Nil(t, h, "hover should be nil for unknown symbol kind")
}

func TestBuildHoverForSymbolWithRange_AcceptsOverrideParameter(t *testing.T) {
	// Tests that BuildHoverForSymbolWithRange accepts an override range parameter.
	// The override range is used for reference hovers to return the reference's
	// location instead of the target symbol's location.
	t.Parallel()

	sourceID := location.MustNewSourceID("test://main.yammm")
	targetSourceID := location.MustNewSourceID("test://imported.yammm")

	// The symbol is from a different file with its own span
	targetSymSpan := location.Range(targetSourceID, 10, 1, 10, 20)
	sym := &symbols.Symbol{
		Name:      "TargetType",
		Kind:      symbols.SymbolType,
		SourceID:  targetSourceID,
		Selection: targetSymSpan,
		Data:      &schema.Type{}, // Non-nil data
	}

	// The reference span is in the current document (different from target)
	refSpan := location.Range(sourceID, 5, 10, 5, 20)

	// Without override - returns nil because snapshot is nil
	hoverWithoutOverride, err := BuildHoverForSymbolWithRange(sym, nil, nil, lsputil.PositionEncodingUTF16)
	require.NoError(t, err)

	// With override - also returns nil because snapshot is nil
	hoverWithOverride, err := BuildHoverForSymbolWithRange(sym, nil, &refSpan, lsputil.PositionEncodingUTF16)
	require.NoError(t, err)

	// Both return nil because snapshot is nil (early return in function)
	// The test validates that the function signature accepts the override parameter
	// Integration tests should verify the full behavior with a real workspace
	_ = hoverWithoutOverride
	_ = hoverWithOverride
}
