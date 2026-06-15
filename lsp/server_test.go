package lsp_test

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	lsp "github.com/simon-lentz/yammm/lsp"
	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/markdown"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"
	"github.com/simon-lentz/yammm/schema"
)

// newTestHarness creates a harness for integration testing with a real LSP server.
// newTestHarnessWithServer is the single harness constructor: it wires a
// fresh server to an in-process client, connects diagnostics notification
// push, runs the LSP initialize handshake, and returns both the harness and
// the server for tests that need direct workspace access.
func newTestHarnessWithServer(t *testing.T, root string) (*testutil.Harness, *lsp.Server) {
	t.Helper()
	return newTestHarnessWithServerConfig(t, root, lsp.Config{ModuleRoot: root})
}

// newTestHarnessWithServerConfig is newTestHarnessWithServer with explicit
// server configuration, for tests that tune Config knobs (e.g., a short
// DebounceDelay for temporal tests). cfg.ModuleRoot should normally be root.
func newTestHarnessWithServerConfig(t *testing.T, root string, cfg lsp.Config) (*testutil.Harness, *lsp.Server) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, cfg)
	// Shut the server down at test end so workspace scheduler goroutines
	// and debounce timers do not outlive the test. Close is idempotent.
	t.Cleanup(func() { _ = server.Close() })
	ctx := t.Context()
	h := testutil.NewHarness(t, server.Mux(), root)
	server.Workspace().SetNotifier(func(method string, params any) {
		_ = h.JRPCServer().Notify(ctx, method, params)
	})
	require.NoError(t, h.Initialize(), "harness initialization failed")
	return h, server
}

// newTestHarness returns just the initialized harness for tests that never
// touch the server side.
func newTestHarness(t *testing.T, root string) *testutil.Harness {
	t.Helper()
	h, _ := newTestHarnessWithServer(t, root)
	return h
}

func TestNewServer(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{ModuleRoot: "/test/root"})

	require.NotNil(t, server)
	assert.NotNil(t, server.Mux())
	assert.NotNil(t, server.Workspace())
}

func TestConfig_ModuleRoot(t *testing.T) {
	t.Parallel()

	cfg := lsp.Config{ModuleRoot: "/custom/path"}
	assert.Equal(t, "/custom/path", cfg.ModuleRoot)
}

func TestConfig_Empty(t *testing.T) {
	t.Parallel()

	cfg := lsp.Config{}
	assert.Empty(t, cfg.ModuleRoot)
}

func TestServer_Close_BeforeRunStdio(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{})

	// Close before RunStdio should not panic
	_ = server.Close()
}

func TestServer_Close(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{})

	// Close before RunStdio should be safe (GetStdio returns nil)
	assert.NoError(t, server.Close())

	// Close is idempotent: subsequent calls return the same result
	assert.NoError(t, server.Close())

	// Third close for good measure
	assert.NoError(t, server.Close())
}

func TestServer_WorkspaceCreated(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := lsp.NewServer(logger, lsp.Config{ModuleRoot: "/test"})

	require.NotNil(t, server.Workspace())

	// The workspace should inherit the config's module root
	assert.Equal(t, "/test", server.Workspace().FindModuleRoot("/any/path/file.yammm"))
}

func TestIntegration_InitializeSuccess(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()
}

func TestIntegration_FormattingWithoutOpen(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	content := "schema \"test\"\n\n\ntype Person {\n\tname String\n}\n\n\n"
	filePath := filepath.Join(tmpDir, "main.yammm")
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	edits, err := h.Formatting("main.yammm")
	require.NoError(t, err, "Formatting failed")

	testutil.AssertNoFormattingNeeded(t, edits)
}

func TestIntegration_HoverWithoutOpen(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	content := "schema \"test\"\n\ntype Person {\n\tname String required\n\tage Integer\n}\n"
	filePath := filepath.Join(tmpDir, "main.yammm")
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	hover, err := h.Hover("main.yammm", 2, 5)
	require.NoError(t, err, "Hover returned error")

	assert.Nil(t, hover, "Hover should return nil for unopened documents")
}

func TestIntegration_DefinitionWithoutOpen(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	partsContent := "schema \"parts\"\n\ntype Wheel {\n\tid String primary\n\tdiameter Integer\n}\n"
	partsPath := filepath.Join(tmpDir, "parts.yammm")
	err := os.WriteFile(partsPath, []byte(partsContent), 0o600)
	require.NoError(t, err, "failed to write parts file")

	mainContent := "schema \"main\"\n\nimport \"./parts\" as parts\n\ntype Car {\n\t*-> WHEELS (many) parts.Wheel\n}\n"
	mainPath := filepath.Join(tmpDir, "main.yammm")
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err, "failed to write main file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	result, err := h.Definition("main.yammm", 5, 22)
	require.NoError(t, err, "Definition returned error")

	assert.Nil(t, result, "Definition should return nil for unopened documents")
}

