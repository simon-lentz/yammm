package mapping

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Sentinel errors for identifier validation failures.
var (
	ErrEmptyIdentifier   = errors.New("cypher: identifier is empty")
	ErrReservedKeyword   = errors.New("cypher: identifier is a Cypher reserved keyword")
	ErrInvalidIdentifier = errors.New("cypher: identifier contains invalid characters")
)

// neo4jIdentifierRE matches valid Neo4j identifiers (labels, property names, relationship types).
var neo4jIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// identifierReplacer maps the characters [sanitize] rewrites to
// underscores. strings.Replacer compiles a lookup on construction and is safe
// for concurrent use, so it is built once rather than per call — the diff calls
// sanitize once per type per walk.
//
//nolint:gochecknoglobals // Intentional: immutable, concurrency-safe lookup.
var identifierReplacer = strings.NewReplacer(
	" ", "_",
	"-", "_",
	".", "_",
	"/", "_",
	"\\", "_",
)

// cypherReservedKeywords contains Cypher reserved keywords that cannot be used
// as unquoted identifiers in Neo4j queries. Keys are uppercase.
// List based on Neo4j 5.x documentation:
// https://neo4j.com/docs/cypher-manual/current/syntax/reserved/
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

// sanitize rewrites a string by Neo4j's identifier rules, in this order:
//   - Trims white space at each end, and a blank input returns ""
//   - Replaces space, hyphen, period, forward slash, backslash with underscore
//   - Prepends underscore if the string now starts with a digit
//   - Strips all characters except ASCII letters, digits, and underscores
//
// The order matters and the result is NOT guaranteed to be a valid identifier:
// an input whose first invalid character precedes a digit ("«9x") has that
// character stripped after the leading-digit test, so it returns "9x".
// The write path judges the composed label, prefix and separator included, with
// [ValidateIdentifier], and refuses a label that is not valid with
// [InvalidIdentifier] naming it. A composed label can be valid over an
// invalid result: "«9x" under a label prefix.
//
// [Labels.Label] applies it to the schema name and the type name; the prefix
// and the separator stand as configured.
func sanitize(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}

	result := identifierReplacer.Replace(trimmed)

	// Ensure first character is valid (ASCII letter or underscore).
	if len(result) > 0 && isASCIIDigit(rune(result[0])) {
		result = "_" + result
	}

	// Remove any remaining invalid characters (keep only ASCII letters, digits, underscore).
	var clean strings.Builder
	for _, r := range result {
		if isASCIILetter(r) || isASCIIDigit(r) || r == '_' {
			clean.WriteRune(r)
		}
	}

	return clean.String()
}

// ValidateIdentifier checks whether a name is a valid Neo4j identifier.
// Returns an error if the name:
//   - Is empty
//   - Does not match ^[A-Za-z_][A-Za-z0-9_]*$
//   - Matches a Cypher reserved keyword (case-insensitive)
//
// The context parameter is included in error messages for debugging
// (e.g., "property 'run_id'", "type 'Issue'").
func ValidateIdentifier(name, context string) error {
	if name == "" {
		return fmt.Errorf("%w: %s", ErrEmptyIdentifier, context)
	}
	if !neo4jIdentifierRE.MatchString(name) {
		return fmt.Errorf("%w: %s %q", ErrInvalidIdentifier, context, name)
	}
	if _, reserved := cypherReservedKeywords[strings.ToUpper(name)]; reserved {
		return fmt.Errorf("%w: %s %q", ErrReservedKeyword, context, name)
	}
	return nil
}

func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
