package neo4j

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func TestSanitizeIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"book_catalog", "book_catalog"},
		{"geo-regions", "geo_regions"},
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

func TestLabel_Default(t *testing.T) {
	t.Parallel()

	a := New()
	ctx := context.Background()
	tests := []struct {
		schema string
		typ    string
		want   string
	}{
		{"book_catalog", "Publisher", "book_catalog__Publisher"},
		{"geo_regions", "District", "geo_regions__District"},
		{"", "Person", "Person"},
		{"book_catalog", "", ""},
		{"store_promotions", "Promotion", "store_promotions__Promotion"},
		{"catalog_geo", "PublisherRegionLink", "catalog_geo__PublisherRegionLink"},
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
	got := a.Label(context.Background(), "book_catalog", "Publisher")
	want := "book_catalog_Publisher"
	if got != want {
		t.Errorf("Label with custom separator = %q; want %q", got, want)
	}
}

func TestLabel_WithPrefix(t *testing.T) {
	t.Parallel()

	a := New(WithLabelPrefix("app_"))
	got := a.Label(context.Background(), "book_catalog", "Publisher")
	want := "app_book_catalog__Publisher"
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
		{"geo-regions", "District", "geo_regions__District"},
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

	s, result := schema.Load(context.Background(), filepath.Join("testdata", "multiple_types.yammm"))
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
	// "Foo-Bar" and "Foo_Bar" both sanitize to label "collide__Foo_Bar".
	s := collidingSchema(t)

	result := New().DetectLabelCollisions(context.Background(), s)
	if !result.HasErrors() {
		t.Fatal("expected a collision error for types Foo-Bar and Foo_Bar")
	}

	found := false
	for issue := range result.Errors() {
		if issue.Code() != E_NEO4J_LABEL_COLLISION {
			continue
		}
		found = true
		for _, typeName := range []string{"Foo-Bar", "Foo_Bar"} {
			if !strings.Contains(issue.Message(), typeName) {
				t.Errorf("collision message should name type %q: %s", typeName, issue.Message())
			}
		}
	}
	if !found {
		t.Error("expected an E_NEO4J_LABEL_COLLISION issue")
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

	// The registry holds the Cypher 5 reserved words; assert a floor so a
	// refactor that accidentally truncates the set fails loudly.
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
