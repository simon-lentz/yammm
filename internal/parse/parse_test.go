package parse

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"unicode"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// TestParsersBuild proves the grammar and rule table compile, which is the
// failure Parse and Lex panic on rather than return. It reads the shared build
// state directly, so the panic-free path costs the package no accessor.
func TestParsersBuild(t *testing.T) {
	lexerOnce.Do(buildParsers)
	if sharedErr != nil {
		t.Fatalf("build parsers: %v", sharedErr)
	}
	if shared == nil {
		t.Fatal("parsers are nil with no error")
	}
}

const smokeSource = `schema "demo"

import "other.yammm" as other

/* A reusable code. */
type Code = String[1, 8]

/* A vehicle. */
abstract type Vehicle {
	/* Its identity. */
	id String primary
	wheels Integer[1, 18] @index
	kind Enum["car", "truck"]
	ratio Float[-1.5, 2.5]
	made Timestamp["2006-01-02"]
	tag Code
	badge other.Badge
	slug Pattern["^[a-z]+$"]
	vec Vector[128]
	tags List<String>[0, 4]
	when Date
	uid UUID
	ok Boolean
	@@audit(owner, "fleet")
	! "wheels must be positive" wheels > 0
}
`

func TestParse_Smoke(t *testing.T) {
	file, issues := Parse([]byte(smokeSource), location.NewSourceID("demo.yammm"))
	if len(issues) != 0 {
		for _, iss := range issues {
			t.Errorf("unexpected %s at %s: %s", iss.Code(), iss.Span(), iss.Message())
		}
	}
	if file.Name != "demo" {
		t.Errorf("Name = %q, want %q", file.Name, "demo")
	}
	if file.SchemaNameFailed {
		t.Error("SchemaNameFailed = true, want false")
	}
	if file.Doc != "" {
		t.Errorf("Doc = %q, want empty", file.Doc)
	}
	if len(file.Imports) != 1 {
		t.Fatalf("Imports = %d, want 1", len(file.Imports))
	}
	imp := file.Imports[0]
	if imp.Path != "other.yammm" || imp.Alias != "other" || !imp.HasAlias {
		t.Errorf("import = %+v, want path other.yammm alias other", imp)
	}
	if len(file.DataTypes) != 1 {
		t.Fatalf("DataTypes = %d, want 1", len(file.DataTypes))
	}
	if got := file.DataTypes[0]; got.Name != "Code" || got.Doc != "A reusable code." {
		t.Errorf("datatype = %+v, want Code with doc", got)
	}
	if len(file.Types) != 1 {
		t.Fatalf("Types = %d, want 1", len(file.Types))
	}
	assertVehicle(t, file.Types[0])
}