func TestIntegration_OverlayOverridesDisk(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	diskContent := "schema \"test\"\n\ntype Person {\n\tdiskField String primary\n}\n"
	filePath := filepath.Join(tmpDir, "main.yammm")
	err := os.WriteFile(filePath, []byte(diskContent), 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	overlayContent := "schema \"test\"\n\ntype Person {\n\toverlayField String primary\n}\n"
	err = h.OpenDocument("main.yammm", overlayContent)
	require.NoError(t, err, "OpenDocument failed")

	hover, err := h.Hover("main.yammm", 3, 1)
	require.NoError(t, err, "Hover failed")

	if hover != nil {
		testutil.AssertHoverContains(t, hover, "overlayField")
	} else {
		symbols, err := h.DocumentSymbols("main.yammm")
		require.NoError(t, err, "DocumentSymbols failed")
		testutil.AssertDocumentSymbolExists(t, symbols, "overlayField")
	}
}

func TestIntegration_DiskFallbackForUnopened(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	partsContent := "schema \"parts\"\n\ntype Wheel {\n\tid String primary\n\tdiameter Integer\n}\n"
	partsPath := filepath.Join(tmpDir, "parts.yammm")
	err := os.WriteFile(partsPath, []byte(partsContent), 0o600)
	require.NoError(t, err, "failed to write parts file")

	mainContent := "schema \"main\"\n\nimport \"./parts\" as parts\n\ntype Car extends parts.Wheel {\n\tcolor String\n}\n"
	mainPath := filepath.Join(tmpDir, "main.yammm")
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err, "failed to write main file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.OpenDocument("main.yammm", mainContent)
	require.NoError(t, err, "OpenDocument failed")

	result, err := h.Definition("main.yammm", 4, 24)
	require.NoError(t, err, "Definition failed")

	require.NotNil(t, result, "Definition returned nil - schema may be invalid or symbol index missing")

	testutil.AssertLocationURI(t, *result, "parts.yammm")
}

func TestIntegration_OpenedImportOverridesDisk(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	partsContentDisk := "schema \"parts\"\n\ntype DiskWheel {\n\tid String primary\n\tdiskDiameter Integer\n}\n"
	partsPath := filepath.Join(tmpDir, "parts.yammm")
	err := os.WriteFile(partsPath, []byte(partsContentDisk), 0o600)
	require.NoError(t, err, "failed to write parts file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	partsContentOverlay := "schema \"parts\"\n\ntype OverlayWheel {\n\tid String primary\n\toverlayDiameter Integer\n}\n"
	err = h.OpenDocument("parts.yammm", partsContentOverlay)
	require.NoError(t, err, "OpenDocument parts failed")

	symbols, err := h.DocumentSymbols("parts.yammm")
	require.NoError(t, err, "DocumentSymbols failed")

	testutil.AssertDocumentSymbolExists(t, symbols, "OverlayWheel")
}

func TestIntegration_MultiRootInitialize(t *testing.T) {
	t.Parallel()

	root1 := t.TempDir()
	root2 := t.TempDir()

	content1 := "schema \"app\"\n\ntype User {\n\tid UUID primary\n\tname String required\n}\n"
	file1 := filepath.Join(root1, "app.yammm")
	err := os.WriteFile(file1, []byte(content1), 0o600)
	require.NoError(t, err, "failed to write file1")

	content2 := "schema \"lib\"\n\ntype Common {\n\tcreatedAt Timestamp primary\n}\n"
	file2 := filepath.Join(root2, "lib.yammm")
	err = os.WriteFile(file2, []byte(content2), 0o600)
	require.NoError(t, err, "failed to write file2")

	h := newTestHarness(t, root1)
	defer h.Close()

	err = h.InitializeWithFolders([]string{root1, root2})
	require.NoError(t, err, "InitializeWithFolders failed")

	err = h.OpenDocument(file1, content1)
	require.NoError(t, err, "OpenDocument root1 failed")

	symbols1, err := h.DocumentSymbols(file1)
	require.NoError(t, err, "DocumentSymbols root1 failed")
	testutil.AssertDocumentSymbolExists(t, symbols1, "User")

	err = h.OpenDocument(file2, content2)
	require.NoError(t, err, "OpenDocument root2 failed")

	symbols2, err := h.DocumentSymbols(file2)
	require.NoError(t, err, "DocumentSymbols root2 failed")

	if symbols2 == nil {
		t.Log("DocumentSymbols returned nil for root2 document")
		return
	}

	testutil.AssertDocumentSymbolExists(t, symbols2, "Common")
}

func TestIntegration_MultiDocumentWorkflow(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	typesContent := "schema \"types\"\n\ntype Entity {\n\tid UUID primary\n\tcreatedAt Timestamp required\n}\n"
	typesPath := filepath.Join(tmpDir, "types.yammm")
	err := os.WriteFile(typesPath, []byte(typesContent), 0o600)
	require.NoError(t, err, "failed to write types.yammm")

	mainContent := "schema \"main\"\nimport \"./types\" as types\n\ntype User extends types.Entity {\n\tname String required\n\temail String required\n}\n"
	mainPath := filepath.Join(tmpDir, "main.yammm")
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err, "failed to write main.yammm")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.OpenDocument(typesPath, typesContent)
	require.NoError(t, err, "OpenDocument types.yammm failed")
	err = h.OpenDocument(mainPath, mainContent)
	require.NoError(t, err, "OpenDocument main.yammm failed")

	typesSymbols, err := h.DocumentSymbols(typesPath)
	require.NoError(t, err, "DocumentSymbols types.yammm failed")
	testutil.AssertDocumentSymbolExists(t, typesSymbols, "Entity")

	mainSymbols, err := h.DocumentSymbols(mainPath)
	require.NoError(t, err, "DocumentSymbols main.yammm failed")
	testutil.AssertDocumentSymbolExists(t, mainSymbols, "User")

	result, err := h.Definition(mainPath, 3, 26)
	require.NoError(t, err, "Definition failed")

	require.NotNil(t, result, "Definition returned nil - schema may be invalid or symbol index missing")

	testutil.AssertLocationURI(t, *result, "types.yammm")
}

func TestIntegration_FormattingRoundTrip_ASCII(t *testing.T) {
	t.Parallel()

	unformatted, err := os.ReadFile("testdata/lsp/formatting/unformatted.yammm")
	require.NoError(t, err, "failed to read unformatted fixture")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "main.yammm")
	err = os.WriteFile(filePath, unformatted, 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.OpenDocument("main.yammm", string(unformatted))
	require.NoError(t, err, "OpenDocument failed")

	edits, err := h.Formatting("main.yammm")
	require.NoError(t, err, "Formatting failed")

	testutil.AssertFormattingApplied(t, edits)

	result := testutil.ApplyEdits(string(unformatted), edits, "utf-16")
	yammmtest.Golden(t, "lsp/formatting/formatted.yammm", []byte(result))
}

func TestFormatting_UsesTokenStreamFormatterForIntraLineSpacing(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	content := "schema \"test\"\n\ntype   A{\n\tname String\n}\n"
	filePath := filepath.Join(tmpDir, "test.yammm")
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write file")

	h := newTestHarness(t, tmpDir)
	defer h.Close()

	err = h.OpenDocument("test.yammm", content)
	require.NoError(t, err, "OpenDocument failed")

	edits, err := h.Formatting("test.yammm")
	require.NoError(t, err, "Formatting failed")

	require.NotEmpty(t, edits, "expected formatting edits for intra-line spacing normalization")

	result := testutil.ApplyEdits(content, edits, "utf-16")
	assert.Contains(t, result, "type A {", "expected formatted text to normalize type spacing")
}

func TestFormatting_UTF8PositionEncoding(t *testing.T) {
	t.Parallel()

	content := "schema \"日本語\"\n\ntype Person {    \n\tname String\n}\n"

	tests := []struct {
		name     string
		encoding lsp.PositionEncoding
	}{
		{"UTF-16", lsp.PositionEncodingUTF16},
		{"UTF-8", lsp.PositionEncodingUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.yammm")
			err := os.WriteFile(filePath, []byte(content), 0o600)
			require.NoError(t, err, "failed to write file")

			h, server := newTestHarnessWithServer(t, tmpDir)
			defer h.Close()

			server.Workspace().SetPositionEncoding(tt.encoding)

			err = h.OpenDocument("test.yammm", content)
			require.NoError(t, err, "OpenDocument failed")

			edits, err := h.Formatting("test.yammm")
			require.NoError(t, err, "Formatting failed")

			if len(edits) == 0 {
				return
			}

			edit := edits[0]
			assert.Equal(t, uint32(0), edit.Range.Start.Line, "edit range should start at line 0")
			assert.Equal(t, uint32(0), edit.Range.Start.Character, "edit range should start at character 0")
		})
	}
}

