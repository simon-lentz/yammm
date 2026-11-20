package hover

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/lsp/internal/symbols"
	"github.com/simon-lentz/yammm/schema"
)

// TestRenderInvariant_WritesTheExpression holds the invariant hover to the
// declaration as written, and to a fence that a backtick run in the expression
// cannot close.
func TestRenderInvariant_WritesTheExpression(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, expr, want string
	}{
		{"plain", "n > 0 // positive", "**invariant** `m`\n\n```yammm\n! \"m\" n > 0\n```\n"},
		{"backticks", "s != \"a```b\"", "**invariant** `m`\n\n````yammm\n! \"m\" s != \"a```b\"\n````\n"},
		{"short runs", "s != \"`a`b`\"", "**invariant** `m`\n\n```yammm\n! \"m\" s != \"`a`b`\"\n```\n"},
		{"longest run first", "s != \"a````b`c\"", "**invariant** `m`\n\n`````yammm\n! \"m\" s != \"a````b`c\"\n`````\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer\n\ts String\n\t! \"m\" " + tt.expr + "\n}\n"
			s, res := schema.LoadString(t.Context(), src, "s.yammm")
			if res.HasErrors() {
				t.Fatalf("load: %s", res)
			}
			typ, _ := s.Type("T")
			var inv schema.Invariant
			for i := range typ.Invariants() {
				inv = i
			}
			if got := renderInvariant(&symbols.Symbol{Kind: symbols.SymbolInvariant, Data: inv}); got != tt.want {
				t.Errorf("renderInvariant = %q, want %q", got, tt.want)
			}
		})
	}
}

// The hover's declaration loads back to the invariant it shows: its message is
// written as a literal, escapes and all.
func TestRenderInvariant_TheDeclarationLoadsBack(t *testing.T) {
	t.Parallel()
	for _, msg := range []string{`"say \"hi\""`, `"a\\b"`, `"a\nb"`, `'it\'s'`, `"nul\0"`} {
		src := "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer\n\t! " + msg + " n > 0\n}\n"
		inv := onlyInvariant(t, src)
		hover := renderInvariant(&symbols.Symbol{Kind: symbols.SymbolInvariant, Data: inv})
		_, rest, _ := strings.Cut(hover, "```yammm\n")
		decl, _, _ := strings.Cut(rest, "\n```")
		back := onlyInvariant(t, "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer\n\t"+decl+"\n}\n")
		if back.Name() != inv.Name() || back.Expression() != inv.Expression() {
			t.Errorf("%s: the hover's %q loads as %q %q, want %q %q", msg, decl, back.Name(), back.Expression(), inv.Name(), inv.Expression())
		}
	}
}

func onlyInvariant(t *testing.T, src string) schema.Invariant {
	t.Helper()
	s, res := schema.LoadString(t.Context(), src, "s.yammm")
	if res.HasErrors() {
		t.Fatalf("load %q: %s", src, res)
	}
	typ, _ := s.Type("T")
	for inv := range typ.Invariants() {
		return inv
	}
	t.Fatalf("no invariant in %q", src)
	return schema.Invariant{}
}
