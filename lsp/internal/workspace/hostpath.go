package workspace

import (
	"path/filepath"

	"github.com/simon-lentz/yammm/location"
)

// hostPath is the editor's door to [location.ResolveHostPath], so a key the
// LSP mints matches the SourceID the loader mints for one file. A path the
// resolver refuses keeps its cleaned absolute form, because a deleted file
// still needs a stable key and its event must not be dropped.
func hostPath(path string) string {
	if resolved, err := location.ResolveHostPath(path); err == nil {
		return resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		return filepath.Clean(abs)
	}
	return path
}