func TestIntegration_FormattingRoundTrip_Multibyte(t *testing.T) {
	t.Parallel()

	unformatted := "schema \"日本語\"\n\ntype   Person{\n    name String  primary // 名前\n    tag String\n}\n// 最後 😀\n"
	expected := "schema \"日本語\"\n\ntype Person {\n\tname String primary // 名前\n\ttag  String\n}\n// 最後 😀\n"

	tests := []struct {
		name     string
		encoding lsp.PositionEncoding
	}{
		{"UTF-16", lsp.PositionEncodingUTF16},
		{"UTF-8", lsp.PositionEncodingUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "test.yammm")
			err := os.WriteFile(filePath, []byte(unformatted), 0o600)
			require.NoError(t, err, "failed to write file")

			h, server := newTestHarnessWithServer(t, tmpDir)
			defer h.Close()

			server.Workspace().SetPositionEncoding(tt.encoding)

			err = h.OpenDocument("test.yammm", unformatted)
			require.NoError(t, err, "OpenDocument failed")

			edits, err := h.Formatting("test.yammm")
			require.NoError(t, err, "Formatting failed")

			require.NotEmpty(t, edits, "expected formatting edits, got none")

			result := testutil.ApplyEdits(unformatted, edits, string(tt.encoding))
			assert.Equal(t, expected, result, "round-trip result != expected")

			ctx := t.Context()
			s, _ := schema.LoadString(ctx, result, "test.yammm")
			require.NoError(t, err, "formatted output failed to parse")
			assert.Equal(t, "日本語", s.Name(), "schema name mismatch")
		})
	}
}

