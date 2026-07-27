package docs_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/mdsnippet"
)

// SPEC.md's examples are gated three ways, weakest claim to strongest:
//
//   - every block of real yammm code PARSES;
//   - every ```yammm-schema block additionally LOADS clean;
//   - every block naming a diagnostic code in a comment PRODUCES that code.
//
// Loading is deliberately opt-in rather than blanket. Most examples here are
// illustrative fragments: a property line whose type is declared three sections
// earlier, two spellings of one property shown as alternatives, a type body
// demonstrating relation syntax with no primary key because keys are not the
// subject. Making them all load would mean editing them into test fixtures —
// declaring types nobody reads, adding keys to examples about something else —
// which is a bad trade for a document whose job is to be read. Parsing is the
// right blanket guarantee; ```yammm-schema marks the blocks whose SEMANTICS are
// the point, where a plausible-looking mistake would survive parsing.
//
// The fence vocabulary and the gates live in
// [github.com/simon-lentz/yammm/internal/mdsnippet] so that this document and
// the plugin reference corpus cannot drift apart on what a tag means.

const specFile = "SPEC.md"

func specBlocks(t *testing.T) []mdsnippet.Block {
	t.Helper()
	blocks, err := mdsnippet.ExtractFiles(specFile)
	if err != nil {
		t.Fatalf("extracting blocks: %v", err)
	}
	if len(blocks) == 0 {
		t.Fatalf("no yammm code blocks found in %s", specFile)
	}
	return blocks
}

func TestSpecExamples(t *testing.T) {
	t.Parallel()
	checked := mdsnippet.AssertParses(t, specBlocks(t))
	if checked == 0 {
		t.Error("no block was parse-checked; this test is asserting nothing")
	}
	t.Logf("parse-checked %d blocks", checked)
}

func TestSpecLoadableExamples(t *testing.T) {
	t.Parallel()
	checked := mdsnippet.AssertLoads(t, specBlocks(t))
	if checked == 0 {
		t.Errorf("no ```%s blocks found; the load gate is asserting nothing", mdsnippet.TagSchema)
	}
	t.Logf("load-checked %d blocks", checked)
}

func TestSpecInvalidExamples(t *testing.T) {
	t.Parallel()
	// SPEC.md need not carry any deliberately-invalid example, so the count is
	// logged rather than asserted; the plugin corpus is where this gate earns
	// its keep.
	t.Logf("invalid-checked %d blocks", mdsnippet.AssertInvalid(t, specBlocks(t)))
}

func TestSpecDocumentedDiagnostics(t *testing.T) {
	t.Parallel()
	checked := mdsnippet.AssertDocumentedDiagnostics(t, specBlocks(t))
	if checked == 0 {
		t.Error("no example names a diagnostic code in a comment; this test is asserting nothing")
	}
	t.Logf("diagnostic-checked %d blocks", checked)
}