func assertVehicle(t *testing.T, ty *TypeDecl) {
	t.Helper()
	if ty.Name != "Vehicle" || !ty.IsAbstract || ty.IsPart {
		t.Errorf("type = %q abstract=%v part=%v", ty.Name, ty.IsAbstract, ty.IsPart)
	}
	if ty.Doc != "A vehicle." {
		t.Errorf("type doc = %q", ty.Doc)
	}
	if len(ty.Properties) != 13 {
		t.Fatalf("Properties = %d, want 13", len(ty.Properties))
	}
	if len(ty.Annotations) != 1 || len(ty.Invariants) != 1 {
		t.Fatalf("Annotations = %d, Invariants = %d, want 1 and 1",
			len(ty.Annotations), len(ty.Invariants))
	}

	id := ty.Properties[0]
	if id.Name != "id" || !id.IsPrimaryKey || id.Doc != "Its identity." {
		t.Errorf("id = %+v", id)
	}
	if id.Constraint.Kind != ConstraintString {
		t.Errorf("id kind = %v, want String", id.Constraint.Kind)
	}

	wheels := ty.Properties[1]
	if got := wheels.Constraint; got.IntMin == nil || *got.IntMin != 1 || got.IntMax == nil || *got.IntMax != 18 {
		t.Errorf("wheels bounds = %+v, want 1..18", got)
	}
	if len(wheels.Annotations) != 1 || wheels.Annotations[0].Name != "index" {
		t.Errorf("wheels annotations = %+v", wheels.Annotations)
	}

	if got := ty.Properties[2].Constraint.EnumValues(); len(got) != 2 || got[0] != "car" || got[1] != "truck" {
		t.Errorf("kind enum = %v, want [car truck]", got)
	}
	if got := ty.Properties[3].Constraint; got.FloatMin == nil || *got.FloatMin != -1.5 {
		t.Errorf("ratio min = %+v, want -1.5", got.FloatMin)
	}
	if got, ok := ty.Properties[4].Constraint.Format(); !ok || got != "2006-01-02" {
		t.Errorf("made format = %q ok=%v", got, ok)
	}
	if got := ty.Properties[5].Constraint; got.Kind != ConstraintAlias || got.Alias.Name != "Code" {
		t.Errorf("tag = %+v, want alias Code", got)
	}
	if got := ty.Properties[6].Constraint; got.Alias == nil || got.Alias.Qualifier != "other" {
		t.Errorf("badge = %+v, want qualified alias", got)
	}
	if got := ty.Properties[7].Constraint.PatternRegexps(); len(got) != 1 {
		t.Errorf("slug patterns = %v, want 1", got)
	}
	if got := ty.Properties[8].Constraint; got.VectorDims == nil || *got.VectorDims != 128 {
		t.Errorf("vec dims = %+v, want 128", got.VectorDims)
	}
	if got := ty.Properties[9].Constraint; got.Elem == nil || got.Elem.Kind != ConstraintString {
		t.Errorf("tags elem = %+v, want String", got.Elem)
	}

	ann := ty.Annotations[0]
	if ann.Name != "audit" || len(ann.Args) != 2 {
		t.Fatalf("type annotation = %+v", ann)
	}
	if ann.Args[0].Kind != ArgIdentifier || ann.Args[0].Text != "owner" {
		t.Errorf("arg 0 = %+v, want identifier owner", ann.Args[0])
	}
	if ann.Args[1].Kind != ArgString || ann.Args[1].Text != "fleet" || ann.Args[1].Raw != `"fleet"` {
		t.Errorf("arg 1 = %+v, want string fleet", ann.Args[1])
	}

	inv := ty.Invariants[0]
	if inv.Message != "wheels must be positive" || inv.Expr == nil {
		t.Errorf("invariant = %+v", inv)
	}
}

// TestParse_AnnotationArgumentsKeepBothIdentifierSpellings covers the two
// identifier branches of arg. ArgIdentifier is the zero Kind, so a branch that
// stops recording its text yields an argument that still reads as an
// identifier and carries "" — which only the text assertion catches.
func TestParse_AnnotationArgumentsKeepBothIdentifierSpellings(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary @audit(Owner, owner)\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	args := file.Types[0].Properties[0].Annotations[0].Args
	if len(args) != 2 {
		t.Fatalf("got %d arguments, want 2", len(args))
	}
	for i, want := range []string{"Owner", "owner"} {
		if args[i].Kind != ArgIdentifier || args[i].Text != want {
			t.Errorf("arg %d = %+v, want identifier %q", i, args[i], want)
		}
		if got := src[args[i].Span.Start.Byte:args[i].Span.End.Byte]; got != want {
			t.Errorf("arg %d span covers %q, want %q", i, got, want)
		}
	}
}

// TestParse_DiscardedInvariantReportsOnlyItsMessage pins the order the two
// checks run in. An invariant whose message will not unquote is dropped, so
// reporting its expression's literal errors first asks the author to fix a
// construct that is being thrown away — a diagnostic the oracle never emits.
func TestParse_DiscardedInvariantReportsOnlyItsMessage(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\\x\" id == 99999999999999999999999999\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d diagnostics, want 1: %v", len(issues), issues)
	}
	if !strings.Contains(issues[0].Message(), "invalid invariant message") {
		t.Errorf("diagnostic = %q, want the message failure", issues[0].Message())
	}
	if got := len(file.Types[0].Invariants); got != 0 {
		t.Errorf("kept %d invariants, want 0", got)
	}
}

