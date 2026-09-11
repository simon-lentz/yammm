package schema_test

import (
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// TestCaptureSources_KeepsAFailedLoadsSources holds the option to what a caller
// uses it for: a load that fails returns no schema, and the sources it captured
// still serve the failure's excerpt.
func TestCaptureSources_KeepsAFailedLoadsSources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "bad.yammm")
	const content = "schema \"bad\"\n\ntype T {\n\tid String primary\n\tname Strin\n}\n"
	writeHostPathFile(t, p, content)

	var captured *schema.Sources
	s, res := schema.Load(t.Context(), p, schema.WithModuleRoot(dir), schema.CaptureSources(&captured))
	if s != nil || res.OK() {
		t.Fatalf("the fixture loaded: schema %v, result %v", s, res)
	}
	var issue diag.Issue
	for is := range res.Issues() {
		if is.Code() == diag.E_UNKNOWN_TYPE && is.HasSpan() {
			issue = is
		}
	}
	if !issue.HasSpan() {
		t.Fatalf("no spanned E_UNKNOWN_TYPE in %v", issueCodes(res))
	}
	if captured == nil {
		t.Fatal("the load captured no sources")
	}
	if got, ok := captured.Content(issue.Span()); !ok || string(got) != content {
		t.Errorf("the captured sources serve %q, %t for the failure's span; want the file's content", got, ok)
	}
}

// TestCaptureSources_NilCapturesNothing pins the documented no-op.
func TestCaptureSources_NilCapturesNothing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	p := filepath.Join(dir, "ok.yammm")
	writeHostPathFile(t, p, "schema \"ok\"\n\ntype T {\n\tid String primary\n}\n")

	s, res := schema.Load(t.Context(), p, schema.WithModuleRoot(dir), schema.CaptureSources(nil))
	if s == nil || !res.OK() {
		t.Fatalf("a load with CaptureSources(nil) failed: %v", res)
	}
}
