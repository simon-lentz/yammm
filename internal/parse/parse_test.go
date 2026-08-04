package parse

import (
	"reflect"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
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
	}
	names := grammarTypeNames()
	if len(names) == 0 {
		t.Fatal("no grammar type names found — the extractor is broken, not the parser")
	}
	for _, src := range sources {
		_, issues := Parse([]byte(src), location.SourceID{})
		if len(issues) == 0 {
			t.Errorf("%q reported nothing, so it guards nothing", src)
		}
		for _, iss := range issues {
			msg := strings.ToLower(iss.Message())
			for _, name := range names {
				if strings.Contains(msg, strings.ToLower(name)) {
					t.Errorf("%q: diagnostic names the Go type %s: %s", src, name, iss.Message())
				}
			}
		}
	}
}

// grammarTypeNames returns every node struct reachable from the parser roots.
// participle title-cases them in its EBNF, so callers compare case-insensitively.
func grammarTypeNames() []string {
	seen := map[reflect.Type]bool{}
	var names []string
	var walk func(reflect.Type)
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Ptr || rt.Kind() == reflect.Slice {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		if strings.HasSuffix(rt.PkgPath(), "/internal/parse") {
			names = append(names, rt.Name())
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
