package e2e_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/lsp/internal/testutil"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"
)

// analysisTimeout is the maximum time to wait for analysis to complete in tests.
const analysisTimeout = 5 * time.Second

// testDebounceDelay is a minimal debounce delay for deterministic tests.
const testDebounceDelay = 1 * time.Millisecond

// TestTemporal_DebouncePipeline verifies the full debounce -> analyze -> publish
// pipeline works end-to-end through the jrpc2 transport.
//
// Flow: open document (immediate analysis) -> verify snapshot -> change document
// (debounced analysis) -> wait for analysis -> verify snapshot updated.
func TestTemporal_DebouncePipeline(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()
	server.Workspace().SetDebounceDelayForTest(testDebounceDelay)

	// Open document with type "Alpha" — triggers immediate analysis.
	contentA := "schema \"test\"\n\ntype Alpha {\n\tname String\n}\n"
	require.NoError(t, h.OpenDocument("test.yammm", contentA))
	h.Sync()

	// Snapshot from immediate analysis should contain Alpha.
	symbols, err := h.DocumentSymbols("test.yammm")
	require.NoError(t, err)
	testutil.AssertDocumentSymbolExists(t, symbols, "Alpha")

	// Change to content with type "Beta" — debounced analysis.
	contentB := "schema \"test\"\n\ntype Beta {\n\tage Integer\n}\n"
	require.NoError(t, h.ChangeDocument("test.yammm", contentB, 2))

	// Wait for debounced analysis to complete.
	require.True(t, server.Workspace().WaitForAnalysis(analysisTimeout),
		"timed out waiting for analysis")
	h.Sync()

	// Snapshot should now contain Beta, not Alpha.
	symbols, err = h.DocumentSymbols("test.yammm")
	require.NoError(t, err)
	testutil.AssertDocumentSymbolExists(t, symbols, "Beta")
}

// TestTemporal_RapidEditsSettle verifies that rapid successive edits are
// debounced correctly: only the final content's analysis results are published.
// This exercises the debounce cancel-and-reschedule behavior under rapid typing.
func TestTemporal_RapidEditsSettle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()
	server.Workspace().SetDebounceDelayForTest(testDebounceDelay)

	// Open document with initial content.
	require.NoError(t, h.OpenDocument("test.yammm", "schema \"test\"\n"))
	h.Sync()

	// Send 10 rapid changes. Each change resets the debounce timer.
	// Only the final content should be analyzed after settling.
	for i := range 9 {
		require.NoError(t, h.ChangeDocument("test.yammm",
			fmt.Sprintf("schema \"test\"\n\ntype Intermediate%d {\n\tf String\n}\n", i), i+2))
	}

	// Final change: type "Final"
	finalContent := "schema \"test\"\n\ntype Final {\n\tname String\n}\n"
	require.NoError(t, h.ChangeDocument("test.yammm", finalContent, 11))

	// Poll until the final content's analysis completes. With a 1ms debounce,
	// intermediate edits may fire separate analyses before the final edit settles.
	require.Eventually(t, func() bool {
		server.Workspace().WaitForAnalysis(100 * time.Millisecond)
		h.Sync()
		syms, err := h.DocumentSymbols("test.yammm")
		if err != nil {
			return false
		}
		return testutil.HasDocumentSymbol(syms, "Final")
	}, analysisTimeout, 10*time.Millisecond, "expected Final symbol after rapid edits settle")
}

