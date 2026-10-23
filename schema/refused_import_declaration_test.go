package schema_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestLoadString_AnImportWithNoPathValueIsStillADeclaration pins that an
// import whose path literal has no value is counted as an import where a
// context takes none, with or without an alias, for each of the three ways a
// path has no value. The refusal counts these declarations, and the path's own
// diagnostic stands beside it.
func TestLoadString_AnImportWithNoPathValueIsStillADeclaration(t *testing.T) {
	t.Parallel()
	const (
		notAllowed = "E_IMPORT_NOT_ALLOWED import declarations are not allowed in this context"
		rawByte    = "E_SYNTAX invalid UTF-8 encoding"
		badEscape  = "E_SYNTAX invalid import path: unquote string: the value its escapes write is not valid UTF-8"
		noEscape   = "E_SYNTAX invalid import path: unquote string: invalid syntax"
	)
	paths := map[string]struct{ literal, own string }{
		"a raw invalid byte":          {"\"./\xff\"", rawByte},
		"an escape that is not UTF-8": {`"./d\xff"`, badEscape},
		"a malformed escape":          {`"./d\xZZ"`, noEscape},
	}
	for name, path := range paths {
		for _, alias := range []string{"", " as d"} {
			for count := 1; count <= 2; count++ {
				t.Run(name+alias+strings.Repeat(", twice", count-1), func(t *testing.T) {
					t.Parallel()
					var imports strings.Builder
					for i := range count {
						imports.WriteString("import " + path.literal)
						if alias != "" {
							imports.WriteString(alias + strconv.Itoa(i))
						}
						imports.WriteString("\n")
					}
					source := "schema \"s\"\n" + imports.String() + "type T {\n\tid String primary\n}\n"
					_, result := schema.LoadString(context.Background(), source, "s.yammm")
					var got []string
					counted := ""
					for iss := range result.Issues() {
						got = append(got, iss.Code().String()+" "+iss.Message())
						if iss.Code() == diag.E_IMPORT_NOT_ALLOWED {
							if line := iss.Span().Start.Line; line != 2 {
								t.Errorf("the refusal is at line %d, want 2, the first import declaration", line)
							}
							for _, d := range iss.Details() {
								if d.Key == diag.DetailKeyImportCount {
									counted = d.Value
								}
							}
						}
					}
					want := []string{notAllowed}
					for range count {
						want = append(want, path.own)
					}
					slices.Sort(got)
					slices.Sort(want)
					if !slices.Equal(got, want) {
						t.Errorf("issues:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
					}
					if wantCount := strconv.Itoa(count); counted != wantCount {
						t.Errorf("the refusal counts %q imports, want %s", counted, wantCount)
					}
				})
			}
		}
	}
}

// TestLoad_ImportsWithNoPathValueAndNoAliasDrawTheirOwnDiagnosticsAlone pins
// that two imports with no path value and no alias, which name nothing a
// reference could use, draw the two path diagnostics and no report about an
// alias.
func TestLoad_ImportsWithNoPathValueAndNoAliasDrawTheirOwnDiagnosticsAlone(t *testing.T) {
	t.Parallel()
	source := "schema \"s\"\nimport \"./\xff\"\nimport \"./\xfe\"\ntype T {\n\tid String primary\n}\n"
	got := loadIssues(t, source, nil)
	if want := []string{invalidUTF8, invalidUTF8}; !slices.Equal(got, want) {
		t.Errorf("issues:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// TestBuilder_AnImportWithAnEmptyAliasIsRefused pins that the alias rules
// still read a Builder's import whose alias is empty: its path has a value, so
// it is no declaration the completer passes over.
func TestBuilder_AnImportWithAnEmptyAliasIsRefused(t *testing.T) {
	t.Parallel()
	dep, depResult := schema.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Base").
		WithPrimaryKey("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	if dep == nil || depResult.HasErrors() {
		t.Fatalf("the imported schema does not build: %v", depResult)
	}
	registry := schema.NewRegistry()
	if err := registry.Register(dep); err != nil {
		t.Fatal(err)
	}
	built, result := schema.NewBuilder().
		WithName("entry").
		WithSourceID(location.MustNewSourceID("test://entry.yammm")).
		WithRegistry(registry).
		AddImport("common", "").
		AddType("T").
		WithPrimaryKey("id", schema.NewUUIDConstraint()).
		Done().
		Build()
	if built != nil || !result.HasCode(diag.E_INVALID_ALIAS) {
		t.Errorf("Build = %v, %v; want no schema and E_INVALID_ALIAS", built, result)
	}
}

// TestLoad_ARefusedImportsAliasIsStillDeclared pins that an alias written on
// an import with no path value is held to the alias rules: a second import
// under the same alias is a duplicate.
func TestLoad_ARefusedImportsAliasIsStillDeclared(t *testing.T) {
	t.Parallel()
	source := "schema \"s\"\nimport \"./\xff\" as b\nimport \"./\xfe\" as b\ntype T {\n\tid String primary\n}\n"
	got := loadIssues(t, source, nil)
	duplicates := 0
	for _, issue := range got {
		if strings.HasPrefix(issue, "E_DUPLICATE_IMPORT ") {
			duplicates++
		}
	}
	if duplicates != 1 || len(got) != 3 {
		t.Errorf("issues:\n  %s\nwant the two bytes' diagnostics and one E_DUPLICATE_IMPORT", strings.Join(got, "\n  "))
	}
}
