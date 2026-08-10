package parse

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// propertyLegalKeywords are the eleven reserved spellings that are still legal
// as a property name. They are keywords by position, not by token.
var propertyLegalKeywords = []string{
	"schema", "type", "datatype", "required", "primary",
	"extends", "includes", "abstract", "one", "many", "import",
}

// TestReserved_PropertyNamesAcceptContextualKeywords pins that a keyword the
// grammar needs only in one position stays usable as a name everywhere else.
func TestReserved_PropertyNamesAcceptContextualKeywords(t *testing.T) {
	for _, name := range propertyLegalKeywords {
		t.Run(name, func(t *testing.T) {
			src := fmt.Sprintf("schema \"s\"\ntype T {\n\tid String primary\n\t%s String\n}\n", name)
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Fatalf("%q rejected as a property name: %v", name, issues)
			}
			props := file.Types[0].Properties
			if len(props) != 2 || props[1].Name != name {
				t.Errorf("properties = %+v, want a second named %q", props, name)
			}
		})
	}
}

// TestReserved_PropertyNamesRejectTheSix pins the six spellings that are not
// names anywhere: three the grammar claims outright, and three the lexer's own
// literal rules outrank an identifier with.
func TestReserved_PropertyNamesRejectTheSix(t *testing.T) {
	for name := range propertyNameExclusions {
		t.Run(name, func(t *testing.T) {
			src := fmt.Sprintf("schema \"s\"\ntype T {\n\tid String primary\n\t%s String\n}\n", name)
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) == 0 {
				t.Errorf("%q accepted as a property name, want rejected", name)
			}
		})
	}
}

// TestReserved_TypeNamesRejectDatatypeKeywords pins that a built-in datatype
// name cannot be redeclared as a type.
func TestReserved_TypeNamesRejectDatatypeKeywords(t *testing.T) {
	for name := range datatypeKeywords {
		t.Run(name, func(t *testing.T) {
			src := fmt.Sprintf("schema \"s\"\ntype %s {\n\tid String primary\n}\n", name)
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) == 0 {
				t.Errorf("%q accepted as a type name, want rejected", name)
			}
			if len(file.Types) != 0 {
				t.Errorf("a rejected type declaration still produced %d types", len(file.Types))
			}
		})
	}
}

// TestReserved_ImportAliasRejectsEveryReservedSpelling pins the widest
// exclusion set: an alias may not be any reserved lowercase spelling, nor a
// datatype name.
func TestReserved_ImportAliasRejectsEveryReservedSpelling(t *testing.T) {
	for name := range reservedLC {
		t.Run("lc_"+name, func(t *testing.T) {
			src := fmt.Sprintf("schema \"s\"\nimport \"a.yammm\" as %s\ntype T {\n\tid String primary\n}\n", name)
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) == 0 {
				t.Errorf("alias %q accepted, want rejected", name)
			}
		})
	}
	for name := range datatypeKeywords {
		t.Run("uc_"+name, func(t *testing.T) {
			src := fmt.Sprintf("schema \"s\"\nimport \"a.yammm\" as %s\ntype T {\n\tid String primary\n}\n", name)
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) == 0 {
				t.Errorf("alias %q accepted, want rejected", name)
			}
		})
	}
}

// TestReserved_ModifierLookaheadResolvesTheNextPropertyAmbiguity pins the
// two-token lookahead on the modifier position. Without it a trailing
// 'primary' or 'required' is read as the next property's name, and the
// committed-choice parser cannot back out.
func TestReserved_ModifierLookaheadResolvesTheNextPropertyAmbiguity(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		wantNames    []string
		wantPrimary  []bool
		wantRequired []bool
	}{
		{
			name:         "a trailing modifier is a modifier",
			body:         "id String primary\n\tname String",
			wantNames:    []string{"id", "name"},
			wantPrimary:  []bool{true, false},
			wantRequired: []bool{false, false},
		},
		{
			name:         "a following property named primary is a property",
			body:         "id String\n\tprimary String",
			wantNames:    []string{"id", "primary"},
			wantPrimary:  []bool{false, false},
			wantRequired: []bool{false, false},
		},
		{
			name:         "a following property named required is a property",
			body:         "id String\n\trequired Integer",
			wantNames:    []string{"id", "required"},
			wantPrimary:  []bool{false, false},
			wantRequired: []bool{false, false},
		},
		{
			name:         "a modifier followed by a qualified type is still a modifier",
			body:         "id String required\n\tother a.Thing",
			wantNames:    []string{"id", "other"},
			wantPrimary:  []bool{false, false},
			wantRequired: []bool{true, false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"s\"\ntype T {\n\t" + tc.body + "\n}\n"
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Fatalf("unexpected issues: %v", issues)
			}
			props := file.Types[0].Properties
			if len(props) != len(tc.wantNames) {
				t.Fatalf("got %d properties, want %d", len(props), len(tc.wantNames))
			}
			for i, p := range props {
				if p.Name != tc.wantNames[i] {
					t.Errorf("property %d name = %q, want %q", i, p.Name, tc.wantNames[i])
				}
				if p.IsPrimaryKey != tc.wantPrimary[i] {
					t.Errorf("property %d primary = %v, want %v", i, p.IsPrimaryKey, tc.wantPrimary[i])
				}
				if p.IsRequired != tc.wantRequired[i] {
					t.Errorf("property %d required = %v, want %v", i, p.IsRequired, tc.wantRequired[i])
				}
			}
		})
	}
}

// TestReserved_DatatypeAliasTargetsAreBuiltInOnly pins that an alias may not
// alias another alias, which the grammar enforces by admitting only a built-in
// on the right of '='.
func TestReserved_DatatypeAliasTargetsAreBuiltInOnly(t *testing.T) {
	src := "schema \"s\"\ntype Code = String[1, 8]\ntype Other = Code\ntype T {\n\tid String primary\n}\n"
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) == 0 {
		t.Fatal("an alias targeting another alias was accepted, want rejected")
	}
	if len(file.DataTypes) != 1 || file.DataTypes[0].Name != "Code" {
		t.Errorf("datatypes = %+v, want only Code", file.DataTypes)
	}
}
