package schema_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// escaped writes every byte of v as a \xHH escape, so the DSL can write any
// byte string as a literal, whether or not it then accepts it.
func escaped(v string) string {
	var b strings.Builder
	for i := range len(v) {
		fmt.Fprintf(&b, `\x%02x`, v[i])
	}
	return b.String()
}

// jsonKeeps reports whether v survives a JSON round trip byte for byte, which
// is what every generator and wire a string value reaches requires of it.
func jsonKeeps(t *testing.T, v string) bool {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var back string
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	return back == v
}

// TestStringValues_TheDSLAndTheBuilderAcceptWhatJSONKeeps holds the two front
// doors to one rule at five places a schema holds a string value, against a
// third reading: a value is accepted exactly when encoding/json writes it back
// unchanged, as it does every valid UTF-8 string and no other.
func TestStringValues_TheDSLAndTheBuilderAcceptWhatJSONKeeps(t *testing.T) {
	values := []string{"café", "a\xffb", "a\xe2\x82", "\xc3", "a\x00b", "a\uFEFFb", "\U0001F600"}
	pk := schema.NewStringConstraint()
	id := expr.SExpr{expr.Op("$"), expr.NewLiteral("id")}
	places := []struct {
		name  string
		dsl   func(lit string) string
		build func(v string) *schema.Builder
	}{
		{
			"schema name",
			func(lit string) string { return `schema "` + lit + "\"\n\ntype Car {\n\tid String primary\n}\n" },
			func(v string) *schema.Builder {
				return schema.NewBuilder().WithName(v).AddType("Car").WithPrimaryKey("id", pk).Done()
			},
		},
		{
			"enum value",
			func(lit string) string {
				return "schema \"s\"\n\ntype Car {\n\tid String primary\n\tk Enum[\"" + lit + "\", \"zz\"]\n}\n"
			},
			func(v string) *schema.Builder {
				return schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
					WithProperty("k", schema.NewEnumConstraint([]string{v, "zz"})).Done()
			},
		},
		{
			"timestamp format",
			func(lit string) string {
				return "schema \"s\"\n\ntype Car {\n\tid String primary\n\tt Timestamp[\"" + lit + "\"]\n}\n"
			},
			func(v string) *schema.Builder {
				return schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
					WithProperty("t", schema.NewTimestampConstraintFormatted(v)).Done()
			},
		},
		{
			"invariant message",
			func(lit string) string {
				return "schema \"s\"\n\ntype Car {\n\tid String primary\n\t! \"" + lit + "\" id != \"x\"\n}\n"
			},
			func(v string) *schema.Builder {
				return schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
					WithInvariant(v, expr.SExpr{expr.Op("!="), id, expr.NewLiteral("x")}, "").Done()
			},
		},
		{
			"expression literal",
			func(lit string) string {
				return "schema \"s\"\n\ntype Car {\n\tid String primary\n\t! \"m\" id != \"" + lit + "\"\n}\n"
			},
			func(v string) *schema.Builder {
				return schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
					WithInvariant("m", expr.SExpr{expr.Op("!="), id, expr.NewLiteral(v)}, "").Done()
			},
		},
	}
	for _, place := range places {
		for _, v := range values {
			t.Run(fmt.Sprintf("%s %q", place.name, v), func(t *testing.T) {
				want := jsonKeeps(t, v)
				_, lres := schema.LoadString(context.Background(), place.dsl(escaped(v)), "s.yammm")
				if loads := !lres.HasErrors(); loads != want {
					t.Errorf("the DSL loads %q: %v, want %v (%s)", v, loads, want, lres.String())
				}
				s, bres := place.build(v).Build()
				if builds := s != nil && !bres.HasErrors(); builds != want {
					t.Errorf("the Builder builds %q: %v, want %v (%s)", v, builds, want, bres.String())
				}
			})
		}
	}
}

