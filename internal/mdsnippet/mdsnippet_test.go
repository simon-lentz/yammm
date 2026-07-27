package mdsnippet

import (
	"strings"
	"testing"
)

// recorder is a [TB] that captures failures instead of reporting them, so each
// gate's failure path can be exercised. A gate is only worth having if it
// fails when it should, and every Assert here is checked in both directions.
type recorder struct{ msgs []string }

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.msgs = append(r.msgs, format)
	_ = args
}
func (r *recorder) failed() bool { return len(r.msgs) > 0 }

func block(tag, content string) Block {
	return Block{Tag: tag, Content: content, File: "test.md", Line: 1}
}

func TestExtract_CollectsTaggedBlocksAndIgnoresOthers(t *testing.T) {
	t.Parallel()
	src := strings.Join([]string{
		"# Doc",
		"```yammm",
		"type A { id UUID primary }",
		"```",
		"```go",
		"func main() {}",
		"```",
		"```text",
		"--> NAME (multiplicity) Target",
		"```",
		"```yammm-invalid",
		"id UUID primary required",
		"```",
	}, "\n")

	blocks := Extract("test.md", src)
	if len(blocks) != 2 {
		t.Fatalf("Extract returned %d blocks, want 2 (go and text must be ignored): %+v", len(blocks), blocks)
	}
	if got, want := blocks[0].Tag, TagYammm; got != want {
		t.Errorf("first block tag = %q, want %q", got, want)
	}
	// The line must point at the OPENING fence so a failure message names a
	// line the reader can search for.
	if got, want := blocks[0].Line, 2; got != want {
		t.Errorf("first block line = %d, want %d (the opening fence)", got, want)
	}
	if got, want := blocks[1].Tag, TagInvalid; got != want {
		t.Errorf("second block tag = %q, want %q", got, want)
	}
	if got, want := blocks[1].Line, 11; got != want {
		t.Errorf("second block line = %d, want %d", got, want)
	}
}

func TestExtract_UnterminatedFenceIsDropped(t *testing.T) {
	t.Parallel()
	// A fence that never closes has no well-defined content, and silently
	// treating the rest of the document as yammm would produce a failure
	// pointing at unrelated prose.
	blocks := Extract("test.md", "```yammm\ntype A { id UUID primary }\n")
	if len(blocks) != 0 {
		t.Errorf("Extract returned %d blocks for an unterminated fence, want 0", len(blocks))
	}
}

func TestWrap_HoistsImportsAboveTheTypeShell(t *testing.T) {
	t.Parallel()
	// An "import plus member line" fragment spans two scopes: the import
	// belongs at schema level and the relation inside a type. Forcing both
	// into the type shell is a syntax error the example did not commit.
	b := block(TagSnippet, "import \"./products\" as products\n--> PRODUCT (one) products.Product\n")

	wrapped := b.WrapForParsing()
	if errs := ParseErrors(wrapped); len(errs) > 0 {
		t.Fatalf("hoisted fragment failed to parse: %v\nwrapped:\n%s", errs, wrapped)
	}

	importAt := strings.Index(wrapped, "import ")
	typeAt := strings.Index(wrapped, "type W {")
	if importAt < 0 || typeAt < 0 {
		t.Fatalf("wrapped output missing import or type shell:\n%s", wrapped)
	}
	if importAt > typeAt {
		t.Errorf("import was not hoisted above the type shell:\n%s", wrapped)
	}
}

func TestWrap_SchemaBearingContentIsUntouched(t *testing.T) {
	t.Parallel()
	content := "schema \"Own\"\ntype A { id UUID primary }\n"
	if got := block(TagSchema, content).WrapForLoading(); got != content {
		t.Errorf("content declaring its own schema was rewritten:\n%s", got)
	}
}

func TestWrap_OnlyLoadingInjectsAPrimaryKey(t *testing.T) {
	t.Parallel()
	b := block(TagSnippet, "name String required\n")

	// The key exists so the synthesized SHELL does not fail E_NO_PRIMARY_KEY
	// and mask the example. Parsing does not need it, and injecting it there
	// would put a line in the parse gate's failure output that the author
	// never wrote.
	if strings.Contains(b.WrapForParsing(), probeKey) {
		t.Error("WrapForParsing injected a primary key; only WrapForLoading should")
	}
	if !strings.Contains(b.WrapForLoading(), probeKey) {
		t.Error("WrapForLoading did not inject a primary key into the synthesized shell")
	}
}

