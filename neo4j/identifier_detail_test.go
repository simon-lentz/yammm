package neo4j

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

func detailsOf(t *testing.T, res diag.Result, code diag.Code) map[string]string {
	t.Helper()
	for issue := range res.Issues() {
		if issue.Code() != code {
			continue
		}
		out := map[string]string{}
		for _, d := range issue.Details() {
			out[d.Key] = d.Value
		}
		return out
	}
	return nil
}

// The scope site carries the same detail contract as the other five: a type
// detail that is a TYPE, plus the property, format and detail keys.
//
// It attributes an inherited property to its DECLARING scope rather than to
// whichever heir the walk reached first, and it used to render that scope
// straight into the type key. schema.DeclaringScope renders a relation-scoped
// property as a relation NAME, so the key could carry a relation.
//
// Mutation: dropping subj.Property in reportIndexTarget turns this red.
func TestInvalidIdentifier_ScopeSiteMatchesTheContract(t *testing.T) {
	t.Parallel()

	s, err := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"base.yammm":  []byte("schema \"base\"\n\ntype Thing {\n\tid String primary\n\twhere String\n}\n"),
		"entry.yammm": []byte("schema \"entry\"\n\nimport \"./base\" as base\n\ntype Heir extends base.Thing {\n\textra String\n\n\t@@index(where, extra)\n}\n"),
	}, "entry.yammm", schema.WithSourcesOnly(true), schema.WithModuleRoot("."))
	if s.IsZero() {
		t.Fatalf("load: %s", err)
	}
	_, res := indexStatements(t, cypher.Config{}, s)

	got := detailsOf(t, res, cypher.E_NEO4J_INVALID_IDENTIFIER)
	if got == nil {
		t.Fatalf("no E_NEO4J_INVALID_IDENTIFIER for a property named %q: %s", "where", res)
	}
	for _, key := range []string{"format", "detail", "type", "property"} {
		if got[key] == "" {
			t.Errorf("scope-site issue is missing the %q detail: %v", key, got)
		}
	}
	if got["type"] != "Thing" {
		t.Errorf("type detail = %q, want the DECLARING type %q", got["type"], "Thing")
	}
	if got["property"] != "where" {
		t.Errorf("property detail = %q, want %q", got["property"], "where")
	}
	if got["relation"] != "" {
		t.Errorf("a type-scoped property carried a relation detail %q", got["relation"])
	}
}

// Every site emitting the code carries the same detail contract: format and
// detail always, and a schema, a type and a label only when the issue is about
// one. A label setting names none of them; a type's label names all three.
func TestInvalidIdentifier_LabelSitesCarryTheSameContract(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, err := schema.LoadString(ctx, "schema \"A\"\n\ntype C {\n\tid String primary\n}\n", "x.yammm")
	if s.IsZero() {
		t.Fatalf("load: %s", err)
	}
	for _, tc := range []struct {
		name string
		c    cypher.Config
		want map[string]string
	}{
		{"a prefix starting with a digit", cypher.Config{LabelPrefix: "9"}, map[string]string{"schema_name": "", "type": "", "label": ""}},
		{"a label that is a keyword", cypher.Config{LabelSeparator: "S"}, map[string]string{"schema_name": "A", "type": "C", "label": "ASC"}},
	} {
		_, res := diagtest.Issues(t, func() (*graphShape, error) {
			return shapeFor(tc.c, s)
		})
		got := detailsOf(t, res, cypher.E_NEO4J_INVALID_IDENTIFIER)
		if got == nil {
			t.Fatalf("%s: no E_NEO4J_INVALID_IDENTIFIER: %s", tc.name, res)
		}
		for _, key := range []string{"format", "detail"} {
			if got[key] == "" {
				t.Errorf("%s: the issue is missing the %q detail: %v", tc.name, key, got)
			}
		}
		for key, want := range tc.want {
			if got[key] != want {
				t.Errorf("%s: %q detail = %q, want %q", tc.name, key, got[key], want)
			}
		}
	}
}

// Each label break carries its reason as the detail, and the subject at its
// grain: a schema break names the schema alone, a type's label all three.
func TestInvalidIdentifier_EachLabelBreakCarriesItsReason(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src string
		c         cypher.Config
		want      map[string]string
	}{
		{
			"a schema name holding the separator", "schema \"a__B\"\n\ntype C {\n\tid String primary\n}\n",
			cypher.Config{},
			map[string]string{
				"schema_name": "a__B", "type": "", "label": "",
				"detail": `the separator "__" occurs in the name or runs into it from the name's end, so the label reads two ways`,
			},
		},
		{
			"a label that is a keyword", "schema \"A\"\n\ntype C {\n\tid String primary\n}\n",
			cypher.Config{LabelSeparator: "S"},
			map[string]string{"schema_name": "A", "type": "C", "label": "ASC", "detail": "the label is a Cypher reserved keyword"},
		},
		{
			"a separator that is no identifier text", "schema \"A\"\n\ntype C {\n\tid String primary\n}\n",
			cypher.Config{LabelSeparator: "-"},
			map[string]string{"schema_name": "", "detail": "the separator is not ASCII letters, digits and underscores"},
		},
	} {
		s, err := schema.LoadString(t.Context(), tc.src, "x.yammm")
		if s.IsZero() {
			t.Fatalf("%s: load: %s", tc.name, err)
		}
		_, res := diagtest.Issues(t, func() (*graphShape, error) {
			return shapeFor(tc.c, s)
		})
		got := detailsOf(t, res, cypher.E_NEO4J_INVALID_IDENTIFIER)
		if got == nil {
			t.Fatalf("%s: no E_NEO4J_INVALID_IDENTIFIER: %s", tc.name, res)
		}
		for key, want := range tc.want {
			if got[key] != want {
				t.Errorf("%s: %q detail = %q, want %q", tc.name, key, got[key], want)
			}
		}
	}
}
