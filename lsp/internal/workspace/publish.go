package workspace

import (
	"encoding/json"
	"hash/fnv"
	"sync"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
)

// diagHashState holds the diagnostic hash state.
// Separate from Workspace.mu because publishDiagnostics
// is called outside the workspace lock (to avoid deadlock on I/O).
type diagHashState struct {
	mu     sync.Mutex
	hashes map[string]uint64 // URI -> FNV hash of last published diagnostics
}

// publishSnapshotDiagnostics publishes diagnostics from a snapshot.
// This method computes the publication plan under lock, then releases the lock
// before sending notifications to avoid potential deadlock if Notify blocks.
// No-op if the workspace notifier has not been set.
func (w *Workspace) publishSnapshotDiagnostics(entryURI string, snapshot *analysis.Snapshot) {
	diagsByURI, staleURIs, versionsByURI := w.computePublicationPlan(entryURI, snapshot)

	for _, uri := range staleURIs {
		w.publishDiagnostics(uri, versionsByURI[uri], nil)
	}
	for uri, diags := range diagsByURI {
		w.publishDiagnostics(uri, versionsByURI[uri], diags)
	}
}

// computePublicationPlan computes the publication plan for diagnostics.
// This method acquires the workspace lock internally.
func (w *Workspace) computePublicationPlan(entryURI string, snapshot *analysis.Snapshot) (diagsByURI map[string][]protocol.Diagnostic, staleURIs []string, versionsByURI map[string]*protocol.Integer) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mapper.computePublicationPlan(entryURI, snapshot, w.docs.Open)
}

// publishDiagnostics publishes diagnostics for a URI using the stored notifier.
// No-op when the notifier has not been configured (e.g., in tests without a transport).
// This method does not require the workspace lock as it only sends a notification
// and does not access any workspace state. Calling notify under lock risks
// deadlock if the notification blocks on I/O.
func (w *Workspace) publishDiagnostics(uri string, version *protocol.Integer, diagnostics []protocol.Diagnostic) {
	w.notifyMu.RLock()
	fn := w.notifyFn
	w.notifyMu.RUnlock()
	if fn == nil {
		return
	}

	if diagnostics == nil {
		diagnostics = []protocol.Diagnostic{}
	}

	if hash, ok := hashDiagnostics(diagnostics); ok {
		w.diagHash.mu.Lock()
		if prev, exists := w.diagHash.hashes[uri]; exists && prev == hash {
			w.diagHash.mu.Unlock()
			return
		}
		w.diagHash.hashes[uri] = hash
		w.diagHash.mu.Unlock()
	}

	fn(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Version:     version,
		Diagnostics: diagnostics,
	})
}

// clearDiagHash removes the diagnostic hash entry for a URI.
// Called when documents are closed to ensure fresh diagnostics on re-open.
func (w *Workspace) clearDiagHash(uri string) {
	w.diagHash.mu.Lock()
	delete(w.diagHash.hashes, uri)
	w.diagHash.mu.Unlock()
}

// hashDiagnostics computes a FNV-1a hash of a diagnostics slice for deduplication.
// Returns false if marshaling fails (caller should publish unconditionally).
func hashDiagnostics(diags []protocol.Diagnostic) (uint64, bool) {
	data, err := json.Marshal(diags)
	if err != nil {
		return 0, false
	}
	h := fnv.New64a()
	_, _ = h.Write(data)
	return h.Sum64(), true
}
