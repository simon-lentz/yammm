package parse

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// TestParse_ConcurrentCallsShareTheParsersSafely exercises the shared parser
// values that replace per-parse construction. It means nothing without -race,
// which is why the package gate runs with it.
func TestParse_ConcurrentCallsShareTheParsersSafely(t *testing.T) {
	sources := []string{
		smokeSource,
		"schema \"a\"\ntype A {\n\tid String primary\n}\n",
		"schema \"b\"\ntype B {\n\tid String primary\n\t! \"m\" id != nil\n}\n",
		"schema \"c\"\ntype C {\n\tbroken @\n}\n",
		// Pipeline arguments and lambda parameters render through
		// fingerprint.expr's two special cases. Without them the generic
		// branch prints an Expression pointer, so the same source renders
		// differently on every parse and this test is what notices.
		"schema \"d\"\ntype D {\n\tt List<String> primary\n\t! \"m\" t -> Contains(\"x\")\n}\n",
		"schema \"e\"\ntype E {\n\tt List<Integer> primary\n\t! \"m\" t -> Count |$x| { $x > 0 }\n}\n",
		"not a schema at all",
		"",
	}

	var mu sync.Mutex
	first := make(map[int]string, len(sources))
	yammmtest.RunConcurrent(8, 25, func() {
		for i, src := range sources {
			file, issues := Parse([]byte(src), location.NewSourceID("c.yammm"))
			got := render(file, issues)
			mu.Lock()
			if want, seen := first[i]; seen && want != got {
				t.Errorf("source %d parsed differently under concurrency:\n got %s\nwant %s", i, got, want)
			} else if !seen {
				first[i] = got
			}
			mu.Unlock()
		}
	})
}

// TestLex_ConcurrentCallsShareTheDefinitionSafely covers the other entry point
// onto the same shared state.
func TestLex_ConcurrentCallsShareTheDefinitionSafely(t *testing.T) {
	want := len(Lex(smokeSource))
	var mu sync.Mutex
	yammmtest.RunConcurrent(8, 25, func() {
		got := len(Lex(smokeSource))
		mu.Lock()
		defer mu.Unlock()
		if got != want {
			t.Errorf("token count = %d, want %d", got, want)
		}
	})
}