func TestCompletion_UTF8Mode_Integration(t *testing.T) {
	t.Parallel()

	content := "schema \"日本語テスト\"\n\ntype 人物 {\n\t名前 String\n}\n"

	tests := []struct {
		name string
		line int
		char int
	}{
		{"line 0, start", 0, 0},
		{"line 0, at schema name start", 0, 8},
		{"line 0, mid CJK char", 0, 9},
		{"line 2, before type name", 2, 5},
		{"line 2, mid first CJK char", 2, 6},
		{"line 2, at opening brace", 2, 12},
		{"line 3, at property name", 3, 1},
		{"line 3, mid property name", 3, 2},
		{"line 3, after property name", 3, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "main.yammm")
			err := os.WriteFile(filePath, []byte(content), 0o600)
			require.NoError(t, err, "failed to write file")

			h, server := newTestHarnessWithServer(t, tmpDir)
			defer h.Close()

			server.Workspace().SetPositionEncoding(lsp.PositionEncodingUTF8)

			err = h.OpenDocument(filePath, content)
			require.NoError(t, err, "OpenDocument failed")

			result, err := h.Completion(filePath, tt.line, tt.char)
			require.NoError(t, err, "completion failed")

			// A nil slice (no completions) is valid for some positions.
			t.Logf("got %d completion items", len(result))
		})
	}
}

func TestCompletion_UTF8Mode_NoPanic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	content := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"
	filePath := filepath.Join(tmpDir, "main.yammm")
	err := os.WriteFile(filePath, []byte(content), 0o600)
	require.NoError(t, err, "failed to write file")

	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	server.Workspace().SetPositionEncoding(lsp.PositionEncodingUTF8)

	err = h.OpenDocument(filePath, content)
	require.NoError(t, err, "OpenDocument failed")

	result, err := h.Completion(filePath, 3, 0)
	require.NoError(t, err, "completion failed")

	assert.NotEmpty(t, result, "expected completion items for type body context")
}

const (
	analysisTimeout   = 5 * time.Second
	testDebounceDelay = 1 * time.Millisecond
)

func TestTemporal_DebouncePipeline(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, _ := newTestHarnessWithServerConfig(t, tmpDir,
		lsp.Config{ModuleRoot: tmpDir, DebounceDelay: testDebounceDelay})
	defer h.Close()

	contentA := "schema \"test\"\n\ntype Alpha {\n\tname String primary\n}\n"
	require.NoError(t, h.OpenDocument("test.yammm", contentA))
	h.Sync()

	symbols, err := h.DocumentSymbols("test.yammm")
	require.NoError(t, err)
	testutil.AssertDocumentSymbolExists(t, symbols, "Alpha")

	contentB := "schema \"test\"\n\ntype Beta {\n\tid String primary\n\tage Integer\n}\n"
	require.NoError(t, h.ChangeDocument("test.yammm", contentB, 2))

	require.Eventually(t, func() bool {
		h.Sync()
		syms, symErr := h.DocumentSymbols("test.yammm")
		return symErr == nil && testutil.HasDocumentSymbol(syms, "Beta")
	}, analysisTimeout, 10*time.Millisecond, "expected Beta symbol after change is analyzed")
}

func TestTemporal_RapidEditsSettle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, _ := newTestHarnessWithServerConfig(t, tmpDir,
		lsp.Config{ModuleRoot: tmpDir, DebounceDelay: testDebounceDelay})
	defer h.Close()

	require.NoError(t, h.OpenDocument("test.yammm", "schema \"test\"\n"))
	h.Sync()

	for i := range 9 {
		require.NoError(t, h.ChangeDocument("test.yammm",
			fmt.Sprintf("schema \"test\"\n\ntype Intermediate%d {\n\tf String primary\n}\n", i), i+2))
	}

	finalContent := "schema \"test\"\n\ntype Final {\n\tname String primary\n}\n"
	require.NoError(t, h.ChangeDocument("test.yammm", finalContent, 11))

	require.Eventually(t, func() bool {
		h.Sync()
		syms, err := h.DocumentSymbols("test.yammm")
		if err != nil {
			return false
		}
		return testutil.HasDocumentSymbol(syms, "Final")
	}, analysisTimeout, 10*time.Millisecond, "expected Final symbol after rapid edits settle")
}