// TestParse_CallArgumentsMatchTheGrammarsCommaRule pins both directions of the
// argument-list relaxation. The generated grammar admits one trailing comma
// anywhere in the list, so f(x,) is legal and f(x,,) is not; every row here was
// measured against schema.LoadString.
func TestParse_CallArgumentsMatchTheGrammarsCommaRule(t *testing.T) {
	for _, tc := range []struct {
		call   string
		reject bool
	}{
		{`f(x,,)`, true},
		{`f(,,)`, true},
		{`f(x,)`, false},
		{`f(,)`, false},
		{`f()`, false},
	} {
		src := "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id -> " + tc.call + "\n}\n"
		_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
		if got := len(issues) > 0; got != tc.reject {
			t.Errorf("%s: rejected=%v, want %v (%v)", tc.call, got, tc.reject, issues)
		}
	}
}

// TestParse_BareCommaCallHasNonNilArguments covers the third spelling that
// reaches an empty argument list. exprcomp builds its own through make, and a
// nil slice compares unequal to that, so the differential fails on the call.
func TestParse_BareCommaCallHasNonNilArguments(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id -> lower(,)\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("got %v, want a clean parse", issues)
	}
	lists := argsLiterals(file.Types[0].Invariants[0].Expr)
	if len(lists) != 1 {
		t.Fatalf("found %d argument lists, want 1", len(lists))
	}
	if lists[0] == nil {
		t.Error("the bare-comma call carries a nil argument slice, want an empty one")
	}
}

func TestParse_SpansAreByteExact(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	ty := file.Types[0]
	if got := src[ty.NameSpan.Start.Byte:ty.NameSpan.End.Byte]; got != "T" {
		t.Errorf("type name span covers %q, want %q", got, "T")
	}
	prop := ty.Properties[0]
	if got := src[prop.Span.Start.Byte:prop.Span.End.Byte]; got != "id String primary" {
		t.Errorf("property span covers %q", got)
	}
	if prop.NameSpan.Start.Line != 3 || prop.NameSpan.Start.Column != 2 {
		t.Errorf("property name at line %d col %d, want 3:2",
			prop.NameSpan.Start.Line, prop.NameSpan.Start.Column)
	}
}

func TestParse_NeverReturnsNilFile(t *testing.T) {
	for _, src := range []string{"", "@@@", "schema", "type {"} {
		file, issues := Parse([]byte(src), location.SourceID{})
		if file == nil {
			t.Fatalf("Parse(%q) returned a nil file", src)
		}
		if len(issues) == 0 {
			t.Errorf("Parse(%q) reported no issues", src)
		}
	}
}

func TestParse_ZeroSourceIDIsSupported(t *testing.T) {
	file, issues := Parse([]byte(smokeSource), location.SourceID{})
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	span := file.Types[0].NameSpan
	if !span.Source.IsZero() {
		t.Errorf("span source = %v, want zero", span.Source)
	}
	if span.Start.Byte < 0 || span.Start.Line == 0 {
		t.Errorf("span lost its offsets: %+v", span)
	}
}

// TestParse_DiagnosticsNameNoGrammarTypes pins that user-facing text stays in
// the DSL's vocabulary. participle renders its expected-set as the EBNF of
// this package's own node structs, which names nothing in the language and
// changes shape whenever a struct or a tag is edited.
func TestParse_DiagnosticsNameNoGrammarTypes(t *testing.T) {
	sources := []string{
		"type A {\n\tid String primary\n}\n",
		"schema \"s\"\nimport\n",
		"schema \"s\"\ntype X = \n",
		"schema \"s\"\ntype T {\n\t(\n}\n",
		"schema \"s\"\ntype T {\n\ta Pattern[]\n}\n",
		"schema \"s\"\ntype T {\n\ts String[-1, 2]\n}\n",
		"schema \"s\"\ntype T {\n\t--> rel 123\n}\n",
		"schema \"s\"\nimport \"a.yammm\" as one\n",
		"schema \"s\"\ntype T {\n\tn Integer[99999999999999999999, 5]\n}\n",
		// The Pratt parser's failures are the only ones that reach syntaxErr's
		// participle.Error branch, which passes a message through untouched —
		// the one channel by which a Go type name can leak.
		"schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" +\n}\n",
		"schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id ->\n}\n",
		"schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" 1 +* 2\n}\n",
		"schema \"s\"\ntype T {\n\tf Float[1e999, 2]\n}\n",
		"schema \"s\"\ntype T {\n\ts String[99999999999999999999, 2]\n}\n",
		"schema \"s\"\ntype T {\n\tv Vector(99999999999999999999)\n}\n",
	}
	banned := map[string]string{}
	for _, name := range grammarTypeNames() {
		banned[strings.ToLower(name)] = name
	}
	if len(banned) == 0 {
		t.Fatal("no grammar type names found — the extractor is broken, not the parser")
	}
	for _, src := range sources {
		_, issues := Parse([]byte(src), location.SourceID{})
		if len(issues) == 0 {
			t.Errorf("%q reported nothing, so it guards nothing", src)
		}
		for _, iss := range issues {
			for _, word := range identifierWords(iss.Message()) {
				if name, ok := banned[word]; ok {
					t.Errorf("%q: diagnostic names the Go type %s: %s", src, name, iss.Message())
				}
			}
		}
	}
}

