package mapping

import "strings"

// cypherReservedKeywords holds, upper-cased, the Cypher keywords the generated
// Cypher never writes as an unquoted name, after
// https://neo4j.com/docs/cypher-manual/current/syntax/reserved/.
//
//nolint:gochecknoglobals // Intentional: static lookup table for validation.
var cypherReservedKeywords = map[string]struct{}{
	// Clauses
	"CALL": {}, "CREATE": {}, "DELETE": {}, "DETACH": {}, "EXISTS": {}, "FOREACH": {},
	"LOAD": {}, "MATCH": {}, "MERGE": {}, "OPTIONAL": {}, "REMOVE": {}, "RETURN": {},
	"SET": {}, "START": {}, "UNION": {}, "UNWIND": {}, "WITH": {},
	// Sub-clauses
	"LIMIT": {}, "ORDER": {}, "SKIP": {}, "WHERE": {}, "YIELD": {},
	// Modifiers
	"ASC": {}, "ASCENDING": {}, "BY": {}, "DESC": {}, "DESCENDING": {}, "ON": {},
	// Expressions
	"ALL": {}, "CASE": {}, "ELSE": {}, "END": {}, "THEN": {}, "WHEN": {},
	// Operators
	"AND": {}, "AS": {}, "CONTAINS": {}, "DISTINCT": {}, "ENDS": {}, "IN": {},
	"IS": {}, "NOT": {}, "OR": {}, "STARTS": {}, "XOR": {},
	// Literals
	"FALSE": {}, "NULL": {}, "TRUE": {},
	// Schema
	"CONSTRAINT": {}, "DO": {}, "FOR": {}, "REQUIRE": {}, "UNIQUE": {},
	// Graph patterns
	"MANDATORY": {}, "SCALAR": {}, "OF": {},
	// Database management
	"ADD": {}, "DROP": {},
}

// The reasons [IdentifierProblem] gives, each completing "the name …".
const (
	reasonEmpty      = "is empty"
	reasonCharacters = "is not an ASCII letter or underscore followed by ASCII letters, digits and underscores"
	reasonReserved   = "is a Cypher reserved keyword"
)

// IsIdentifier reports whether name is a Neo4j identifier the generated Cypher
// can write unquoted: an ASCII letter or underscore, then ASCII letters, digits
// and underscores, and no Cypher reserved keyword in any case.
func IsIdentifier(name string) bool {
	return IdentifierProblem(name) == ""
}

// IdentifierProblem returns why name is not an identifier under [IsIdentifier],
// as a phrase that completes "the name …", or "" when it is one.
func IdentifierProblem(name string) string {
	switch {
	case name == "":
		return reasonEmpty
	case !hasIdentifierCharacters(name):
		return reasonCharacters
	}
	if _, reserved := cypherReservedKeywords[strings.ToUpper(name)]; reserved {
		return reasonReserved
	}
	return ""
}

// hasIdentifierCharacters reports whether name is non-empty, starts with an
// ASCII letter or underscore, and holds only ASCII letters, digits and
// underscores. It ignores the keyword table.
func hasIdentifierCharacters(name string) bool {
	if name == "" || isASCIIDigit(name[0]) {
		return false
	}
	return hasIdentifierTail(name)
}

// hasIdentifierTail reports whether every byte of s is an ASCII letter, digit
// or underscore. A multi-byte rune fails on its first byte.
func hasIdentifierTail(s string) bool {
	for i := range len(s) {
		if b := s[i]; !isASCIILetter(b) && !isASCIIDigit(b) && b != '_' {
			return false
		}
	}
	return true
}

func isASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isASCIIDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