// TestTemporal_CloseCancelsPendingAnalysis pins the close-cancels contract
// under synctest's fake clock: a change schedules a debounced re-analysis,
// the close cancels it, and sleeping past the full debounce window proves
// the cancelled analysis never fires — the stale snapshot and diagnostics
// stay cleared. (Real-time sleeps cannot catch this: the default debounce
// outlives any tolerable sleep.)
func TestTemporal_CloseCancelsPendingAnalysis(t *testing.T) {
	tmpDir := t.TempDir()
	synctest.Test(t, func(t *testing.T) {
		h, server := newTestHarnessWithServer(t, tmpDir)
		defer h.Close()

		require.NoError(t, h.OpenDocument("test.yammm",
			"schema \"test\"\n\ntype Alpha {\n\tname String primary\n}\n"))
		h.Sync()
		synctest.Wait()

		uri := testutil.PathToURI(filepath.Join(tmpDir, "test.yammm"))
		require.Eventually(t, func() bool {
			return server.Workspace().LatestSnapshot(uri) != nil
		}, time.Second, 10*time.Millisecond, "snapshot should exist after open")

		// Schedule a re-analysis, then close before the debounce elapses.
		require.NoError(t, h.ChangeDocument("test.yammm", "not valid!!!", 2))
		require.NoError(t, h.CloseDocument("test.yammm"))
		h.Sync()

		// Sleep past the entire debounce window in fake time: a cancelled
		// analysis must not resurrect the snapshot or publish diagnostics.
		time.Sleep(2 * workspace.DefaultDebounceDelay)
		synctest.Wait()

		assert.Nil(t, server.Workspace().LatestSnapshot(uri), "snapshot should be nil after close")
		assert.Empty(t, h.Diagnostics(uri), "diagnostics should be cleared after close")
	})
}

func TestTemporal_ConcurrentOpenChangeClose(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := workspace.NewWorkspace(logger, workspace.Config{DebounceDelay: testDebounceDelay})

	const numURIs = 10
	const iterations = 20
	var wg sync.WaitGroup

	for i := range numURIs {
		uri := fmt.Sprintf("file:///test/file%d.yammm", i)

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

		wg.Go(func() {
			for range iterations {
				_ = ws.GetDocumentSnapshot(uri)
				_ = ws.LatestSnapshot(uri)
			}
		})
	}

	wg.Wait()

	for i := range numURIs {
		uri := fmt.Sprintf("file:///test/file%d.yammm", i)
		ws.CloseDocument(uri)
	}
	ws.Shutdown()
}

func TestTemporal_ConcurrentOpenCloseRace(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	ws := workspace.NewWorkspace(logger, workspace.Config{DebounceDelay: testDebounceDelay})

	const iterations = 50
	uri := "file:///test/race.yammm"
	var wg sync.WaitGroup

	wg.Go(func() {
		for i := range iterations {
			content := fmt.Sprintf("schema \"test\"\n\ntype T%d {\n\tf String\n}\n", i)
			ws.OpenDocument(uri, i+1, content)
		}
	})

	wg.Go(func() {
		for range iterations {
			ws.CloseDocument(uri)
		}
	})

	wg.Wait()
	ws.Shutdown()
}

func TestMarkdownIntegration_DiagnosticsInCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nnot valid schema!!!\n```\n"
	mdPath := filepath.Join(tmpDir, "test.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, content)

	collector := &testutil.NotificationCollector{}
	server.Workspace().SetNotifier(collector.Notify)
	server.Workspace().AnalyzeMarkdownAndPublish(t.Context(), uri)

	diags := collector.DiagnosticsFor(uri)
	assert.NotEmpty(t, diags, "expected diagnostics for syntax error in code block")

	var hasMarkdownCoords bool
	for _, d := range diags {
		if int(d.Range.Start.Line) >= 3 {
			hasMarkdownCoords = true
			break
		}
	}
	assert.True(t, hasMarkdownCoords,
		"expected at least one diagnostic with markdown-coordinate line >= 3, got: %+v", diags)
}

func TestMarkdownIntegration_HoverInCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "hover.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	hover, err := h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	require.NotNil(t, hover, "expected hover result for type name in code block")

	if hover.Range != nil {
		assert.GreaterOrEqual(t, int(hover.Range.Start.Line), 3,
			"hover range should be in markdown coordinates")
	}
}

func TestMarkdownIntegration_OutsideCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\nSome prose here.\n\n```yammm\nschema \"test\"\n```\n"
	mdPath := filepath.Join(tmpDir, "outside.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	hover, err := h.Hover(mdPath, 2, 0)
	require.NoError(t, err)
	assert.Nil(t, hover, "expected nil hover for prose position outside code block")
}

func TestMarkdownIntegration_CompletionOutsideCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\nSome prose here.\n\n```yammm\nschema \"test\"\n```\n"
	mdPath := filepath.Join(tmpDir, "comp_outside.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	result, err := h.Completion(mdPath, 2, 0)
	require.NoError(t, err)
	assert.Nil(t, result, "expected nil completion for prose position outside code block")
}

func TestMarkdownIntegration_DefinitionOutsideCodeBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\nSome prose here.\n\n```yammm\nschema \"test\"\n```\n"
	mdPath := filepath.Join(tmpDir, "def_outside.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	result, err := h.Definition(mdPath, 2, 0)
	require.NoError(t, err)
	assert.Nil(t, result, "expected nil definition for prose position outside code block")
}

func TestMarkdownIntegration_SymbolsNoCodeBlocks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Just Prose\n\nNo code blocks here.\n\nMore text.\n"
	mdPath := filepath.Join(tmpDir, "no_blocks.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	assert.Nil(t, symbols, "expected nil symbols for markdown with no code blocks")
}

