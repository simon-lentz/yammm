package schema_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// hasCode reports whether res contains an issue with the given code.
func hasCode(res diag.Result, code diag.Code) bool {
	for issue := range res.Issues() {
		if issue.Code() == code {
			return true
		}
	}
	return false
}

// TestCompletion_ConcreteTypeRequiresPrimaryKey pins the schema-level enforcement: a
// concrete (non-abstract, non-part) type with no primary key — own or inherited — is
// rejected at load with E_NO_PRIMARY_KEY, hoisting the graph layer's existing
// E_GRAPH_MISSING_PK check (a node needs identity) to fail-fast at schema-load.
func TestCompletion_ConcreteTypeRequiresPrimaryKey(t *testing.T) {
	_, res := schema.LoadString(context.Background(),
		"schema \"geo\"\n\ntype Tag {\n\tlabel String required\n}\n", "t.yammm")
	if !res.HasErrors() || !hasCode(res, diag.E_NO_PRIMARY_KEY) {
		t.Fatalf("expected E_NO_PRIMARY_KEY for a PK-less concrete type; got: %v", res.Err())
	}
}

// TestCompletion_PrimaryKeyExemptions pins that the rule does NOT fire for the cases a
// primary key is not required: abstract types (not instantiable), part types (embedded,
// no independent identity — PK-less parts are a supported graph feature), and a concrete
// type whose primary key is inherited from a parent.
func TestCompletion_PrimaryKeyExemptions(t *testing.T) {
	cases := map[string]string{
		"abstract_parent_concrete_child_has_own_pk": "schema \"s\"\n\nabstract type Base {\n\tname String required\n}\n\ntype Doc extends Base {\n\tid String primary\n}\n",
		"part_type_is_exempt":                       "schema \"s\"\n\npart type Note {\n\tbody String required\n}\n\ntype Doc {\n\tid String primary\n\t*-> HAS_NOTE (many) Note\n}\n",
		"concrete_inherits_local_pk":                "schema \"s\"\n\nabstract type Base {\n\tid String primary\n}\n\ntype Doc extends Base {\n\ttitle String required\n}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			_, res := schema.LoadString(context.Background(), src, name+".yammm")
			if hasCode(res, diag.E_NO_PRIMARY_KEY) {
				t.Errorf("unexpected E_NO_PRIMARY_KEY for %s; got: %v", name, res.Err())
			}
		})
	}
}

// TestCompletion_PrimaryKeyInheritedCrossSchema is the regression for the cross-schema
// timing: a concrete type whose sole primary key is inherited from an IMPORTED parent
// must not be flagged. The loader resolves imports topologically, so the inherited PK is
// present by the time the presence check runs.
func TestCompletion_PrimaryKeyInheritedCrossSchema(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte("schema \"main\"\n\nimport \"base.yammm\" as base\n\ntype Doc extends base.Entity {\n\ttitle String required\n}\n"),
		"base.yammm": []byte("schema \"base\"\n\nabstract type Entity {\n\tid String primary\n}\n"),
	}
	_, res := schema.LoadSourcesWithEntry(context.Background(), sources, "main.yammm", ".")
	if hasCode(res, diag.E_NO_PRIMARY_KEY) {
		t.Errorf("unexpected E_NO_PRIMARY_KEY for a concrete type inheriting a cross-schema PK; got: %v", res.Err())
	}
}

// TestCompletion_PrimaryKeyDeferredCrossSchemaSupertype_NotFlagged pins the guard that
// keeps the presence check from firing when a concrete type's only supertype is an
// unresolved cross-schema reference — the registry is absent, as during single-file
// analysis before imports are loaded. The primary key may be inherited from the
// not-yet-visible parent, so emitting E_NO_PRIMARY_KEY here would be a false positive:
// resolveTypeRef returns nil for a qualified ref when the registry is nil, so
// hasUnresolvedSupertype skips the type.
func TestCompletion_PrimaryKeyDeferredCrossSchemaSupertype_NotFlagged(t *testing.T) {
	model := &schema.TestModel{
		Name: "main",
		Imports: []*schema.TestImportDecl{
			{Path: "base", Alias: "base"},
		},
		Types: []*schema.TestTypeDecl{
			{
				Name: "Doc",
				Inherits: []*schema.TestASTTypeRef{
					{Qualifier: "base", Name: "Entity"},
				},
				// No own primary key: it would come from base.Entity, which a nil
				// registry cannot resolve yet.
			},
		},
	}

	collector := diag.NewCollector(0)
	schema.TestCompleteModel(model, sourceID(t, "deferred_pk.yammm"), collector, nil, nil)

	if hasCode(collector.Result(), diag.E_NO_PRIMARY_KEY) {
		t.Errorf("unexpected E_NO_PRIMARY_KEY for a concrete type whose supertype is a deferred cross-schema reference; got: %v", collector.Result().Err())
	}
}

// TestCompletion_PrimaryKeyDeferredTransitiveSupertype_NotFlagged pins that the deferral
// guard reaches the FULL inheritance chain, not only direct supertypes. Leaf's own
// supertype is the LOCAL Mid (which resolves), but Mid in turn extends an unresolved
// cross-schema supertype (registry absent), so Leaf's primary key may be inherited from
// the not-yet-visible root. A direct-only guard would resolve Leaf's supertype, see no
// inherited key, and emit a false-positive E_NO_PRIMARY_KEY; the transitive walk skips
// Leaf because the deferral lives one level up.
func TestCompletion_PrimaryKeyDeferredTransitiveSupertype_NotFlagged(t *testing.T) {
	model := &schema.TestModel{
		Name: "main",
		Imports: []*schema.TestImportDecl{
			{Path: "base", Alias: "base"},
		},
		Types: []*schema.TestTypeDecl{
			{
				// Extends an unresolved cross-schema supertype: deferred under a nil
				// registry, so its key (if any) is not yet visible.
				Name: "Mid",
				Inherits: []*schema.TestASTTypeRef{
					{Qualifier: "base", Name: "Entity"},
				},
			},
			{
				// Sole supertype is the LOCAL Mid (resolves), so the deferral is one
				// level up — Leaf's key may come transitively from base.Entity.
				Name: "Leaf",
				Inherits: []*schema.TestASTTypeRef{
					{Qualifier: "", Name: "Mid"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	schema.TestCompleteModel(model, sourceID(t, "deferred_transitive_pk.yammm"), collector, nil, nil)

	if hasCode(collector.Result(), diag.E_NO_PRIMARY_KEY) {
		t.Errorf("unexpected E_NO_PRIMARY_KEY for a concrete type whose key is deferred through a transitive cross-schema supertype; got: %v", collector.Result().Err())
	}
}
