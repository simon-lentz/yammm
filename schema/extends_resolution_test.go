package schema_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// TestLoad_QualifiedExtends_UndefinedAlias_Errors pins that an `extends` clause whose
// qualifier names no import is reported (E_UNKNOWN_TYPE) at load, not silently dropped.
// A registry-backed load (every Load/LoadString path supplies one) can fully resolve a
// qualified supertype, so an unresolvable one is a genuine error — mirroring how an
// unresolvable qualified relation target is already handled. Silently dropping it would
// strip the child of every inherited member with no diagnostic.
func TestLoad_QualifiedExtends_UndefinedAlias_Errors(t *testing.T) {
	_, res := schema.LoadString(context.Background(),
		"schema \"geo\"\n\ntype Doc extends nope.Base {\n\tid String primary\n}\n", "doc.yammm")
	if !res.HasErrors() || !hasCode(res, diag.E_UNKNOWN_TYPE) {
		t.Fatalf("expected E_UNKNOWN_TYPE for an extends clause with an undefined qualifier; got: %v", res.Err())
	}
}

// TestLoad_QualifiedExtends_MissingType_Errors pins the same rule when the qualifier is a
// real import but the referenced type does not exist in the imported schema.
func TestLoad_QualifiedExtends_MissingType_Errors(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte("schema \"main\"\n\nimport \"base.yammm\" as base\n\ntype Doc extends base.Missing {\n\tid String primary\n}\n"),
		"base.yammm": []byte("schema \"base\"\n\nabstract type Entity {\n\tid String primary\n}\n"),
	}
	_, res := schema.LoadSourcesWithEntry(context.Background(), sources, "main.yammm", ".")
	if !res.HasErrors() || !hasCode(res, diag.E_UNKNOWN_TYPE) {
		t.Fatalf("expected E_UNKNOWN_TYPE for an extends clause referencing a missing imported type; got: %v", res.Err())
	}
}

// TestLoad_QualifiedExtends_Resolvable_OK is the control: a qualified `extends` that DOES
// resolve to an imported type must still load cleanly, so the tightened error path does
// not regress legitimate cross-schema inheritance.
func TestLoad_QualifiedExtends_Resolvable_OK(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte("schema \"main\"\n\nimport \"base.yammm\" as base\n\ntype Doc extends base.Entity {\n\ttitle String required\n}\n"),
		"base.yammm": []byte("schema \"base\"\n\nabstract type Entity {\n\tid String primary\n}\n"),
	}
	_, res := schema.LoadSourcesWithEntry(context.Background(), sources, "main.yammm", ".")
	if res.HasErrors() {
		t.Fatalf("a resolvable qualified extends must load cleanly; got: %v", res.Err())
	}
}
