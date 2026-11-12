package workspace

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/lsp/internal/testutil"
)

// A duplicate declaration in a Markdown code block names its first declaration
// by a related location, which stands at the document's line and not the
// block's. The message holds no position.
func TestMarkdown_ADuplicateNamesItsFirstDeclarationAtTheDocumentLine(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		block   string
		message string
	}{
		"a type after a datatype, no schema line": {
			block:   "type Money = Float\n\ntype Money {\n    id String primary\n}\n",
			message: `type "Money" is defined multiple times; the name is first defined as a datatype`,
		},
		"a type after a datatype, under a schema line": {
			block:   "schema \"s\"\n\ntype Money = Float\n\ntype Money {\n    id String primary\n}\n",
			message: `type "Money" is defined multiple times; the name is first defined as a datatype`,
		},
		"two datatypes": {
			block:   "type Money = Float\n\ntype Money = Integer\n\ntype T {\n    id String primary\n}\n",
			message: `datatype "Money" is defined multiple times`,
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			w := newTestWorkspace(t, slog.Default(), Config{})
			uri := "file:///test/duplicate.md"
			collector := &testutil.NotificationCollector{}
			w.SetNotifier(collector.Notify)
			content := "# T\n" + strings.Repeat("prose line\n", 20) + "```yammm\n" + tc.block + "```\n"
			w.markdownDocumentOpened(uri, 1, content)
			w.AnalyzeMarkdownAndPublish(t.Context(), uri)

			var declared []uint32
			for i, line := range strings.Split(content, "\n") {
				if strings.HasPrefix(line, "type Money") {
					declared = append(declared, uint32(i))
				}
			}
			if len(declared) != 2 {
				t.Fatalf("the document declares Money on %d lines, want 2", len(declared))
			}

			diags := collector.DiagnosticsFor(uri)
			if len(diags) != 1 {
				t.Fatalf("got %d diagnostics, want 1: %v", len(diags), diags)
			}
			d := diags[0]
			if d.Message != tc.message {
				t.Errorf("message = %q, want %q", d.Message, tc.message)
			}
			if d.Range.Start.Line != declared[1] {
				t.Errorf("the diagnostic stands at line %d, want the later declaration's, %d", d.Range.Start.Line, declared[1])
			}
			if len(d.RelatedInformation) != 1 {
				t.Fatalf("got %d related locations, want 1", len(d.RelatedInformation))
			}
			related := d.RelatedInformation[0]
			if related.Message != "first defined here" || related.Location.URI != uri {
				t.Errorf("related = %q at %s, want \"first defined here\" at %s", related.Message, related.Location.URI, uri)
			}
			if related.Location.Range.Start.Line != declared[0] {
				t.Errorf("the related location stands at line %d, want the first declaration's, %d",
					related.Location.Range.Start.Line, declared[0])
			}
		})
	}
}
