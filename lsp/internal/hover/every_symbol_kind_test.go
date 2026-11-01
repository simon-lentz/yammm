package hover

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/symbols"
	"github.com/simon-lentz/yammm/schema"
)

const everyKindSource = `schema "main"

type Tag = String

abstract type Base {
	label Tag
}

part type Line {
	sku String primary
}

type Company {
	id String primary
}

type Sub extends Base {
	id String primary
	note Tag
	! "m" id != "x"
	--> OWNER (one) Company
	*-> LINES (many) Line
}

type Sub2 extends Sub {
	extra String
}
`

func TestRenderSymbol_EveryKindRendersFromItsIndex(t *testing.T) {
	s, res := schema.LoadString(t.Context(), everyKindSource, "main.yammm")
	if res.HasErrors() {
		t.Fatal(res)
	}
	idx := symbols.BuildSymbolIndex(s, nil)
	seen := map[symbols.SymbolKind]bool{}
	for i := range idx.Symbols {
		sym := &idx.Symbols[i]
		seen[sym.Kind] = true
		if sym.Kind != symbols.SymbolSchema && RenderSymbol(sym, "") == "" {
			t.Errorf("%v %s renders nothing", sym.Kind, sym.Name)
		}
	}
	for _, k := range []symbols.SymbolKind{symbols.SymbolType, symbols.SymbolDataType, symbols.SymbolProperty, symbols.SymbolAssociation, symbols.SymbolComposition, symbols.SymbolInvariant} {
		if !seen[k] {
			t.Errorf("no %v symbol: the fixture asserts nothing for it", k)
		}
	}
	text := RenderSymbol(idx.FindByName("Sub2", symbols.SymbolType), "")
	for _, want := range []string{"extends `Sub`", "- Properties: 1\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("Sub2 hover %q lacks %q", text, want)
		}
	}
	if strings.Contains(text, "Compositions") {
		t.Errorf("Sub2 declares no composition, and its hover counts one: %q", text)
	}
}