// TestRender_Discriminates keeps the fingerprint above from going vacuous.
// Each pair differs in exactly one thing a shared derivation could corrupt,
// and agrees on everything else, so a fingerprint blind to that thing renders
// the pair alike. The issue-bearing pairs are what cover render's diagnostic
// half, which no clean-parsing pair reaches.
func TestRender_Discriminates(t *testing.T) {
	tests := []struct {
		what, a, b string
	}{
		{
			"span",
			"schema \"a\"\ntype A {\n\tid String primary\n}\n",
			"schema \"a\"\ntype A {\n\n\tid String primary\n}\n",
		},
		{
			"relation kind",
			"schema \"a\"\ntype A {\n\tid String primary\n\t--> R B\n}\n",
			"schema \"a\"\ntype A {\n\tid String primary\n\t*-> R B\n}\n",
		},
		{
			"invariant operator",
			"schema \"a\"\ntype A {\n\tn Integer primary\n\t! \"m\" n > 0\n}\n",
			"schema \"a\"\ntype A {\n\tn Integer primary\n\t! \"m\" n < 0\n}\n",
		},
		{
			"import alias",
			"schema \"a\"\nimport \"x.yammm\" as alpha\ntype A {\n\tid String primary\n}\n",
			"schema \"a\"\nimport \"x.yammm\" as gamma\ntype A {\n\tid String primary\n}\n",
		},
		{
			"annotation name",
			"schema \"a\"\ntype A {\n\tid String primary @index\n}\n",
			"schema \"a\"\ntype A {\n\tid String primary @xndex\n}\n",
		},
		{
			"annotation argument",
			"schema \"a\"\ntype A {\n\tid String primary @vector(16)\n}\n",
			"schema \"a\"\ntype A {\n\tid String primary @vector(32)\n}\n",
		},
		{
			// Both sides must parse clean. Inverted bounds differ in their
			// diagnostic too, so the issue loop alone told the old pair apart.
			"constraint value",
			"schema \"a\"\ntype A {\n\tn Integer[1, 9]\n}\n",
			"schema \"a\"\ntype A {\n\tn Integer[1, 8]\n}\n",
		},
		{
			"datatype name",
			"schema \"a\"\ntype C = String[1, 8]\n",
			"schema \"a\"\ntype D = String[1, 8]\n",
		},
		{
			"datatype constraint",
			"schema \"a\"\ntype C = String[1, 8]\n",
			"schema \"a\"\ntype C = String[1, 9]\n",
		},
		{
			"import path",
			"schema \"a\"\nimport \"x.yammm\" as alpha\ntype A {\n\tid String primary\n}\n",
			"schema \"a\"\nimport \"y.yammm\" as alpha\ntype A {\n\tid String primary\n}\n",
		},
		{
			"abstract flag",
			"schema \"a\"\ntype A {\n\tid String primary\n}\n",
			"schema \"a\"\nabstract type A {\n\tid String primary\n}\n",
		},
		{
			"part flag",
			"schema \"a\"\ntype A {\n\tid String primary\n}\n",
			"schema \"a\"\npart type A {\n\tid String primary\n}\n",
		},
		{
			"extends target",
			"schema \"a\"\ntype A extends B {\n\tid String primary\n}\n",
			"schema \"a\"\ntype A extends C {\n\tid String primary\n}\n",
		},
		{
			"required flag",
			"schema \"a\"\ntype A {\n\tid String primary\n\tn Integer\n}\n",
			"schema \"a\"\ntype A {\n\tid String primary\n\tn Integer required\n}\n",
		},
		{
			"relation multiplicity",
			"schema \"a\"\ntype A {\n\tid String primary\n\t--> R (one) B\n}\n",
			"schema \"a\"\ntype A {\n\tid String primary\n\t--> R (many) B\n}\n",
		},
		{
			"relation target",
			"schema \"a\"\ntype A {\n\tid String primary\n\t--> R B\n}\n",
			"schema \"a\"\ntype A {\n\tid String primary\n\t--> R C\n}\n",
		},
		{
			"edge property",
			"schema \"a\"\ntype A {\n\tid String primary\n\t--> R B { since Timestamp }\n}\n",
			"schema \"a\"\ntype A {\n\tid String primary\n\t--> R B { until Timestamp }\n}\n",
		},
		{
			"annotation detached line",
			"schema \"a\"\ntype A {\n\tid String primary @index\n}\n",
			"schema \"a\"\ntype A {\n\tid String primary\n\t@index\n}\n",
		},
		{
			"invariant message",
			"schema \"a\"\ntype A {\n\tn Integer primary\n\t! \"alpha\" n > 0\n}\n",
			"schema \"a\"\ntype A {\n\tn Integer primary\n\t! \"gamma\" n > 0\n}\n",
		},
		{
			// Reaches fingerprint.expr's ArgsLiteral branch, which no other
			// pair enters.
			"pipeline call arguments",
			"schema \"a\"\ntype A {\n\tt List<String> primary\n\t! \"m\" t -> Contains(\"x\")\n}\n",
			"schema \"a\"\ntype A {\n\tt List<String> primary\n\t! \"m\" t -> Contains(\"y\")\n}\n",
		},
		{
			// Identical bodies: the parameter list is the only difference, so
			// the pair cannot pass on a body difference instead.
			"lambda parameters",
			"schema \"a\"\ntype A {\n\tt List<Integer> primary\n\t! \"m\" t -> Count |$x| { 1 > 0 }\n}\n",
			"schema \"a\"\ntype A {\n\tt List<Integer> primary\n\t! \"m\" t -> Count |$y| { 1 > 0 }\n}\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			fa, ia := Parse([]byte(tc.a), location.NewSourceID("c.yammm"))
			fb, ib := Parse([]byte(tc.b), location.NewSourceID("c.yammm"))
			if render(fa, ia) == render(fb, ib) {
				t.Errorf("fingerprint is blind to %s: two parses differing only there render alike", tc.what)
			}
		})
	}
}

