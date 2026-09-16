package workspace

import (
	"fmt"

	"github.com/simon-lentz/yammm/location"
)

// hostPath is the editor's door to [location.ResolveHostPath], so a key the LSP
// mints matches the one the loader mints for one file. A deleted file resolves
// like any path that does not exist; the error is a path that can never name a
// file.
func hostPath(path string) (string, error) {
	resolved, err := location.ResolveHostPath(path)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", path, err)
	}
	return resolved, nil
}
