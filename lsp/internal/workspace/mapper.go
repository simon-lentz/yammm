package workspace

import (
	"path/filepath"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// uriMapper manages canonical path mapping and per-entry publication tracking.
//
// All fields are protected by Workspace.mu (external lock).
// Callers must hold Workspace.mu before calling any method.
type uriMapper struct {
	// Cached mapping from canonical paths to client URIs (for symlink resolution).
	//
	// Path normalization invariants:
	//   - Keys use forward-slash normalized paths matching SourceID.String() format
	//   - This enables cross-platform consistency (Windows paths converted to forward slashes)
	//   - The location.CanonicalPath type guarantees: absolute, clean, NFC-normalized, forward slashes
	//   - FindModuleRoot operates on OS-native paths separately (uses filepath.Separator)
	//
	// Nil when invalidated; lazily rebuilt on first access after mutation.
	// Invalidated by documentOpened/documentClosed.
	canonicalToURI map[string]string

	// Previously published diagnostics URIs, keyed by entry URI.
	// This per-entry tracking ensures that analyzing one entry doesn't
	// clear diagnostics published by a different entry file.
	publishedByEntry map[string]map[string]struct{}
}

// invalidateCache marks the canonical-to-URI cache as stale.
// Must be called with Workspace.mu held.
func (m *uriMapper) invalidateCache() {
	m.canonicalToURI = nil
}

// ensureCache rebuilds the canonical-to-URI cache if invalidated.
// Returns the current cache. Must be called with Workspace.mu held.
func (m *uriMapper) ensureCache(open map[string]*docstate.Document) map[string]string {
	if m.canonicalToURI == nil {
		m.canonicalToURI = m.buildCanonicalToURIMap(open)
	}
	return m.canonicalToURI
}

// buildCanonicalToURIMap builds a mapping from canonical (symlink-resolved)
// paths to the URIs used by clients to open documents.
//
// This is needed because the schema loader resolves symlinks (via makeCanonicalPath),
// so diagnostics reference canonical paths. But clients identify documents by the
// URI they used to open them, which may be a symlink path. This mapping allows
// us to translate diagnostic URIs back to client-expected URIs.
//
// Must be called with Workspace.mu held.
func (m *uriMapper) buildCanonicalToURIMap(open map[string]*docstate.Document) map[string]string {
	canonicalToURI := make(map[string]string, len(open))
	openOrderByCanonical := make(map[string]int, len(open))

	for uri, doc := range open {
		var canonical string

		// Prefer SourceID which is already canonicalized at open time
		if !doc.SourceID.IsZero() {
			canonical = doc.SourceID.String()
		} else {
			// Fallback for non-file URIs or when SourceID unavailable
			path, err := lsputil.URIToPath(uri)
			if err != nil {
				continue
			}

			// Resolve symlinks to get the canonical path (matching loader behavior).
			// filepath.EvalSymlinks also cleans the path.
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				// If symlink resolution fails (broken symlink, permissions, etc.),
				// fall back to the cleaned path. This ensures we still have a mapping
				// even when EvalSymlinks fails.
				canonical = filepath.Clean(path)
			} else {
				canonical = resolved
			}
			// Convert to forward slashes to match SourceID.String() format
			canonical = filepath.ToSlash(canonical)
		}

		// For determinism when multiple URIs resolve to the same canonical path,
		// prefer the document that was opened first (lowest OpenOrder).
		if existingOrder, exists := openOrderByCanonical[canonical]; !exists || doc.OpenOrder < existingOrder {
			canonicalToURI[canonical] = uri
			openOrderByCanonical[canonical] = doc.OpenOrder
		}
	}

	return canonicalToURI
}

// remapToOpenDocURI remaps a diagnostic URI to an open document URI if the
// underlying file matches an open buffer.
//
// Must be called with Workspace.mu held.
func (m *uriMapper) remapToOpenDocURI(diagURI string, cache map[string]string) string {
	path, err := lsputil.URIToPath(diagURI)
	if err != nil {
		if lsputil.HasURIScheme(diagURI) {
			return diagURI
		}
		path = diagURI
	}

	lookupPath := filepath.ToSlash(path)

	if docURI, ok := cache[lookupPath]; ok {
		return docURI
	}

	if err != nil {
		return lsputil.PathToURI(path)
	}
	return diagURI
}

