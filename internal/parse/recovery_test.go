package parse

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// TestRecovery_IndependentDefectsAllReport pins the property the whole
// recovery loop exists for: one broken member costs that member alone, so a
// file with several unrelated mistakes reports all of them in one pass.
func TestRecovery_IndependentDefectsAllReport(t *testing.T) {
	src := `schema "s"
type A {
	id String primary
	x @
}
type B {
	id String primary
	y @
}
type C {
	id String primary
}
`
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 2 {
		t.Errorf("got %d issues, want 2", len(issues))
		for _, iss := range issues {
			t.Logf("  %s [%d,%d) %s", iss.Code(), iss.Span().Start.Byte, iss.Span().End.Byte, iss.Message())
		}
	}
	if len(file.Types) != 3 {
		t.Fatalf("got %d types, want 3 — a broken member must not lose its type", len(file.Types))
	}
	for _, want := range []string{"A", "B", "C"} {
		if !hasType(file, want) {
			t.Errorf("type %s did not survive recovery", want)
		}
	}
	if got := len(file.Types[2].Properties); got != 1 {
		t.Errorf("type C has %d properties, want 1 — a later type must be untouched", got)
	}
}

// TestRecovery_GoodMembersSurviveABadOne pins that recovery re-syncs to the
// next member rather than abandoning the body.
func TestRecovery_GoodMembersSurviveABadOne(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\tbroken @\n\tname String\n\tage Integer\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Errorf("got %d issues, want 1", len(issues))
	}
	props := file.Types[0].Properties
	var names []string
	for _, p := range props {
		names = append(names, p.Name)
	}
	if strings.Join(names, ",") != "id,name,age" {
		t.Errorf("surviving properties = %v, want id,name,age", names)
	}
}

// TestRecovery_MalformedNumberOwnsTheDiagnosis pins the override: whatever
// token the parser stopped on, a malformed numeric literal inside the failed
// construct is what gets reported.
func TestRecovery_MalformedNumberOwnsTheDiagnosis(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantText string
		wantAt   string
	}{
		{
			name:     "in a constraint bound, where the parser stops before the literal",
			src:      "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer[0x10, 5]\n}\n",
			wantText: "0x10",
			wantAt:   "0x10",
		},
		{
			name:     "in an expression, where the parser stops on the literal",
			src:      "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" 42abc > 1\n}\n",
			wantText: "42abc",
			wantAt:   "42abc",
		},
		{
			name:     "the first of two in one construct owns it",
			src:      "schema \"s\"\ntype T {\n\tid String primary\n\tn Integer[0x10, 0x20]\n}\n",
			wantText: "0x10",
			wantAt:   "0x10",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
			}
			iss := issues[0]
			if want := InvalidNumberMessage(tc.wantText); iss.Message() != want {
				t.Errorf("message = %q, want %q", iss.Message(), want)
			}
			if got := tc.src[iss.Span().Start.Byte:iss.Span().End.Byte]; got != tc.wantAt {
				t.Errorf("anchored on %q, want %q", got, tc.wantAt)
			}
		})
	}
}

// TestRecovery_SpansAreNeverInverted pins the invariant that keeps a malformed
// input from crashing the parser. The inversion half is asserted by the
// absence of a panic: location.RangeWithBytes rejects an end before its start,
// so an inverted span never returns to be compared and a comparison here would
// be unreachable. The helpers below carry the half that panic cannot cover — a
// span that is ordered and still runs past the end of the source.
func TestRecovery_SpansAreNeverInverted(t *testing.T) {
	sources := []string{
		"", "{", "}", "schema", "schema \"s\"\ntype", "schema \"s\"\ntype T {",
		"schema \"s\"\ntype T {\n\tid\n", "schema \"s\"\ntype T {\n\t}\n\tx String\n}",
		"schema \"s\"\ntype T {\n\t! \"m\"\n}", "schema \"s\"\ntype T {\n\tn Integer[\n}",
		"schema \"s\"\nimport", "schema \"s\"\nimport \"a\" as", "@@@@", "))))",
		"schema \"s\"\ntype T {\n\tv Vector[\n}\n", "schema \"s\"\ntype T {\n\tl List<\n}\n",
		// Relation, extends, annotation-argument and alias shapes: without
		// these the walk below descends into nothing.
		"schema \"s\"\ntype T extends A, {\n\t--> r B\n}\n",
		"schema \"s\"\ntype T extends a.B {\n\t--> r C /back { w Integer }\n\t*-> c D\n}\n",
		"schema \"s\"\ntype T {\n\t--> r\n}\n",
		"schema \"s\"\ntype T {\n\t--> r B /\n}\n",
		"schema \"s\"\ntype T {\n\t*-> c\n}\n",
		"schema \"s\"\ntype T {\n\t--> r B { w Enum[\"a\",\"b\"] required }\n}\n",
		"schema \"s\"\ntype T {\n\tid String primary @a(1, \"x\") @b\n\t@@t(y)\n}\n",
		"schema \"s\"\nimport \"a.yammm\" as al\ntype C = Pattern[\"^a$\"]\ntype T {\n\tx al.Thing\n\ty List<String>[1, 2]\n\tz Timestamp[\"2006\"]\n}\n",
	}
	for _, src := range sources {
		t.Run(src, func(t *testing.T) {
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			assertSpansWithinSource(t, src, issues)
			assertNodeSpansWithinSource(t, src, file)
		})
	}
}

