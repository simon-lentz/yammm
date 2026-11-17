package mapping

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func loadInline(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), src, "mapping.yammm")
	if err := result.Err(); err != nil {
		t.Fatalf("fixture does not load: %v", err)
	}
	return s
}

// An empty Separator stands for the default, and a configured one replaces it.
func TestLabels_Label(t *testing.T) {
	t.Parallel()
	s := loadInline(t, "schema \"book_catalog\"\n\ntype Publisher {\n\tid String primary\n}\n")
	ty, ok := s.Type("Publisher")
	if !ok {
		t.Fatal("no type Publisher")
	}
	for _, tc := range []struct {
		l    Labels
		want string
	}{
		{Labels{}, "book_catalog__Publisher"},
		{Labels{Separator: "_"}, "book_catalog_Publisher"},
		{Labels{Prefix: "app_"}, "app_book_catalog__Publisher"},
		{Labels{Prefix: "Tenant1_", Separator: "Z"}, "Tenant1_book_catalogZPublisher"},
	} {
		if got := tc.l.Label(ty); got != tc.want {
			t.Errorf("%+v.Label(Publisher) = %q, want %q", tc.l, got, tc.want)
		}
	}
}

// labelBreak is the label rule over raw parts, as Check applies it: the
// setting, then the schema name, then the type and its label.
func labelBreak(prefix, separator, schemaName, typeName string) string {
	l := Labels{Prefix: prefix, Separator: separator}
	sep := l.separator()
	if p := settingProblem(prefix, sep); p != "" {
		return p
	}
	if p := schemaNameProblem(schemaName, sep); p != "" {
		return p
	}
	return IdentifierProblem(prefix + schemaName + sep + typeName)
}

// Each row is one part of the label rule. The decoding part looks at the schema
// name and the separator alone: the type may hold the separator, since a label
// decodes at the separator's first occurrence after the prefix.
func TestLabels_Rule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                         string
		prefix, sep, schemaName, typ string
		refused                      bool
	}{
		{"default", "", "", "book_catalog", "Publisher", false},
		{"the type may hold the separator", "", "", "a", "B__C", false},
		{"a prefix and a separator as configured", "app_", "_x_", "book_catalog", "Publisher", false},
		{"a name ending in an underscore before a letter separator", "", "Z", "geo_", "T", false},
		{"the separator inside the name", "", "", "a__B", "C", true},
		{"a trailing underscore runs into the separator", "", "", "geo_", "T", true},
		{"one underscore as the separator", "", "_", "book_catalog", "Publisher", true},
		{"a name ending in the separator's start", "", "_x_", "a_x", "T", true},
		{"a letter separator the name holds", "", "Z", "Zed", "T", true},
		{"a separator that starts with a digit", "", "9", "app", "T", false},
		{"a composed keyword", "", "S", "A", "C", true},
		{"a prefix that starts with a digit", "9", "", "app", "T", true},
		{"a separator that is no identifier tail", "", "-", "app", "T", true},
	}
	for _, tt := range tests {
		got := labelBreak(tt.prefix, tt.sep, tt.schemaName, tt.typ)
		if (got != "") != tt.refused {
			t.Errorf("%s: labelBreak(%q, %q, %q, %q) = %q, want refused %v",
				tt.name, tt.prefix, tt.sep, tt.schemaName, tt.typ, got, tt.refused)
		}
	}
}

// Check reports each break once at its grain: a label setting once for the
// call, a schema name once per schema whatever its type count, and a type's
// label once per type. A schema with no labelled type composes no label, so
// its name is not judged.
func TestLabels_Check(t *testing.T) {
	t.Parallel()
	twoTypes := "type A {\n\tid String primary\n}\n\ntype B {\n\tid String primary\n}\n"
	tests := []struct {
		name   string
		l      Labels
		src    string
		issues int
		want   string
	}{
		{"a clean schema", Labels{}, "schema \"app\"\n\n" + twoTypes, 0, ""},
		{"a schema name holding the separator", Labels{}, "schema \"a__B\"\n\n" + twoTypes, 1, `schema "a__B" composes no node label`},
		{"a trailing underscore", Labels{}, "schema \"geo_\"\n\n" + twoTypes, 1, `schema "geo_" composes no node label`},
		{"a separator", Labels{Separator: "-"}, "schema \"app\"\n\n" + twoTypes, 1, `separator "-" compose no node label: the separator is not`},
		{"a prefix", Labels{Prefix: "9"}, "schema \"app\"\n\n" + twoTypes, 1, `label prefix "9" and separator "__" compose no node label: the prefix is not`},
		{"a composed keyword", Labels{Separator: "S"}, "schema \"A\"\n\ntype C {\n\tid String primary\n}\n", 1, `type "C" of schema "A" has no node label "ASC"`},
		{"an abstract type only", Labels{}, "schema \"geo_\"\n\nabstract type A {\n\tid String primary\n}\n", 0, ""},
		{"a schema break is reported once, its keyword label too", Labels{Separator: "T"}, "schema \"STAR\"\n\ntype S {\n\tid String primary\n}\n", 1, `schema "STAR" composes no node label`},
	}
	for _, tt := range tests {
		res := tt.l.Check(loadInline(t, tt.src))
		issues := 0
		for issue := range res.Issues() {
			issues++
			if issue.Code() != InvalidIdentifier || issue.Severity() != diag.Error {
				t.Errorf("%s: issue %v, want an %s error", tt.name, issue, InvalidIdentifier)
			}
			if !strings.Contains(issue.Message(), tt.want) {
				t.Errorf("%s: message %q, want it to hold %q", tt.name, issue.Message(), tt.want)
			}
		}
		if issues != tt.issues {
			t.Errorf("%s: %d issues, want %d: %v", tt.name, issues, tt.issues, res)
		}
	}
}

// An imported schema's labels are judged with the entry's.
func TestLabels_CheckJudgesTheClosure(t *testing.T) {
	t.Parallel()
	sources := map[string][]byte{
		"/virtual/main.yammm": []byte("schema \"main\"\n\nimport \"./geo\" as geo\n\ntype Site {\n\tid String primary\n\t--> AT (one) geo.Region\n}\n"),
		"/virtual/geo.yammm":  []byte("schema \"geo_\"\n\ntype Region {\n\tid String primary\n}\n"),
	}
	s, result := schema.LoadSourcesWithEntry(t.Context(), sources, "/virtual/main.yammm", "/virtual")
	if err := result.Err(); err != nil {
		t.Fatalf("fixture does not load: %v", err)
	}
	res := Labels{}.Check(s)
	if !res.HasCode(InvalidIdentifier) || !strings.Contains(res.String(), `schema "geo_"`) {
		t.Fatalf("Check over a closure that imports schema geo_ = %v, want its refusal", res)
	}
}