func TestWrap_BlockDeclaringItsOwnTypesKeepsItsOwnKeys(t *testing.T) {
	t.Parallel()
	// A block bringing its own type declarations must not receive the probe
	// key, or an example ABOUT a missing primary key would silently acquire one.
	b := block(TagInvalid, "type Doc {\n    content String required\n}\n")
	if strings.Contains(b.WrapForLoading(), probeKey) {
		t.Errorf("probe key leaked into a block that declares its own types:\n%s", b.WrapForLoading())
	}
}

func TestDiagnosticCode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		content string
		want    string
		wantOK  bool
	}{
		{"named in a line comment", "// WRONG: E_SYNTAX\nid UUID primary required\n", "E_SYNTAX", true},
		{"warning code", "// drops it: W_ANNOTATION_SHADOWED\nx String\n", "W_ANNOTATION_SHADOWED", true},
		{"no comment", "id UUID primary\n", "", false},
		// Anchored to a comment so a property whose name merely starts with
		// E_ is not mistaken for an expectation.
		{"property named like a code", "E_NOT_A_CODE String required\n", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := block(TagSnippet, tc.content).DiagnosticCode()
			if ok != tc.wantOK || got != tc.want {
				t.Errorf("DiagnosticCode() = (%q, %v), want (%q, %v)", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestAssertParses_ReportsAndSkipsInvalidBlocks(t *testing.T) {
	t.Parallel()

	var r recorder
	if n := AssertParses(&r, []Block{block(TagSnippet, "id UUID primary required\n")}); n != 1 {
		t.Errorf("checked %d blocks, want 1", n)
	}
	if !r.failed() {
		t.Error("AssertParses did not report an unparseable block")
	}

	// The same source under TagInvalid must be skipped: failing to parse is
	// one of the ways an invalid example is allowed to be wrong.
	var r2 recorder
	if n := AssertParses(&r2, []Block{block(TagInvalid, "id UUID primary required\n")}); n != 0 {
		t.Errorf("checked %d invalid blocks, want 0", n)
	}
	if r2.failed() {
		t.Error("AssertParses reported a TagInvalid block; it must skip them")
	}
}

func TestAssertInvalid_ReportsABlockThatLoadsClean(t *testing.T) {
	t.Parallel()

	// The gate's whole purpose: an example labelled WRONG that the loader
	// accepts. If this stops failing, the corpus can drift back to teaching
	// mistakes that are not mistakes.
	var r recorder
	if n := AssertInvalid(&r, []Block{block(TagInvalid, "name String required\n")}); n != 1 {
		t.Errorf("checked %d blocks, want 1", n)
	}
	if !r.failed() {
		t.Error("AssertInvalid did not report a block that loads clean")
	}

	var r2 recorder
	AssertInvalid(&r2, []Block{block(TagInvalid, "id UUID primary required\n")})
	if r2.failed() {
		t.Errorf("AssertInvalid reported a genuinely invalid block: %v", r2.msgs)
	}
}

func TestAssertLoads_ReportsAFailingSchemaBlock(t *testing.T) {
	t.Parallel()

	var r recorder
	AssertLoads(&r, []Block{block(TagSchema, "state String @nosuchannotation\n")})
	if !r.failed() {
		t.Error("AssertLoads did not report a yammm-schema block that fails to load")
	}

	var r2 recorder
	if n := AssertLoads(&r2, []Block{block(TagSchema, "state String @index\n")}); n != 1 {
		t.Errorf("checked %d blocks, want 1", n)
	}
	if r2.failed() {
		t.Errorf("AssertLoads reported a clean block: %v", r2.msgs)
	}
}

func TestAssertDocumentedDiagnostics_ReportsAWrongCode(t *testing.T) {
	t.Parallel()

	var r recorder
	AssertDocumentedDiagnostics(&r, []Block{
		block(TagInvalid, "// claims the wrong code: E_NO_PRIMARY_KEY\nid UUID primary required\n"),
	})
	if !r.failed() {
		t.Error("AssertDocumentedDiagnostics did not report a block naming a code the loader never produced")
	}

	var r2 recorder
	AssertDocumentedDiagnostics(&r2, []Block{
		block(TagInvalid, "// parse error: E_SYNTAX\nid UUID primary required\n"),
	})
	if r2.failed() {
		t.Errorf("AssertDocumentedDiagnostics reported a correctly-coded block: %v", r2.msgs)
	}
}