// TestParse_DataTypeConstraintIsForwarded pins that a datatype declaration
// carries the constraint its right-hand side built. The promoted checks run
// inside builtin, so a declaration that stopped forwarding would ship
// String[1, 8] as an unbounded Integer and report no inverted bounds at all.
func TestParse_DataTypeConstraintIsForwarded(t *testing.T) {
	src := "schema \"s\"\ntype Code = String[1, 8]\ntype Bad = Integer[9, 2]\n"

	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))

	if len(file.DataTypes) != 2 {
		t.Fatalf("DataTypes = %d, want 2", len(file.DataTypes))
	}
	code := file.DataTypes[0].Constraint
	if code == nil || code.Kind != ConstraintString {
		t.Fatalf("Code constraint = %+v, want a String", code)
	}
	if code.LenMin == nil || *code.LenMin != 1 || code.LenMax == nil || *code.LenMax != 8 {
		t.Errorf("Code bounds = %+v/%+v, want 1..8", code.LenMin, code.LenMax)
	}
	if len(issues) != 1 || issues[0].Code() != diag.E_INVALID_CONSTRAINT {
		t.Fatalf("got %v, want one E_INVALID_CONSTRAINT for Bad", issues)
	}
}

// TestParse_AnnotationWithArgumentsRecordsItsParens pins Annotation.HasParens
// on the true side. node.go defines false as "no argument list was parsed", so
// a consumer that tells @name from @name(args) reads a wrong flag as bare.
// This package has no annotation registry, so the name here is arbitrary and
// carries no claim that the layer above accepts it.
func TestParse_AnnotationWithArgumentsRecordsItsParens(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary @a(1)\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(file.Types) != 1 {
		t.Fatalf("got %d types, want 1", len(file.Types))
	}
	if got := len(file.Types[0].Properties); got != 1 {
		t.Fatalf("got %d properties, want 1", got)
	}
	anns := file.Types[0].Properties[0].Annotations
	if len(anns) != 1 {
		t.Fatalf("Annotations = %d, want 1", len(anns))
	}
	if !anns[0].HasParens {
		t.Error("HasParens = false for @a(1), want true")
	}
	if len(anns[0].Args) != 1 {
		t.Errorf("Args = %d, want 1", len(anns[0].Args))
	}
}

// TestParse_ImportAliasSpanCoversTheAlias pins AliasSpan to the identifier
// alone. Its other readers check only self-consistency, so a widened span
// would make an LSP rename replace the whole import statement.
func TestParse_ImportAliasSpanCoversTheAlias(t *testing.T) {
	src := "schema \"s\"\nimport \"x.yammm\" as alpha\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(file.Imports) != 1 {
		t.Fatalf("Imports = %d, want 1", len(file.Imports))
	}
	sp := file.Imports[0].AliasSpan
	if got := src[sp.Start.Byte:sp.End.Byte]; got != "alpha" {
		t.Errorf("AliasSpan covers %q, want %q", got, "alpha")
	}
}

