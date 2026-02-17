package alias

import (
	"maps"
	"regexp"
	"strings"
)

// validAliasRE matches identifiers that are valid per the grammar: start with
// a letter (A-Z or a-z), followed by any combination of letters, digits, or
// underscores.
var validAliasRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// reservedKeywords contains all tokens that cannot be used as import aliases
// because the lexer tokenizes them as literal keywords rather than identifiers.
//
// This map must remain synchronized with the grammar. The Grammar-Alias
// Synchronization Test in alias_test.go verifies this.
var reservedKeywords = map[string]bool{
	// DSL keywords (from lc_keyword rule and others)
	"schema":   true,
	"import":   true,
	"as":       true,
	"type":     true,
	"datatype": true,
	"required": true,
	"primary":  true,
	"extends":  true,
	"includes": true,
	"abstract": true,
	"part":     true,
	"one":      true,
	"many":     true,
	"in":       true,

	// Built-in type keywords (from datatypeKeyword rule)
	"Integer":   true,
	"Float":     true,
	"Boolean":   true,
	"String":    true,
	"Enum":      true,
	"Pattern":   true,
	"Timestamp": true,
	"Date":      true,
	"UUID":      true,
	"Vector":    true,

	// Boolean literals
	"true":  true,
	"false": true,

	// Nil literal keyword
	"nil": true,
}

// ReservedKeywords returns a copy of all reserved keywords that cannot be used
// as import aliases. This is primarily for testing and diagnostic purposes.
func ReservedKeywords() map[string]bool {
	return maps.Clone(reservedKeywords)
}

// IsReservedKeyword returns true if the alias is a reserved keyword that cannot
// be used as an import alias. Reserved keywords are tokenized by the lexer as
// literal tokens rather than identifiers, making them unusable as qualifiers.
func IsReservedKeyword(alias string) bool {
	return reservedKeywords[alias]
}

// IsValidAlias returns true if the alias is a valid identifier per the grammar.
// Valid aliases must start with a letter (A-Z or a-z) and contain only letters,
// digits, and underscores. This does NOT check for reserved keywords; use
// IsReservedKeyword for that.
func IsValidAlias(alias string) bool {
	return validAliasRE.MatchString(alias)
}

// DeriveAliasFromPath extracts the default alias from an import path.
//
// Rules:
//  1. Strip trailing slashes
//  2. Extract last path segment (after final `/`)
//  3. Remove .yammm extension if present
//  4. Replace non-alphanumeric/underscore chars with underscore
//  5. Prepend "n" if first character is a digit (produces valid identifier)
//  6. Return "n" if result is empty
//
// The alias is NOT uppercased: "./parts" → "parts", not "Parts".
func DeriveAliasFromPath(path string) string {
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
	// (e.g., "2parts" → "n2parts", "___" → "n___"). This ensures the derived alias
	// passes IsValidAlias which requires starting with a letter.
	first := segment[0]
	isLetter := (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
	if !isLetter {
		segment = "n" + segment
	}

	return segment
}
