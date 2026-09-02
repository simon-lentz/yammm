package workspace

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/schema"
)

// hasCode reports whether res carries an issue with the given code.
func hasCode(res diag.Result, code diag.Code) bool {
	for issue := range res.Issues() {
		if issue.Code() == code {
			return true
		}
	}
	return false
}

// singleIssueResult wraps one issue as a diag.Result, the shape the workspace
// hands SnapshotForResult when discovery fails before any load.
func singleIssueResult(issue diag.Issue) diag.Result {
	c := diag.NewCollectorUnlimited()
	c.Collect(issue)
	return c.Result()
}

// waitFor blocks until the analysis-completed hook reports want, or the test
// deadline passes.
func waitFor(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case got := <-ch:
			if got == want {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for analysis of %s", want)
		}
	}
}

// quietLogger keeps test output to real failures.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// markerTree builds root/<marker>, root/lib/dep.yammm and root/sub/entry.yammm,
// where the entry's module-style import resolves only against root.
func markerTree(t *testing.T, markerContent string) (root, entry string) {
	t.Helper()
	root = t.TempDir()
	for _, d := range []string{"lib", "sub"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, schema.ModuleRootMarker), []byte(markerContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "dep.yammm"),
		[]byte("schema \"dep\"\n\ntype P {\n\tpid String primary\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry = filepath.Join(root, "sub", "entry.yammm")
	if err := os.WriteFile(entry,
		[]byte("schema \"entry\"\n\nimport \"lib/dep\" as dep\n\ntype T {\n\tid String primary\n\t--> USES (one) dep.P\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, entry
}

// TestModuleRoot_EditorAndLoaderAgree is the instrument the exported finder
// exists for. The editor never calls schema.Load — it loads through
// LoadSourcesWithEntry with the root as an argument — so a discovery mechanism
// placed inside Load alone would give `yammm validate` one root and the editor
// another for the same file. Nothing else pins that they agree: lsp/ is
// permanently outside the review program's scope.
func TestModuleRoot_EditorAndLoaderAgree(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root, entry := markerTree(t, "# project root\n")
	canonical := lsputil.CanonicalPath(root)

	direct, found, err := schema.FindModuleRoot(filepath.Dir(entry))
	if err != nil || !found {
		t.Fatalf("schema.FindModuleRoot = (%q, %v, %v)", direct, found, err)
	}
	if direct != canonical {
		t.Errorf("schema.FindModuleRoot = %q, want %q", direct, canonical)
	}

	ws := newTestWorkspace(t, quietLogger(), Config{})
	editor, err := ws.FindModuleRoot(lsputil.CanonicalPath(entry))
	if err != nil {
		t.Fatalf("Workspace.FindModuleRoot: %v", err)
	}
	if editor != canonical {
		t.Errorf("Workspace.FindModuleRoot = %q, want %q — the editor and the CLI must answer one root", editor, canonical)
	}

	s, res := schema.Load(t.Context(), entry)
	if res.HasErrors() {
		t.Fatalf("schema.Load: %v", res.Err())
	}
	if s.ModuleRoot() != canonical {
		t.Errorf("Load recorded %q, want %q", s.ModuleRoot(), canonical)
	}
}

// TestModuleRoot_MarkerBeatsWorkspaceFolder pins the ladder's third rung: the
// marker is a file the author committed, the folder is wherever the editor
// happened to be opened.
func TestModuleRoot_MarkerBeatsWorkspaceFolder(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root, entry := markerTree(t, "")
	outer := filepath.Dir(root)

	ws := newTestWorkspace(t, quietLogger(), Config{})
	ws.AddRoot(lsputil.PathToURI(outer))

	got, err := ws.FindModuleRoot(lsputil.CanonicalPath(entry))
	if err != nil {
		t.Fatalf("Workspace.FindModuleRoot: %v", err)
	}
	if want := lsputil.CanonicalPath(root); got != want {
		t.Errorf("FindModuleRoot = %q, want the marker's directory %q over the workspace folder %q", got, want, outer)
	}
}

// TestModuleRoot_ExplicitConfigBeatsMarker pins tier 1 in the editor, matching
// the loader's own rule that an explicit root is never overridden.
func TestModuleRoot_ExplicitConfigBeatsMarker(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	_, entry := markerTree(t, "")

	ws := newTestWorkspace(t, quietLogger(), Config{ModuleRoot: "/configured/root"})
	got, err := ws.FindModuleRoot(lsputil.CanonicalPath(entry))
	if err != nil {
		t.Fatalf("Workspace.FindModuleRoot: %v", err)
	}
	if got != "/configured/root" {
		t.Errorf("FindModuleRoot = %q, want the configured root", got)
	}
}

// TestModuleRoot_MalformedMarkerReachesTheEditor pins that the editor reports
// the same failure the CLI reports. Without a failure path the editor's only
// options were a silent fall-through to the workspace folder — the ignored
// marker this mechanism exists to remove — or nothing at all.
func TestModuleRoot_MalformedMarkerReachesTheEditor(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	_, entry := markerTree(t, "module example.com/thing\n")

	// The CLI half.
	_, res := schema.Load(t.Context(), entry)
	if !hasCode(res, diag.E_LOAD_MODULE_ROOT_MALFORMED) {
		t.Fatalf("Load did not report the malformed marker: %v", res.Err())
	}

	// The editor half: discovery fails, and the failure becomes a document
	// diagnostic rather than a dropped analysis.
	ws := newTestWorkspace(t, quietLogger(), Config{})
	if _, err := ws.FindModuleRoot(lsputil.CanonicalPath(entry)); err == nil {
		t.Fatal("Workspace.FindModuleRoot returned no error for a malformed marker")
	} else {
		issue := schema.ModuleRootIssue(err)
		if issue.Code() != diag.E_LOAD_MODULE_ROOT_MALFORMED {
			t.Errorf("editor issue code = %v, want E_LOAD_MODULE_ROOT_MALFORMED", issue.Code())
		}
		if issue.Severity() != diag.Error {
			t.Errorf("editor issue severity = %v, want Error", issue.Severity())
		}
	}
}

// TestSnapshotForResult_KeepsAnalyzeContract pins the entry the workspace uses
// when it must publish a diagnostic without loading: an Error-only result
// publishes through the success path, a Fatal one returns the error, exactly
// as Analyze does.
func TestSnapshotForResult_KeepsAnalyzeContract(t *testing.T) {
	t.Parallel()

	a := analysis.NewAnalyzer(quietLogger())
	entry := filepath.Join(t.TempDir(), "entry.yammm")

	errorOnly := singleIssueResult(diag.NewIssue(diag.Error, diag.E_LOAD_MODULE_ROOT_MALFORMED, "bad marker").Build())
	snap, err := a.SnapshotForResult(entry, errorOnly, lsputil.PositionEncodingUTF16)
	if err != nil {
		t.Errorf("SnapshotForResult on an Error-only result returned %v, want nil", err)
	}
	if snap == nil {
		t.Fatal("SnapshotForResult returned no snapshot")
	}
	if snap.Schema != nil {
		t.Error("snapshot carries a schema; nothing was loaded")
	}
	if len(snap.LSPDiagnostics) != 1 {
		t.Fatalf("snapshot carries %d diagnostics, want 1", len(snap.LSPDiagnostics))
	}
	if want := lsputil.PathToURI(entry); snap.LSPDiagnostics[0].URI != want {
		t.Errorf("diagnostic URI = %q, want the entry document %q", snap.LSPDiagnostics[0].URI, want)
	}

	fatal := singleIssueResult(diag.NewIssue(diag.Fatal, diag.E_LOAD_IO_FAILURE, "cannot read marker").Build())
	if _, err := a.SnapshotForResult(entry, fatal, lsputil.PositionEncodingUTF16); err == nil {
		t.Error("SnapshotForResult on a Fatal result returned nil, want the error Analyze returns")
	}
}

// TestFileChanged_MarkerReanalyzesOpenDocuments pins that creating, editing or
// deleting a marker refreshes the editor. Nothing is cached, so a marker event
// is the only signal that a root moved.
func TestFileChanged_MarkerReanalyzesOpenDocuments(t *testing.T) {
	t.Parallel()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)

	root, entry := markerTree(t, "")
	ws := newTestWorkspace(t, quietLogger(), Config{})

	uri := lsputil.PathToURI(lsputil.CanonicalPath(entry))
	content, err := os.ReadFile(entry)
	if err != nil {
		t.Fatal(err)
	}
	analyzed := make(chan string, 8)
	ws.setAnalysisCompletedHook(func(u string) {
		select {
		case analyzed <- u:
		default:
		}
	})
	ws.OpenDocument(uri, 1, string(content))
	waitFor(t, analyzed, uri)

	markerURI := lsputil.PathToURI(filepath.Join(root, schema.ModuleRootMarker))
	ws.FileChanged(markerURI, 2 /* Changed */)
	waitFor(t, analyzed, uri)
}
