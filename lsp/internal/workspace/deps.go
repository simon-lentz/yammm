package workspace

import (
	"log/slog"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// depGraph tracks forward and reverse import dependencies between entry files.
//
// All fields are protected by Workspace.mu (external lock).
// Callers must hold Workspace.mu before calling any method.
type depGraph struct {
	// Forward dependencies: entry URI -> set of imported URIs
	importsByEntry map[string]map[string]struct{}

	// Reverse dependencies: imported URI -> set of entry URIs that import it
	reverseDeps map[string]map[string]struct{}
}

// updateDependencies updates the dependency tracking for an entry file.
// importedPaths contains the absolute paths of all imported files in the closure.
// This uses an atomic update algorithm: remove old edges, add new edges, store new set.
//
// Must be called with Workspace.mu held.
func (dg *depGraph) updateDependencies(entryURI string, importedPaths []string, logger *slog.Logger) {
	// Convert imported paths to URIs
	importedURIs := make(map[string]struct{}, len(importedPaths))
	for _, path := range importedPaths {
		importedURIs[lsputil.PathToURI(path)] = struct{}{}
	}

	// Step 1: Remove old edges from reverse deps
	if oldImports, ok := dg.importsByEntry[entryURI]; ok {
		for importURI := range oldImports {
			if entries, exists := dg.reverseDeps[importURI]; exists {
				delete(entries, entryURI)
				// Clean up empty reverse dep entries
				if len(entries) == 0 {
					delete(dg.reverseDeps, importURI)
				}
			}
		}
	}

	// Step 2: Add new edges to reverse deps
	for importURI := range importedURIs {
		if dg.reverseDeps[importURI] == nil {
			dg.reverseDeps[importURI] = make(map[string]struct{})
		}
		dg.reverseDeps[importURI][entryURI] = struct{}{}
	}

	// Step 3: Store new import set (or delete if empty)
	if len(importedURIs) == 0 {
		delete(dg.importsByEntry, entryURI)
	} else {
		dg.importsByEntry[entryURI] = importedURIs
	}

	logger.Debug(
		"updated dependencies",
		slog.String("entry", entryURI),
		slog.Int("imports", len(importedURIs)),
	)
}

// reverseDependents returns a copy of the set of entry URIs that import
// the given canonical URI. Returns nil if there are no dependents.
//
// Must be called with Workspace.mu held (at least RLock).
func (dg *depGraph) reverseDependents(canonicalURI string) map[string]struct{} {
	entries := dg.reverseDeps[canonicalURI]
	if len(entries) == 0 {
		return nil
	}
	result := make(map[string]struct{}, len(entries))
	for k := range entries {
		result[k] = struct{}{}
	}
	return result
}
