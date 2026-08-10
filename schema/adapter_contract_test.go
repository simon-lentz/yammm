package schema_test

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Two properties of the parse-to-decl adapter that every other test in this
// package leaves free. Both were found by mutating the adapter and watching the
// whole suite stay green, so both are here because nothing else sees them.

// A datatype reference's span covers the whole reference, qualifier included.
// The LSP navigates from it, so narrowing it to the name alone would move
// go-to-definition off the qualifier without failing anything else.
func TestAdapter_DataTypeRefSpanCoversTheQualifier(t *testing.T) {
	t.Parallel()

	const src = "schema \"main\"\n" +
		"type Email = String[3, 200]\n" +
		"type Person {\n" +
		"\tid String primary\n" +
		"\taddress Email\n" +
		"}\n"

	s, res := schema.LoadString(context.Background(), src, "ref.yammm")
	if s == nil {
		t.Fatalf("fixture does not load: %v", res)
	}

	prop := findProperty(t, s, "Person", "address")
	ref := prop.DataTypeRef()
	if ref.IsZero() {
		t.Fatal("an alias-typed property carries no DataTypeRef")
	}

	span := ref.Span()
	got := src[span.Start.Byte:span.End.Byte]
	if got != "Email" {
		t.Errorf("DataTypeRef span slices to %q, want the whole reference %q", got, "Email")
	}
}

// The qualified form is what distinguishes the whole reference from the name:
// narrowing the span drops "types." and nothing else notices. It runs through
// the parser rather than a load, because an import cannot resolve from a string.
func TestAdapter_DataTypeRefSpanCoversTheQualifier_Qualified(t *testing.T) {
	t.Parallel()

	const src = "schema \"main\"\n" +
		"import \"types.yammm\" as types\n" +
		"type Person {\n" +
		"\tid String primary\n" +
		"\taddress types.Email\n" +
		"}\n"

	parser := schema.TestNewParser(location.MustNewSourceID("test://ref.yammm"), diag.NewCollector(0))
	model := parser.Parse([]byte(src))
	if model == nil || len(model.Types) != 1 || len(model.Types[0].Properties) != 2 {
		t.Fatalf("fixture did not parse into one type with two properties: %+v", model)
	}

	ref := model.Types[0].Properties[1].DataTypeRef
	if ref.IsZero() {
		t.Fatal("the qualified reference recorded no DataTypeRef")
	}
	span := ref.Span()
	if got := src[span.Start.Byte:span.End.Byte]; got != "types.Email" {
		t.Errorf("DataTypeRef span slices to %q, want the whole reference %q", got, "types.Email")
	}
}

// A source that names no usable schema stops before completion. Dropping that
// short-circuit hands a nameless model to the completer, which reports its own
// findings on top of the one defect the author has to fix first.
//
// The body below is deliberately incomplete — no primary key, an unknown
// datatype — because a body that completes cleanly cannot tell a load that
// stopped early from one that ran completion and found nothing.
func TestAdapter_NoSchemaNameStopsBeforeCompletion(t *testing.T) {
	t.Parallel()

	const src = "type Alpha {\n\tid Missing\n\tname String required\n}\n"

	s, res := schema.LoadString(context.Background(), src, "headerless.yammm")
	if s != nil {
		t.Fatal("a source with no schema name must not produce a schema")
	}

	var codes []string
	for iss := range res.Issues() {
		codes = append(codes, iss.Code().String())
		if iss.Code() != diag.E_SYNTAX {
			t.Errorf("completion ran on a nameless model: %s %s", iss.Code(), iss.Message())
		}
	}
	if len(codes) != 1 {
		t.Errorf("diagnostics = %v, want exactly one E_SYNTAX", codes)
	}
	if !strings.Contains(strings.Join(codes, ","), "E_SYNTAX") {
		t.Errorf("diagnostics = %v, want the header syntax error", codes)
	}
}

func findProperty(t *testing.T, s *schema.Schema, typeName, propName string) *schema.Property {
	t.Helper()
	for _, typ := range s.TypesSlice() {
		if typ.Name() != typeName {
			continue
		}
		for _, p := range typ.PropertiesSlice() {
			if p.Name() == propName {
				return p
			}
		}
	}
	t.Fatalf("property %s.%s not found", typeName, propName)
	return nil
}
