package lsp

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/creachadair/jrpc2"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
	"github.com/simon-lentz/yammm/lsp/internal/workspace"

	"github.com/stretchr/testify/require"
)

// Compile-time interface satisfaction checks.
var (
	_ Resolver        = (*fakeResolver)(nil)
	_ DocumentManager = (*fakeDocManager)(nil)
	_ Resolver        = (*workspace.Workspace)(nil)
	_ DocumentManager = (*workspace.Workspace)(nil)
)

// fakeResolver implements Resolver with settable return values.
// Place in test files since it's only used for handler unit tests.
type fakeResolver struct {
	unit     *workspace.Unit
	units    []workspace.Unit
	encoding lsputil.PositionEncoding
	docSnap  *docstate.Snapshot
	uriMap   map[string]string
}

func (f *fakeResolver) ResolveUnit(_ string, _, _ int, _ bool) *workspace.Unit {
	return f.unit
}

func (f *fakeResolver) ResolveAllUnits(_ string) []workspace.Unit {
	return f.units
}

func (f *fakeResolver) PositionEncoding() lsputil.PositionEncoding {
	if f.encoding == "" {
		return lsputil.PositionEncodingUTF16
	}
	return f.encoding
}

func (f *fakeResolver) GetDocumentSnapshot(_ string) *docstate.Snapshot {
	return f.docSnap
}

func (f *fakeResolver) RemapPathToURI(input string) string {
	if f.uriMap != nil {
		if mapped, ok := f.uriMap[input]; ok {
			return mapped
		}
	}
	return lsputil.PathToURI(input)
}

// fakeDocManager implements DocumentManager with call tracking.
type fakeDocManager struct {
	openCalls   []string
	changeCalls []string
	closeCalls  []string
}

func (f *fakeDocManager) OpenDocument(uri string, _ int, _ string) {
	f.openCalls = append(f.openCalls, uri)
}

func (f *fakeDocManager) ChangeDocument(uri string, _ int, _ string) {
	f.changeCalls = append(f.changeCalls, uri)
}

func (f *fakeDocManager) CloseDocument(uri string)                  { f.closeCalls = append(f.closeCalls, uri) }
func (f *fakeDocManager) FileChanged(_ string, _ protocol.UInteger) {}
func (f *fakeDocManager) AddRoot(_ string)                          {}
func (f *fakeDocManager) RemoveRoot(_ string)                       {}
func (f *fakeDocManager) ReanalyzeOpenDocuments()                   {}

// callHandler invokes a jrpc2.Handler with the given params and decodes the result.
func callHandler(t *testing.T, h jrpc2.Handler, params any, result any) error {
	t.Helper()
	data, err := json.Marshal(params)
	require.NoError(t, err)
	req, err := jrpc2.ParseRequests([]byte(`{"jsonrpc":"2.0","id":1,"method":"test","params":` + string(data) + `}`))
	require.NoError(t, err)
	require.Len(t, req, 1)
	resp, err := h(t.Context(), req[0].ToRequest())
	if err != nil {
		return err
	}
	if result != nil && resp != nil {
		respData, jsonErr := json.Marshal(resp)
		require.NoError(t, jsonErr)
		require.NoError(t, json.Unmarshal(respData, result))
	}
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// buildTestSnapshot creates a minimal analysis.Snapshot with a type symbol at a
// known position. The source content is registered in the source.Registry so
// that PositionFromLSP succeeds.
//
// Source layout (0-indexed lines):
//
//	0: schema "test"
//	1: (empty)
//	2: type Person {
//	3:     name String
//	4: }
func buildTestSnapshot(t *testing.T) (*analysis.Snapshot, *docstate.Snapshot) {
	t.Helper()

	const content = "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	reg := source.NewRegistry()
	s, result := schema.LoadString(t.Context(), content, "test.yammm", schema.WithSourceRegistry(reg))
	require.True(t, result.OK(), "schema should parse without errors: %v", result)

	sourceID := s.SourceID()

	typ, ok := s.Type("Person")
	require.True(t, ok, "schema should contain type Person")

	// Build the symbol index from the parsed schema so spans are
	// parser-generated rather than hand-crafted.
	idx := symbols.BuildSymbolIndex(s, reg)

	snapshot := &analysis.Snapshot{
		EntrySourceID:   sourceID,
		EntryVersion:    1,
		Root:            "/test",
		Schema:          s,
		Sources:         reg,
		SymbolsBySource: map[location.SourceID]*symbols.SymbolIndex{sourceID: idx},
	}

	// Verify the symbol index contains the expected symbols so callers
	// can rely on hover, definition, and completion finding them.
	require.NotNil(t, idx.FindByName("test", symbols.SymbolSchema),
		"symbol index should contain schema symbol 'test'")
	personSym := idx.FindByName("Person", symbols.SymbolType)
	require.NotNil(t, personSym, "symbol index should contain type symbol 'Person'")
	require.Equal(t, typ, personSym.Data, "Person symbol should reference the parsed type")

	doc := &docstate.Snapshot{
		URI:      "file:///test.yammm",
		SourceID: sourceID,
		Version:  1,
		Text:     content,
	}

	return snapshot, doc
}

// buildCrossSchemaSnapshot creates a snapshot with cross-schema imports using
// the real analyzer. It writes two schema files to a temp dir:
//   - foundation.yammm: defines a Region type with properties
//   - entry.yammm: imports foundation as "region" and extends region.Region
//
// Returns the snapshot (with resolved imports and symbol indices), docSnapshot
// for the entry file, and the canonicalized temp dir.
func buildCrossSchemaSnapshot(t *testing.T) (*analysis.Snapshot, *docstate.Snapshot, string) {
	t.Helper()

	tmpDir := t.TempDir()
	// Canonicalize for macOS /var → /private/var symlink.
	tmpDir, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)

	foundationContent := "schema \"foundation\"\n\ntype Region {\n\tarea_code String\n\tname String\n}\n"
	entryContent := "schema \"entry\"\n\nimport \"./foundation\" as region\n\ntype Listing extends region.Region {\n\textra String\n}\n"

	foundationPath := filepath.Join(tmpDir, "foundation.yammm")
	entryPath := filepath.Join(tmpDir, "entry.yammm")

	require.NoError(t, os.WriteFile(foundationPath, []byte(foundationContent), 0o600))
	require.NoError(t, os.WriteFile(entryPath, []byte(entryContent), 0o600))

	analyzer := analysis.NewAnalyzer(testLogger())
	overlays := map[string][]byte{
		entryPath: []byte(entryContent),
	}

	snapshot, err := analyzer.Analyze(t.Context(), entryPath, overlays, tmpDir, lsputil.PositionEncodingUTF16)
	require.NoError(t, err)
	require.NotNil(t, snapshot)
	require.NotNil(t, snapshot.Schema, "schema should parse successfully")
	require.True(t, snapshot.Result.OK(), "schema should have no errors")

	entrySourceID, err := location.SourceIDFromAbsolutePath(entryPath)
	require.NoError(t, err)

	doc := &docstate.Snapshot{
		URI:      lsputil.PathToURI(entryPath),
		SourceID: entrySourceID,
		Version:  1,
		Text:     entryContent,
	}

	return snapshot, doc, tmpDir
}

// completionLabels extracts just the Label field from completion items for diagnostics.
func completionLabels(items []protocol.CompletionItem) []string {
	labels := make([]string, len(items))
	for i, item := range items {
		labels[i] = item.Label
	}
	return labels
}