// TestRender_CarriesEveryDiagnostic covers render's issue loop directly. No
// discrimination pair reaches it: two sources whose diagnostics differ differ
// in their trees as well, so the node half alone tells them apart and the loop
// can be deleted with every pair above still green.
func TestRender_CarriesEveryDiagnostic(t *testing.T) {
	src := "schema \"a\"\ntype A {\n\tn Integer[9, 2]\n\tv Vector[0]\n}\n"

	file, issues := Parse([]byte(src), location.NewSourceID("c.yammm"))

	if len(issues) < 2 {
		t.Fatalf("fixture reported %d issues, want at least 2", len(issues))
	}
	got := render(file, issues)
	for _, iss := range issues {
		var span strings.Builder
		fingerprint{out: &span}.span(iss.Span())
		for _, want := range []string{iss.Code().String(), iss.Message(), span.String()} {
			if !strings.Contains(got, want) {
				t.Errorf("fingerprint omits %q for %s", want, iss.Code())
			}
		}
	}
}

// TestRender_CarriesEveryNodeField covers the fields no discrimination pair can
// isolate. Changing a source to flip one of them also moves the spans around
// it, so the pair differs either way and proves nothing about the field. This
// asserts the rendered value appears instead.
func TestRender_CarriesEveryNodeField(t *testing.T) {
	src := "schema \"a\"\n" +
		"import \"x.yammm\" as alpha\n" +
		"abstract type Base {\n\tid String primary\n}\n" +
		"part type A extends Base {\n" +
		"\tid String primary\n" +
		"\tn Integer required\n" +
		"\t@index\n" +
		"\t--> R (many) B { since Timestamp }\n" +
		"}\n"

	file, issues := Parse([]byte(src), location.NewSourceID("c.yammm"))
	if len(issues) != 0 {
		t.Fatalf("fixture must parse clean: %v", issues)
	}
	got := render(file, issues)

	for _, want := range []struct{ what, text string }{
		{"import path", "x.yammm"},
		{"import alias", "alpha"},
		{"abstract flag", "Base/truefalse"},
		{"part flag", "A/falsetrue"},
		{"extends target", "^.Base"},
		{"primary and required flags", "n/falsetrue"},
		{"relation name and multiplicity", "/R/truetrue"},
		{"relation target", ">.B"},
		{"edge property", "since/falsefalse"},
		{"annotation detached line", "@index/false/8"},
	} {
		if !strings.Contains(got, want.text) {
			t.Errorf("fingerprint omits the %s: no %q in\n%s", want.what, want.text, got)
		}
	}
}

// render reduces a parse to a comparable summary, so a concurrency defect
// shows up as a difference rather than as a data race alone. It covers every
// subtree the concurrency corpus parses: a field left out here is a field a
// shared derivation may corrupt with the suite green.
func render(f *File, issues []diag.Issue) string {
	var out strings.Builder
	fp := fingerprint{out: &out}

	out.WriteString(f.Name)
	fp.span(f.NameSpan)
	out.WriteByte('|')
	for _, imp := range f.Imports {
		fmt.Fprintf(&out, "%s/%s/%v", imp.Path, imp.Alias, imp.HasAlias)
		fp.span(imp.Span)
		fp.span(imp.AliasSpan)
		out.WriteByte('.')
	}
	out.WriteByte('|')
	for _, dt := range f.DataTypes {
		out.WriteString(dt.Name)
		fp.span(dt.Span)
		fp.constraint(dt.Constraint)
		out.WriteByte('.')
	}
	out.WriteByte('|')
	for _, ty := range f.Types {
		fmt.Fprintf(&out, "%s/%v%v", ty.Name, ty.IsAbstract, ty.IsPart)
		fp.span(ty.Span)
		for _, ext := range ty.Extends {
			fmt.Fprintf(&out, "^%s.%s", ext.Qualifier, ext.Name)
			fp.span(ext.Span)
		}
		out.WriteByte(':')
		for _, p := range ty.Properties {
			fp.property(p)
		}
		for _, r := range ty.Relations {
			fp.relation(r)
		}
		for _, inv := range ty.Invariants {
			out.WriteString(inv.Message)
			fp.span(inv.MessageSpan)
			fp.span(inv.ExprSpan)
			fp.expr(inv.Expr)
			out.WriteByte('~')
		}
		for _, a := range ty.Annotations {
			fp.annotation(a)
		}
		out.WriteByte(';')
	}
	out.WriteByte('|')
	for _, iss := range issues {
		out.WriteString(iss.Code().String())
		out.WriteByte('=')
		out.WriteString(iss.Message())
		fp.span(iss.Span())
		out.WriteByte(';')
	}
	return out.String()
}