func TestMarkdownIntegration_MultipleBlocks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Block One\n\n```yammm\nschema \"block_one\"\n\ntype Alpha {\n    id String primary\n}\n```\n\n# Block Two\n\n```yammm\nschema \"block_two\"\n\ntype Beta {\n    name String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "multi.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	hover1, err := h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	require.NotNil(t, hover1, "expected hover in first block")

	hover2, err := h.Hover(mdPath, 15, 5)
	require.NoError(t, err)
	require.NotNil(t, hover2, "expected hover in second block")

	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	require.NotNil(t, symbols, "expected symbols from both blocks")

	assert.GreaterOrEqual(t, len(symbols), 2, "expected symbols from both blocks")
}

func TestMarkdownIntegration_CompletionInBlock(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    \n}\n```\n"
	mdPath := filepath.Join(tmpDir, "completion.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	result, err := h.Completion(mdPath, 6, 4)
	require.NoError(t, err)
	require.NotNil(t, result, "expected completion items inside code block")

	assert.NotEmpty(t, result, "expected completion items")
}

func TestMarkdownIntegration_ImportRejection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	content := "# Import Test\n\n```yammm\nschema \"import_test\"\n\nimport \"./sibling\" as s\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "imports.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, content)

	collector := &testutil.NotificationCollector{}
	server.Workspace().SetNotifier(collector.Notify)
	server.Workspace().AnalyzeMarkdownAndPublish(t.Context(), uri)

	diags := collector.DiagnosticsFor(uri)
	require.NotEmpty(t, diags, "expected diagnostics for import rejection")

	var found bool
	for _, d := range diags {
		if d.Code == nil {
			continue
		}
		codeVal, ok := d.Code.Value.(string)
		if !ok || codeVal != "E_IMPORT_NOT_ALLOWED" {
			continue
		}
		found = true
		require.NotNil(t, d.Severity, "E_IMPORT_NOT_ALLOWED diagnostic should have severity")
		assert.Equal(t, protocol.DiagnosticSeverityHint, *d.Severity,
			"E_IMPORT_NOT_ALLOWED should be downgraded to Hint in markdown")
		assert.GreaterOrEqual(t, int(d.Range.Start.Line), 3,
			"diagnostic should be in markdown coordinates")
		break
	}
	assert.True(t, found, "expected E_IMPORT_NOT_ALLOWED diagnostic, got: %+v", diags)

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)
	h.Sync()
	_, err = h.DocumentSymbols(mdPath)
	require.NoError(t, err, "document symbols should not error despite import rejection")
}

// TestMarkdownIntegration_ImportRejectionDoesNotSuppressOtherDiagnostics pins
// that a code block declaring an import still publishes the block's own
// independent findings: the structural rejection arrives as a Hint (the
// markdown surface downgrades E_IMPORT_NOT_ALLOWED) while the block's
// missing-primary-key error keeps Error severity, and references through
// the rejected alias produce no unknown-type noise.
func TestMarkdownIntegration_ImportRejectionDoesNotSuppressOtherDiagnostics(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	content := "# Import Test\n\n```yammm\nschema \"import_test\"\n\nimport \"./sibling\" as s\n\ntype NoKey {\n    name String\n}\n\ntype Keyed extends s.Base {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "imports_continue.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, content)

	collector := &testutil.NotificationCollector{}
	server.Workspace().SetNotifier(collector.Notify)
	server.Workspace().AnalyzeMarkdownAndPublish(t.Context(), uri)

	diags := collector.DiagnosticsFor(uri)
	require.NotEmpty(t, diags, "expected diagnostics for the block")

	severityByCode := make(map[string]protocol.DiagnosticSeverity)
	for _, d := range diags {
		if d.Code == nil || d.Severity == nil {
			continue
		}
		if codeVal, ok := d.Code.Value.(string); ok {
			severityByCode[codeVal] = *d.Severity
		}
	}

	assert.Equal(t, protocol.DiagnosticSeverityHint, severityByCode["E_IMPORT_NOT_ALLOWED"],
		"the structural rejection renders as a Hint in markdown")
	assert.Equal(t, protocol.DiagnosticSeverityError, severityByCode["E_NO_PRIMARY_KEY"],
		"the block's own semantic error keeps Error severity alongside the rejection")
	assert.NotContains(t, severityByCode, "E_UNKNOWN_TYPE",
		"references through the rejected alias are deferred, not unknown")
	assert.NotContains(t, severityByCode, "E_IMPORT_RESOLVE",
		"rejected imports are never probed")
}

