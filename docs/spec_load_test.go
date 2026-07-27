package docs_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// [TestSpecExamples] parses every example; these two LOAD selected ones, which
// is a much stronger claim and deliberately not made about every block.
//
// Most examples in this document are illustrative fragments: a property line
// whose type is declared three sections earlier, two spellings of one property
// shown as alternatives, a type body demonstrating relation syntax with no
// primary key because keys are not the subject. Measured against the loader,
// 28 of 43 blocks legitimately do not load standalone. Making them load would
// mean editing them into test fixtures — declaring types nobody reads, adding
// keys to examples about something else — which is a bad trade for a document
// whose job is to be read. Parsing is the right blanket guarantee.
//
// So loading is OPT-IN, in two forms:
//
//   - ```yammm-schema marks a block that is a complete, loadable model. Use it
//     where an example's SEMANTICS are the point and a plausible-looking
//     mistake would survive parsing — an unknown @vector keyword, a @@index
//     naming a property that is not there.
//   - A block that names a diagnostic code in a trailing comment is asserted to
//     PRODUCE that code, whatever its fence. That turns a claim the prose makes
//     about the loader into a test of the loader.

// specDiagnosticCode matches a diagnostic code named in an example's own
// comment, e.g. "// E_INVALID_ANNOTATION". Anchored to a comment so a code
// mentioned in surrounding prose, or a property that merely starts with E_,
// cannot be mistaken for an expectation.
var specDiagnosticCode = regexp.MustCompile(`//[^\n]*\b([EW]_[A-Z0-9_]+)\b`)

// wrapForLoading wraps a block the way [wrapForParsing] does, but supplies the
// synthetic type with a primary key.
//
// Every concrete type needs one, and a bare property fragment has no way to
// carry it, so without this the shell — not the example — is what fails. The
// key is injected only into the synthesized shell; a block that brings its own
// type declarations is untouched and keeps whatever keys it declares.
func wrapForLoading(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return content
	}
	first := firstNonCommentToken(trimmed)
	switch {
	case first == "schema":
		return content
	case isTopLevelKeyword(first):
		return "schema \"Test\"\n" + content
	default:
		return "schema \"Test\"\ntype W {\nprobe_id UUID primary\n" + content + "}\n"
	}
}

// loadSpecBlocks reads SPEC.md and returns its fenced yammm blocks.
func loadSpecBlocks(t *testing.T) []codeBlock {
	t.Helper()
	source, err := os.ReadFile("SPEC.md")
	if err != nil {
		t.Fatalf("reading SPEC.md: %v", err)
	}
	return extractCodeBlocks(string(source))
}

// TestSpecLoadableExamples asserts that every ```yammm-schema block compiles
// clean, not merely parses.
func TestSpecLoadableExamples(t *testing.T) {
	t.Parallel()
	blocks := loadSpecBlocks(t)

	var checked int
	for _, b := range blocks {
		if b.tag != "yammm-schema" {
			continue
		}
		checked++
		_, result := schema.LoadString(context.Background(), wrapForLoading(b.content), "spec.yammm")
		if err := result.Err(); err != nil {
			t.Errorf("SPEC.md:%d (```%s) does not load:\n%v\n  source:\n%s",
				b.line, b.tag, err, indent(wrapForLoading(b.content)))
		}
	}
	if checked == 0 {
		t.Error("no ```yammm-schema blocks found; the load gate is asserting nothing")
	}
	t.Logf("load-checked %d blocks", checked)
}

// TestSpecDocumentedDiagnostics asserts that an example naming a diagnostic
// code in a comment actually produces it.
//
// This is the direction that rots silently: prose saying "this is reported as
// E_INVALID_ANNOTATION" stays readable and convincing long after the loader
// stops reporting it, or starts reporting something else.
func TestSpecDocumentedDiagnostics(t *testing.T) {
	t.Parallel()
	blocks := loadSpecBlocks(t)

	var checked int
	for _, b := range blocks {
		if shouldSkip(b.content) != "" {
			continue
		}
		m := specDiagnosticCode.FindStringSubmatch(b.content)
		if m == nil {
			continue
		}
		want := m[1]
		checked++

		// Matched by STRING rather than by constructing a diag.Code: NewCode
		// panics on a name already registered, which every real code is.
		_, result := schema.LoadString(context.Background(), wrapForLoading(b.content), "spec.yammm")
		var got []string
		found := false
		for iss := range result.Issues() {
			code := iss.Code().String()
			got = append(got, code)
			if code == want {
				found = true
			}
		}
		if !found {
			t.Errorf("SPEC.md:%d (```%s) documents %s but the loader reported [%s]",
				b.line, b.tag, want, strings.Join(got, " "))
		}
	}
	if checked == 0 {
		t.Error("no example names a diagnostic code in a comment; this test is asserting nothing")
	}
	t.Logf("diagnostic-checked %d blocks", checked)
}