type fingerprint struct{ out *strings.Builder }

func (fp fingerprint) span(s location.Span) {
	fmt.Fprintf(fp.out, "@%d-%d/%d:%d", s.Start.Byte, s.End.Byte, s.Start.Line, s.Start.Column)
}

func (fp fingerprint) property(p *Property) {
	fmt.Fprintf(fp.out, "%s/%v%v", p.Name, p.IsPrimaryKey, p.IsRequired)
	fp.span(p.Span)
	fp.span(p.NameSpan)
	fp.constraint(p.Constraint)
	for _, a := range p.Annotations {
		fp.annotation(a)
	}
	fp.out.WriteByte(',')
}

func (fp fingerprint) relation(r *Relation) {
	fmt.Fprintf(fp.out, "%d/%s/%v%v",
		r.Kind, r.Name, r.Optional, r.Many)
	fp.span(r.Span)
	fp.span(r.NameSpan)
	if r.Target != nil {
		fmt.Fprintf(fp.out, ">%s.%s", r.Target.Qualifier, r.Target.Name)
		fp.span(r.Target.Span)
	}
	fp.out.WriteByte('[')
	for _, ep := range r.Properties {
		fp.property(ep)
	}
	fp.out.WriteString("];")
}

func (fp fingerprint) annotation(a *Annotation) {
	fmt.Fprintf(fp.out, "@%s/%v/%d", a.Name, a.HasParens, a.DetachedFromLine)
	fp.span(a.Span)
	fp.span(a.NameSpan)
	for _, arg := range a.Args {
		fmt.Fprintf(fp.out, "(%d:%s:%s", arg.Kind, arg.Text, arg.Raw)
		fp.span(arg.Span)
		fp.out.WriteByte(')')
	}
	fp.out.WriteByte('!')
}

func (fp fingerprint) constraint(c *Constraint) {
	if c == nil {
		fp.out.WriteString("{}")
		return
	}
	fmt.Fprintf(fp.out, "{%d", c.Kind)
	fp.span(c.Span)
	fmt.Fprintf(fp.out, "%v/%v/%v/%v/%v/%v/%v",
		deref(c.IntMin), deref(c.IntMax), deref(c.FloatMin), deref(c.FloatMax),
		deref(c.LenMin), deref(c.LenMax), deref(c.VectorDims))
	fmt.Fprintf(fp.out, "%v/%v", c.EnumValues(), c.PatternRegexps())
	if f, ok := c.Format(); ok {
		fmt.Fprintf(fp.out, "f%s", f)
	}
	if c.Alias != nil {
		fmt.Fprintf(fp.out, "a%s.%s", c.Alias.Qualifier, c.Alias.Name)
	}
	fp.constraint(c.Elem)
	fp.out.WriteByte('}')
}

// expr renders an expression tree. An argument list is handled before the
// generic branch because its Literal is a slice of Expression pointers, whose
// addresses differ between parses and would make every fingerprint unique.
// Lambda parameters need no such case: their Literal is a []string.
func (fp fingerprint) expr(e expr.Expression) {
	if e == nil {
		fp.out.WriteString("<nil>")
		return
	}
	if args, ok := expr.ArgsLiteral(e); ok {
		fp.out.WriteString("args(")
		for _, a := range args {
			fp.expr(a)
			fp.out.WriteByte(' ')
		}
		fp.out.WriteByte(')')
		return
	}
	fmt.Fprintf(fp.out, "%s|%v", e.Op(), e.Literal())
	if kids := e.Children(); len(kids) > 0 {
		fp.out.WriteByte('(')
		for _, c := range kids {
			fp.expr(c)
			fp.out.WriteByte(' ')
		}
		fp.out.WriteByte(')')
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
