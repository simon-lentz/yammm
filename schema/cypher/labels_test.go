package cypher

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// IsIdentifier answers for a name the generated Cypher writes unquoted.
func TestIsIdentifier(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"publisher_id", "Publisher", "book_catalog__Publisher", "_private", "A", "x123"} {
		if !IsIdentifier(name) {
			t.Errorf("IsIdentifier(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "123abc", "foo-bar", "foo bar", "foo.bar", "café", "MATCH", "match", "null", "True"} {
		if IsIdentifier(name) {
			t.Errorf("IsIdentifier(%q) = true, want false", name)
		}
	}
}

func TestCheckLabels_NoCollision(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "multiple_types.yammm")
	if err := (Config{}).CheckLabels(s).Err(); err != nil {
		t.Errorf("CheckLabels returned errors for non-colliding schema: %v", err)
	}
}

// Label composes the label from the name of the schema that declares the type,
// under the configured prefix and separator.
func TestConfig_Label(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "multiple_types.yammm")
	widget, ok := s.Type("Widget")
	if !ok {
		t.Fatal("no type Widget")
	}
	for _, tc := range []struct {
		c    Config
		want string
	}{
		{Config{}, "multi_test__Widget"},
		{Config{LabelSeparator: "_"}, "multi_test_Widget"},
		{Config{LabelPrefix: "app_"}, "app_multi_test__Widget"},
	} {
		if got := tc.c.Label(widget); got != tc.want {
			t.Errorf("%+v.Label(Widget) = %q, want %q", tc.c, got, tc.want)
		}
	}
}

// A label setting that breaks the rule is one issue, however many types the
// schema holds.
func TestConstraints_ALabelSettingIsReportedOnce(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"g\"\n\ntype T {\n\tid String primary\n}\n\ntype U {\n\tid String primary\n}\n")
	_, result := Config{LabelSeparator: "-"}.Constraints(s)
	if got := result.CodeCounts(diag.Error)[E_NEO4J_INVALID_IDENTIFIER]; got != 1 {
		t.Errorf("%d E_NEO4J_INVALID_IDENTIFIER issues, want one for the separator:\n%s", got, result)
	}
}

// Every generator refuses a schema whose labels break the rule, with
// CheckLabels' issues alone: schema "a__B" writes a__B__C for its type C,
// which schema "a" writes for its type B__C. The property match is refused by
// every emitter, so a generator that judged on after CheckLabels would report it.
func TestGenerators_RefuseALabelThatDoesNotDecode(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"a__B\"\n\ntype C {\n\tid String primary\n\tmatch String required @index\n}\n")
	want := Config{}.CheckLabels(s)
	if !want.HasErrors() || !strings.Contains(want.String(), `schema "a__B" composes no node label`) {
		t.Fatalf("CheckLabels = %v, want the schema refused", want)
	}
	constraints, cResult := Config{}.Constraints(s)
	indexes, iResult := Config{}.Indexes(s)
	if constraints != nil || indexes != nil {
		t.Errorf("Constraints = %v, Indexes = %v, want nil for a refused schema", constraints, indexes)
	}
	for _, got := range []diag.Result{cResult, iResult} {
		if got.String() != want.String() {
			t.Errorf("a generator's result =\n%s\nwant CheckLabels' alone =\n%s", got, want)
		}
	}
}
