package typetag

import (
	"strings"
	"unicode/utf8"
)

// Error represents a type tag validation failure with structured detail.
// The Detail field contains a canonical reason string for programmatic inspection.
type Error struct {
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
	DetailAliasReserved     = "alias is a reserved keyword"
)

// datatypeKeywords are the eleven built-in type names. The grammar refuses
// them wherever a type name is written, so a tag naming one names a type no
// schema can declare.
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
	"List":      true,
}

// reservedLC are the seventeen lowercase spellings the grammar treats as
// keywords or literals rather than names. They reach only the alias half of a
// qualified tag: the type half refuses anything not starting with an uppercase
// ASCII letter before it consults a vocabulary.
var reservedLC = map[string]bool{
	"schema": true, "type": true, "datatype": true, "required": true,
	"primary": true, "extends": true, "includes": true, "abstract": true,
	"one": true, "many": true, "import": true, "as": true, "part": true,
	"in": true, "nil": true, "true": true, "false": true,
}

// Validate checks that typeName matches DSL grammar syntax for type names.
//
// Valid forms:
//   - Unqualified: "Person" (starts with uppercase)
//   - Qualified: "common.Entity" (alias.TypeName)
//
// Neither half may spell a reserved word: the alias refuses every spelling the
// grammar refuses where either case is admitted, and the type name refuses the
// built-in datatype names.
//
// Returns nil if valid, or *Error with Detail describing the failure.
func Validate(typeName string) error {
	if typeName == "" {
		return &Error{Detail: DetailEmptyTag}
	}

	// Check for qualified form (contains a dot)
	if idx := strings.Index(typeName, "."); idx != -1 {
		return validateQualified(typeName, idx)
	}

	// Unqualified form
	return validateTypeName(typeName)
}

// validateTypeName validates a type name: the whole of an unqualified tag, or
// the half after the dot of a qualified one. Both halves read this one copy, so
// a test of either covers the rule for both.
func validateTypeName(typeName string) error {
	// First rune must be uppercase ASCII letter
	first, size := utf8.DecodeRuneInString(typeName)
	if !isUpperASCII(first) {
		return &Error{Detail: DetailMustStartUpper}
	}

	// Remaining characters must be ASCII alphanumeric or underscore
	for _, r := range typeName[size:] {
		if !isWordChar(r) {
			return &Error{Detail: DetailInvalidChars}
		}
	}

	// Check for reserved datatype keywords
	if isDatatypeKeyword(typeName) {
		return &Error{Detail: DetailReservedDatatype}
	}

	return nil
}

// validateQualified validates a qualified type name (contains dot).
func validateQualified(typeName string, dotIdx int) error {
	// Leading dot check
	if dotIdx == 0 {
		return &Error{Detail: DetailLeadingDot}
	}

	// Trailing dot check
	if dotIdx == len(typeName)-1 {
		return &Error{Detail: DetailTrailingDot}
	}

	// Multiple dots check
	if strings.Contains(typeName[dotIdx+1:], ".") {
		return &Error{Detail: DetailMultipleDots}
	}

	alias := typeName[:dotIdx]
	typeNamePart := typeName[dotIdx+1:]

	// Validate alias: must start with ASCII letter
	firstAlias, sizeAlias := utf8.DecodeRuneInString(alias)
	if !isASCIILetter(firstAlias) {
		return &Error{Detail: DetailAliasStartLetter}
	}

	// Remaining alias characters must be ASCII alphanumeric or underscore
	for _, r := range alias[sizeAlias:] {
		if !isWordChar(r) {
			return &Error{Detail: DetailAliasInvalidChars}
		}
	}

	// An import alias admits either case, so the alias half refuses the whole
	// reserved vocabulary where the type half refuses the datatype names alone.
	if isReservedName(alias) {
		return &Error{Detail: DetailAliasReserved}
	}

	return validateTypeName(typeNamePart)
}

// isReservedName reports whether name is a spelling the grammar refuses in a
// position that admits either case, which is what an import alias is.
func isReservedName(name string) bool {
	return isDatatypeKeyword(name) || reservedLC[name]
}

// isDatatypeKeyword reports whether name is a reserved datatype keyword.
// Datatype keywords are case-sensitive (PascalCase per grammar).
func isDatatypeKeyword(name string) bool {
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
