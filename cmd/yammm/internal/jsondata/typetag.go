package jsondata

import (
	"strings"
	"unicode/utf8"
)

// The reasons [typeTagFault] gives. Each reaches an operator in the message and in
// the structured detail of an E_INVALID_TYPE_TAG issue.
const (
	tagFaultEmptyTag          = "empty type tag"
	tagFaultMustStartUpper    = "type name must start with uppercase letter"
	tagFaultInvalidChars      = "type name contains invalid characters"
	tagFaultLeadingDot        = "leading dot in qualified name"
	tagFaultTrailingDot       = "trailing dot in qualified name"
	tagFaultMultipleDots      = "multiple dots in qualified name"
	tagFaultReservedDatatype  = "reserved datatype keyword"
	tagFaultAliasInvalidChars = "alias contains invalid characters"
	tagFaultAliasStartLetter  = "alias must start with letter"
	tagFaultAliasReserved     = "alias is a reserved keyword"
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

// typeTagFault returns why typeName is no type name the DSL grammar admits, or ""
// when it is one: a name starting with an uppercase ASCII letter ("Person"), or
// an alias and such a name around one dot ("common.Entity").
//
// Neither half may spell a reserved word: the alias refuses every spelling the
// grammar refuses where either case is admitted, and the type name refuses the
// built-in datatype names. This judges syntax before the validator resolves the
// name against a schema.
func typeTagFault(typeName string) string {
	if typeName == "" {
		return tagFaultEmptyTag
	}

	if idx := strings.Index(typeName, "."); idx != -1 {
		return qualifiedFault(typeName, idx)
	}

	return typeNameFault(typeName)
}

// typeNameFault judges a type name: the whole of an unqualified tag, or the half
// after the dot of a qualified one. Both halves read this one copy, so a test of
// either covers the rule for both.
func typeNameFault(typeName string) string {
	first, size := utf8.DecodeRuneInString(typeName)
	if !isUpperASCII(first) {
		return tagFaultMustStartUpper
	}

	for _, r := range typeName[size:] {
		if !isWordChar(r) {
			return tagFaultInvalidChars
		}
	}

	if isDatatypeKeyword(typeName) {
		return tagFaultReservedDatatype
	}

	return ""
}

// qualifiedFault judges a tag whose first dot is at dotIdx.
func qualifiedFault(typeName string, dotIdx int) string {
	if dotIdx == 0 {
		return tagFaultLeadingDot
	}

	if dotIdx == len(typeName)-1 {
		return tagFaultTrailingDot
	}

	if strings.Contains(typeName[dotIdx+1:], ".") {
		return tagFaultMultipleDots
	}

	alias := typeName[:dotIdx]
	typeNamePart := typeName[dotIdx+1:]

	firstAlias, sizeAlias := utf8.DecodeRuneInString(alias)
	if !isASCIILetter(firstAlias) {
		return tagFaultAliasStartLetter
	}

	for _, r := range alias[sizeAlias:] {
		if !isWordChar(r) {
			return tagFaultAliasInvalidChars
		}
	}

	// An import alias admits either case, so the alias half refuses the whole
	// reserved vocabulary where the type half refuses the datatype names alone.
	if isReservedName(alias) {
		return tagFaultAliasReserved
	}

	return typeNameFault(typeNamePart)
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