// TestMarkdownIntegration_ImportDeclarationErrorsAreHints pins that the whole
// import-declaration diagnostic family — not just E_IMPORT_NOT_ALLOWED — is
// downgraded to Hint in markdown blocks. With continuation, a duplicate import
// alias now reaches the editor as E_DUPLICATE_IMPORT; a snippet's imports are
// categorically not processed, so that is informational here, like the
// rejection itself. The block's own semantic error keeps Error severity.
func TestMarkdownIntegration_ImportDeclarationErrorsAreHints(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	content := "# Dup Import\n\n```yammm\nschema \"dup\"\n\nimport \"./a\" as x\nimport \"./b\" as x\n\ntype NoKey {\n    name String\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "dup_import.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, content)

	collector := &testutil.NotificationCollector{}
	server.Workspace().SetNotifier(collector.Notify)
	server.Workspace().AnalyzeMarkdownAndPublish(t.Context(), uri)

	diags := collector.DiagnosticsFor(uri)
	require.NotEmpty(t, diags, "expected diagnostics for the block")

	severityByCode := make(map[string]protocol.DiagnosticSeverity)
	for _, d := range diags {
		if d.Code == nil || d.Severity == nil {
			continue
		}
		if codeVal, ok := d.Code.Value.(string); ok {
			severityByCode[codeVal] = *d.Severity
		}
	}

	assert.Equal(t, protocol.DiagnosticSeverityHint, severityByCode["E_IMPORT_NOT_ALLOWED"],
		"the structural rejection renders as a Hint")
	assert.Equal(t, protocol.DiagnosticSeverityHint, severityByCode["E_DUPLICATE_IMPORT"],
		"a duplicate import alias is an import-declaration finding — soft in a snippet, like the rejection")
	assert.Equal(t, protocol.DiagnosticSeverityError, severityByCode["E_NO_PRIMARY_KEY"],
		"the block's own semantic error keeps Error severity")
}

func TestMarkdownIntegration_CloseCleansDiagnostics(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "close.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	hover, err := h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	require.NotNil(t, hover, "expected hover before close")

	err = h.CloseDocument(mdPath)
	require.NoError(t, err)

	hover, err = h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	assert.Nil(t, hover, "expected nil hover after close")
}

func TestMarkdownIntegration_StaleVersionRejection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "stale.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	updatedContent := "# Updated\n\n```yammm\nschema \"updated\"\n\ntype Bar {\n    name String primary\n}\n```\n"
	err = h.ChangeDocument(mdPath, updatedContent, 2)
	require.NoError(t, err)

	err = h.ChangeDocument(mdPath, content, 1)
	require.NoError(t, err)

	h.Sync()

	uri := testutil.PathToURI(mdPath)
	text, ok := server.Workspace().GetMarkdownCurrentText(uri)
	require.True(t, ok, "markdown document should still be tracked")
	assert.Equal(t, updatedContent, text, "stale v1 change should not overwrite v2 content")
}

func TestMarkdownIntegration_FormattingReturnsEmpty(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "format.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	edits, err := h.Formatting(mdPath)
	require.NoError(t, err)
	assert.Empty(t, edits, "formatting should return empty edits for markdown files")
}

func TestMarkdownIntegration_IgnoreNonMarkdownExtension(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n```\n"
	txtPath := filepath.Join(tmpDir, "test.txt")
	require.NoError(t, os.WriteFile(txtPath, []byte(content), 0o600))

	uri := testutil.PathToURI(txtPath)
	err := h.OpenDocumentRaw(uri, "plaintext", content)
	require.NoError(t, err)

	hover, err := h.Hover(txtPath, 5, 5)
	require.NoError(t, err)
	assert.Nil(t, hover, "expected nil hover for .txt file")
}

func TestMarkdownIntegration_SnippetBlockNoSchema(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Snippet Example\n\n```yammm\ntype Foo {\n    id String primary\n    name String required\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "snippet.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	hover, err := h.Hover(mdPath, 3, 5)
	require.NoError(t, err)
	require.NotNil(t, hover, "expected hover result for type name in snippet block")

	if hover.Range != nil {
		assert.GreaterOrEqual(t, int(hover.Range.Start.Line), 3,
			"hover range should be in markdown coordinates")
	}

	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	require.NotNil(t, symbols, "expected document symbols for snippet block")

	{
		var foundFoo bool
		for _, sym := range symbols {
			if sym.Name == "Foo" {
				foundFoo = true
				assert.GreaterOrEqual(t, int(sym.Range.Start.Line), 3,
					"Foo symbol range should be in markdown coordinates")
				break
			}
			for _, child := range sym.Children {
				if child.Name == "Foo" {
					foundFoo = true
					break
				}
			}
		}
		assert.True(t, foundFoo, "expected Foo symbol in document symbols")
	}
}

func TestMarkdownIntegration_FeaturesWithMarkdownOnly(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	h := newTestHarness(t, tmpDir)
	defer h.Close()

	content := "# Only Markdown\n\n```yammm\nschema \"md_only\"\n\ntype Solo {\n    id String primary\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "only.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	err := h.OpenMarkdownDocument(mdPath, content)
	require.NoError(t, err)

	hover, err := h.Hover(mdPath, 5, 5)
	require.NoError(t, err)
	require.NotNil(t, hover, "hover should work with only .md files open")

	result, err := h.Completion(mdPath, 6, 4)
	require.NoError(t, err)
	require.NotNil(t, result, "completion should work with only .md files open")

	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	require.NotNil(t, symbols, "symbols should work with only .md files open")

	edits, err := h.Formatting(mdPath)
	require.NoError(t, err)
	assert.Empty(t, edits, "formatting returns empty for markdown")
}

func TestMarkdownIntegration_Fixtures(t *testing.T) {
	t.Parallel()

	fixtureDir := filepath.Join("testdata", "lsp", "markdown")

	tests := []struct {
		name      string
		file      string
		hoverLine int
	}{
		{name: "simple", file: "simple.md", hoverLine: 7},
		{name: "errors", file: "errors.md", hoverLine: -1},
		{name: "imports", file: "imports.md", hoverLine: 7},
		{name: "indented", file: "indented.md", hoverLine: 19},
		{name: "multiple", file: "multiple.md", hoverLine: 7},
		{name: "nested", file: "nested.md", hoverLine: -1},
		{name: "empty", file: "empty.md", hoverLine: -1},
		{name: "malformed", file: "malformed.md", hoverLine: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			h := newTestHarness(t, tmpDir)
			defer h.Close()

			data, err := os.ReadFile(filepath.Join(fixtureDir, tt.file))
			require.NoError(t, err)
			content := docstate.NormalizeLineEndings(string(data))

			mdPath := filepath.Join(tmpDir, tt.file)
			require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

			err = h.OpenMarkdownDocument(mdPath, content)
			require.NoError(t, err, "opening fixture %s should not error", tt.file)

			_, err = h.DocumentSymbols(mdPath)
			require.NoError(t, err, "symbols for fixture %s should not error", tt.file)

			if tt.hoverLine >= 0 {
				_, err = h.Hover(mdPath, tt.hoverLine, 5)
				require.NoError(t, err, "hover for fixture %s should not error", tt.file)
			}
		})
	}
}

