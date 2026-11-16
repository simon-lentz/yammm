package cypher

import (
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

func TestValidateIdentifier_Valid(t *testing.T) {
	t.Parallel()

	valid := []string{
		"publisher_id",
		"Publisher",
		"book_catalog__Publisher",
		"_private",
		"A",
		"x123",
		"ALLCAPS",
		"a_b_c",
	}

	for _, name := range valid {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateIdentifier(name, "test"); err != nil {
				t.Errorf("ValidateIdentifier(%q) returned error: %v", name, err)
			}
		})
	}
}

func TestValidateIdentifier_Invalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"empty", "", ErrEmptyIdentifier},
		{"starts with digit", "123abc", ErrInvalidIdentifier},
		{"contains hyphen", "foo-bar", ErrInvalidIdentifier},
		{"contains space", "foo bar", ErrInvalidIdentifier},
		{"contains dot", "foo.bar", ErrInvalidIdentifier},
		{"reserved MATCH", "MATCH", ErrReservedKeyword},
		{"reserved match lowercase", "match", ErrReservedKeyword},
		{"reserved null", "null", ErrReservedKeyword},
		{"reserved CREATE", "CREATE", ErrReservedKeyword},
		{"reserved RETURN", "RETURN", ErrReservedKeyword},
		{"reserved True mixed", "True", ErrReservedKeyword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateIdentifier(tt.input, "test field")
			if err == nil {
				t.Fatalf("ValidateIdentifier(%q) returned nil; want error", tt.input)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateIdentifier(%q) error = %v; want errors.Is(%v)", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateIdentifier_ContextInMessage(t *testing.T) {
	t.Parallel()
	// The caller-supplied context string must appear in the error so a
	// failure names which field carried the bad identifier.
	err := ValidateIdentifier("", "property 'run_id'")
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); !strings.Contains(msg, "property 'run_id'") {
		t.Errorf("error message %q does not contain context", msg)
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

// Every type whose label is not an identifier is reported, not only the first.
func TestConstraints_EveryInvalidLabelIsReported(t *testing.T) {
	t.Parallel()

	s := loadInline(t, "schema \"g\"\n\ntype T {\n\tid String primary\n}\n\ntype U {\n\tid String primary\n}\n")
	_, result := Config{LabelSeparator: "-"}.Constraints(s)
	if got := result.CodeCounts(diag.Error)[E_NEO4J_INVALID_IDENTIFIER]; got != 2 {
		t.Errorf("%d E_NEO4J_INVALID_IDENTIFIER issues, want one for each of the two types:\n%s", got, result)
	}
}
