package parse

import (
	"fmt"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// TestParse_AnInvariantNamesEachLiteralWithNoValue pins that an invariant
// whose expression holds a literal with no value stays in the tree, holding a
// nil literal in its place that NoValue names with the kind of literal the
// source wrote, so a later check can tell it from a nil the source wrote. Each kind of literal that can have no value is
// a row: refused text, an escape whose value is not UTF-8, an escape the
// language does not have, a number out of range and a regex that does not
// compile.
func TestParse_AnInvariantNamesEachLiteralWithNoValue(t *testing.T) {
	t.Parallel()
	for name, row := range map[string]struct {
		expr string
		kind LiteralKind
	}{
		"a string holding a NUL":           {"id + \"a\x00\" == \"x\"", StringLiteral},
		"a string holding an invalid byte": {"id + \"a\xff\" == \"x\"", StringLiteral},
		"an escape not UTF-8":              {`id + "\xff" == "x"`, StringLiteral},
		"malformed escape digits":          {`id + "\xZZ" == "x"`, StringLiteral},
		"an integer out of range":          {"n + 99999999999999999999 > 0", IntegerLiteral},
		"a float out of range":             {"f + 1.0e999 > 0", FloatLiteral},
		"a regex that does not compile":    {"id =~ /(/", RegexLiteral},
		"a regex holding an invalid byte":  {"id =~ /a\xff/", RegexLiteral},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer\n\tf Float\n\t! \"m\" " + row.expr + "\n}\n"
			f, _ := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if bad := oneLiteralWithNoValue(f); bad != "" {
				t.Fatal(bad)
			}
			if kind := f.Types[0].Invariants[0].NoValue[0].Kind; kind != row.kind {
				t.Errorf("the literal with no value is of kind %d, want %d", kind, row.kind)
			}
		})
	}
	t.Run("a nil the source wrote", func(t *testing.T) {
		t.Parallel()
		src := "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id != nil && id =~ /a/ && 1 + 2.5 > 0\n}\n"
		f, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
		if len(issues) != 0 || len(f.Types[0].Invariants) != 1 {
			t.Fatalf("a clean invariant: %d issues, %d invariants; want none and one: %s", len(issues), len(f.Types[0].Invariants), messages(issues))
		}
		if nv := f.Types[0].Invariants[0].NoValue; len(nv) != 0 {
			t.Errorf("NoValue names %d literals, want none", len(nv))
		}
	})
}

// oneLiteralWithNoValue describes how f departs from one invariant on its
// first type whose NoValue names one nil literal of its expression and whose
// extent is f's one expression, or is empty when it does not.
func oneLiteralWithNoValue(f *File) string {
	invs := f.Types[0].Invariants
	if len(invs) != 1 {
		return fmt.Sprintf("the tree holds %d invariants, want one", len(invs))
	}
	if len(f.Expressions) != 1 {
		return fmt.Sprintf("Expressions holds %d extents, want the invariant's one", len(f.Expressions))
	}
	inv := invs[0]
	if len(inv.NoValue) != 1 {
		return fmt.Sprintf("NoValue names %d literals, want one", len(inv.NoValue))
	}
	lit := inv.NoValue[0].Literal
	if lit.Val != nil {
		return fmt.Sprintf("the literal with no value holds %v", lit.Val)
	}
	if !slices.Contains(literalsOf(inv.Expr), lit) {
		return fmt.Sprintf("the literal NoValue names is not in the expression %v", inv.Expr)
	}
	return ""
}

// literalsOf lists every literal in e.
func literalsOf(e expr.Expression) []*expr.Literal {
	switch ex := e.(type) {
	case *expr.Literal:
		out := []*expr.Literal{ex}
		if args, ok := ex.Val.([]expr.Expression); ok {
			for _, arg := range args {
				out = append(out, literalsOf(arg)...)
			}
		}
		return out
	case expr.SExpr:
		var out []*expr.Literal
		for _, child := range ex.Children() {
			out = append(out, literalsOf(child)...)
		}
		return out
	}
	return nil
}

// TestParse_AnAnnotationArgumentHoldingRefusedTextKeepsItsSpelling pins that a
// quoted annotation argument holding text the source rules refuse is a bare
// literal with its written spelling, as one that will not unquote is, and
// draws the source rules' diagnostic alone: it never holds a value the source
// did not write.
func TestParse_AnAnnotationArgumentHoldingRefusedTextKeepsItsSpelling(t *testing.T) {
	t.Parallel()
	for name, lit := range map[string]string{
		"an invalid byte":   "\"a\xff\"",
		"a NUL":             "\"a\x00\"",
		"a byte order mark": "\"a\uFEFF\"",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\ntype T {\n\tid String primary @a(" + lit + ")\n}\n"
			f, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Errorf("%d issues, want the source rules' one: %s", len(issues), messages(issues))
			}
			anns := f.Types[0].Properties[0].Annotations
			if len(anns) != 1 || len(anns[0].Args) != 1 {
				t.Fatalf("annotations %+v, want one with one argument", anns)
			}
			if arg := anns[0].Args[0]; arg.Kind != ArgLiteral || arg.Text != lit || arg.Raw != "" {
				t.Errorf("argument %+v, want a bare literal spelled %q", arg, lit)
			}
		})
	}
}
