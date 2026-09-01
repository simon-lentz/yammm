package neo4j

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// neo4jIdentifierRE matches valid Neo4j identifiers (labels, property names, relationship types).
var neo4jIdentifierRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// identifierReplacer maps the characters [SanitizeIdentifier] rewrites to
// underscores. strings.Replacer compiles a lookup on construction and is safe
// for concurrent use, so it is built once rather than per call — the diff calls
// SanitizeIdentifier once per type per walk.
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

// SanitizeIdentifier applies Neo4j identifier sanitization rules to a string:
//   - Replaces space, hyphen, period, forward slash, backslash with underscore
//   - Strips all characters except ASCII letters, digits, and underscores
//   - Prepends underscore if the result starts with a digit
//
// This is the same transformation applied to each component in [Adapter.Label].
func SanitizeIdentifier(s string) string {
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

// Label generates a namespaced Neo4j node label from schema and type names.
//
// The label is built as: prefix + sanitize(schemaName) + separator + sanitize(typeName).
// If schemaName is empty, returns sanitize(typeName) only (legacy/unscoped behavior).
//
// Examples:
//
//	Label("book_catalog", "Publisher")       -> "book_catalog__Publisher"
//	Label("geo_regions", "District")    -> "geo_regions__District"
//	Label("", "Person")                -> "Person"
//
//nolint:revive // ctx reserved for future use
func (a *Adapter) Label(ctx context.Context, schemaName, typeName string) string {
	trimmedType := strings.TrimSpace(typeName)
	if trimmedType == "" {
		return ""
	}
	trimmedSchema := strings.TrimSpace(schemaName)
	if trimmedSchema == "" {
		return SanitizeIdentifier(trimmedType)
	}
	return a.config.labelPrefix +
		SanitizeIdentifier(trimmedSchema) +
		a.config.labelSeparator +
		SanitizeIdentifier(trimmedType)
}

// DetectLabelCollisions checks whether any non-abstract types in the schema
// would produce the same Neo4j label after sanitization.
//
// Both schema front doors (the parser and [schema.NewBuilder]) enforce the
// DSL name productions, on which [SanitizeIdentifier] is the identity
// function — so two distinct type names from either front door cannot
// collide. The check is retained as defense-in-depth for construction
// paths that bypass both.
//
// Returns a [diag.Result] containing one [E_NEO4J_LABEL_COLLISION] issue per
// colliding label, e.g.:
//
//	label "my_schema__FooBar" produced by types: Foo-Bar, Foo_Bar
func (a *Adapter) DetectLabelCollisions(ctx context.Context, s *schema.Schema) diag.Result {
	collector := diag.NewCollector(0)
	labelToTypes := make(map[string][]schema.TypeID)

	for t, label := range a.labeledTypes(ctx, s) {
		labelToTypes[label] = append(labelToTypes[label], t.ID())
	}

	for label, ids := range labelToTypes {
		if len(ids) > 1 {
			collector.Collect(labelCollisionIssue(label, ids))
		}
	}

	return collector.Result()
}

// labelCollisionIssue builds the one [E_NEO4J_LABEL_COLLISION] every site
// emits.
//
// One constructor per code, so a consumer reading a detail gets the same kind
// of value whichever site raised the issue. The two sites that built this code
// by hand disagreed: one attached the label alone, the other added a joined
// list of display NAMES under the type detail while its sibling code carried an
// identity there. A collision is between identities — two types that render one
// label are distinguishable only by identity — so identity is what it reports.
func labelCollisionIssue(label string, ids []schema.TypeID) diag.Issue {
	rendered := make([]string, len(ids))
	for i, id := range ids {
		rendered[i] = id.String()
	}
	slices.Sort(rendered)
	return diag.NewIssue(diag.Error, E_NEO4J_LABEL_COLLISION,
		fmt.Sprintf("types %s all render label %q", strings.Join(rendered, " and "), label)).
		WithDetail(diag.DetailKeyFormat, "neo4j").
		WithDetail(detailKeyLabel, label).
		WithDetail(diag.DetailKeyTypeName, strings.Join(rendered, ", ")).
		Build()
}

func isASCIILetter(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}

func isASCIIDigit(r rune) bool {
	return r >= '0' && r <= '9'
}