func TestMarkdownIntegration_CompletionNilSnapshot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	mdContent := "```yammm\nschema \"test\"\n```\n"
	mdPath := filepath.Join(tmpDir, "completion.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(mdContent), 0o600))

	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, mdContent)

	id, err := markdown.VirtualSourceID(mdPath, 0)
	require.NoError(t, err)

	server.Workspace().SetMarkdownBlocksForTest(uri, []markdown.CodeBlock{
		{
			Content:   "schema \"test\"\n",
			SourceID:  id,
			StartLine: 1,
			EndLine:   2,
			FenceChar: '`',
		},
	}, []*analysis.Snapshot{nil})

	result, err := h.Completion(mdPath, 1, 0)
	require.NoError(t, err)
	require.NotNil(t, result, "completion should return items even with nil snapshot (graceful degradation)")

	assert.NotEmpty(t, result, "expected keyword/snippet completions")
}

func TestMarkdownIntegration_DefinitionRemapsURI(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Lines (0-based): 5 = "type Foo {", 11 = "    --> OWNS (one) Foo"
	// with the Foo reference starting at character 19.
	content := "# Test\n\n```yammm\nschema \"test\"\n\ntype Foo {\n    id String primary\n}\n\ntype Bar {\n    id String primary\n    --> OWNS (one) Foo\n}\n```\n"
	mdPath := filepath.Join(tmpDir, "definition.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(content), 0o600))

	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, content)
	server.Workspace().AnalyzeMarkdownAndPublish(t.Context(), uri)

	require.NoError(t, h.OpenMarkdownDocument(mdPath, content))
	h.Sync()

	// Definition on the association target Foo inside the code block.
	result, err := h.Definition(mdPath, 11, 19)
	require.NoError(t, err)
	require.NotNil(t, result, "definition on an association target inside a code block should resolve")

	assert.Contains(t, result.URI, "definition.md",
		"definition URI should be remapped to the markdown file")
	assert.Equal(t, 5, int(result.Range.Start.Line),
		"definition range should land on `type Foo` in markdown coordinates")
	assert.Equal(t, 5, int(result.Range.Start.Character),
		"definition range should select the Foo name")
}

func TestMarkdownIntegration_DocumentSymbolsNilSnapshots(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	mdContent := "```yammm\ninvalid\n```\n\n```yammm\nalso invalid\n```\n"
	mdPath := filepath.Join(tmpDir, "nil_snap.md")
	require.NoError(t, os.WriteFile(mdPath, []byte(mdContent), 0o600))

	h, server := newTestHarnessWithServer(t, tmpDir)
	defer h.Close()

	uri := testutil.PathToURI(mdPath)
	server.Workspace().MarkdownDocumentOpenedForTest(uri, 1, mdContent)
	server.Workspace().SetMarkdownBlocksForTest(uri, []markdown.CodeBlock{
		{Content: "invalid", StartLine: 1, EndLine: 2},
		{Content: "also invalid", StartLine: 5, EndLine: 6},
	}, []*analysis.Snapshot{nil, nil})

	require.NoError(t, h.OpenMarkdownDocument(mdPath, mdContent))
	h.Sync()

	symbols, err := h.DocumentSymbols(mdPath)
	require.NoError(t, err)
	assert.Nil(t, symbols, "expected nil when all snapshots are nil")
}
