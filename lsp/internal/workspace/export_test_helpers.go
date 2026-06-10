// This file ships in the compiled package (it has no _test.go suffix) as a
// deliberate carve-out: the exported *ForTest helpers below are consumed by
// the external lsp package's tests, which cannot reach workspace internals.
// Helpers needed only by package workspace tests belong in _test.go files,
// not here.
package workspace

import (
	"time"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/markdown"
)

// MarkdownDocumentOpenedForTest is a test-only export of markdownDocumentOpened.
func (w *Workspace) MarkdownDocumentOpenedForTest(uri string, version int, text string) {
	w.markdownDocumentOpened(uri, version, text)
}

// SetMarkdownBlocksForTest injects blocks and snapshots into an open markdown
// document's internal state. This exists solely for test code that needs to
// set up specific block configurations (e.g., nil snapshots) that cannot be
// produced through the normal analysis pipeline.
//
// The URI must already be open via markdownDocumentOpened.
func (w *Workspace) SetMarkdownBlocksForTest(uri string, blocks []markdown.CodeBlock, snapshots []*analysis.Snapshot) {
	w.mu.Lock()
	defer w.mu.Unlock()
	md := w.markdownDocs[uri]
	if md == nil {
		return
	}
	md.Blocks = blocks
	md.Snapshots = snapshots
}

// SetDebounceDelayForTest overrides the debounce delay for testing.
// Use a small value (e.g., 1ms) to make temporal tests deterministic.
func (w *Workspace) SetDebounceDelayForTest(d time.Duration) {
	w.sched.debounceDelay = d
}