// TestRecovery_UnclosedBodyReportsAtEOF pins that an unterminated type body is
// reported rather than silently accepted.
func TestRecovery_UnclosedBodyReportsAtEOF(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 || issues[0].Code() != diag.E_SYNTAX {
		t.Fatalf("got %v, want one E_SYNTAX", issues)
	}
	if issues[0].Span().Start.Byte != len(src) {
		t.Errorf("anchored at %d, want end of input %d", issues[0].Span().Start.Byte, len(src))
	}
	if len(file.Types) != 1 || len(file.Types[0].Properties) != 1 {
		t.Error("the recovered type lost its content")
	}
}

// TestRecovery_HeaderFailureIsFatalAndFlagged pins the two facts a loader
// needs: the schema has no usable name, and the rest of the file still parsed.
func TestRecovery_HeaderFailureIsFatalAndFlagged(t *testing.T) {
	src := "schema\ntype T {\n\tid String primary\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
	}
	if issues[0].Severity() != diag.Fatal {
		t.Errorf("severity = %s, want fatal", issues[0].Severity())
	}
	if !file.SchemaNameFailed {
		t.Error("SchemaNameFailed = false, want true")
	}
	if len(file.Types) != 1 {
		t.Error("the declaration after the broken header did not survive")
	}
}

func hasType(f *File, name string) bool {
	for _, ty := range f.Types {
		if ty.Name == name {
			return true
		}
	}
	return false
}

func assertSpansWithinSource(t *testing.T, src string, issues []diag.Issue) {
	t.Helper()
	for _, iss := range issues {
		s := iss.Span()
		if s.End.Byte > len(src) {
			t.Errorf("diagnostic span [%d,%d) runs past the %d-byte source", s.Start.Byte, s.End.Byte, len(src))
		}
	}
}

// assertNodeSpansWithinSource walks every span the node tree can hold. A span
// built without spanOf's clamp reaches location.RangeWithBytes and panics, so
// a branch this walk does not reach is a crash nothing is positioned to catch.
func assertNodeSpansWithinSource(t *testing.T, src string, f *File) {
	t.Helper()
	check := func(what string, s location.Span) {
		if s.IsZero() {
			return
		}
		if s.End.Byte > len(src) {
			t.Errorf("%s span [%d,%d) runs past the %d-byte source", what, s.Start.Byte, s.End.Byte, len(src))
		}
	}
	checkRef := func(what string, r *TypeRef) {
		if r == nil {
			return
		}
		check(what, r.Span)
		check(what+" name", r.NameSpan)
	}
	var checkConstraint func(what string, c *Constraint)
	checkConstraint = func(what string, c *Constraint) {
		if c == nil {
			return
		}
		check(what, c.Span)
		if c.Bounds != nil {
			check(what+" bounds", c.Bounds.Span)
			check(what+" min bound", c.Bounds.Min.Span)
			check(what+" max bound", c.Bounds.Max.Span)
		}
		for _, lit := range c.EnumLits {
			check(what+" enum value", lit.Span)
		}
		for _, lit := range c.PatternLits {
			check(what+" pattern", lit.Span)
		}
		if c.FormatLit != nil {
			check(what+" format", c.FormatLit.Span)
		}
		if c.DimsLit != nil {
			check(what+" dimensions", c.DimsLit.Span)
		}
		checkRef(what+" alias", c.Alias)
		checkConstraint(what+" element", c.Elem)
	}
	checkAnnotation := func(what string, a *Annotation) {
		check(what, a.Span)
		check(what+" name", a.NameSpan)
		for _, arg := range a.Args {
			check(what+" argument", arg.Span)
		}
	}
	checkProperty := func(what string, p *Property) {
		check(what, p.Span)
		check(what+" name", p.NameSpan)
		checkConstraint(what+" constraint", p.Constraint)
		for _, a := range p.Annotations {
			checkAnnotation(what+" annotation", a)
		}
	}

	check("file", f.Span)
	check("schema name", f.NameSpan)
	for _, imp := range f.Imports {
		check("import", imp.Span)
		check("import path", imp.PathSpan)
		check("import alias", imp.AliasSpan)
	}
	for _, dt := range f.DataTypes {
		check("datatype", dt.Span)
		check("datatype name", dt.NameSpan)
		checkConstraint("datatype constraint", dt.Constraint)
	}
	for _, ty := range f.Types {
		check("type", ty.Span)
		check("type name", ty.NameSpan)
		for _, ref := range ty.Extends {
			checkRef("supertype", ref)
		}
		for _, p := range ty.Properties {
			checkProperty("property", p)
		}
		for _, rel := range ty.Relations {
			check("relation", rel.Span)
			check("relation name", rel.NameSpan)
			check("relation backref", rel.BackrefSpan)
			checkRef("relation target", rel.Target)
			for _, p := range rel.Properties {
				checkProperty("edge property", p)
			}
		}
		for _, inv := range ty.Invariants {
			check("invariant", inv.Span)
			check("invariant message", inv.MessageSpan)
			check("invariant expression", inv.ExprSpan)
		}
		for _, ann := range ty.Annotations {
			checkAnnotation("type annotation", ann)
		}
	}
}
