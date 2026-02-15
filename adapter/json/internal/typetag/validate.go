// Package typetag provides validation for JSON $type field values.
//
// Type names in instance data must conform to DSL grammar syntax:
//   - Unqualified: "Person" (UC_WORD pattern)
//   - Qualified: "common.Entity" (alias.UC_WORD pattern)
//
// This package avoids importing internal/* to maintain adapter layer
// independence per (Adapter Separation).
package typetag

import (
	"strings"
	"unicode/utf8"
)

// Error represents a type tag validation failure with structured detail.
// The Detail field contains a canonical reason string for programmatic inspection.
type Error struct {
	Tag    string // The invalid tag value
	Detail string // Canonical reason string
}

// Error implements the error interface.
func (e *Error) Error() string {
	return e.Detail
}

// Canonical detail strings for validation failures.
const (
	DetailEmptyTag          = "empty type tag"
	DetailMustStartUpper    = "type name must start with uppercase letter"
	DetailInvalidChars      = "type name contains invalid characters"
	DetailLeadingDot        = "leading dot in qualified name"
	DetailTrailingDot       = "trailing dot in qualified name"
	DetailMultipleDots      = "multiple dots in qualified name"
	DetailReservedDatatype  = "reserved datatype keyword"
	DetailAliasInvalidChars = "alias contains invalid characters"
	DetailAliasStartLetter  = "alias must start with letter"
)

// datatypeKeywords are reserved datatype names that cannot be used as type names.
// These correspond to the built-in types defined in the DSL grammar.
var datatypeKeywords = map[string]bool{
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
}

// Validate checks that typeName matches DSL grammar syntax for type names.
//
// Valid forms:
//   - Unqualified: "Person" (starts with uppercase)
//   - Qualified: "common.Entity" (alias.TypeName)
//
// Returns nil if valid, or *Error with Tag and Detail describing the failure.
func Validate(typeName string) error {
	if typeName == "" {
		return &Error{Tag: typeName, Detail: DetailEmptyTag}
	}

	// Check for qualified form (contains a dot)
	if idx := strings.Index(typeName, "."); idx != -1 {
		return validateQualified(typeName, idx)
	}

	// Unqualified form
	return validateUnqualified(typeName)
}

// validateUnqualified validates an unqualified type name (no dot).
func validateUnqualified(typeName string) error {
	// First rune must be uppercase ASCII letter
	first, size := utf8.DecodeRuneInString(typeName)
	if first == utf8.RuneError || !isUpperASCII(first) {
		return &Error{Tag: typeName, Detail: DetailMustStartUpper}
	}

	// Remaining characters must be ASCII alphanumeric or underscore
	for _, r := range typeName[size:] {
		if !isWordChar(r) {
			return &Error{Tag: typeName, Detail: DetailInvalidChars}
		}
	}

	// Check for reserved datatype keywords
	if IsDatatypeKeyword(typeName) {
		return &Error{Tag: typeName, Detail: DetailReservedDatatype}
	}

	return nil
}

// validateQualified validates a qualified type name (contains dot).
func validateQualified(typeName string, dotIdx int) error {
	// Leading dot check
	if dotIdx == 0 {
		return &Error{Tag: typeName, Detail: DetailLeadingDot}
	}

	// Trailing dot check
	if dotIdx == len(typeName)-1 {
		return &Error{Tag: typeName, Detail: DetailTrailingDot}
	}

	// Multiple dots check
	if strings.Contains(typeName[dotIdx+1:], ".") {
		return &Error{Tag: typeName, Detail: DetailMultipleDots}
	}

	alias := typeName[:dotIdx]
	typeNamePart := typeName[dotIdx+1:]

	// Validate alias: must start with ASCII letter
	firstAlias, sizeAlias := utf8.DecodeRuneInString(alias)
	if firstAlias == utf8.RuneError || !isASCIILetter(firstAlias) {
		return &Error{Tag: typeName, Detail: DetailAliasStartLetter}
	}

	// Remaining alias characters must be ASCII alphanumeric or underscore
	for _, r := range alias[sizeAlias:] {
		if !isWordChar(r) {
			return &Error{Tag: typeName, Detail: DetailAliasInvalidChars}
		}
	}

	// Validate type name part: must start with uppercase ASCII letter
	firstType, sizeType := utf8.DecodeRuneInString(typeNamePart)
	if firstType == utf8.RuneError || !isUpperASCII(firstType) {
		return &Error{Tag: typeName, Detail: DetailMustStartUpper}
	}

	// Remaining type characters must be ASCII alphanumeric or underscore
	for _, r := range typeNamePart[sizeType:] {
		if !isWordChar(r) {
			return &Error{Tag: typeName, Detail: DetailInvalidChars}
		}
	}

	// Check for reserved datatype keywords in type name part
	if IsDatatypeKeyword(typeNamePart) {
		return &Error{Tag: typeName, Detail: DetailReservedDatatype}
	}

	return nil
}

// IsValidUnqualified checks if name matches the UC_WORD pattern.
// UC_WORD: [A-Z][A-Za-z0-9_]*
//
// Returns true if valid, false otherwise.
func IsValidUnqualified(name string) bool {
	return validateUnqualified(name) == nil
}

// IsValidQualified checks if name matches the qualified pattern.
// Qualified: alias.TypeName where alias is LC_WORD|UC_WORD and TypeName is UC_WORD.
//
// Valid examples: "common.Entity", "Common.Entity", "parts.Wheel"
// Invalid: ".Entity", "common.", "common.entity", "a.b.c"
func IsValidQualified(name string) bool {
	if name == "" {
		return false
	}
	dotIdx := strings.Index(name, ".")
	if dotIdx == -1 {
		return false
	}
	return validateQualified(name, dotIdx) == nil
}

// IsDatatypeKeyword checks if name is a reserved datatype keyword.
// Datatype keywords are case-sensitive (PascalCase per grammar).
func IsDatatypeKeyword(name string) bool {
	return datatypeKeywords[name]
}

// isUpperASCII returns true if r is an uppercase ASCII letter [A-Z].
func isUpperASCII(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

// isASCIILetter returns true if r is an ASCII letter [A-Za-z].
func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

// isASCIIDigit returns true if r is an ASCII digit [0-9].
func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// isWordChar returns true if r is a valid word character [A-Za-z0-9_].
func isWordChar(r rune) bool {
	return isASCIILetter(r) || isASCIIDigit(r) || r == '_'
}