// computePublicationPlan computes the publication plan for diagnostics.
// It returns the diagnostics grouped by URI (with symlink remapping applied),
// the list of stale URIs that need to be cleared, and document versions.
//
// Must be called with Workspace.mu held.
func (m *uriMapper) computePublicationPlan(entryURI string, snapshot *analysis.Snapshot, open map[string]*docstate.Document) (diagsByURI map[string][]protocol.Diagnostic, staleURIs []string, versionsByURI map[string]*protocol.Integer) {
	cache := m.ensureCache(open)

	currentURIs := make(map[string]struct{})

	diagsByURI = make(map[string][]protocol.Diagnostic)

	for _, lspDiag := range snapshot.LSPDiagnostics {
		pubURI := m.remapToOpenDocURI(lspDiag.URI, cache)

		diag := lspDiag.Diagnostic
		if len(diag.RelatedInformation) > 0 {
			remapped := make([]protocol.DiagnosticRelatedInformation, len(diag.RelatedInformation))
			for i, rel := range diag.RelatedInformation {
				remapped[i] = protocol.DiagnosticRelatedInformation{
					Location: protocol.Location{
						URI:   m.remapToOpenDocURI(rel.Location.URI, cache),
						Range: rel.Location.Range,
					},
					Message: rel.Message,
				}
			}
			diag.RelatedInformation = remapped
		}

		diagsByURI[pubURI] = append(diagsByURI[pubURI], diag)
		currentURIs[pubURI] = struct{}{}
	}

	previousURIs := m.publishedByEntry[entryURI]
	staleURIs = make([]string, 0)
	for uri := range previousURIs {
		if _, ok := currentURIs[uri]; !ok {
			staleURIs = append(staleURIs, uri)
		}
	}

	m.publishedByEntry[entryURI] = currentURIs

	versionsByURI = make(map[string]*protocol.Integer)
	for uri := range diagsByURI {
		if doc, ok := open[uri]; ok {
			v := protocol.Integer(doc.Version) //nolint:gosec // LSP version fits int32
			versionsByURI[uri] = &v
		}
	}
	for _, uri := range staleURIs {
		if doc, ok := open[uri]; ok {
			v := protocol.Integer(doc.Version) //nolint:gosec // LSP version fits int32
			versionsByURI[uri] = &v
		}
	}

	return diagsByURI, staleURIs, versionsByURI
}

// clearEntryPublications removes and returns the set of URIs published by the
// given entry. Used during documentClosed to determine what diagnostics to clear.
//
// Must be called with Workspace.mu held.
func (m *uriMapper) clearEntryPublications(entryURI string) map[string]struct{} {
	published := m.publishedByEntry[entryURI]
	delete(m.publishedByEntry, entryURI)
	return published
}

// uriStillPublishedByOthers checks if any other entry still publishes diagnostics
// to the given URI. Used during documentClosed to avoid clearing diagnostics that
// are still relevant from another entry's analysis.
//
// Must be called with Workspace.mu held.
func (m *uriMapper) uriStillPublishedByOthers(pubURI string) bool {
	for _, otherPubs := range m.publishedByEntry {
		if _, ok := otherPubs[pubURI]; ok {
			return true
		}
	}
	return false
}

// remapPathToURI maps a path to the client's document URI if the file is open.
//
// Must be called with Workspace.mu held.
func (m *uriMapper) remapPathToURI(input string, open map[string]*docstate.Document) string {
	var rawPath string
	if path, err := lsputil.URIToPath(input); err == nil {
		rawPath = path
	} else if lsputil.HasURIScheme(input) {
		return input
	} else {
		rawPath = input
	}

	cleanedPath := filepath.ToSlash(filepath.Clean(rawPath))

	cache := m.ensureCache(open)

	if docURI, ok := cache[cleanedPath]; ok {
		return docURI
	}

	if resolved, err := filepath.EvalSymlinks(rawPath); err == nil {
		canonicalPath := filepath.ToSlash(filepath.Clean(resolved))
		if canonicalPath != cleanedPath {
			if docURI, ok := cache[canonicalPath]; ok {
				return docURI
			}
		}
		return lsputil.PathToURI(canonicalPath)
	}

	return lsputil.PathToURI(cleanedPath)
}
