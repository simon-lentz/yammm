package neo4j

import (
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
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
	ctx := context.Background()

	s, lres := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"base.yammm":  []byte("schema \"base\"\n\ntype Thing {\n\tid String primary\n\twhere String\n}\n"),
		"entry.yammm": []byte("schema \"entry\"\n\nimport \"./base\" as base\n\ntype Heir extends base.Thing {\n\textra String\n\n\t@@index(where, extra)\n}\n"),
	}, "entry.yammm", ".", schema.WithSourcesOnly(true))
	if s == nil {
		t.Fatalf("load: %s", lres)
	}
	_, res := New().IndexesForSchema(ctx, s)

	got := detailsOf(t, res, E_NEO4J_INVALID_IDENTIFIER)
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
// detail always, and a type only when the subject really is a type.
func TestInvalidIdentifier_LabelSiteCarriesTheSameContract(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	s, lres := schema.LoadString(ctx, "schema \"«9x\"\n\ntype Person {\n\tid String primary\n}\n", "x.yammm")
	if s == nil {
		t.Fatalf("load: %s", lres)
	}
	_, res := New().ShapeForSchema(ctx, s)

	got := detailsOf(t, res, E_NEO4J_INVALID_IDENTIFIER)
	if got == nil {
		t.Fatalf("no E_NEO4J_INVALID_IDENTIFIER for a label starting with a digit: %s", res)
	}
	for _, key := range []string{"format", "detail", "type", "label"} {
		if got[key] == "" {
			t.Errorf("label-site issue is missing the %q detail: %v", key, got)
		}
	}
	if got["type"] != "Person" {
		t.Errorf("type detail = %q, want the type name %q", got["type"], "Person")
	}
}

// The generated text says what it is. Introspection recovers labels and
// properties; type identity, association-versus-composition and import
// structure are not in a database, so the output names each guess rather than
// looking like a schema that should load.
//
// Mutation: removing the header block turns this red.
func TestInferSchema_SaysItIsAStartingPoint(t *testing.T) {
	t.Parallel()

	dsl := New().InferSchema([]RemoteConstraint{{
		Name:          "c",
		EntityType:    "NODE",
		LabelsOrTypes: []string{"app__Thing"},
		Properties:    []string{"id"},
		Type:          "UNIQUENESS",
	}}, nil, "app")

	for _, want := range []string{
		"STARTING POINT",
		"not expected to load",
		"association or a composition",
		"no index metadata is read",
	} {
		if !strings.Contains(dsl, want) {
			t.Errorf("generated text does not state %q:\n%s", want, dsl)
		}
	}
}