// TestBuilder_RefusesAStringValueNotValidUTF8 pins each refusal's code: the
// one each place's other refusals carry.
func TestBuilder_RefusesAStringValueNotValidUTF8(t *testing.T) {
	pk := schema.NewStringConstraint()
	id := expr.SExpr{expr.Op("$"), expr.NewLiteral("id")}
	cases := []struct {
		name  string
		b     *schema.Builder
		code  diag.Code
		quote string
	}{
		{
			"schema name", schema.NewBuilder().WithName("g\xff").AddType("Car").WithPrimaryKey("id", pk).Done(),
			diag.E_INVALID_NAME, `schema name "g\xff" is not valid UTF-8`,
		},
		{
			"import path", schema.NewBuilder().WithName("s").WithSourceID(location.MustNewSourceID("test://s.yammm")).
				AddImport("a\xff", "a").AddType("Car").WithPrimaryKey("id", pk).Done(),
			diag.E_IMPORT_RESOLVE, `import path "a\xff" is not valid UTF-8`,
		},
		{
			"enum value", schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
				WithProperty("k", schema.NewEnumConstraint([]string{"a\xff", "b"})).Done(),
			diag.E_INVALID_CONSTRAINT, `enum value "a\xff" is not valid UTF-8`,
		},
		{
			"a list's timestamp format", schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
				WithProperty("ts", schema.NewListConstraint(schema.NewTimestampConstraintFormatted("\xff"))).Done(),
			diag.E_INVALID_CONSTRAINT, `timestamp format "\xff" is not valid UTF-8`,
		},
		{
			"invariant message", schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
				WithInvariant("m\xff", expr.SExpr{expr.Op("!="), id, expr.NewLiteral("x")}, "").Done(),
			diag.E_INVALID_INVARIANT, `invariant message "m\xff" in type "Car" is not valid UTF-8`,
		},
		{
			"a []string literal's element", schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
				WithInvariant("m", expr.SExpr{expr.Op("in"), id, expr.NewLiteral([]string{"a", "b\xff"})}, "").Done(),
			diag.E_INVALID_INVARIANT, `holds the string literal "b\xff", which is not valid UTF-8`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, res := tc.b.Build()
			if s != nil || !hasCode(res, tc.code) || !strings.Contains(res.String(), tc.quote) {
				t.Errorf("Build = %v, %s; want %s naming %s", s, res.String(), tc.code, tc.quote)
			}
			if diag.IsImportResolutionCode(tc.code.String()) {
				assertResolutionShape(t, res)
			}
		})
	}
	if err := schema.CheckConstraint(schema.NewEnumConstraint([]string{"a\xff", "b"})); err == nil {
		t.Error("CheckConstraint accepted an enum value that is not valid UTF-8")
	}
}

// TestBuilder_RefusesADocNoDocCommentCanCarry pins that a documentation string
// holding what the source rules refuse, or the "*/" that ends a doc comment,
// is E_SYNTAX at every place the Builder takes one, and that the DSL refuses
// each of them but "*/" in a doc comment too.
func TestBuilder_RefusesADocNoDocCommentCanCarry(t *testing.T) {
	pk := schema.NewStringConstraint()
	id := expr.SExpr{expr.Op("$"), expr.NewLiteral("id")}
	places := map[string]func(doc string) *schema.Builder{
		"schema": func(doc string) *schema.Builder {
			return schema.NewBuilder().WithName("s").WithDocumentation(doc).AddType("Car").WithPrimaryKey("id", pk).Done()
		},
		"type": func(doc string) *schema.Builder {
			return schema.NewBuilder().WithName("s").AddType("Car").WithTypeDocumentation(doc).WithPrimaryKey("id", pk).Done()
		},
		"invariant": func(doc string) *schema.Builder {
			return schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", pk).
				WithInvariant("m", expr.SExpr{expr.Op("!="), id, expr.NewLiteral("x")}, doc).Done()
		},
	}
	refused := map[string]string{
		"a */ b":    `"*/", which ends a doc comment`,
		"a\uFEFFb":  "byte order mark (U+FEFF)",
		"a\x00b":    "NUL character (U+0000)",
		"a\xffb":    "invalid UTF-8 encoding",
		"a\xe2\x82": "invalid UTF-8 encoding",
	}
	for place, build := range places {
		for doc, why := range refused {
			t.Run(fmt.Sprintf("%s %q", place, doc), func(t *testing.T) {
				s, res := build(doc).Build()
				if s != nil || !hasCode(res, diag.E_SYNTAX) || !strings.Contains(res.String(), why) {
					t.Errorf("Build = %v, %s; want E_SYNTAX naming %s", s, res.String(), why)
				}
			})
		}
		for _, doc := range []string{"a /* b", "two\nlines", "tab\there", "\\x00 as text"} {
			t.Run(fmt.Sprintf("%s %q builds", place, doc), func(t *testing.T) {
				if s, res := build(doc).Build(); s == nil || res.HasErrors() {
					t.Errorf("Build refused: %s", res.String())
				}
			})
		}
	}
	for doc := range refused {
		if strings.Contains(doc, "*/") {
			continue
		}
		src := "schema \"s\"\n\n/* " + doc + " */\ntype Car {\n\tid String primary\n}\n"
		if _, res := schema.LoadString(context.Background(), src, "s.yammm"); !hasCode(res, diag.E_SYNTAX) {
			t.Errorf("the DSL loads a doc comment holding %q: %s", doc, res.String())
		}
	}
}

