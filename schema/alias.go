package schema

import (
	"strings"
)

// deriveAliasFromPath extracts the default alias from an import path.
//
// Rules:
//  1. Strip trailing slashes
//  2. Extract last path segment (after final `/`)
//  3. Remove .yammm extension if present
//  4. Replace non-alphanumeric/underscore chars with underscore
//  5. Prepend "n" if first character is a digit (produces valid identifier)
//  6. Return "n" if result is empty
//
// The alias is NOT uppercased: "./parts" -> "parts", not "Parts".
func deriveAliasFromPath(path string) string {
	// Strip trailing slashes
	path = strings.TrimRight(path, "/")

	// Get last segment
	lastSlash := strings.LastIndex(path, "/")
	segment := path
	if lastSlash >= 0 {
		segment = path[lastSlash+1:]
	}

	// Strip .yammm extension
	segment = strings.TrimSuffix(segment, ".yammm")

	// Sanitize: replace non-alphanumeric/underscore with underscore
	var sanitized strings.Builder
	for _, r := range segment {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			sanitized.WriteRune(r)
		} else {
			// Replace hyphens, dots, and other chars with underscore
			sanitized.WriteRune('_')
		}
	}
	segment = sanitized.String()

	// If empty, return "n" (a valid identifier)
	if len(segment) == 0 {
		return "n"
	}
	// If first character is not a letter, prepend "n" to produce a valid identifier
	// (e.g., "2parts" -> "n2parts", "___" -> "n___"). This ensures the derived alias
	// passes parse.IsIdentifier, which requires starting with a letter.
	first := segment[0]
	isLetter := (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
	if !isLetter {
		segment = "n" + segment
	}

	return segment
}