// TestParse_DetachedAnnotationRecordsItsPropertyLine pins the mechanism that
// reports an annotation whose written position and grammatical binding
// disagree: a trailing annotation binds to the property above it even across a
// line break, and the tree records which property it left.
func TestParse_DetachedAnnotationRecordsItsPropertyLine(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want int
	}{
		{
			"on its own line",
			"schema \"s\"\ntype T {\n\tid String primary\n\t@index\n}\n",
			3,
		},
		{
			"on the property's line",
			"schema \"s\"\ntype T {\n\tid String primary @index\n}\n",
			0,
		},
		{
			// A bare CR is a line break to internal/source and to the LSP, but
			// not to the lexer, so reading the lexer's own line misses it.
			"on its own line, bare CR",
			"schema \"s\"\rtype T {\r\tid String primary\r\t@index\r}\r",
			3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Fatalf("unexpected issues: %v", issues)
			}
			anns := file.Types[0].Properties[0].Annotations
			if len(anns) != 1 {
				t.Fatalf("Annotations = %d, want 1", len(anns))
			}
			if got := anns[0].DetachedFromLine; got != tc.want {
				t.Errorf("DetachedFromLine = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestParse_InvariantExprSpanCoversTheExpression pins the extent a formatter
// slices to leave an invariant's own spacing untouched. A collapsed span reads
// as an empty string and deletes the expression on rewrite.
func TestParse_InvariantExprSpanCoversTheExpression(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tn Integer primary\n\t! \"m\" n > 0\n}\n"

	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))

	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	inv := file.Types[0].Invariants[0]
	if got := src[inv.ExprSpan.Start.Byte:inv.ExprSpan.End.Byte]; got != "n > 0" {
		t.Errorf("ExprSpan covers %q, want %q", got, "n > 0")
	}
}

// TestParse_EmptyCallArgumentsAreNonNil pins that a zero-argument call carries
// an empty argument slice rather than a nil one, which is what
// exprcomp.VisitArguments builds. cmp and reflect.DeepEqual report the two as
// unequal, so a nil here makes the A/B differential mismatch on every
// zero-argument call and buries the real mismatches.
func TestParse_EmptyCallArgumentsAreNonNil(t *testing.T) {
	// Two sites build the empty list, and only one is reached per spelling:
	// exprListUntil when the parentheses are written, fcall's own fallback
	// when they are not.
	tests := []struct{ name, call string }{
		{"empty parentheses", "id->lower()"},
		{"no parentheses", "id->lower"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" " + tc.call + "\n}\n"

			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))

			if len(issues) != 0 {
				t.Fatalf("unexpected issues: %v", issues)
			}
			lists := argsLiterals(file.Types[0].Invariants[0].Expr)
			if len(lists) != 1 {
				t.Fatalf("found %d argument lists, want 1", len(lists))
			}
			if lists[0] == nil {
				t.Error("a zero-argument call carries a nil argument slice, want an empty one")
			}
			if len(lists[0]) != 0 {
				t.Errorf("argument list = %v, want empty", lists[0])
			}
		})
	}
}

// argsLiterals collects every call-argument slice in an expression tree, in
// the order the walk reaches them.
func argsLiterals(e expr.Expression) [][]expr.Expression {
	var out [][]expr.Expression
	var walk func(expr.Expression)
	walk = func(n expr.Expression) {
		if n == nil {
			return
		}
		if args, ok := expr.ArgsLiteral(n); ok {
			out = append(out, args)
			for _, a := range args {
				walk(a)
			}
			return
		}
		for _, c := range n.Children() {
			walk(c)
		}
	}
	walk(e)
	return out
}