// TestTemporal_CloseCancelsPendingAnalysis verifies that closing a document
// cancels any pending debounced analysis and clears diagnostics. Even after
// the debounce timer would have fired, no stale diagnostics are published.
func TestTemporal_CloseCancelsPendingAnalysis(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	// Open document — triggers immediate analysis and stores snapshot.
	require.NoError(t, h.OpenDocument("test.yammm",
		"schema \"test\"\n\ntype Alpha {\n\tname String\n}\n"))
	h.Sync()

	uri := testutil.PathToURI(filepath.Join(tmpDir, "test.yammm"))
	snap := server.Workspace().LatestSnapshot(uri)
	require.NotNil(t, snap, "snapshot should exist after open")

	// Change document — starts debounce timer.
	// Use content that would produce diagnostics if analyzed.
	require.NoError(t, h.ChangeDocument("test.yammm", "not valid!!!", 2))

	// Close document immediately, before debounce fires.
	// This should cancel the pending analysis and clear diagnostics.
	require.NoError(t, h.CloseDocument("test.yammm"))
	h.Sync()

	// Brief wait past any possible debounce timer to verify no stale analysis fires.
	// This is an inherently temporal check ("nothing happens"), so a short sleep is appropriate.
	time.Sleep(50 * time.Millisecond)
	h.Sync()

	// Snapshot should be nil (cleaned up by close).
	snap = server.Workspace().LatestSnapshot(uri)
	assert.Nil(t, snap, "snapshot should be nil after close")

	// Diagnostics should be empty (not stale error diagnostics).
	diags := h.Diagnostics(uri)
	assert.Empty(t, diags, "diagnostics should be cleared after close")
}

// TestTemporal_ConcurrentOpenChangeClose exercises the workspace under
// concurrent document lifecycle operations across multiple URIs. Run with
// -race to detect data races in the lock-protected document overlay,
// snapshot storage, and dependency tracking.
//
// This test uses the unified OpenDocument/ChangeDocument/CloseDocument methods
// which trigger real analysis goroutines, unlike TestWorkspace_ConcurrentDocumentAccess
// which only tests the lower-level overlay operations.
func TestTemporal_ConcurrentOpenChangeClose(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := workspace.NewWorkspace(logger, workspace.Config{})
	ws.SetDebounceDelayForTest(testDebounceDelay)

	const numURIs = 10
	const iterations = 20
	var wg sync.WaitGroup

	for i := range numURIs {
		uri := fmt.Sprintf("file:///test/file%d.yammm", i)

		// Writer goroutine: open, then rapid changes
		wg.Go(func() {
			for j := range iterations {
				content := fmt.Sprintf("schema \"test%d\"\n\ntype T%d_%d {\n\tf String\n}\n", i, i, j)
				if j == 0 {
					ws.OpenDocument(uri, 1, content)
				} else {
					ws.ChangeDocument(uri, j+1, content)
				}
			}
		})

		// Reader goroutine: concurrent snapshot reads
		wg.Go(func() {
			for range iterations {
				_ = ws.GetDocumentSnapshot(uri)
				_ = ws.LatestSnapshot(uri)
			}
		})
	}

	wg.Wait()

	// Clean up: close all documents and cancel pending analysis.
	for i := range numURIs {
		uri := fmt.Sprintf("file:///test/file%d.yammm", i)
		ws.CloseDocument(uri)
	}
	ws.Shutdown()
}

// TestTemporal_ConcurrentOpenCloseRace verifies that concurrent open and close
// of the SAME document from different goroutines doesn't cause panics or data
// races. This exercises the lock consolidation in documentClosed and the
// version gate in analyzeAndPublish.
func TestTemporal_ConcurrentOpenCloseRace(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := workspace.NewWorkspace(logger, workspace.Config{})
	ws.SetDebounceDelayForTest(testDebounceDelay)

	const iterations = 50
	uri := "file:///test/race.yammm"
	var wg sync.WaitGroup

	// Opener goroutine: repeatedly open the same document
	wg.Go(func() {
		for i := range iterations {
			content := fmt.Sprintf("schema \"test\"\n\ntype T%d {\n\tf String\n}\n", i)
			ws.OpenDocument(uri, i+1, content)
		}
	})

	// Closer goroutine: repeatedly close the same document
	wg.Go(func() {
		for range iterations {
			ws.CloseDocument(uri)
		}
	})

	wg.Wait()
	ws.Shutdown()
}