// TestBuilder_RefusesAPatternWritingASurrogate pins the Builder's half of the
// surrogate rule, at Build and at CheckConstraint, in the DSL's words.
func TestBuilder_RefusesAPatternWritingASurrogate(t *testing.T) {
	c := schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`a\x{D800}`)})
	want := `regex pattern "a\\x{D800}" names the surrogate code point U+D800, which no string holds`
	s, res := schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("p", c).Done().Build()
	if s != nil || !hasCode(res, diag.E_INVALID_CONSTRAINT) || !strings.Contains(res.String(), want) {
		t.Errorf("Build = %v, %s; want E_INVALID_CONSTRAINT: %s", s, res.String(), want)
	}
	if err := schema.CheckConstraint(c); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("CheckConstraint = %v, want %s", err, want)
	}
	src := "schema \"s\"\n\ntype Car {\n\tid String primary\n\tp Pattern[\"a\\\\x{D800}\"]\n}\n"
	if _, lres := schema.LoadString(context.Background(), src, "s.yammm"); !strings.Contains(lres.String(), want) {
		t.Errorf("the DSL: %s; want %s", lres.String(), want)
	}
}

// TestBuilder_RefusesAStringValueInsideACallArgument pins that the walk reaches
// a string literal held in an []expr.Expression argument list.
func TestBuilder_RefusesAStringValueInsideACallArgument(t *testing.T) {
	id := expr.SExpr{expr.Op("$"), expr.NewLiteral("id")}
	call := expr.SExpr{expr.Op("StartsWith"), id, expr.NewLiteral([]expr.Expression{expr.NewLiteral("a\xff")}), expr.NewLiteral([]string{}), nil}
	s, res := schema.NewBuilder().WithName("s").AddType("Car").WithPrimaryKey("id", schema.NewStringConstraint()).
		WithInvariant("m", call, "").Done().Build()
	if s != nil || !hasCode(res, diag.E_INVALID_INVARIANT) || !strings.Contains(res.String(), "which is not valid UTF-8") {
		t.Errorf("Build = %v, %s; want E_INVALID_INVARIANT", s, res.String())
	}
}

// TestNewPatternConstraint_ReportsTheFirstFault pins that a later pattern's
// surrogate does not replace an earlier pattern's compile fault.
func TestNewPatternConstraint_ReportsTheFirstFault(t *testing.T) {
	c := schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompilePOSIX("a**"), regexp.MustCompile(`\x{D800}`)})
	if err := schema.CheckConstraint(c); err == nil || !strings.Contains(err.Error(), `"a**"`) {
		t.Errorf("CheckConstraint = %v, want the first pattern's fault", err)
	}
}
