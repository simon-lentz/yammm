package workspace

import (
	"time"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/markdown"
)

// DocumentOpenedForTest is a test-only export of documentOpened.
func (w *Workspace) DocumentOpenedForTest(uri string, version int, text string) {
	w.documentOpened(uri, version, text)
}

// MarkdownDocumentOpenedForTest is a test-only export of markdownDocumentOpened.
func (w *Workspace) MarkdownDocumentOpenedForTest(uri string, version int, text string) {
	w.markdownDocumentOpened(uri, version, text)
}

// UpdateDepsForTest is a test-only export of updateDeps.
func (w *Workspace) UpdateDepsForTest(entryURI string, importedPaths []string) {
	w.updateDeps(entryURI, importedPaths)
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

// WaitForAnalysis blocks until a scheduled analysis completes.
// It uses a condition variable that is broadcast after each analysis finishes.
// The timeout prevents indefinite blocking in tests.
func (w *Workspace) WaitForAnalysis(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		w.sched.doneMu.Lock()
		w.sched.doneCond.Wait()
		w.sched.doneMu.Unlock()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		// Broadcast to unblock the goroutine so it doesn't leak.
		w.sched.doneCond.Broadcast()
		return false
	}
}