// TestParse_PositionsMatchTheSourceRegistry pins this package's line rule to
// internal/source, which is where every other yammm position comes from and
// what lsputil.SpanToLSPRange ultimately reports. The participle lexer breaks
// lines on "\n" alone, so without normalisation a bare "\r" puts every span on
// line 1 and an editor underlines the wrong line.
func TestParse_PositionsMatchTheSourceRegistry(t *testing.T) {
	sources := []struct{ name, src string }{
		{"LF", "schema \"s\"\ntype T {\n\tid String primary\n}\n"},
		{"CRLF", "schema \"s\"\r\ntype T {\r\n\tid String primary\r\n}\r\n"},
		{"bare CR", "schema \"s\"\rtype T {\r\tid String primary\r}\r"},
		{"mixed", "schema \"s\"\r\ntype T {\r\tid String primary\n}\r\n"},
		{"multi-byte runes", "schema \"s\"\ntype T {\n\tid Enum[\"café\", \"thé\"] primary\n}\n"},
	}
	for _, tc := range sources {
		t.Run(tc.name, func(t *testing.T) {
			id := location.NewSourceID("s.yammm")
			reg := source.NewRegistry()
			if err := reg.Register(id, []byte(tc.src)); err != nil {
				t.Fatal(err)
			}
			file, issues := Parse([]byte(tc.src), id)
			if len(issues) != 0 {
				t.Fatalf("unexpected issues: %v", issues)
			}

			prop := file.Types[0].Properties[0]
			spans := []struct {
				what string
				span location.Span
			}{
				{"schema name", file.NameSpan},
				{"type name", file.Types[0].NameSpan},
				{"property name", prop.NameSpan},
				{"property extent end", prop.Span},
			}
			// Enum literals put a multi-byte rune before a later span on the
			// same line, which is what makes the column math rune-counted.
			for i, lit := range prop.Constraint.EnumLits {
				spans = append(spans, struct {
					what string
					span location.Span
				}{fmt.Sprintf("enum literal %d", i), lit.Span})
			}
			// Both ends, not only starts: an end taking the start's own line
			// and column agrees with the registry on any span within one line.
			for _, s := range spans {
				for _, end := range []struct {
					which string
					pos   location.Position
				}{{"start", s.span.Start}, {"end", s.span.End}} {
					want := reg.PositionAt(id, end.pos.Byte)
					if end.pos.Line != want.Line || end.pos.Column != want.Column {
						t.Errorf("%s %s at byte %d: parse says %d:%d, the registry says %d:%d",
							s.what, end.which, end.pos.Byte,
							end.pos.Line, end.pos.Column, want.Line, want.Column)
					}
				}
			}
		})
	}
}

// TestParse_ElidedTokensNeverReachTheGrammar covers the live elide path. The
// PeekingLexer Parse builds is where participle reads its elide set, so a kind
// missing from it arrives at the grammar and is rejected as an unexpected
// token — and nothing else in this package parses a line comment.
func TestParse_ElidedTokensNeverReachTheGrammar(t *testing.T) {
	src := "// leading\nschema \"s\"\n// between\ntype T {\n\tid String primary // trailing\n}\n"

	file, issues := Parse([]byte(src), location.SourceID{})

	if len(issues) != 0 {
		t.Fatalf("a source carrying line comments reported %v", issues)
	}
	if len(file.Types) != 1 || len(file.Types[0].Properties) != 1 {
		t.Errorf("the comments reached the tree: %+v", file.Types)
	}
}

// TestParse_SyntaxDiagnosticsPinTheirComposedText pins the text syntaxErr
// builds and the construct name each of the four recovery sites supplies. The
// guard above is negative and stays true for text that is empty, constant, or
// labelled with the wrong construct.
func TestParse_SyntaxDiagnosticsPinTheirComposedText(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{
			"schema header",
			"type A {\n\tid String primary\n}\n",
			`unexpected token "type" in the schema header`,
		},
		{
			"import declaration",
			"schema \"s\"\nimport\n",
			`unexpected token "<EOF>" in an import declaration`,
		},
		{
			"declaration",
			"schema \"s\"\ntype X = \n",
			`unexpected token "<EOF>" in a declaration`,
		},
		{
			"type body",
			"schema \"s\"\ntype T {\n\t(\n}\n",
			`unexpected token "(" in a type body`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(tc.src), location.SourceID{})
			var got []string
			for _, iss := range issues {
				if iss.Message() == tc.want {
					return
				}
				got = append(got, iss.Message())
			}
			t.Errorf("no diagnostic reads %q; got %q", tc.want, got)
		})
	}
}

// TestParse_GrammarTypeNamesReachesInterfaces pins that the extractor sees
// exprCapture. It is the one grammar type participle renders into an
// expected-set, and a struct-only walk never reaches it.
func TestParse_GrammarTypeNamesReachesInterfaces(t *testing.T) {
	if !slices.Contains(grammarTypeNames(), "exprCapture") {
		t.Error("grammarTypeNames omits exprCapture, so an expected-set naming it would pass")
	}
}

