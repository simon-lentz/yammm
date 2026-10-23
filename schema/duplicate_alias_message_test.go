package schema_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// duplicateAliasMessage is the diagnostic for a second import under the alias
// d, with each path as the message is to show it.
func duplicateAliasMessage(existing, repeated string) string {
	return "E_DUPLICATE_IMPORT alias \"d\" already used for import " + existing +
		"; use explicit 'as' to disambiguate\n    existing: import " + existing +
		"\n    new:      import " + repeated
}

// TestLoad_ADuplicateAliasShowsEachImportsPath pins the paths the duplicate
// alias diagnostic shows. An import whose path has a value shows that value
// quoted. One whose path literal has no value shows the literal as the source
// wrote it, quotes included, and a byte or a character a message cannot carry
// as its escape; the message never shows an empty path for it. The first
// declaration of the alias is the existing one, whatever the later ones hold.
func TestLoad_ADuplicateAliasShowsEachImportsPath(t *testing.T) {
	t.Parallel()
	siblings := map[string]string{
		"dep.yammm":  "schema \"dep\"\ntype K {\n\tid String primary\n}\n",
		"dep2.yammm": "schema \"dep2\"\ntype K2 {\n\tid String primary\n}\n",
	}
	rows := map[string]struct {
		imports []string
		want    []string
	}{
		"two paths with a value": {
			[]string{`"./dep"`, `'./dep2'`},
			[]string{duplicateAliasMessage(`"./dep"`, `"./dep2"`)},
		},
		"two malformed escapes": {
			[]string{`"./d\xZZ"`, `"./e\xZZ"`},
			[]string{duplicateAliasMessage(`"./d\xZZ"`, `"./e\xZZ"`)},
		},
		"a single-quoted malformed escape, then a path with a value": {
			[]string{`'./d\xZZ'`, `"./dep"`},
			[]string{duplicateAliasMessage(`'./d\xZZ'`, `"./dep"`)},
		},
		"a path with a value, then a malformed escape": {
			[]string{`'./dep'`, `"./e\uD800"`},
			[]string{duplicateAliasMessage(`"./dep"`, `"./e\uD800"`)},
		},
		"two escapes whose value is not UTF-8": {
			[]string{`"./d\xff"`, `"./e\xfe"`},
			[]string{duplicateAliasMessage(`"./d\xff"`, `"./e\xfe"`)},
		},
		"two raw bytes that are not UTF-8": {
			[]string{"\"./\xff\"", "\"./\xfe\""},
			[]string{duplicateAliasMessage(`"./\xff"`, `"./\xfe"`)},
		},
		"two NULs": {
			[]string{"\"./a\x00\"", "\"./b\x00\""},
			[]string{duplicateAliasMessage(`"./a\x00"`, `"./b\x00"`)},
		},
		"two byte order marks": {
			[]string{"\"./a\uFEFF\"", "\"./b\uFEFF\""},
			[]string{duplicateAliasMessage(`"./a\ufeff"`, `"./b\ufeff"`)},
		},
		"a literal holding a character that is not ASCII beside a refused byte": {
			[]string{"\"./é\xff\"", `"./dep"`},
			[]string{duplicateAliasMessage(`"./é\xff"`, `"./dep"`)},
		},
		"a literal holding the replacement character it wrote": {
			[]string{"\"./" + string(rune(0xFFFD)) + "\\xZZ\"", `"./dep"`},
			[]string{duplicateAliasMessage(`"./`+string(rune(0xFFFD))+`\xZZ"`, `"./dep"`)},
		},
		"three imports name the first as the existing one": {
			[]string{`"./d\xZZ"`, `"./dep"`, `"./e\xZZ"`},
			[]string{
				duplicateAliasMessage(`"./d\xZZ"`, `"./dep"`),
				duplicateAliasMessage(`"./d\xZZ"`, `"./e\xZZ"`),
			},
		},
	}
	for name, row := range rows {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var source strings.Builder
			source.WriteString("schema \"s\"\n")
			for _, path := range row.imports {
				source.WriteString("import " + path + " as d\n")
			}
			source.WriteString("type T {\n\tid String primary\n}\n")
			var got []string
			for _, issue := range loadIssues(t, source.String(), siblings) {
				if !strings.HasPrefix(issue, "E_DUPLICATE_IMPORT ") {
					continue
				}
				got = append(got, issue)
				if !utf8.ValidString(issue) || strings.ContainsAny(issue, "\x00\uFEFF") {
					t.Errorf("the message carries a byte no message can show: %q", issue)
				}
				if strings.Contains(issue, `import ""`) {
					t.Errorf("the message shows an empty path: %q", issue)
				}
			}
			if strings.Join(got, "\n--\n") != strings.Join(row.want, "\n--\n") {
				t.Errorf("duplicate alias diagnostics:\n%s\nwant:\n%s", strings.Join(got, "\n--\n"), strings.Join(row.want, "\n--\n"))
			}
		})
	}
}

// TestLoadString_ADuplicateAliasShowsEachImportsPath pins the same message
// where no import is allowed: the alias rules still hold each declaration.
func TestLoadString_ADuplicateAliasShowsEachImportsPath(t *testing.T) {
	t.Parallel()
	source := "schema \"s\"\nimport \"./d\\xZZ\" as d\nimport \"./e\\xZZ\" as d\ntype T {\n\tid String primary\n}\n"
	_, result := schema.LoadString(context.Background(), source, "s.yammm")
	var got []string
	for iss := range result.Issues() {
		if iss.Code() == diag.E_DUPLICATE_IMPORT {
			got = append(got, iss.Code().String()+" "+iss.Message())
		}
	}
	if want := duplicateAliasMessage(`"./d\xZZ"`, `"./e\xZZ"`); len(got) != 1 || got[0] != want {
		t.Errorf("duplicate alias diagnostics:\n%s\nwant:\n%s", strings.Join(got, "\n--\n"), want)
	}
}

// TestBuilder_ADuplicateAliasShowsEachImportsPath pins that a Builder's
// imports, which have no source spelling, show their paths quoted.
func TestBuilder_ADuplicateAliasShowsEachImportsPath(t *testing.T) {
	t.Parallel()
	registry := schema.NewRegistry()
	for _, name := range []string{"one", "two"} {
		dep, result := schema.NewBuilder().
			WithName(name).
			WithSourceID(location.MustNewSourceID("test://"+name+".yammm")).
			AddType("Base").
			WithPrimaryKey("id", schema.NewUUIDConstraint()).
			Done().
			Build()
		if dep == nil || result.HasErrors() {
			t.Fatalf("the imported schema %q does not build: %v", name, result)
		}
		if err := registry.Register(dep); err != nil {
			t.Fatal(err)
		}
	}
	_, result := schema.NewBuilder().
		WithName("entry").
		WithSourceID(location.MustNewSourceID("test://entry.yammm")).
		WithRegistry(registry).
		AddImport("one", "d").
		AddImport("two", "d").
		AddType("T").
		WithPrimaryKey("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	var got []string
	for iss := range result.Issues() {
		if iss.Code() == diag.E_DUPLICATE_IMPORT {
			got = append(got, iss.Code().String()+" "+iss.Message())
		}
	}
	if want := duplicateAliasMessage(`"one"`, `"two"`); len(got) != 1 || got[0] != want {
		t.Errorf("duplicate alias diagnostics:\n%s\nwant:\n%s", strings.Join(got, "\n--\n"), want)
	}
}
