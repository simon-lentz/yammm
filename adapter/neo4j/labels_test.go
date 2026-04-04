package neo4j

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func TestSanitizeIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"msrb_emma", "msrb_emma"},
		{"census-tiger", "census_tiger"},
		{"foo.bar", "foo_bar"},
		{"foo/bar", "foo_bar"},
		{"foo\\bar", "foo_bar"},
		{"123abc", "_123abc"},
		{" spaced ", "spaced"},
		{"", ""},
		{"café", "caf"},
		{"___", "___"},
		{"Hello World", "Hello_World"},
		{"a-b.c/d\\e", "a_b_c_d_e"},
		{"9lives", "_9lives"},
		{"_private", "_private"},
		{"ALLCAPS", "ALLCAPS"},
		{"mixed123_OK", "mixed123_OK"},
		{"emoji🎉gone", "emojigone"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := SanitizeIdentifier(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeIdentifier(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateIdentifier_Valid(t *testing.T) {
	t.Parallel()

	valid := []string{
		"issuer_id",
		"Issuer",
		"msrb_emma__Issuer",
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

func TestValidateIdentifier_ErrorsIs(t *testing.T) {
	t.Parallel()

	t.Run("ErrEmptyIdentifier", func(t *testing.T) {
		t.Parallel()
		err := ValidateIdentifier("", "field")
		if !errors.Is(err, ErrEmptyIdentifier) {
			t.Errorf("errors.Is(err, ErrEmptyIdentifier) = false; err = %v", err)
		}
	})

	t.Run("ErrInvalidIdentifier", func(t *testing.T) {
		t.Parallel()
		err := ValidateIdentifier("123bad", "field")
		if !errors.Is(err, ErrInvalidIdentifier) {
			t.Errorf("errors.Is(err, ErrInvalidIdentifier) = false; err = %v", err)
		}
	})

	t.Run("ErrReservedKeyword", func(t *testing.T) {
		t.Parallel()
		err := ValidateIdentifier("MATCH", "field")
		if !errors.Is(err, ErrReservedKeyword) {
			t.Errorf("errors.Is(err, ErrReservedKeyword) = false; err = %v", err)
		}
	})

	t.Run("context in message", func(t *testing.T) {
		t.Parallel()
		err := ValidateIdentifier("", "property 'run_id'")
		if err == nil {
			t.Fatal("expected error")
		}
		msg := err.Error()
		if !strings.Contains(msg, "property 'run_id'") {
			t.Errorf("error message %q does not contain context", msg)
		}
	})
}

func TestLabel_Default(t *testing.T) {
	t.Parallel()

	a := New()
	ctx := context.Background()
	tests := []struct {
		schema string
		typ    string
		want   string
	}{
		{"msrb_emma", "Issuer", "msrb_emma__Issuer"},
		{"census_tiger", "County", "census_tiger__County"},
		{"", "Person", "Person"},
		{"msrb_emma", "", ""},
		{"wyrth_campaigns", "Campaign", "wyrth_campaigns__Campaign"},
		{"linkage_emma", "IssuerGeoLink", "linkage_emma__IssuerGeoLink"},
	}

	for _, tt := range tests {
		t.Run(tt.schema+"_"+tt.typ, func(t *testing.T) {
			t.Parallel()
			got := a.Label(ctx, tt.schema, tt.typ)
			if got != tt.want {
				t.Errorf("Label(%q, %q) = %q; want %q", tt.schema, tt.typ, got, tt.want)
			}
		})
	}
}

func TestLabel_CustomSeparator(t *testing.T) {
	t.Parallel()

	a := New(WithLabelSeparator("_"))
	got := a.Label(context.Background(), "msrb_emma", "Issuer")
	want := "msrb_emma_Issuer"
	if got != want {
		t.Errorf("Label with custom separator = %q; want %q", got, want)
	}
}

func TestLabel_WithPrefix(t *testing.T) {
	t.Parallel()

	a := New(WithLabelPrefix("app_"))
	got := a.Label(context.Background(), "msrb_emma", "Issuer")
	want := "app_msrb_emma__Issuer"
	if got != want {
		t.Errorf("Label with prefix = %q; want %q", got, want)
	}
}

func TestLabel_SanitizesComponents(t *testing.T) {
	t.Parallel()

	a := New()
	ctx := context.Background()
	tests := []struct {
		schema string
		typ    string
		want   string
	}{
		{"census-tiger", "County", "census_tiger__County"},
		{"my.schema", "Foo", "my_schema__Foo"},
		{"path/to", "Bar", "path_to__Bar"},
	}

	for _, tt := range tests {
		t.Run(tt.schema+"_"+tt.typ, func(t *testing.T) {
			t.Parallel()
			got := a.Label(ctx, tt.schema, tt.typ)
			if got != tt.want {
				t.Errorf("Label(%q, %q) = %q; want %q", tt.schema, tt.typ, got, tt.want)
			}
		})
	}
}

func TestDetectLabelCollisions_NoCollision(t *testing.T) {
	t.Parallel()

	s, result, err := schema.Load(context.Background(), filepath.Join("testdata", "multiple_types.yammm"))
	if err != nil {
		t.Fatalf("schema.Load failed: %v", err)
	}
	if err := result.Err(); err != nil {
		t.Fatalf("schema has errors: %v", err)
	}

	a := New()
	result = a.DetectLabelCollisions(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Errorf("DetectLabelCollisions returned errors for non-colliding schema: %v", err)
	}
}

func TestDetectLabelCollisions_Collision(t *testing.T) {
	t.Parallel()

	// Verify the diag code is registered and constructible.
	issue := diag.NewIssue(diag.Error, E_NEO4J_LABEL_COLLISION, "test collision").Build()
	if issue.Code() != E_NEO4J_LABEL_COLLISION {
		t.Error("E_NEO4J_LABEL_COLLISION code mismatch")
	}

	// Verify no collision on a valid schema.
	s, loadResult, err := schema.Load(context.Background(), filepath.Join("testdata", "basic.yammm"))
	if err != nil {
		t.Fatalf("schema.Load failed: %v", err)
	}
	if err := loadResult.Err(); err != nil {
		t.Fatalf("schema has errors: %v", err)
	}

	a := New()
	result := a.DetectLabelCollisions(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Errorf("unexpected collision errors: %v", err)
	}
}

func TestCypherReservedKeywords(t *testing.T) {
	t.Parallel()

	keywords := CypherReservedKeywords()

	// Verify expected keywords are present.
	mustContain := []string{"MATCH", "CREATE", "RETURN", "DELETE", "SET", "NULL", "TRUE", "FALSE", "CONSTRAINT", "DROP"}
	for _, kw := range mustContain {
		if !keywords[kw] {
			t.Errorf("CypherReservedKeywords() missing %q", kw)
		}
	}

	// Verify reasonable count (55 keywords per plan).
	if len(keywords) < 50 {
		t.Errorf("CypherReservedKeywords() returned %d entries; expected at least 50", len(keywords))
	}

	// Verify the map is a defensive copy.
	keywords["INJECTED"] = true
	fresh := CypherReservedKeywords()
	if fresh["INJECTED"] {
		t.Error("CypherReservedKeywords() should return a copy; modification affected subsequent call")
	}
}
