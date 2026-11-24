package source

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/simon-lentz/yammm/location"
)

// NormalizeSyntheticKey returns key in the one form a synthetic root joins:
// slash-separated text, cleaned as [path.Clean] cleans it, in NFC, with a
// leading ".." kept for a source outside the root. It refuses a key holding a
// backslash, a key whose raw or normalized form [location.NewSourceID] refuses,
// and a key that names the root or a directory above it rather than a file.
// A normalized key is a fixed point, and its form does not depend on the host.
func NormalizeSyntheticKey(key string) (string, error) {
	if key != "" {
		if _, err := location.NewSourceID(key); errors.Is(err, location.ErrAbsolutePathSourceID) {
			return "", fmt.Errorf("source key %q must be relative to the synthetic root: %w", key, err)
		} else if err != nil {
			return "", fmt.Errorf("source key %q: %w", key, err)
		}
	}
	// One host reads a backslash as a separator and another as part of a file
	// name, so no single reading of such a key holds on every host.
	if strings.ContainsRune(key, '\\') {
		return "", fmt.Errorf("source key %q holds a backslash; a synthetic key separates its segments with / alone", key)
	}
	normalized := norm.NFC.String(path.Clean(key)) // "" cleans to "."
	switch {
	case normalized == ".":
		return "", fmt.Errorf("source key %q resolves to the synthetic root itself", key)
	case normalized == ".." || strings.HasSuffix(normalized, "/.."):
		return "", fmt.Errorf("source key %q names a directory above the synthetic root, not a file", key)
	}
	// Cleaning and NFC can each make a key look absolute: "./C:/x.yammm" cleans
	// to "C:/x.yammm", and NFC maps U+212A KELVIN SIGN to "K".
	if _, err := location.NewSourceID(normalized); err != nil {
		return "", fmt.Errorf("source key %q must be relative to the synthetic root once normalized to %q: %w", key, normalized, err)
	}
	return normalized, nil
}