// identifierWords splits text into lowercased runs of letters and digits, which
// is how participle renders a type name inside an expected-set. Matching whole
// words rather than substrings keeps strC from matching a reported strconv
// error, which no check diagnostic could otherwise carry.
func identifierWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// grammarTypeNames returns every node type reachable from the parser roots.
// Interfaces count: exprCapture is the one grammar type participle renders into
// an expected-set, through its custom node.
func grammarTypeNames() []string {
	seen := map[reflect.Type]bool{}
	var names []string
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if seen[rt] {
			return
		}
		seen[rt] = true
		if rt.Name() != "" && strings.HasSuffix(rt.PkgPath(), "/internal/parse") {
			names = append(names, rt.Name())
		}
		if rt.Kind() != reflect.Struct {
			return
		}
		for f := range rt.Fields() {
			walk(f.Type)
		}
	}
	for _, rt := range grammarRoots() {
		walk(rt)
	}
	return names
}

// TestParse_CategorySeparatesSyntaxFromConstraints pins the contract the
// package doc states for callers: a caller wanting syntax errors alone
// filters on diag.CategorySyntax, which yields the text-level errors and
// excludes the constraint errors. The source below carries one of each, so
// the filter has something to separate.
func TestParse_CategorySeparatesSyntaxFromConstraints(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tn Integer[9, 2]\n\t@\n}\n"
	_, issues := Parse([]byte(src), location.SourceID{})

	var syntax, rest []diag.Issue
	for _, iss := range issues {
		if iss.Code().Category() == diag.CategorySyntax {
			syntax = append(syntax, iss)
			continue
		}
		rest = append(rest, iss)
	}
	if len(syntax) != 1 || syntax[0].Code() != diag.E_SYNTAX {
		t.Errorf("syntax-category issues = %v, want exactly one E_SYNTAX", syntax)
	}
	if len(rest) != 1 || rest[0].Code() != diag.E_INVALID_CONSTRAINT {
		t.Errorf("other issues = %v, want exactly one E_INVALID_CONSTRAINT", rest)
	}
}

// TestParse_ReportsEveryDocumentedCode pins the third code the package doc
// names. E_INVALID_INVARIANT is diag.CategorySchema, so it survives neither
// the category filter above nor a caller enumerating the other two codes — a
// P2a adapter written to a two-code contract drops every malformed literal
// inside an invariant expression.
func TestParse_ReportsEveryDocumentedCode(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want diag.Code
	}{
		{
			"E_SYNTAX",
			"schema \"s\"\ntype T {\n\t@\n}\n",
			diag.E_SYNTAX,
		},
		{
			"E_INVALID_CONSTRAINT",
			"schema \"s\"\ntype T {\n\tn Integer[9, 2]\n}\n",
			diag.E_INVALID_CONSTRAINT,
		},
		{
			"E_INVALID_INVARIANT",
			"schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id != 'ab'\n}\n",
			diag.E_INVALID_INVARIANT,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(tc.src), location.SourceID{})
			if len(issues) != 1 || issues[0].Code() != tc.want {
				t.Fatalf("got %v, want exactly one %s", issues, tc.want)
			}
		})
	}

	if got := diag.E_INVALID_INVARIANT.Category(); got != diag.CategorySchema {
		t.Errorf("E_INVALID_INVARIANT category = %v, want %v — the doc's warning "+
			"about the CategorySyntax filter no longer holds", got, diag.CategorySchema)
	}
}

// TestParse_CallArgumentsAcceptOneBareComma pins the shape the grammar's
// arguments production admits and the list and index forms do not: its optional
// trailing COMMA sits outside the argument group, so "f(,)" is a legal empty
// argument list. Rejecting it stopped a schema that loads today from loading.
func TestParse_CallArgumentsAcceptOneBareComma(t *testing.T) {
	body := func(e string) string {
		return "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" " + e + "\n}\n"
	}
	t.Run("accepted in a call", func(t *testing.T) {
		_, issues := Parse([]byte(body("id -> f(,)")), location.NewSourceID("s.yammm"))
		if len(issues) != 0 {
			t.Errorf("f(,) drew %v, want none", issues)
		}
	})
	for _, tc := range []struct{ name, expr, want string }{
		// The message is pinned because the expression parser's own rejection
		// anchors inside the call, where the member loop's fallback anchors on
		// the closing paren a token later.
		{"two commas in a call", "id -> f(,,)", "expected a closing delimiter after ','"},
		{"a bare comma in a list literal", "[,] != nil", ""},
		{"a bare comma in an index", "id[,] != nil", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(body(tc.expr)), location.NewSourceID("s.yammm"))
			if len(issues) != 1 {
				t.Fatalf("%s drew %d diagnostics, want 1: %v", tc.expr, len(issues), issues)
			}
			if tc.want != "" && issues[0].Message() != tc.want {
				t.Errorf("message = %q, want %q", issues[0].Message(), tc.want)
			}
		})
	}
}

// TestParse_IntegerBoundsRejectAFloat pins the split between the two numeric
// bound sets. An Integer bound is INTEGER or '_'; a Float bound also takes
// FLOAT. Sharing one set let Integer[1.5, 3] parse and then fail a constraint
// check, which moves the diagnostic out of diag.CategorySyntax — the category
// the package doc tells callers to filter on.
func TestParse_IntegerBoundsRejectAFloat(t *testing.T) {
	prop := func(p string) string {
		return "schema \"s\"\ntype T {\n\tid String primary\n\t" + p + "\n}\n"
	}
	t.Run("Integer rejects a float bound as syntax", func(t *testing.T) {
		_, issues := Parse([]byte(prop("n Integer[1.5, 3]")), location.NewSourceID("s.yammm"))
		if len(issues) != 1 {
			t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
		}
		if issues[0].Code() != diag.E_SYNTAX {
			t.Errorf("code = %s, want E_SYNTAX — a constraint code leaves the syntax category",
				issues[0].Code())
		}
	})
	for _, tc := range []struct{ name, prop string }{
		{"Float takes a float bound", "n Float[1.5, 3.5]"},
		{"Float takes an integer bound", "n Float[1, 3]"},
		{"Integer takes an integer bound", "n Integer[1, 3]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(prop(tc.prop)), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Errorf("%s drew %v, want none", tc.prop, issues)
			}
		})
	}
}

// TestParse_InvariantWithAnUnquotableMessageIsNotKept pins that an invariant
// whose message will not unquote is dropped rather than recorded with an empty
// one. The message is what the invariant shows when it fails, so keeping it put
// blank text in front of the user; production drops it here too.
func TestParse_InvariantWithAnUnquotableMessageIsNotKept(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\\x\" id != nil\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
	}
	if len(file.Types) != 1 {
		t.Fatalf("got %d types, want 1", len(file.Types))
	}
	if got := len(file.Types[0].Invariants); got != 0 {
		t.Errorf("recorded %d invariants, want 0 — one was kept with no message", got)
	}
}

// TestParse_ExpressionFailuresCarryAnExtent pins that a Pratt-parser failure
// underlines the token it stopped on. Every such failure reaches syntaxErr's
// participle.Error branch, which reported the bare position — a zero-width
// range highlights no character, so an editor showed nothing at all.
func TestParse_ExpressionFailuresCarryAnExtent(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"a binary operator with no right operand", "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" id >\n}\n"},
		{"an unclosed group", "schema \"s\"\ntype T {\n\tid String primary\n\t! \"m\" ((id\n}\n"},
		{"a datatype declaration whose constraint is junk", "schema \"s\"\ntype A = ?\ntype B {\n\tid String primary\n}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			if len(issues) == 0 {
				t.Fatal("no diagnostic")
			}
			sp := issues[0].Span()
			if sp.Start.Byte >= len(tc.src) {
				t.Fatalf("span starts at %d, past the end of a %d-byte source — the row asserts nothing",
					sp.Start.Byte, len(tc.src))
			}
			if sp.End.Byte <= sp.Start.Byte {
				t.Errorf("span [%d,%d) is zero-width inside the source — nothing is underlined",
					sp.Start.Byte, sp.End.Byte)
			}
		})
	}
}
